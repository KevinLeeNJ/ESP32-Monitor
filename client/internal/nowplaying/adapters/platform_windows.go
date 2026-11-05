package adapters

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"esp32-monitor/client/internal/windowshelper"
)

func platformCommand(Options) (string, string, func(context.Context) (*exec.Cmd, error)) {
	return "gsmtc", "system_current_source", nil
}

func runNative(ctx context.Context, a *streamAdapter) {
	helper := a.helper
	if helper == nil {
		helper = windowshelper.NewPlatform()
		defer helper.Close()
	}
	events, unsubscribe := helper.SubscribeMusic()
	defer unsubscribe()
	// Music silence is channel-local. Restarting the shared process here would
	// reload PawnIO and disrupt sensors when only the SMTC broker is wedged.
	timer := time.NewTimer(StaleAfter)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			a.update(Message{ErrorCode: "music_timeout"})
			timer.Reset(StaleAfter)
		case line := <-events:
			var m Message
			if json.Unmarshal(line, &m) != nil {
				m = Message{ErrorCode: "music_protocol_error"}
			}
			a.update(m)
			timer.Reset(StaleAfter)
		}
	}
}
