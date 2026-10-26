package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/providers"
	"strings"
	"testing"
)

func TestDisabledSnapshotAndInput(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), nil, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var s companion.Snapshot
	if json.Unmarshal(out.Bytes(), &s) != nil || s.SchemaVersion != 1 || s.GitHub.Health.State != providers.Disabled || s.Codex.Quota.Health.State != providers.Disabled || s.Music.Health.State != providers.Disabled || s.Music.Current != nil {
		t.Fatal(out.String())
	}
	for _, args := range [][]string{{"unexpected"}, {"--github-user", "../../bad"}, {"--unknown"}} {
		if Run(context.Background(), args, &out, &errOut) == nil {
			t.Fatal("invalid input accepted")
		}
	}
}
func TestWatchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	if err := Run(ctx, []string{"--watch"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"schema_version":1`) {
		t.Fatal("no snapshot")
	}
}
