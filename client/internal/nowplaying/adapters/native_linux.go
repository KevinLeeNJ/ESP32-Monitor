//go:build linux

package adapters

import (
	"context"
	"sort"
	"strings"
	"time"

	"esp32-monitor/client/internal/nowplaying"
	"github.com/godbus/dbus/v5"
)

const mprisPath = dbus.ObjectPath("/org/mpris/MediaPlayer2")
const mprisPlayer = "org.mpris.MediaPlayer2.Player"

func runNative(ctx context.Context, a *streamAdapter) {
	for ctx.Err() == nil {
		conn, err := dbus.ConnectSessionBus()
		if err == nil {
			signals := make(chan *dbus.Signal, 16)
			conn.Signal(signals)
			err = conn.AddMatchSignalContext(ctx, dbus.WithMatchInterface("org.freedesktop.DBus.Properties"), dbus.WithMatchMember("PropertiesChanged"), dbus.WithMatchObjectPath(mprisPath))
			if err == nil {
				err = conn.AddMatchSignalContext(ctx, dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"))
			}
			if err == nil {
				ticker := time.NewTicker(2 * time.Second)
				a.update(readMPRIS(ctx, conn))
			loop:
				for {
					select {
					case <-ctx.Done():
						break loop
					case <-conn.Context().Done():
						break loop
					case <-ticker.C:
						a.update(readMPRIS(ctx, conn))
					case s := <-signals:
						if s != nil && (s.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(s.Body) > 0 && strings.HasPrefix(asString(s.Body[0]), "org.mpris.MediaPlayer2.")) {
							a.update(readMPRIS(ctx, conn))
						}
					}
				}
				ticker.Stop()
			}
			conn.Close()
		}
		if ctx.Err() != nil {
			return
		}
		a.update(Message{ErrorCode: "music_session_unavailable"})
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func asString(v any) string { s, _ := v.(string); return s }

func readMPRIS(ctx context.Context, conn *dbus.Conn) Message {
	var names []string
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	err := conn.BusObject().CallWithContext(callCtx, "org.freedesktop.DBus.ListNames", 0).Store(&names)
	cancel()
	if err != nil {
		return Message{ErrorCode: "music_session_unavailable"}
	}
	sort.Strings(names)
	var selected *nowplaying.Current
	failed := false
	for _, name := range names {
		if !strings.HasPrefix(name, "org.mpris.MediaPlayer2.") {
			continue
		}
		obj := conn.Object(name, mprisPath)
		var props map[string]dbus.Variant
		callCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
		err = obj.CallWithContext(callCtx, "org.freedesktop.DBus.Properties.GetAll", 0, mprisPlayer).Store(&props)
		cancel()
		if err != nil {
			failed = true
			continue
		}
		current := mprisCurrent(props)
		if current == nil {
			failed = true
			continue
		}
		var identity dbus.Variant
		callCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
		err = obj.CallWithContext(callCtx, "org.freedesktop.DBus.Properties.Get", 0, "org.mpris.MediaPlayer2", "Identity").Store(&identity)
		cancel()
		if err == nil {
			if s := asString(identity.Value()); s != "" {
				current.Source = &s
			}
		}
		if selected == nil || current.State == nowplaying.Playing && selected.State != nowplaying.Playing ||
			current.State == selected.State && metadataScore(current) > metadataScore(selected) {
			selected = current
		}
	}
	if selected != nil {
		return Message{Current: selected}
	}
	if failed {
		return Message{ErrorCode: "music_read_failed"}
	}
	return Message{}
}

func metadataScore(current *nowplaying.Current) int {
	score := 0
	if current.Title != nil && strings.TrimSpace(*current.Title) != "" {
		score++
	}
	if current.Artist != nil && strings.TrimSpace(*current.Artist) != "" {
		score++
	}
	return score
}

func mprisCurrent(props map[string]dbus.Variant) *nowplaying.Current {
	status := asString(props["PlaybackStatus"].Value())
	if status != "Playing" && status != "Paused" && status != "Stopped" {
		return nil
	}
	current := &nowplaying.Current{State: nowplaying.NotPlaying}
	if status == "Playing" {
		current.State = nowplaying.Playing
	}
	metadata, _ := props["Metadata"].Value().(map[string]dbus.Variant)
	if title := asString(metadata["xesam:title"].Value()); title != "" {
		current.Title = &title
	}
	if artists, ok := metadata["xesam:artist"].Value().([]string); ok && len(artists) > 0 {
		s := strings.Join(artists, ", ")
		current.Artist = &s
	}
	if duration, ok := metadata["mpris:length"].Value().(int64); ok {
		v := float64(duration) / 1e6
		current.Duration = &v
	}
	if position, ok := props["Position"].Value().(int64); ok {
		v := float64(position) / 1e6
		current.Position = &v
	}
	return current
}
