//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLinuxFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeLinuxMDMember(t *testing.T, md, name, parent, state, errors, badBlocks string) {
	t.Helper()
	writeLinuxFile(t, filepath.Join(md, "dev-"+name, "state"), state)
	writeLinuxFile(t, filepath.Join(md, "dev-"+name, "errors"), errors)
	writeLinuxFile(t, filepath.Join(md, "dev-"+name, "bad_blocks"), badBlocks)
	if err := os.Symlink("../../../../../block/"+parent+"/"+name, filepath.Join(md, "dev-"+name, "block")); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxMDHealth(t *testing.T) {
	base := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		md := filepath.Join(root, "md127", "md")
		writeLinuxFile(t, filepath.Join(md, "array_state"), "clean\n")
		writeLinuxFile(t, filepath.Join(md, "degraded"), "0\n")
		for _, disk := range []string{"sdc", "sdd", "sde"} {
			writeLinuxMDMember(t, md, disk+"1", disk, "in_sync\n", "0\n", "0\n")
		}
		return root
	}
	for _, tc := range []struct {
		name   string
		change func(*testing.T, string)
		smart  map[string]float64
		want   float64
	}{
		{name: "healthy", want: 1},
		{name: "active", change: func(t *testing.T, root string) {
			writeLinuxFile(t, filepath.Join(root, "md127", "md", "array_state"), "active\n")
		}, want: 1},
		{name: "readonly", change: func(t *testing.T, root string) {
			writeLinuxFile(t, filepath.Join(root, "md127", "md", "array_state"), "readonly\n")
		}, want: 0},
		{name: "degraded", change: func(t *testing.T, root string) {
			writeLinuxFile(t, filepath.Join(root, "md127", "md", "degraded"), "1\n")
		}, want: 0},
		{name: "faulty member", change: func(t *testing.T, root string) {
			writeLinuxFile(t, filepath.Join(root, "md127", "md", "dev-sdd1", "state"), "faulty\n")
		}, want: 0},
		{name: "member errors", change: func(t *testing.T, root string) {
			writeLinuxFile(t, filepath.Join(root, "md127", "md", "dev-sdc1", "errors"), "2\n")
		}, want: 0},
		{name: "member bad blocks", change: func(t *testing.T, root string) {
			writeLinuxFile(t, filepath.Join(root, "md127", "md", "dev-sde1", "bad_blocks"), "1\n")
		}, want: 0},
		{name: "bad blocks not tracked", change: func(t *testing.T, root string) {
			writeLinuxFile(t, filepath.Join(root, "md127", "md", "dev-sde1", "bad_blocks"), "")
		}, want: 1},
		{name: "smart healthy", smart: map[string]float64{"sdc": 1, "sdd": 1, "sde": 1}, want: 1},
		{name: "smart unknown", smart: map[string]float64{"sdc": 1}, want: 1},
		{name: "smart failing", smart: map[string]float64{"sdc": 1, "sdd": 0, "sde": 1}, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := base(t)
			if tc.change != nil {
				tc.change(t, root)
			}
			got := linuxMDHealth(root, tc.smart)
			if len(got) != 1 || got["md127"] != tc.want {
				t.Fatalf("health = %v, want md127=%v", got, tc.want)
			}
		})
	}
}
