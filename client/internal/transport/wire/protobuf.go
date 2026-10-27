package wire

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"

	"esp32-monitor/client/internal/transport/wire/pb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const MaxMessage = 16384
const WireVersion = 2
const ContentType = "application/x-protobuf"

// Marshal projects business DTOs into typed, generated Protobuf messages. JSON
// is only an in-process adapter for existing DTOs, never a wire payload.
func Marshal(value any) ([]byte, error) {
	t := reflect.TypeOf(value)
	if t == nil {
		return nil, ErrMessage
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	field := messageField(t.Name())
	if field == nil {
		return nil, ErrMessage
	}
	raw, err := json.Marshal(value)
	if err != nil || ValidateJSON(raw, 256*1024) != nil {
		return nil, ErrMessage
	}
	env := &pb.Envelope{WireVersion: proto.Uint32(WireVersion)}
	msg := env.ProtoReflect().NewField(field).Message()
	if err = protojson.Unmarshal(raw, msg.Interface()); err != nil {
		return nil, ErrMessage
	}
	env.ProtoReflect().Set(field, protoreflect.ValueOfMessage(msg))
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(env)
	if err != nil {
		return nil, ErrMessage
	}
	return data, nil
}

func messageField(name string) protoreflect.FieldDescriptor {
	fs := (&pb.Envelope{}).ProtoReflect().Descriptor().Fields()
	for i := 1; i < fs.Len(); i++ {
		if string(fs.Get(i).Message().Name()) == name {
			return fs.Get(i)
		}
	}
	return nil
}

func parse(data []byte, limit int) (*pb.Envelope, error) {
	if len(data) == 0 || len(data) > limit {
		return nil, ErrMessage
	}
	env := &pb.Envelope{}
	if strictFields(data, env.ProtoReflect().Descriptor(), 0) != nil {
		return nil, ErrMessage
	}
	if (proto.UnmarshalOptions{RecursionLimit: 16}).Unmarshal(data, env) != nil || env.GetWireVersion() != WireVersion || env.Payload == nil {
		return nil, ErrMessage
	}
	return env, nil
}

func Validate(data []byte, limit int) error { _, err := parse(data, limit); return err }

func Decode(data []byte, out any, limit int) error {
	env, err := parse(data, limit)
	if err != nil {
		return err
	}
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return ErrMessage
	}
	field := messageField(v.Elem().Type().Name())
	if field == nil || env.ProtoReflect().WhichOneof(env.ProtoReflect().Descriptor().Oneofs().Get(0)) != field {
		return ErrMessage
	}
	msg := env.ProtoReflect().Get(field).Message()
	raw, err := (protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}).Marshal(msg.Interface())
	if err != nil {
		return ErrMessage
	}
	// ProtoJSON renders 64-bit integers as decimal strings. Restore DTO numeric
	// values by schema, preserving exact integers rather than passing float64.
	var value map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return ErrMessage
	}
	restoreNumbers(value, field.Message())
	omitNullOptional(value, v.Elem().Type())
	raw, err = json.Marshal(value)
	if err != nil {
		return ErrMessage
	}
	// The destination is updated only after the whole message was validated.
	tmp := reflect.New(v.Elem().Type())
	if DecodeJSON(raw, tmp.Interface(), 256*1024) != nil {
		return ErrMessage
	}
	v.Elem().Set(tmp.Elem())
	return nil
}

func restoreNumbers(value map[string]any, desc protoreflect.MessageDescriptor) {
	fs := desc.Fields()
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		key := string(f.Name())
		x := value[key]
		convert := func(v any) any {
			if m, ok := v.(map[string]any); ok && f.Kind() == protoreflect.MessageKind {
				restoreNumbers(m, f.Message())
			}
			if s, ok := v.(string); ok && (f.Kind() == protoreflect.Int64Kind || f.Kind() == protoreflect.Sint64Kind || f.Kind() == protoreflect.Uint64Kind || f.Kind() == protoreflect.Fixed64Kind || f.Kind() == protoreflect.Sfixed64Kind) {
				if _, err := strconv.ParseInt(s, 10, 64); err == nil {
					return json.Number(s)
				}
			}
			return v
		}
		if xs, ok := x.([]any); ok {
			for j := range xs {
				xs[j] = convert(xs[j])
			}
		} else {
			value[key] = convert(x)
		}
	}
}

