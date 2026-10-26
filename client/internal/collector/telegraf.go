package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type source struct {
	id, category, platforms, dependency, reason string
	options                                     map[string]any
}

var sources = []source{
	{id: "cpu", category: "cpu", options: map[string]any{"percpu": true, "totalcpu": true, "report_active": true}},
	{id: "mem", category: "memory"}, {id: "swap", category: "memory"},
	{id: "disk", category: "disk"}, {id: "diskio", category: "disk"},
	{id: "net", category: "network"}, {id: "netstat", category: "network", platforms: "linux darwin windows freebsd"},
	{id: "system", category: "system"}, {id: "processes", category: "system", platforms: "linux darwin freebsd"},
	{id: "procstat", category: "system", reason: "explicit_process_selection_not_implemented"},
	{id: "nvidia_smi", category: "hardware", dependency: "nvidia-smi"},
	{id: "sensors", category: "hardware", platforms: "linux", dependency: "sensors"},
	{id: "smart", category: "hardware", reason: "requires_explicit_opt_in_to_avoid_disk_wakeup"},
	{id: "platform_sensors", category: "hardware", reason: "no_validated_telegraf_template_for_battery_apple_gpu_or_cpu_frequency"},
}

func baseTOML(interval int) map[string]any {
	return map[string]any{"agent": map[string]any{"interval": fmt.Sprintf("%ds", interval), "flush_interval": "1s", "round_interval": false, "metric_batch_size": 100, "metric_buffer_limit": 1000, "omit_hostname": true, "quiet": true, "skip_processors_after_aggregators": true}}
}
func inputTables(ids []string, config *Config) map[string]any {
	out := map[string]any{}
	for _, id := range ids {
		for _, s := range sources {
			if s.id != id {
				continue
			}
			opts := map[string]any{}
			for k, v := range s.options {
				opts[k] = v
			}
			if config != nil {
				fields := []string{}
				for _, sel := range config.Selections {
					p := strings.Split(sel.ID, "/")
					if p[0] == id && !contains(fields, p[2]) {
						fields = append(fields, p[2])
					}
				}
				sort.Strings(fields)
				opts["fieldinclude"] = fields
			}
			out[id] = []map[string]any{opts}
		}
	}
	return out
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func renderProbe(id string) ([]byte, error) {
	doc := baseTOML(1)
	doc["inputs"] = inputTables([]string{id}, nil)
	doc["outputs"] = map[string]any{"file": []map[string]any{{"files": []string{"stdout"}, "use_batch_format": true, "data_format": "json", "json_timestamp_units": "1s"}}}
	return toml.Marshal(doc)
}
func renderConfig(c Config, endpoint, token string) ([]byte, error) {
	ids := []string{}
	for _, s := range c.Selections {
		p := strings.Split(s.ID, "/")
		if !contains(ids, p[0]) {
			ids = append(ids, p[0])
		}
	}
	sort.Strings(ids)
	doc := baseTOML(c.IntervalSeconds)
	doc["inputs"] = inputTables(ids, &c)
	doc["outputs"] = map[string]any{"http": []map[string]any{{"url": endpoint, "method": "POST", "timeout": "3s", "use_system_proxy": false, "data_format": "json", "json_timestamp_units": "1s", "use_batch_format": true, "non_retryable_statuscodes": []int{400, 401, 403, 413}, "headers": map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}}}}
	return toml.Marshal(doc)
}

type limitedBuffer struct {
	b     bytes.Buffer
	limit int
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if w.b.Len()+n > w.limit {
		return 0, errors.New("telegraf_output_too_large")
	}
	return w.b.Write(p)
}

type process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func (p *process) stop() {
	if p == nil {
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	if runtime.GOOS == "windows" {
		_ = p.cmd.Process.Kill()
	} else {
		_ = p.cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

type executable struct{ path, dir string }

func (e executable) version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := &limitedBuffer{limit: 4096}
	cmd := exec.CommandContext(ctx, e.path, "version")
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return "", errors.New("telegraf_not_found_or_not_executable")
	}
	words := strings.Fields(out.b.String())
	if len(words) < 2 || words[0] != "Telegraf" || words[1] != TelegrafVersion {
		return "", errors.New("telegraf_version_mismatch")
	}
	return words[1], nil
}
func writeTemp(dir string, b []byte) (string, error) {
	f, err := os.CreateTemp(dir, "telegraf-*.conf")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}
func (e executable) probe(ctx context.Context, id string) (Batch, string) {
	data, err := renderProbe(id)
	if err != nil {
		return Batch{}, "config_generation_failed"
	}
	path, err := writeTemp(e.dir, data)
	if err != nil {
		return Batch{}, "config_write_failed"
	}
	defer os.Remove(path)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out := &limitedBuffer{limit: 4 * 1024 * 1024}
	diagnostic := &limitedBuffer{limit: 64 * 1024}
	args := []string{"--config", path, "--strict-env-handling", "--once"}
	probeCtx := ctx
	if id == "cpu" {
		// CPU percentages need deltas. On macOS 1.40.2, --once's 500ms
		// warmup can omit cpu-total even when individual cores are present.
		// Observe several regular one-second gathers instead of reporting a
		// transiently absent CPU object as unsupported.
		args = []string{"--config", path, "--strict-env-handling"}
		var stop context.CancelFunc
		probeCtx, stop = context.WithTimeout(ctx, 3200*time.Millisecond)
		defer stop()
	}
	cmd := exec.CommandContext(probeCtx, e.path, args...)
	cmd.Cancel = func() error {
		if runtime.GOOS == "windows" {
			return cmd.Process.Kill()
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout = out
	cmd.Stderr = diagnostic
	err = cmd.Run()
	if id == "cpu" && probeCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		err = nil // ending the bounded regular-gather observation is expected
	}
	reason := ""
	if err != nil {
		reason = "probe_failed"
		s := strings.ToLower(diagnostic.b.String())
		if strings.Contains(s, "permission denied") || strings.Contains(s, "operation not permitted") {
			reason = "permission_required"
		}
		if ctx.Err() != nil {
			reason = "probe_timeout"
		}
	}
	// File output may emit multiple batches; each is parsed and bounded separately.
	result := Batch{Metrics: []Metric{}}
	dec := json.NewDecoder(bytes.NewReader(out.b.Bytes()))
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			return Batch{}, "probe_invalid_output"
		}
		b, err := parseBatch(raw)
		if err != nil {
			return Batch{}, "probe_invalid_output"
		}
		result.Metrics = append(result.Metrics, b.Metrics...)
	}
	if len(result.Metrics) == 0 && reason == "" {
		reason = "no_numeric_metrics"
	}
	return result, reason
}
func (e executable) start(path string) (*process, error) {
	cmd := exec.Command(e.path, "--config", filepath.Clean(path), "--strict-env-handling")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, errors.New("telegraf_start_failed")
	}
	p := &process{cmd: cmd, done: make(chan struct{})}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	return p, nil
}
