// Package config owns the V1 configuration contract and secret-safe persistence.
package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gofrs/flock"
	"go.yaml.in/yaml/v3"
)

type Config struct {
	Listen                string  `yaml:"listen"`
	PublicBaseURL         string  `yaml:"public_base_url"`
	Database              string  `yaml:"database"`
	OnlineTimeoutSeconds  int64   `yaml:"online_timeout_seconds"`
	MetricTTLSeconds      int64   `yaml:"metric_ttl_seconds"`
	HistoryRetentionHours int64   `yaml:"history_retention_hours"`
	ReadToken             string  `yaml:"read_token"`
	Agents                []Agent `yaml:"agents"`
	Panel                 Panel   `yaml:"panel"`
}
type Agent struct {
	ID         string `yaml:"id"`
	WriteToken string `yaml:"write_token"`
}
type Panel struct {
	Devices []Device `yaml:"devices"`
}
type Source struct {
	Measurement string            `yaml:"measurement"`
	Tags        map[string]string `yaml:"tags"`
	Field       string            `yaml:"field"`
}
type Device struct {
	ID            string `yaml:"id"`
	Name          string `yaml:"name"`
	UptimeSource  Source `yaml:"uptime_source"`
	DiskMountPath string `yaml:"disk_mount_path"`
}

func Defaults() Config {
	return Config{Listen: "127.0.0.1:8080", PublicBaseURL: "https://monitor.example.com", Database: "./data/monitor.db", OnlineTimeoutSeconds: 45, MetricTTLSeconds: 45, HistoryRetentionHours: 24}
}
func DefaultDevice(id, name, mount string) Device {
	return Device{ID: id, Name: name, DiskMountPath: mount, UptimeSource: Source{Measurement: "system", Tags: map[string]string{}, Field: "uptime"}}
}
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("cannot generate token")
	}
	return hex.EncodeToString(b), nil
}

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func ValidID(id string) bool { return idPattern.MatchString(id) }
func validToken(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && s == strings.ToLower(s)
}
func printable(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for _, b := range []byte(s) {
		if b < 32 || b > 126 {
			return false
		}
	}
	return strings.TrimSpace(s) != ""
}
func (c Config) Validate() error {
	_, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return errors.New("listen must be host:port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return errors.New("invalid listen port")
	}
	u, err := url.Parse(c.PublicBaseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" || len(c.PublicBaseURL) > 192 {
		return errors.New("public_base_url must be an HTTP(S) base URL without credentials, path, query or fragment")
	}
	if strings.TrimSpace(c.Database) == "" || strings.ContainsRune(c.Database, 0) || c.Database == ":memory:" || strings.HasPrefix(c.Database, "file:") {
		return errors.New("database must be a local file path")
	}
	if c.OnlineTimeoutSeconds < 1 || c.OnlineTimeoutSeconds > 86400 || c.MetricTTLSeconds < 1 || c.MetricTTLSeconds > 86400 || c.HistoryRetentionHours < 1 || c.HistoryRetentionHours > 8760 {
		return errors.New("invalid timeout or retention")
	}
	if !validToken(c.ReadToken) {
		return errors.New("read_token must be 64 lowercase hexadecimal characters")
	}
	tokens := map[string]bool{c.ReadToken: true}
	ids := map[string]bool{}
	for i, a := range c.Agents {
		if !ValidID(a.ID) || ids[a.ID] {
			return fmt.Errorf("invalid or duplicate agent id at index %d", i)
		}
		ids[a.ID] = true
		if !validToken(a.WriteToken) || tokens[a.WriteToken] {
			return fmt.Errorf("invalid or reused write_token at agent index %d", i)
		}
		tokens[a.WriteToken] = true
	}
	if len(c.Panel.Devices) > 4 {
		return errors.New("panel supports at most four devices")
	}
	shown := map[string]bool{}
	for i, d := range c.Panel.Devices {
		if !ids[d.ID] || shown[d.ID] {
			return fmt.Errorf("unknown or duplicate panel device at index %d", i)
		}
		shown[d.ID] = true
		if !printable(d.Name, 8) {
			return fmt.Errorf("panel name at index %d must be 1-8 printable ASCII bytes", i)
		}
		if strings.TrimSpace(d.DiskMountPath) == "" || len(d.DiskMountPath) > 128 || strings.ContainsRune(d.DiskMountPath, 0) {
			return fmt.Errorf("invalid disk_mount_path at index %d", i)
		}
		if d.UptimeSource.Measurement != "system" || d.UptimeSource.Field != "uptime" || len(d.UptimeSource.Tags) != 0 {
			return fmt.Errorf("uptime_source at index %d must be system.uptime with empty tags", i)
		}
	}
	return nil
}

// Decode never returns a parser error containing YAML source (which may contain tokens).
func Decode(data []byte) (Config, error) {
	c := Defaults()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return Config{}, errors.New("invalid YAML configuration")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Config{}, errors.New("configuration must contain exactly one YAML document")
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Read returns the original bytes for optimistic concurrency checking. Missing is nil.
func Read(path string) ([]byte, error) {
	st, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("cannot stat configuration")
	}
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return nil, errors.New("configuration must be a regular file with permission 0600")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read configuration")
	}
	return b, nil
}
func Load(path string) (Config, error) {
	b, err := Read(path)
	if err != nil {
		return Config{}, err
	}
	if b == nil {
		return Config{}, errors.New("configuration missing; run configure")
	}
	return Decode(b)
}

// Save serializes cooperating writers and refuses to overwrite an externally changed file.
// The lock file is intentionally retained; removing it would allow two different lock inodes.
func Save(path string, c Config, original []byte) error {
	if err := c.Validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("cannot create configuration directory")
	}
	lock := flock.New(path+".lock", flock.SetPermissions(0600))
	ok, err := lock.TryLock()
	if err != nil || !ok {
		return errors.New("configuration is being edited")
	}
	defer lock.Close()
	unchanged := func() bool {
		b, e := Read(path)
		return e == nil && bytes.Equal(b, original) && (b == nil) == (original == nil)
	}
	if !unchanged() {
		return errors.New("configuration changed; reload before saving")
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return errors.New("cannot encode configuration")
	}
	f, err := os.CreateTemp(dir, ".monitor-*.yaml")
	if err != nil {
		return errors.New("cannot create temporary configuration")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		return errors.New("cannot write configuration")
	}
	if err = f.Close(); err != nil {
		return errors.New("cannot close configuration")
	}
	if !unchanged() {
		return errors.New("configuration changed; reload before saving")
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return errors.New("cannot replace configuration")
	}
	d, err := os.Open(dir)
	if err != nil {
		return errors.New("cannot sync configuration directory")
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return errors.New("cannot sync configuration directory")
	}
	return nil
}
