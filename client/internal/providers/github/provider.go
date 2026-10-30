// Package github reads the official GraphQL contribution calendar, never HTML.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"esp32-monitor/client/internal/logging"
	"esp32-monitor/client/internal/platform"
	"esp32-monitor/client/internal/platform/proxy"
	"esp32-monitor/client/internal/providers"
)

type TokenSource func(context.Context, string) (string, error)
type Options struct {
	Login     string
	Enabled   bool
	Token     TokenSource
	CachePath string
	// Now is injectable for deterministic scheduling tests.
	Now func() time.Time
	// HTTPClient may customize TLS/proxy settings; redirects are always disabled.
	HTTPClient *http.Client
}
type Snapshot struct {
	Health        providers.Health `json:"health"`
	Data          *Calendar        `json:"data"`
	Stale         bool             `json:"stale"`
	NextRefreshAt *time.Time       `json:"next_refresh_at"`
}
type cached struct {
	Login     string    `json:"login"`
	FetchedAt time.Time `json:"fetched_at"`
	Data      Calendar  `json:"data"`
}
type Provider struct {
	refreshMu    sync.Mutex
	mu           sync.Mutex
	opts         Options
	client       *http.Client
	url          string
	cache        *cached
	health       providers.Health
	next         time.Time
	blockedUntil time.Time
	lastAttempt  time.Time
	failures     int
	cacheErr     bool
}

