package transport

import (
	"time"

	"esp32-monitor/client/internal/transport/wire"
)

// stampWallClock rewrites send-time fields immediately before marshalling so
// queued values never carry a stale timestamp. Sequence numbers stay with the
// transport that owns them.
func stampWallClock(data any, now time.Time) any {
	switch x := data.(type) {
	case wire.Heartbeat:
		x.SentAt = now.Unix()
		if x.Time != nil {
			t := *x.Time
			t.UnixMS = now.UnixMilli()
			x.Time = &t
		}
		return x
	case wire.Snapshot:
		x.SentAt = now.Unix()
		if pd, ok := x.Sections.Proxy.Data.(wire.ProxyData); ok {
			if td, ok := pd.Time.Data.(wire.TimeData); ok {
				td.UnixMS = now.UnixMilli()
				pd.Time.Data = td
				x.Sections.Proxy.Data = pd
			}
		}
		return x
	default:
		return data
	}
}
