package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"esp32-monitor/server/internal/config"
)

type step struct {
	kind   string
	values []string
	err    error
	hook   func()
}

func selectStep(v string) step   { return step{kind: "select", values: []string{v}} }
func inputStep(v ...string) step { return step{kind: "input", values: v} }
func confirmStep(ok bool) step {
	v := "no"
	if ok {
		v = "yes"
	}
	return step{kind: "confirm", values: []string{v}}
}

type fakeUI struct {
	t     *testing.T
	steps []step
	seen  strings.Builder
}

func (u *fakeUI) next(kind, title, description string) step {
	u.t.Helper()
	u.seen.WriteString(title + "\n" + description + "\n")
	if len(u.steps) == 0 {
		u.t.Fatalf("unexpected %s: %s", kind, title)
	}
	s := u.steps[0]
	u.steps = u.steps[1:]
	if s.kind != kind {
		u.t.Fatalf("wanted %s, got %s (%s)", s.kind, kind, title)
	}
	if s.hook != nil {
		s.hook()
	}
	return s
}
func (u *fakeUI) selectItem(title, description string, options []option) (string, error) {
	s := u.next("select", title, description)
	if s.err != nil {
		return "", s.err
	}
	for _, o := range options {
		if o.value == s.values[0] {
			return o.value, nil
		}
	}
	u.t.Fatalf("missing option %q in %+v", s.values[0], options)
	return "", nil
}
func (u *fakeUI) inputs(title string, inputs []input) error {
	s := u.next("input", title, "")
	if s.err != nil {
		return s.err
	}
	if len(s.values) != len(inputs) {
		u.t.Fatalf("wrong input count in %s", title)
	}
	for i, f := range inputs {
		if f.validate != nil {
			if e := f.validate(s.values[i]); e != nil {
				return e
			}
		}
		*f.value = s.values[i]
	}
	return nil
}
func (u *fakeUI) confirm(title, description string) (bool, error) {
	s := u.next("confirm", title, description)
	if s.err != nil {
		return false, s.err
	}
	return s.values[0] == "yes", nil
}
func (u *fakeUI) note(title, description string) error { return u.next("note", title, description).err }
func runWizard(t *testing.T, path string, steps ...step) (string, string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	u := &fakeUI{t: t, steps: steps}
	err := configure(path, u, out)
	if len(u.steps) > 0 {
		t.Fatalf("unused steps: %+v; err=%v", u.steps, err)
	}
	return out.String(), u.seen.String(), err
}
func initial(t *testing.T) (string, config.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "monitor.yaml")
	out, _, err := runWizard(t, path, selectStep("base"), inputStep("0.0.0.0:18080", "./data/monitor.db", "https://monitor.example.com:1443"), selectStep("add"), inputStep("nas"), selectStep("show"), inputStep("NAS", "/vol1", "1"), selectStep("save"), confirmStep(true))
	if err != nil {
		t.Fatal(err)
	}
	c, e := config.Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out, c.ReadToken) || !strings.Contains(out, c.Agents[0].WriteToken) || strings.Contains(out, "[[outputs.http]]") {
		t.Fatal("missing tokens or emitted client config")
	}
	return path, c
}
func load(t *testing.T, path string) config.Config {
	t.Helper()
	c, e := config.Load(path)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func read(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestInitialConfigurationAndCheck(t *testing.T) {
	path, c := initial(t)
	if c.Listen != "0.0.0.0:18080" || c.Panel.Devices[0].DiskMountPath != "/vol1" {
		t.Fatal("wrong config")
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal("unsafe permissions")
	}
	out := &bytes.Buffer{}
	if e := Run([]string{"check-config", "--config", path}, nil, out, out, false, nil); e != nil || strings.Contains(out.String(), c.ReadToken) {
		t.Fatal("check-config", e)
	}
	called := false
	if e := Run([]string{"serve", "--config", path}, nil, out, out, false, func(got config.Config) error {
		called = true
		if got.ReadToken != c.ReadToken {
			t.Fatal("wrong dispatch")
		}
		return errors.New("sentinel")
	}); e == nil || !called {
		t.Fatal("serve not dispatched")
	}
}
func TestEditPreservesTokensAndCancel(t *testing.T) {
	path, c := initial(t)
	out, seen, e := runWizard(t, path, selectStep("edit"), selectStep("nas"), selectStep("show"), inputStep("HOME", "/data", "1"), selectStep("save"), confirmStep(true))
	got := load(t, path)
	if e != nil || got.Panel.Devices[0].Name != "HOME" || got.ReadToken != c.ReadToken || got.Agents[0].WriteToken != c.Agents[0].WriteToken {
		t.Fatal("edit failed", e)
	}
	if strings.Contains(out+seen, c.ReadToken) || strings.Contains(out+seen, c.Agents[0].WriteToken) {
		t.Fatal("ordinary views exposed token")
	}
	before := read(t, path)
	_, _, e = runWizard(t, path, selectStep("rotate-agent"), selectStep("nas"), confirmStep(true), selectStep("rotate-panel"), confirmStep(true), selectStep("quit"), confirmStep(true))
	if e != nil || !bytes.Equal(before, read(t, path)) {
		t.Fatal("cancel wrote config", e)
	}
	_, _, e = runWizard(t, path, selectStep("base"), step{kind: "input", err: huh.ErrUserAborted}, selectStep("quit"))
	if e != nil || !bytes.Equal(before, read(t, path)) {
		t.Fatal("aborted form wrote config", e)
	}
}
func TestRotateViewDelete(t *testing.T) {
	path, c := initial(t)
	out, _, e := runWizard(t, path, selectStep("rotate-agent"), selectStep("nas"), confirmStep(false), selectStep("rotate-panel"), confirmStep(false), selectStep("save"), confirmStep(true))
	same := load(t, path)
	if e != nil || same.ReadToken != c.ReadToken || same.Agents[0].WriteToken != c.Agents[0].WriteToken || strings.Contains(out, c.ReadToken) {
		t.Fatal("declined rotation applied", e)
	}
	out, _, e = runWizard(t, path, selectStep("rotate-agent"), selectStep("nas"), confirmStep(true), selectStep("rotate-panel"), confirmStep(true), selectStep("save"), confirmStep(true))
	rotated := load(t, path)
	if e != nil || rotated.ReadToken == c.ReadToken || rotated.Agents[0].WriteToken == c.Agents[0].WriteToken || !strings.Contains(out, rotated.ReadToken) || !strings.Contains(out, rotated.Agents[0].WriteToken) {
		t.Fatal("rotate failed", e)
	}
	_, seen, e := runWizard(t, path, selectStep("view-agent"), selectStep("nas"), step{kind: "note"}, selectStep("view-panel"), step{kind: "note"}, selectStep("quit"))
	if e != nil || !strings.Contains(seen, rotated.ReadToken) || !strings.Contains(seen, rotated.Agents[0].WriteToken) {
		t.Fatal("explicit reveal failed", e)
	}
	_, _, e = runWizard(t, path, selectStep("delete"), selectStep("nas"), confirmStep(false), selectStep("save"), confirmStep(true))
	if e != nil || len(load(t, path).Agents) != 1 {
		t.Fatal("declined delete applied", e)
	}
	_, _, e = runWizard(t, path, selectStep("delete"), selectStep("nas"), confirmStep(true), selectStep("save"), confirmStep(true))
	deleted := load(t, path)
	if e != nil || len(deleted.Agents) != 0 || len(deleted.Panel.Devices) != 0 {
		t.Fatal("delete failed", e)
	}
}
func TestAddReorderAndHide(t *testing.T) {
	path, c := initial(t)
	_, _, e := runWizard(t, path, selectStep("add"), inputStep("pc"), selectStep("show"), inputStep("PC", "C:", "1"), selectStep("save"), confirmStep(true))
	got := load(t, path)
	if e != nil || got.Panel.Devices[0].ID != "pc" || got.Panel.Devices[1].ID != "nas" || got.ReadToken != c.ReadToken {
		t.Fatal("add failed", e)
	}
	_, _, e = runWizard(t, path, selectStep("edit"), selectStep("pc"), selectStep("hide"), selectStep("save"), confirmStep(true))
	got = load(t, path)
	if e != nil || len(got.Agents) != 2 || len(got.Panel.Devices) != 1 || got.Panel.Devices[0].ID != "nas" {
		t.Fatal("hide failed", e)
	}
}
func TestValidationAndRollback(t *testing.T) {
	path, c := initial(t)
	before := read(t, path)
	_, seen, e := runWizard(t, path,
		selectStep("add"), inputStep("nas"),
		selectStep("base"), inputStep("bad", "./other.db", "https://example.com"),
		selectStep("edit"), selectStep("nas"), selectStep("show"), inputStep("HOME", "/other", "9"),
		selectStep("quit"))
	if e != nil || !bytes.Equal(before, read(t, path)) || !strings.Contains(seen, "ID 已存在") {
		t.Fatal("invalid changes applied", e)
	}
	if c.Panel.Devices[0].Name != "NAS" {
		t.Fatal("baseline mutated")
	}
	for _, s := range []string{"", "中文", "123456789", "\x1b", "        "} {
		if validName(s) == nil {
			t.Errorf("accepted bad name %q", s)
		}
	}
	for _, s := range []string{"", " ", "/a\x00", strings.Repeat("x", 129)} {
		if validMount(s) == nil {
			t.Errorf("accepted bad mount %q", s)
		}
	}
	for _, s := range []string{"0", "5", "x"} {
		if _, e := parsePosition(s, 4); e == nil {
			t.Errorf("accepted bad order %q", s)
		}
	}
}
func TestPanelCapacityAndHiddenAgent(t *testing.T) {
	path, c := initial(t)
	for _, id := range []string{"b", "c", "d"} {
		token, _ := config.GenerateToken()
		c.Agents = append(c.Agents, config.Agent{ID: id, WriteToken: token})
		c.Panel.Devices = append(c.Panel.Devices, config.DefaultDevice(id, id, "/"))
	}
	if e := config.Save(path, c, read(t, path)); e != nil {
		t.Fatal(e)
	}
	_, seen, e := runWizard(t, path, selectStep("add"), inputStep("e"), selectStep("show"), selectStep("add"), inputStep("e"), selectStep("hide"), selectStep("save"), confirmStep(true))
	got := load(t, path)
	if e != nil || !strings.Contains(seen, "最多展示 4 台") || len(got.Agents) != 5 || len(got.Panel.Devices) != 4 {
		t.Fatal("capacity/hidden add failed", e)
	}
}
func TestEscapeUnsavedConfirmationAndEOF(t *testing.T) {
	path, _ := initial(t)
	before := read(t, path)
	_, _, e := runWizard(t, path, selectStep("base"), inputStep("127.0.0.1:18081", "./new.db", "https://example.com"),
		step{kind: "select", err: huh.ErrUserAborted}, confirmStep(false),
		selectStep("save"), confirmStep(false),
		selectStep("edit"), selectStep(""),
		selectStep("quit"), step{kind: "confirm", err: huh.ErrUserAborted},
		step{kind: "select", err: io.EOF})
	if !errors.Is(e, io.EOF) || !bytes.Equal(before, read(t, path)) {
		t.Fatal("EOF applied changes", e)
	}
	missing := filepath.Join(t.TempDir(), "new.yaml")
	_, _, e = runWizard(t, missing, selectStep("quit"), confirmStep(true))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(missing); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("cancel created config")
	}
}
func TestConcurrentEditPreserved(t *testing.T) {
	path, c := initial(t)
	changed := clone(c)
	changed.Listen = "127.0.0.1:18082"
	_, _, e := runWizard(t, path, selectStep("base"), inputStep("127.0.0.1:18081", c.Database, c.PublicBaseURL), selectStep("save"), step{kind: "confirm", values: []string{"yes"}, hook: func() {
		if e := config.Save(path, changed, read(t, path)); e != nil {
			t.Fatal(e)
		}
	}})
	if e == nil || load(t, path).Listen != changed.Listen {
		t.Fatal("external edit overwritten", e)
	}
}
func TestNonInteractiveAndMissingConfig(t *testing.T) {
	out := &bytes.Buffer{}
	path := filepath.Join(t.TempDir(), "missing.yaml")
	for _, args := range [][]string{nil, {"unknown"}, {"configure", "--config", path}, {"check-config", "--config", path}, {"serve", "--config", path}, {"serve", "extra"}} {
		if e := Run(args, strings.NewReader(""), out, out, false, nil); e == nil {
			t.Fatal("invalid command accepted", args)
		}
	}
	if _, e := os.Stat(path); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("created config")
	}
}
