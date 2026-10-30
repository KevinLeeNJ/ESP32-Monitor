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

func TestConfigureV2AndSecretReveal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.yaml")
	out, seen, err := runWizard(t, path, selectStep("base"), inputStep("127.0.0.1:18080", filepath.Join(t.TempDir(), "db"), "https://monitor.example.com"), selectStep("save"), confirmStep(false), selectStep("save"), confirmStep(true))
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:18080" || len(c.Agents) != 0 {
		t.Fatal(c.Listen)
	}
	if strings.Contains(seen, c.ReadToken) || !strings.Contains(out, c.ReadToken) {
		t.Fatal("secret visibility")
	}
	before, _ := os.ReadFile(path)
	_, seen, err = runWizard(t, path, selectStep("view-panel"), step{kind: "note"}, selectStep("rotate-panel"), confirmStep(false), selectStep("quit"))
	if err != nil || !strings.Contains(seen, c.ReadToken) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("cancel changed config")
	}
}
func TestConfigureCancellationAndConcurrentSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.yaml")
	_, _, err := runWizard(t, path, selectStep("base"), step{kind: "input", err: huh.ErrUserAborted}, selectStep("quit"), confirmStep(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancel created file")
	}
	_, _, err = runWizard(t, path, selectStep("save"), step{kind: "confirm", values: []string{"yes"}, hook: func() {
		if e := os.WriteFile(path, []byte("external"), 0600); e != nil {
			t.Fatal(e)
		}
	}})
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal("concurrent overwrite")
	}
}
func TestCLIValidationAndFirstServe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.yaml")
	called := false
	err := Run([]string{"serve", "--config", path}, strings.NewReader(""), io.Discard, io.Discard, false, func(c config.Config) error { called = true; return c.Validate() })
	if err != nil || !called {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("unsafe generated config")
	}
	if err = Run([]string{"configure", "--config", path}, strings.NewReader(""), io.Discard, io.Discard, false, nil); err == nil {
		t.Fatal("noninteractive configure")
	}
}
