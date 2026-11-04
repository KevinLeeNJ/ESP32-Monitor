// monitor-headless runs only the collector and its upload worker.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"esp32-monitor/client/internal/collector"
	"esp32-monitor/client/internal/platform"
	"esp32-monitor/client/internal/registration"
)

type settings struct {
	URL       string `json:"url"`
	TokenFile string `json:"token_file"`
}

func main() {
	setup := flag.Bool("setup", false, "configure server and metric selections interactively")
	state := flag.String("state-dir", "", "private state directory (default: user config directory)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(1)
	}
	if *state == "" {
		base, err := platform.AppDataDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		*state = filepath.Join(base, "headless")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *state, *setup, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dir string, setup bool, in io.Reader, out io.Writer) error {
	return runWithUI(ctx, dir, setup, out, terminalUI{ctx: ctx, in: in, out: out})
}

func runWithUI(ctx context.Context, dir string, setup bool, out io.Writer, ui setupUI) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return errors.New("headless_directory_must_be_private")
	}
	path := filepath.Join(dir, "server.json")
	var s settings
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &s); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	m, err := collector.New(collector.Options{StateDirectory: filepath.Join(dir, "collector"), UploadURL: s.URL, UploadToken: collector.TokenFile(s.TokenFile)})
	if err != nil {
		return err
	}
	defer m.Close()
	if setup {
		return configure(ctx, dir, s, m, ui, out)
	}
	if s.URL == "" {
		return errors.New("run with --setup first")
	}
	if err = m.Restore(ctx); err != nil {
		return err
	}
	if m.Snapshot().Revision == 0 {
		return errors.New("no collector configuration; run --setup")
	}
	fmt.Fprintln(out, "Collector running. Press Ctrl+C to stop.")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := json.NewEncoder(out).Encode(m.Snapshot()); err != nil {
				return err
			}
		}
	}
}

func configure(ctx context.Context, dir string, old settings, m *collector.Manager, ui setupUI, out io.Writer) error {
	prompt := ui.text
	raw, err := prompt("Server URL (empty keeps current server): ")
	if err != nil {
		return err
	}
	reuse := raw == "" && old.URL != ""
	if reuse {
		raw = old.URL
	}
	base, err := registration.NormalizeServerBase(raw)
	if err != nil {
		return err
	}
	code := ""
	if !reuse {
		code, err = prompt("Six-digit code from Server: ")
		if err != nil {
			return err
		}
		if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
			return errors.New("invalid_code")
		}
	}
	if err = m.Load(); err != nil && m.Snapshot().Revision == 0 {
		return err
	}
	previous := m.Snapshot().Config
	caps, err := m.Discover(ctx)
	if err != nil {
		return err
	}
	upload, err := ui.metrics(caps.V2, previous.Upload)
	if err != nil {
		return err
	}
	config := collector.DefaultConfig()
	config.Upload = upload
	url, token := old.URL, old.TokenFile
	newToken := ""
	defer func() {
		if newToken != "" {
			_ = os.Remove(newToken)
		}
	}()
	if !reuse {
		registered, err := registration.ExchangeRegistrationCode(ctx, base, code)
		if err != nil {
			return err
		}
		f, err := os.CreateTemp(dir, "server-token-")
		if err != nil {
			return err
		}
		newToken = f.Name()
		if _, err = f.WriteString(registered.Token + "\n"); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		url, token = base+"/v2/collector/samples", newToken
	}
	if err = m.SetUploadTarget(url, token); err != nil {
		return err
	}
	if _, err = m.Apply(ctx, config); err != nil {
		return err
	}
	data, _ := json.Marshal(settings{URL: url, TokenFile: token})
	if err = platform.WriteFileAtomic(dir, ".server-*", "server.json", data, 0600); err != nil {
		_ = m.SetUploadTarget(old.URL, old.TokenFile)
		if _, rollbackErr := m.Apply(ctx, previous); rollbackErr != nil {
			return fmt.Errorf("save server settings: %v; restore collector settings: %w", err, rollbackErr)
		}
		return err
	}
	newToken = ""
	if !reuse && filepath.Dir(old.TokenFile) == dir && strings.HasPrefix(filepath.Base(old.TokenFile), "server-token-") {
		_ = os.Remove(old.TokenFile)
	}
	fmt.Fprintln(out, "Configuration saved.")
	for {
		answer, err := prompt("Enable systemd user service for autostart? [y/N]: ")
		if err != nil {
			return err
		}
		switch strings.ToLower(answer) {
		case "", "n", "no":
			fmt.Fprintln(out, "Start monitor-headless without --setup to collect and upload.")
			return nil
		case "y", "yes":
			if err := enableSystemd(ctx, dir); err != nil {
				return fmt.Errorf("configuration saved, but systemd setup failed: %w", err)
			}
			fmt.Fprintln(out, "Autostart enabled. After setup exits, start it with: systemctl --user start monitor-headless.service")
			fmt.Fprintln(out, "For boot without user login, an administrator must enable lingering: loginctl enable-linger <user>")
			return nil
		default:
			fmt.Fprintln(out, "Enter y or n.")
		}
	}
}
