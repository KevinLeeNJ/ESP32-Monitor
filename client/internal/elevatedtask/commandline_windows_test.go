//go:build windows

package elevatedtask

import (
	"encoding/xml"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestQuoteArgumentUsesWindowsRules(t *testing.T) {
	cases := map[string]string{
		"":                 `""`,
		"plain":            "plain",
		"with space":       `"with space"`,
		`with"quote`:       `"with\"quote"`,
		"tab\tinside":      "\"tab\tinside\"",
		`C:\path\no-space`: `C:\path\no-space`,
	}
	for input, want := range cases {
		if got := QuoteArgument(input); got != want {
			t.Fatalf("QuoteArgument(%q) = %q, want %q", input, got, want)
		}
	}
	args := []string{"--settings", `C:\Program Files\ESP32 & Monitor\agent.exe`, `a"b`, ""}
	want := `--settings "C:\Program Files\ESP32 & Monitor\agent.exe" "a\"b" ""`
	if got := CommandLine(args); got != want {
		t.Fatalf("CommandLine = %q, want %q", got, want)
	}
}

func TestTaskXMLRegistersHighestAvailableLogonTask(t *testing.T) {
	agent := `C:\Program Files\ESP32 & Monitor\agent.exe`
	args := []string{"--settings", `C:\Users\A & B\settings.json`, `quote"inside`}
	document, err := taskXML(agent, args)
	if err != nil {
		t.Skipf("current user token unavailable: %v", err)
	}
	if !strings.Contains(document, "&amp;") || strings.Contains(document, agent) {
		t.Fatal("task XML did not escape the command path")
	}

	var task struct {
		Triggers struct {
			LogonTrigger struct {
				Enabled bool   `xml:"Enabled"`
				UserID  string `xml:"UserId"`
			} `xml:"LogonTrigger"`
		} `xml:"Triggers"`
		Principals struct {
			Principal struct {
				UserID    string `xml:"UserId"`
				LogonType string `xml:"LogonType"`
				RunLevel  string `xml:"RunLevel"`
			} `xml:"Principal"`
		} `xml:"Principals"`
		Actions struct {
			Exec struct {
				Command   string `xml:"Command"`
				Arguments string `xml:"Arguments"`
			} `xml:"Exec"`
		} `xml:"Actions"`
	}
	decoder := xml.NewDecoder(strings.NewReader(strings.Replace(document, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)))
	if err = decoder.Decode(&task); err != nil {
		t.Fatalf("task XML is invalid: %v", err)
	}
	if !task.Triggers.LogonTrigger.Enabled || !strings.HasPrefix(task.Triggers.LogonTrigger.UserID, "S-1-") {
		t.Fatalf("unexpected logon trigger: %+v", task.Triggers.LogonTrigger)
	}
	principal := task.Principals.Principal
	if principal.UserID != task.Triggers.LogonTrigger.UserID || principal.LogonType != "InteractiveToken" || principal.RunLevel != "HighestAvailable" {
		t.Fatalf("unexpected principal: %+v", principal)
	}
	if task.Actions.Exec.Command != agent || task.Actions.Exec.Arguments != CommandLine(args) {
		t.Fatalf("unexpected action: %+v", task.Actions.Exec)
	}
}

func TestUTF16LEEncoding(t *testing.T) {
	encoded := utf16LE("A中")
	if len(encoded) != 6 || encoded[0] != 0xFF || encoded[1] != 0xFE {
		t.Fatalf("unexpected BOM/length: %x", encoded)
	}
	units := make([]uint16, 0, 2)
	for i := 2; i < len(encoded); i += 2 {
		units = append(units, uint16(encoded[i])|uint16(encoded[i+1])<<8)
	}
	if got := string(utf16.Decode(units)); got != "A中" {
		t.Fatalf("decoded %q", got)
	}
}