// Protobuf normally accepts duplicate singular fields with last-value-wins.
// Reject these (including conflicting oneof alternatives) before authentication
// state or config transactions see them. Unknown fields remain forward compatible.
func strictFields(data []byte, desc protoreflect.MessageDescriptor, depth int) error {
	if depth > 12 {
		return ErrMessage
	}
	seen := map[protoreflect.FieldNumber]bool{}
	oneofs := map[protoreflect.FullName]bool{}
	for len(data) > 0 {
		number, kind, n := protowire.ConsumeTag(data)
		if n < 0 {
			return ErrMessage
		}
		data = data[n:]
		size := protowire.ConsumeFieldValue(number, kind, data)
		if size < 0 || kind == protowire.StartGroupType || kind == protowire.EndGroupType {
			return ErrMessage
		}
		f := desc.Fields().ByNumber(number)
		if f != nil {
			if f.Kind() == protoreflect.DoubleKind && kind == protowire.Fixed64Type {
				x := math.Float64frombits(binary.LittleEndian.Uint64(data[:8]))
				if math.IsNaN(x) || math.IsInf(x, 0) {
					return ErrMessage
				}
			}
			if !f.IsList() && seen[number] {
				return ErrMessage
			}
			seen[number] = true
			if o := f.ContainingOneof(); o != nil {
				if oneofs[o.FullName()] {
					return ErrMessage
				}
				oneofs[o.FullName()] = true
			}
			expected := protowire.VarintType
			switch f.Kind() {
			case protoreflect.StringKind, protoreflect.BytesKind, protoreflect.MessageKind:
				expected = protowire.BytesType
			case protoreflect.DoubleKind, protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind:
				expected = protowire.Fixed64Type
			case protoreflect.FloatKind, protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind:
				expected = protowire.Fixed32Type
			}
			if kind != expected && !(f.IsPacked() && kind == protowire.BytesType) {
				return ErrMessage
			}
			if kind == protowire.VarintType {
				n, _ := protowire.ConsumeVarint(data)
				if !validVarint(f, n) {
					return ErrMessage
				}
			}
			if kind == protowire.BytesType {
				b, k := protowire.ConsumeBytes(data)
				if k < 0 {
					return ErrMessage
				}
				if f.IsPacked() && expected == protowire.VarintType {
					for len(b) > 0 {
						n, k := protowire.ConsumeVarint(b)
						if k < 0 || !validVarint(f, n) {
							return ErrMessage
						}
						b = b[k:]
					}
				}
				if f.Kind() == protoreflect.StringKind && !Text(string(b), MaxMessage) {
					return ErrMessage
				}
				if f.Kind() == protoreflect.MessageKind && strictFields(b, f.Message(), depth+1) != nil {
					return ErrMessage
				}
			}
		}
		data = data[size:]
	}
	fs := desc.Fields()
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		if f.Cardinality() == protoreflect.Required && !seen[f.Number()] {
			return ErrMessage
		}
	}
	return nil
}

// Canonical removes field ordering differences and unknown fields for idempotent
// transaction comparisons; it never interprets bytes as JSON.
func Canonical(data []byte) ([]byte, error) {
	env, err := parse(data, MaxMessage)
	if err != nil {
		return nil, err
	}
	clearUnknown(env.ProtoReflect())
	return (proto.MarshalOptions{Deterministic: true}).Marshal(env)
}
func clearUnknown(msg protoreflect.Message) {
	msg.SetUnknown(nil)
	msg.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.Kind() == protoreflect.MessageKind {
			if f.IsList() {
				xs := v.List()
				for i := 0; i < xs.Len(); i++ {
					clearUnknown(xs.Get(i).Message())
				}
			} else {
				clearUnknown(v.Message())
			}
		}
		return true
	})
}

func omitNullOptional(value map[string]any, t reflect.Type) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			omitNullOptional(value, f.Type)
			continue
		}
		parts := strings.Split(f.Tag.Get("json"), ",")
		x, ok := value[parts[0]]
		if !ok {
			continue
		}
		if x == nil && len(parts) > 1 && parts[1] == "omitempty" {
			delete(value, parts[0])
			continue
		}
		if m, ok := x.(map[string]any); ok {
			omitNullOptional(m, f.Type)
		}
	}
}

// Reject values that the runtime would truncate into an apparently valid int32
// or coerce from an arbitrary integer into true.
func validVarint(f protoreflect.FieldDescriptor, value uint64) bool {
	switch f.Kind() {
	case protoreflect.Sint32Kind, protoreflect.Uint32Kind:
		return value <= math.MaxUint32
	case protoreflect.BoolKind:
		return value <= 1
	}
	return true
}
