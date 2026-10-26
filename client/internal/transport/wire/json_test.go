package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStrictJSON(t *testing.T) {
	cases := []string{`{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `{"x":"\u0000"}`, `{"x":"\n"}`, `{"n":NaN}`, `{} {}`, "\xef\xbb\xbf{}", `{"x":"` + string([]byte{255}) + `"}`, strings.Repeat("[", 12) + `{}` + strings.Repeat("]", 12)}
	for _, c := range cases {
		if Validate([]byte(c), MaxJSON) == nil {
			t.Errorf("accepted invalid %q", c)
		}
	}
	if Validate([]byte(`{"x":null,"unicode":"天气"}`), MaxJSON) != nil {
		t.Fatal("valid JSON rejected")
	}
}
func TestRequiredNullAndTypes(t *testing.T) {
	type dto struct {
		Name  string `json:"name"`
		Count *int   `json:"count"`
	}
	var d dto
	for _, c := range []string{`{"name":"a"}`, `{"name":null,"count":null}`, `{"name":"a","count":"1"}`} {
		if Decode([]byte(c), &d, 100) == nil {
			t.Fatal(c)
		}
	}
	if Decode([]byte(`{"name":"a","count":null,"future":true}`), &d, 100) != nil {
		t.Fatal("null or optional future field rejected")
	}
}
func TestLimitsAndUTF8(t *testing.T) {
	if Clip("天气abc", 7) != "天气a" {
		t.Fatal("split UTF8")
	}
	data, _ := json.Marshal(strings.Repeat("x", MaxJSON-2))
	if Validate(data, MaxJSON) != nil {
		t.Fatal("boundary")
	}
	data = append(data, ' ')
	if Validate(data, MaxJSON) == nil {
		t.Fatal("oversize")
	}
}
