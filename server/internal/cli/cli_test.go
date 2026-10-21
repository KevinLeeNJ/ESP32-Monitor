package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"esp32-monitor/server/internal/config"
)

func run(t *testing.T, path, input string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	err := Run([]string{"configure", "--config", path}, strings.NewReader(input), out, out, true, func(config.Config) error { t.Fatal("unexpected serve"); return nil })
	return out.String(), err
}
func initial(t *testing.T) (string, config.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "monitor.yaml")
	input := strings.Join([]string{"", "", "", "y", "nas", "NAS", "y", "/", "1", "n", "s", "y", ""}, "\n")
	out, err := run(t, path, input)
	if err != nil {
		t.Fatal(err, out)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, c.ReadToken) || !strings.Contains(out, c.Agents[0].WriteToken) || strings.Contains(out, "[[outputs.http]]") {
		t.Fatal("missing tokens or emitted Telegraf config")
	}
	return path, c
}
func TestInitialConfigurationAndCheck(t *testing.T) {
	path, c := initial(t)
	if c.Agents[0].ID != "nas" || c.Panel.Devices[0].Name != "NAS" {
		t.Fatal("wrong config")
	}
	out := &bytes.Buffer{}
	if err := Run([]string{"check-config", "--config", path}, strings.NewReader(""), out, out, false, nil); err != nil || strings.Contains(out.String(), c.ReadToken) {
		t.Fatal(err, out.String())
	}
	called := false
	if err := Run([]string{"serve", "--config", path}, nil, out, out, false, func(got config.Config) error {
		called = true
		if got.ReadToken != c.ReadToken {
			t.Fatal("wrong config")
		}
		return errors.New("serve sentinel")
	}); err == nil || !called {
		t.Fatal("serve not dispatched")
	}
}
func TestEditPreservesTokensAndCancel(t *testing.T) {
	path, c := initial(t)
	out, err := run(t, path, strings.Join([]string{"3", "nas", "HOME", "y", "/data", "1", "s", "y", ""}, "\n"))
	if err != nil {
		t.Fatal(err, out)
	}
	edited, err := config.Load(path)
	if err != nil || edited.Panel.Devices[0].Name != "HOME" || edited.ReadToken != c.ReadToken || edited.Agents[0].WriteToken != c.Agents[0].WriteToken || strings.Contains(out, c.ReadToken) || strings.Contains(out, c.Agents[0].WriteToken) {
		t.Fatal("edit changed/exposed token", err)
	}
	before, _ := os.ReadFile(path)
	if _, err = run(t, path, "6\nnas\n8\nq\n"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("cancel wrote config")
	}
	if _, err = run(t, path, "1\n"); err == nil {
		t.Fatal("EOF was not cancellation")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("EOF wrote config")
	}
}
func TestRotateViewDelete(t *testing.T) {
	path, c := initial(t)
	out, err := run(t, path, "6\nnas\n8\ns\ny\n")
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := config.Load(path)
	if err != nil || rotated.ReadToken == c.ReadToken || rotated.Agents[0].WriteToken == c.Agents[0].WriteToken || !strings.Contains(out, rotated.ReadToken) || !strings.Contains(out, rotated.Agents[0].WriteToken) {
		t.Fatal("rotation failed", err)
	}
	out, err = run(t, path, "5\nnas\n7\nq\n")
	if err != nil || !strings.Contains(out, rotated.ReadToken) || !strings.Contains(out, rotated.Agents[0].WriteToken) {
		t.Fatal("view failed", err)
	}
	if _, err = run(t, path, "4\nnas\ns\ny\n"); err != nil {
		t.Fatal(err)
	}
	deleted, err := config.Load(path)
	if err != nil || len(deleted.Agents) != 0 || len(deleted.Panel.Devices) != 0 {
		t.Fatal("delete failed", err)
	}
}
func TestAddReorderAndHide(t *testing.T) {
	path, c := initial(t)
	out, err := run(t, path, "2\npc\nPC\ny\n/\n1\ns\ny\n")
	if err != nil {
		t.Fatal(err, out)
	}
	got, err := config.Load(path)
	if err != nil || got.Panel.Devices[0].ID != "pc" || got.Panel.Devices[1].ID != "nas" || got.ReadToken != c.ReadToken {
		t.Fatal("bad add", err)
	}
	out, err = run(t, path, "3\npc\nPC\nn\ns\ny\n")
	if err != nil {
		t.Fatal(err, out)
	}
	got, err = config.Load(path)
	if err != nil || len(got.Agents) != 2 || len(got.Panel.Devices) != 1 || got.Panel.Devices[0].ID != "nas" {
		t.Fatal("bad hide", err)
	}
}
func TestNonInteractiveAndMissingConfig(t *testing.T) {
	out := &bytes.Buffer{}
	path := filepath.Join(t.TempDir(), "missing.yaml")
	for _, args := range [][]string{nil, {"unknown"}, {"configure", "--config", path}, {"check-config", "--config", path}, {"serve", "--config", path}, {"serve", "extra"}} {
		if err := Run(args, strings.NewReader(""), out, out, false, nil); err == nil {
			t.Fatal("invalid command accepted", args)
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unexpected config created")
	}
}
