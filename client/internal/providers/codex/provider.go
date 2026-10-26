package codex

import (
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/platform/proxy"
	"esp32-monitor/client/internal/providers"
	"esp32-monitor/client/internal/taskmonitor"
	"esp32-monitor/client/internal/taskmonitor/adapters"
	"sync"
	"time"
)

type Options struct {
	Enabled        bool
	Executable     string
	Now            func() time.Time
	DesktopEnabled bool
	Home           string
	ThreadIDs      []string
	TaskAdapter    string
	Tasks          taskmonitor.Adapter // Optional injected adapter; takes precedence over platform selection.
}
type Provider struct {
	tasks     taskmonitor.Adapter
	connect   func(context.Context, string) (*Connection, error)
	refreshMu sync.Mutex
	mu        sync.Mutex
	opts      Options
	snapshot  Snapshot
	next      time.Time
	failures  int
}

func New(opts Options) *Provider {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Executable == "" {
		opts.Executable = "codex"
	}
	state := providers.Starting
	if !opts.Enabled {
		state = providers.Disabled
	}
	p := &Provider{opts: opts, connect: Start, snapshot: Snapshot{
		Quota: Quota{Health: providers.Health{State: state, ObservedAt: opts.Now()}, Windows: []Window{}},
		Tasks: Tasks{Health: providers.Health{State: providers.Disabled, ObservedAt: opts.Now()}, Scope: "disabled", State: Unknown, Items: []Task{}},
	}}
	p.tasks = opts.Tasks
	if p.tasks == nil && (opts.DesktopEnabled || opts.TaskAdapter != "") {
		p.tasks = adapters.New(opts.TaskAdapter, adapters.Options{Home: opts.Home, ThreadIDs: opts.ThreadIDs, Now: opts.Now})
	}
	return p
}

// RunTasks owns the independent task adapter. Quota/network polling cannot
// stall task observation. The caller cancels ctx and waits for RunTasks to exit.
func (p *Provider) RunTasks(ctx context.Context) {
	if p.tasks != nil {
		p.tasks.Run(ctx)
	}
}
func (p *Provider) WaitTasks(ctx context.Context) {
	if p.tasks != nil {
		p.tasks.WaitReady(ctx)
	}
}

// Refresh polls account-wide limits at most once per minute, with bounded backoff.
// An isolated, read-only App Server connection is closed after every refresh;
// task observation runs independently through the configured adapter.
func (p *Provider) Refresh(ctx context.Context) Snapshot {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()
	p.mu.Lock()
	now := p.opts.Now()
	if !p.opts.Enabled || now.Before(p.next) {
		s := p.copy()
		p.mu.Unlock()
		return s
	}
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	connection, err := p.connect(ctx, p.opts.Executable)
	var version string
	var windows []Window
	if err == nil {
		defer connection.Close()
		version, err = connection.RPC.Initialize(ctx)
		if err == nil {
			var account struct {
				Account *struct {
					Type string `json:"type"`
				} `json:"account"`
			}
			err = connection.RPC.Call(ctx, "account/read", map[string]any{"refreshToken": false}, &account)
			if err == nil && account.Account == nil {
				err = errors.New("auth_required")
			}
			if err == nil && account.Account.Type != "chatgpt" {
				err = errors.New("account_type_unsupported")
			}
		}
		if err == nil {
			var data json.RawMessage
			err = connection.RPC.Call(ctx, "account/rateLimits/read", map[string]any{}, &data)
			if err == nil {
				windows, err = parseLimits(data)
			}
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.snapshot.Quota.Health.ObservedAt = now
	p.next = now.Add(time.Minute)
	if err != nil {
		p.failures++
		p.next = now.Add(time.Minute * time.Duration(1<<min(p.failures-1, 4)))
		p.snapshot.Quota.Health.State = providers.Error
		p.snapshot.Quota.Health.ErrorCode = "codex_unavailable"
		for _, code := range []error{proxy.ErrAutomatic, proxy.ErrDetection, proxy.ErrInvalid} {
			if errors.Is(err, code) {
				p.snapshot.Quota.Health.ErrorCode = code.Error()
			}
		}
		if err.Error() == "auth_required" {
			p.snapshot.Quota.Health.State = providers.AuthRequired
			p.snapshot.Quota.Health.ErrorCode = "auth_required"
		}
		if err.Error() == "account_type_unsupported" {
			p.snapshot.Quota.Health.State = providers.Unsupported
			p.snapshot.Quota.Health.ErrorCode = "account_type_unsupported"
		}
		var rpcError *RPCError
		if errors.As(err, &rpcError) {
			switch rpcError.Code {
			case -32601:
				p.snapshot.Quota.Health.State = providers.Unsupported
				p.snapshot.Quota.Health.ErrorCode = "source_version_unsupported"
			case -32001:
				p.snapshot.Quota.Health.ErrorCode = "upstream_error"
			default:
				p.snapshot.Quota.Health.ErrorCode = "quota_read_failed"
			}
		}
		// Old quota must not look current after an unsuccessful account read.
		p.snapshot.Quota.Windows = []Window{}
	} else {
		p.failures = 0
		t := now
		p.snapshot.Quota.Health = providers.Health{State: providers.Healthy, ObservedAt: now, LastSuccessAt: &t, SourceVersion: version}
		p.snapshot.Quota.Windows = windows
		if len(windows) == 0 {
			p.snapshot.Quota.Health.State = providers.Degraded
			p.snapshot.Quota.Health.ErrorCode = "quota_unknown"
		}
	}
	return p.copy()
}
func (p *Provider) Snapshot() Snapshot { p.mu.Lock(); defer p.mu.Unlock(); return p.copy() }
func (p *Provider) copy() Snapshot {
	s := p.snapshot
	s.Quota.Windows = append([]Window{}, s.Quota.Windows...)
	for i, w := range s.Quota.Windows {
		if w.WindowMinutes != nil {
			v := *w.WindowMinutes
			s.Quota.Windows[i].WindowMinutes = &v
		}
		if w.UsedPercent != nil {
			v := *w.UsedPercent
			s.Quota.Windows[i].UsedPercent = &v
		}
		if w.RemainingPercent != nil {
			v := *w.RemainingPercent
			s.Quota.Windows[i].RemainingPercent = &v
		}
		if w.ResetsAt != nil {
			v := *w.ResetsAt
			s.Quota.Windows[i].ResetsAt = &v
		}
	}
	if s.Quota.Health.LastSuccessAt != nil {
		v := *s.Quota.Health.LastSuccessAt
		s.Quota.Health.LastSuccessAt = &v
	}
	s.Tasks.Items = append([]Task{}, s.Tasks.Items...)
	if p.tasks == nil {
		s.Tasks.Health = providers.Health{State: providers.Disabled, ObservedAt: p.opts.Now()}
		s.Tasks.Scope = "disabled"
	}
	if p.tasks != nil {
		s.Tasks = p.tasks.Snapshot()
		s.Tasks.Adapter = p.tasks.ID()
	}
	return s
}
