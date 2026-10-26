//go:build darwin || linux

package adapters

import (
	"esp32-monitor/client/internal/taskmonitor"
	"esp32-monitor/client/internal/taskmonitor/adapters/desktopipc"
)

func platformDefault(opts Options) taskmonitor.Adapter { return platformDesktop(opts) }
func platformDesktop(opts Options) taskmonitor.Adapter {
	return desktopipc.New(opts.Home, opts.ThreadIDs, opts.Now)
}
