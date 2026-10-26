// Package wire defines the V1 wire contract independently of provider models.
package wire

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxJSON = 16384

var ErrMessage = errors.New("invalid_message")
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func UUID(s string) bool { return uuidPattern.MatchString(s) }
func NewID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
func Nonce() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func RawNonce(s string) ([]byte, error) {
	b, e := base64.RawURLEncoding.Strict().DecodeString(s)
	if e != nil || len(b) != 32 {
		return nil, ErrMessage
	}
	return b, nil
}
func Text(s string, limit int) bool {
	if !utf8.ValidString(s) || len(s) > limit {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func Clip(s string, n int) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > n {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Validate rejects duplicate keys before unmarshalling (encoding/json otherwise
// accepts last-key-wins), invalid UTF-8, control strings, trailing JSON and depth.
func Validate(b []byte, limit int) error {
	if len(b) == 0 || len(b) > limit || !utf8.Valid(b) || bytes.HasPrefix(b, []byte{239, 187, 191}) {
		return ErrMessage
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 12 {
			return ErrMessage
		}
		t, e := d.Token()
		if e != nil {
			return ErrMessage
		}
		switch v := t.(type) {
		case json.Delim:
			if v == '{' {
				keys := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return ErrMessage
					}
					s, ok := k.(string)
					if !ok || keys[s] || !Text(s, MaxJSON) {
						return ErrMessage
					}
					keys[s] = true
					if e = walk(depth + 1); e != nil {
						return e
					}
				}
			} else if v == '[' {
				for d.More() {
					if e := walk(depth + 1); e != nil {
						return e
					}
				}
			} else {
				return ErrMessage
			}
			_, e = d.Token()
			return e
		case string:
			if !Text(v, MaxJSON) {
				return ErrMessage
			}
		}
		return nil
	}
	if e := walk(1); e != nil {
		return ErrMessage
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrMessage
	}
	return nil
}

func Decode(b []byte, out any, limit int) error {
	if e := Validate(b, limit); e != nil {
		return e
	}
	if e := json.Unmarshal(b, out); e != nil {
		return ErrMessage
	}
	return required(b, reflect.TypeOf(out).Elem())
}
func required(b []byte, t reflect.Type) error {
	if t.Kind() == reflect.Pointer {
		if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
			return nil
		}
		return required(b, t.Elem())
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		if t.Kind() != reflect.Interface {
			return ErrMessage
		}
		return nil
	}
	if t.Kind() == reflect.Slice && t.Elem().Kind() != reflect.Uint8 {
		var values []json.RawMessage
		if json.Unmarshal(b, &values) != nil {
			return ErrMessage
		}
		for _, v := range values {
			if e := required(v, t.Elem()); e != nil {
				return e
			}
		}
		return nil
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return ErrMessage
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			if e := required(b, f.Type); e != nil {
				return e
			}
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" || f.PkgPath != "" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		if name == "" {
			name = f.Name
		}
		v, ok := m[name]
		if !ok {
			if len(parts) > 1 && parts[1] == "omitempty" {
				continue
			}
			return ErrMessage
		}
		if e := required(v, f.Type); e != nil {
			return e
		}
	}
	return nil
}