func New(opts Options) (*Provider, error) {
	if opts.Enabled && !platform.ValidGitHubLogin(opts.Login) {
		return nil, errors.New("invalid_github_login")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	client := *platform.NewHTTPClient(15 * time.Second)
	if opts.HTTPClient != nil {
		client = *opts.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	p := &Provider{opts: opts, client: &client, url: endpoint, health: providers.Health{State: providers.Starting, ObservedAt: opts.Now(), SourceVersion: "github-graphql-v4"}}
	if !opts.Enabled {
		p.health.State = providers.Disabled
		return p, nil
	}
	if opts.CachePath != "" {
		if info, err := os.Stat(opts.CachePath); err == nil && info.Size() <= 1<<20 {
			data, err := os.ReadFile(opts.CachePath)
			var c cached
			if err == nil && json.Unmarshal(data, &c) == nil && strings.EqualFold(c.Login, opts.Login) && strings.EqualFold(c.Data.Login, opts.Login) && validCalendar(c.Data) && !c.FetchedAt.After(opts.Now()) && opts.Now().Sub(c.FetchedAt) <= 7*24*time.Hour {
				p.cache = &c
				t := c.FetchedAt
				p.health.LastSuccessAt = &t
				p.health.State = providers.Degraded
				p.health.ErrorCode = "cached_data"
			}
			if strings.EqualFold(c.Login, opts.Login) && !c.FetchedAt.IsZero() && opts.Now().Sub(c.FetchedAt) > 7*24*time.Hour {
				_ = os.Remove(opts.CachePath)
			}
		}
	}
	return p, nil
}

// Refresh serializes callers, enforces 60s manual / 15m automatic refresh and upstream reset times.
func (p *Provider) Refresh(ctx context.Context, manual bool) Snapshot {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()
	p.mu.Lock()
	now := p.opts.Now()
	if !p.opts.Enabled {
		s := p.snapshot(now)
		p.mu.Unlock()
		return s
	}
	due := p.next
	if manual && p.failures == 0 {
		due = p.lastAttempt.Add(time.Minute)
		if p.lastAttempt.IsZero() {
			due = time.Time{}
		}
	}
	if p.blockedUntil.After(due) {
		due = p.blockedUntil
	}
	if now.Before(due) {
		s := p.snapshot(now)
		p.mu.Unlock()
		return s
	}
	p.lastAttempt = now
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var token string
	var err error
	if p.opts.Token == nil {
		err = errors.New("missing_token")
	} else {
		token, err = p.opts.Token(ctx, p.opts.Login)
	}
	var calendar Calendar
	var reset time.Time
	if err != nil || strings.TrimSpace(token) == "" {
		code := "auth_required"
		if err != nil {
			// Refresh-token transport failures do not mean the user signed out.
			switch err.Error() {
			case "auth_network_error":
				code = "network_error"
			case "auth_rate_limited":
				code = "rate_limited"
			case "auth_upstream_error":
				code = "upstream_error"
			case "credential_save_failed":
				code = "credential_save_failed"
			case proxy.ErrAutomatic.Error(), proxy.ErrDetection.Error(), proxy.ErrInvalid.Error():
				code = err.Error()
			}
		}
		err = &apiError{code: code}
	} else {
		calendar, reset, err = fetch(ctx, p.client, p.url, token, p.opts.Login, now)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	previousState, previousCode := p.health.State, p.health.ErrorCode
	p.health.ObservedAt = now
	if err != nil {
		p.failures++
		code := errorCode(err)
		p.health.ErrorCode = code
		p.health.State = providers.Error
		if p.cache != nil {
			p.health.State = providers.Degraded
		}
		if code == "auth_required" {
			p.health.State = providers.AuthRequired
		}
		if code == "permission_denied" {
			p.health.State = providers.PermissionDenied
		}
		delay := time.Minute * time.Duration(1<<min(p.failures-1, 4))
		p.next = now.Add(delay)
		var e *apiError
		if errors.As(err, &e) && e.retry.After(p.next) {
			p.next = e.retry
		}
		if errors.As(err, &e) && e.code == "rate_limited" {
			p.blockedUntil = e.retry
		}
	} else {
		p.failures = 0
		p.cache = &cached{p.opts.Login, now, calendar}
		t := now
		p.health.LastSuccessAt = &t
		p.health.State = providers.Healthy
		p.health.ErrorCode = ""
		p.next = now.Add(15 * time.Minute)
		if reset.After(p.next) {
			p.next = reset
		}
		p.blockedUntil = reset
		if p.opts.CachePath != "" {
			p.cacheErr = p.save() != nil
		} else {
			p.cacheErr = false
		}
	}
	if p.health.State != previousState || p.health.ErrorCode != previousCode {
		if p.health.State == providers.Healthy {
			logging.L("github").Info("github_recovered")
		} else {
			logging.L("github").Warn("github_health", "state", p.health.State, "error_code", p.health.ErrorCode)
		}
	}
	return p.snapshot(now)
}
func (p *Provider) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshot(p.opts.Now())
}
func (p *Provider) snapshot(now time.Time) Snapshot {
	if p.cache != nil && now.Sub(p.cache.FetchedAt) > 7*24*time.Hour {
		// Remove only the on-disk entry corresponding to this in-memory cache.
		if p.opts.CachePath != "" {
			data, err := os.ReadFile(p.opts.CachePath)
			var disk cached
			if err == nil && json.Unmarshal(data, &disk) == nil && disk.FetchedAt.Equal(p.cache.FetchedAt) && strings.EqualFold(disk.Login, p.opts.Login) {
				_ = os.Remove(p.opts.CachePath)
			}
		}
		p.cache = nil
		if p.health.State == providers.Healthy {
			p.health.State = providers.Degraded
			p.health.ErrorCode = "cache_expired"
		}
	}
	s := Snapshot{Health: p.health}
	if !p.next.IsZero() {
		t := p.next
		s.NextRefreshAt = &t
	}
	if p.cache != nil && now.Sub(p.cache.FetchedAt) <= 7*24*time.Hour {
		data := p.cache.Data
		data.Days = append([]Day(nil), data.Days...)
		data.Today = nil
		for _, d := range data.Days {
			if d.Date == now.UTC().Format("2006-01-02") {
				n := d.Count
				data.Today = &n
			}
		}
		s.Data = &data
		s.Stale = now.Sub(p.cache.FetchedAt) > 24*time.Hour
	}
	if s.Health.LastSuccessAt != nil {
		t := *s.Health.LastSuccessAt
		s.Health.LastSuccessAt = &t
	}
	if s.Stale || (p.cache != nil && s.Data == nil) {
		if s.Health.State == providers.Healthy {
			s.Health.State = providers.Degraded
			s.Health.ErrorCode = "cache_stale"
		}
	}
	if p.cacheErr && s.Health.State == providers.Healthy {
		s.Health.State = providers.Degraded
		s.Health.ErrorCode = "cache_write_failed"
	}
	return s
}

func validCalendar(c Calendar) bool {
	if len(c.Days) == 0 || len(c.Days) > 371 {
		return false
	}
	total := 0
	previous := ""
	for _, d := range c.Days {
		if _, err := time.Parse("2006-01-02", d.Date); err != nil || d.Date <= previous || d.Count < 0 || d.Level < 0 || d.Level > 4 {
			return false
		}
		total += d.Count
		previous = d.Date
	}
	return total == c.Total
}
func (p *Provider) save() error {
	path := p.opts.CachePath
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, _ := json.Marshal(p.cache)
	return platform.WriteFileAtomic(filepath.Dir(path), ".github-*", filepath.Base(path), data, 0600)
}
