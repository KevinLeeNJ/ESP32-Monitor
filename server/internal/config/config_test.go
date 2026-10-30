package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofrs/flock"
	"go.yaml.in/yaml/v3"
)

func validConfig() Config {
	c := Defaults()
	c.ReadToken = strings.Repeat("1", 64)
	c.Agents = []Agent{{ID: "nas", WriteToken: strings.Repeat("2", 64)}}
	c.Panel.Devices = []Device{DefaultDevice("nas", "NAS", "/")}
	return c
}
func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"listen", func(c *Config) { c.Listen = "bad" }}, {"port", func(c *Config) { c.Listen = "127.0.0.1:0" }},
		{"url", func(c *Config) { c.PublicBaseURL = "https://a/b" }}, {"userinfo", func(c *Config) { c.PublicBaseURL = "https://secret@a" }}, {"query", func(c *Config) { c.PublicBaseURL = "https://a?x=1" }},
		{"db", func(c *Config) { c.Database = ":memory:" }}, {"ttl", func(c *Config) { c.MetricTTLSeconds = 0 }}, {"retention", func(c *Config) { c.HistoryRetentionHours = 0 }},
		{"token", func(c *Config) { c.ReadToken = "secret" }}, {"same token", func(c *Config) { c.Agents[0].WriteToken = c.ReadToken }},
		{"id", func(c *Config) { c.Agents[0].ID = "a/b" }}, {"duplicate", func(c *Config) { c.Agents = append(c.Agents, c.Agents[0]) }},
		{"unknown panel", func(c *Config) { c.Panel.Devices[0].ID = "unknown" }}, {"duplicate panel", func(c *Config) { c.Panel.Devices = append(c.Panel.Devices, c.Panel.Devices[0]) }},
		{"name", func(c *Config) { c.Panel.Devices[0].Name = "超过字数" }}, {"long name", func(c *Config) { c.Panel.Devices[0].Name = "123456789" }},
		{"mount", func(c *Config) { c.Panel.Devices[0].DiskMountPath = "" }}, {"uptime", func(c *Config) { c.Panel.Devices[0].UptimeSource.Field = "load1" }},
		{"uptime tags", func(c *Config) { c.Panel.Devices[0].UptimeSource.Tags = map[string]string{"host": "nas"} }},
		{"five cards", func(c *Config) { c.Panel.Devices = make([]Device, 5) }},
	}
	if err := validConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	httpConfig := validConfig()
	httpConfig.PublicBaseURL = "http://192.168.1.5:8080"
	if err := httpConfig.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.edit(&c)
			if c.Validate() == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	c := validConfig()
	c.Panel.Devices = nil
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestDecodeStrictAndSecretSafe(t *testing.T) {
	c := validConfig()
	data, _ := yaml.Marshal(c)
	decoded, err := Decode(data)
	if err != nil || decoded.ReadToken != c.ReadToken {
		t.Fatal("roundtrip failed", err)
	}
	for _, body := range [][]byte{append(append([]byte{}, data...), []byte("typo: secret\n")...), append(append([]byte{}, data...), []byte("---\nfoo: secret\n")...), []byte("read_token: [secret\n"), []byte("read_token: secret\nread_token: secret\n"), nil} {
		if _, err := Decode(body); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe decode error: %v", err)
		}
	}
}
func TestGenerateToken(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		v, err := GenerateToken()
		if err != nil || !validToken(v) || seen[v] {
			t.Fatal("token generation failed")
		}
		seen[v] = true
	}
}
func TestSaveAtomicPermissionsConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "monitor.yaml")
	c := validConfig()
	if err := Save(path, c, nil); err != nil {
		t.Fatal(err)
	}
	original, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Panel.Devices[0].Name = "PC"
	if err = Save(path, c, original); err != nil {
		t.Fatal(err)
	}
	if err = Save(path, c, original); err == nil {
		t.Fatal("overwrote changed config")
	}
	saved, _ := Read(path)
	bad := c
	bad.ReadToken = "secret"
	if Save(path, bad, saved) == nil {
		t.Fatal("saved invalid config")
	}
	after, _ := Read(path)
	if !bytes.Equal(saved, after) {
		t.Fatal("invalid save changed file")
	}
	lock := flock.New(path + ".lock")
	if err = lock.Lock(); err != nil {
		t.Fatal(err)
	}
	if Save(path, c, saved) == nil {
		t.Fatal("ignored concurrent editor")
	}
	lock.Close()
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path); err == nil {
		t.Fatal("insecure permissions accepted")
	}
}
func TestReadMissingAndSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	b, err := Read(path)
	if b != nil || err != nil {
		t.Fatal(b, err)
	}
	if _, err = Load(path); err == nil {
		t.Fatal("missing accepted")
	}
	target := filepath.Join(t.TempDir(), "target")
	if err = os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err = Read(path); err == nil {
		t.Fatal("symlink accepted")
	}
}
