//go:build darwin

package collector

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// decodePlist accepts the XML output of macOS system tools without invoking a shell.
func decodePlist(s string) (any, error) {
	d := xml.NewDecoder(strings.NewReader(s))
	for {
		t, e := d.Token()
		if e != nil {
			return nil, e
		}
		if start, ok := t.(xml.StartElement); ok && start.Name.Local == "plist" {
			for {
				t, e = d.Token()
				if e != nil {
					return nil, e
				}
				if child, ok := t.(xml.StartElement); ok {
					return plistValue(d, child)
				}
			}
		}
	}
}
func plistValue(d *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "dict":
		m := map[string]any{}
		key := ""
		for {
			t, e := d.Token()
			if e != nil {
				return nil, e
			}
			switch v := t.(type) {
			case xml.StartElement:
				if v.Name.Local == "key" {
					var k string
					if e = d.DecodeElement(&k, &v); e != nil {
						return nil, e
					}
					key = k
				} else {
					x, e := plistValue(d, v)
					if e != nil {
						return nil, e
					}
					if key == "" {
						return nil, fmt.Errorf("missing plist key")
					}
					m[key] = x
					key = ""
				}
			case xml.EndElement:
				if v.Name == start.Name {
					return m, nil
				}
			}
		}
	case "array":
		a := []any{}
		for {
			t, e := d.Token()
			if e != nil {
				return nil, e
			}
			switch v := t.(type) {
			case xml.StartElement:
				x, e := plistValue(d, v)
				if e != nil {
					return nil, e
				}
				a = append(a, x)
			case xml.EndElement:
				if v.Name == start.Name {
					return a, nil
				}
			}
		}
	case "true", "false":
		if e := d.Skip(); e != nil {
			return nil, e
		}
		return start.Name.Local == "true", nil
	default:
		var value string
		if e := d.DecodeElement(&value, &start); e != nil {
			return nil, e
		}
		switch start.Name.Local {
		case "integer", "real":
			v, e := strconv.ParseFloat(strings.TrimSpace(value), 64)
			return v, e
		default:
			return value, nil
		}
	}
}
