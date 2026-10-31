//go:build linux

package adapters

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"esp32-monitor/client/internal/nowplaying"
	"github.com/godbus/dbus/v5"
)

type testMPRIS struct {
	status string
	sparse bool
}

func (p *testMPRIS) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	artists := []string{"A", "B"}
	if p.sparse {
		artists = []string{""}
	}
	return map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant(p.status),
		"Position":       dbus.MakeVariant(int64(12000000)),
		"Metadata":       dbus.MakeVariant(map[string]dbus.Variant{"xesam:title": dbus.MakeVariant("Song"), "xesam:artist": dbus.MakeVariant(artists), "mpris:length": dbus.MakeVariant(int64(30000000))}),
	}, nil
}
func (p *testMPRIS) Get(iface, property string) (dbus.Variant, *dbus.Error) {
	return dbus.MakeVariant("Player"), nil
}

func TestMPRISSession(t *testing.T) {
	if os.Getenv("MONITOR_TEST_MPRIS") == "" {
		runner, err := exec.LookPath("dbus-run-session")
		if err != nil {
			t.Skip("dbus-run-session unavailable")
		}
		cmd := exec.Command(runner, "--", os.Args[0], "-test.run=^TestMPRISSession$")
		cmd.Env = append(os.Environ(), "MONITOR_TEST_MPRIS=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("session: %v\n%s", err, out)
		}
		return
	}
	empty, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	if got := readMPRIS(context.Background(), empty); got.Current != nil || got.ErrorCode != "" {
		t.Fatalf("empty session: %+v", got)
	}
	empty.Close()
	if mprisCurrent(map[string]dbus.Variant{}) != nil {
		t.Fatal("missing playback status accepted")
	}
	for _, tc := range []struct {
		name, status string
		sparse       bool
	}{{"org.mpris.MediaPlayer2.a", "Playing", true}, {"org.mpris.MediaPlayer2.b", "Playing", false}} {
		c, err := dbus.ConnectSessionBus()
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if _, err := c.RequestName(tc.name, 0); err != nil {
			t.Fatal(err)
		}
		if err := c.Export(&testMPRIS{tc.status, tc.sparse}, mprisPath, "org.freedesktop.DBus.Properties"); err != nil {
			t.Fatal(err)
		}
	}
	c, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got := readMPRIS(context.Background(), c)
	if got.Current == nil || got.Current.State != nowplaying.Playing || *got.Current.Title != "Song" || *got.Current.Artist != "A, B" || *got.Current.Source != "Player" || *got.Current.Position != 12 || *got.Current.Duration != 30 {
		t.Fatalf("%+v", got.Current)
	}
	a, err := New(Options{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()
	ready, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	if err := a.WaitReady(ready); err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().Current == nil {
		t.Fatal("native adapter did not read session")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("adapter did not stop")
	}
}
