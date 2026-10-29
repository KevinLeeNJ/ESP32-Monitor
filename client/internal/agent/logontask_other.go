//go:build !windows

package agent

import "esp32-monitor/client/internal/ipc"

func configureLoginTask(*ipc.Options) {}
func refreshLoginTask()               {}
