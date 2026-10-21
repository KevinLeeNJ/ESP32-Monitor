package metrics

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const testNow int64 = 1792454400

func metric(name, tags, fields, ts string) string {
	return fmt.Sprintf(`{"name":%q,"tags":%s,"fields":%s,"timestamp":%s}`, name, tags, fields, ts)
}
func batch(m ...string) []byte { return []byte(`{"metrics":[` + strings.Join(m, ",") + `]}`) }
func TestParseNativeBatchAndNormalization(t *testing.T) {
	// Deterministic contract fixture; this is NOT captured Telegraf output.
	body := batch(metric("cpu", `{"host":"spoof","z":"z","cpu":"cpu-total"}`, `{"usage_active":26.5,"text":"ignore","bool":true,"empty":null,"object":{}}`, "1792454400"), metric("mem", `{}`, `{"used_percent":42.1}`, "1792454400"), metric("disk", `{"path":"/"}`, `{"used_percent":61}`, "1792454400"), metric("system", `{}`, `{"uptime":1234}`, "1792454400"), metric("unknown", `{}`, `{"x":1}`, "1792454400"))
	samples, err := Parse(body, testNow)
	if err != nil || len(samples) != 4 {
		t.Fatal(samples, err)
	}
	if samples[0].TagsJSON != `{"cpu":"cpu-total","z":"z"}` || samples[0].Value != 26.5 {
		t.Fatal(samples[0])
	}
	other, err := Parse(batch(metric("cpu", `{"cpu":"cpu-total","z":"z","host":"renamed"}`, `{"usage_active":26.5}`, "1792454400")), testNow)
	if err != nil || other[0].TagsJSON != samples[0].TagsJSON {
		t.Fatal(other, err)
	}
}
func TestParseRejectsWholeBatch(t *testing.T) {
	good := metric("mem", `{}`, `{"used_percent":42}`, "1792454400")
	tests := []struct {
		name string
		body []byte
		want error
	}{
		{"syntax", []byte(`{"metrics":`), ErrJSON}, {"trailing", append(batch(good), []byte(`{}`)...), ErrJSON}, {"array", []byte(`[]`), ErrPayload}, {"null", []byte(`null`), ErrPayload},
		{"missing", []byte(`{}`), ErrPayload}, {"null metrics", []byte(`{"metrics":null}`), ErrPayload}, {"empty", batch(), ErrPayload},
		{"unknown", batch(metric("x", `{}`, `{"x":1}`, "1792454400")), ErrPayload},
		{"not numeric", batch(metric("mem", `{}`, `{"x":"4"}`, "1792454400")), ErrPayload},
		{"null tag value", batch(metric("mem", `{"host":null}`, `{"x":1}`, "1792454400")), ErrPayload},
		{"null tags", batch(metric("mem", `null`, `{"x":1}`, "1792454400")), ErrPayload},
		{"bad tag", batch(metric("mem", `{"x":1}`, `{"x":1}`, "1792454400")), ErrPayload},
		{"null fields", batch(metric("mem", `{}`, `null`, "1792454400")), ErrPayload},
		{"future", batch(good, metric("mem", `{}`, `{"x":1}`, "1792454431")), ErrPayload},
		{"too old", batch(good, metric("mem", `{}`, `{"x":1}`, "1792367999")), ErrPayload},
		{"fraction ts", batch(metric("mem", `{}`, `{"x":1}`, "1792454400.0")), ErrPayload},
		{"string ts", batch(metric("mem", `{}`, `{"x":1}`, `"1792454400"`)), ErrPayload},
		{"huge ts", batch(metric("mem", `{}`, `{"x":1}`, "9999999999999999999999")), ErrPayload},
		{"nonfinite", batch(good, metric("mem", `{}`, `{"x":1e999}`, "1792454400")), ErrPayload},
		{"long name", batch(metric(strings.Repeat("x", 129), `{}`, `{"x":1}`, "1792454400")), ErrPayload},
		{"long field", batch(metric("cpu", `{}`, fmt.Sprintf(`{%q:1}`, strings.Repeat("x", 129)), "1792454400")), ErrPayload},
		{"long tag", batch(metric("cpu", fmt.Sprintf(`{"x":%q}`, strings.Repeat("中", 43)), `{"x":1}`, "1792454400")), ErrPayload},
		{"oversize", []byte(strings.Repeat(" ", MaxBody+1)), ErrTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := Parse(tt.body, testNow)
			if !errors.Is(err, tt.want) || v != nil {
				t.Fatalf("got %v %v; want %v", v, err, tt.want)
			}
		})
	}
	for _, n := range []int{100, 101} {
		ms := make([]string, n)
		for i := range ms {
			ms[i] = good
		}
		_, err := Parse(batch(ms...), testNow)
		if n == 100 && err != nil || n == 101 && !errors.Is(err, ErrTooLarge) {
			t.Fatal(n, err)
		}
	}
	for _, spec := range []struct{ tags, fields int }{{17, 1}, {1, 65}} {
		tags, fields := []string{}, []string{}
		for i := 0; i < spec.tags; i++ {
			tags = append(tags, fmt.Sprintf(`"t%d":"v"`, i))
		}
		for i := 0; i < spec.fields; i++ {
			fields = append(fields, fmt.Sprintf(`"f%d":1`, i))
		}
		_, err := Parse(batch(metric("mem", "{"+strings.Join(tags, ",")+"}", "{"+strings.Join(fields, ",")+"}", "1792454400")), testNow)
		if !errors.Is(err, ErrPayload) {
			t.Fatal("cardinality accepted")
		}
	}
}
func TestTimestampWindowInclusive(t *testing.T) {
	for _, ts := range []int64{testNow - 86400, testNow + 30} {
		if _, err := Parse(batch(metric("mem", `{}`, `{"x":1}`, fmt.Sprint(ts))), testNow); err != nil {
			t.Fatal(err)
		}
	}
}
func FuzzParse(f *testing.F) {
	f.Add(batch(metric("mem", `{}`, `{"x":1}`, "1792454400")))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := Parse(b, testNow)
		if err == nil && len(s) == 0 {
			t.Fatal("empty successful parse")
		}
	})
}
