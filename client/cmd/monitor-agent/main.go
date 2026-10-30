package main

import (
	"context"
	"errors"
	"esp32-monitor/client/internal/agent"
	"esp32-monitor/client/internal/logging"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	hideOwnConsole()
	if handle, err := logging.Setup(logging.Options{Name: "agent", AlsoStderr: logging.IsTerminal(os.Stderr)}); err == nil {
		defer handle.Close()
	} else {
		fmt.Fprintf(os.Stderr, "log_setup_failed: %v\n", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := agent.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil && !errors.Is(err, flag.ErrHelp) {
		logging.L("agent").Error("agent_exit", "error", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logging.L("agent").Info("agent_exit")
}
