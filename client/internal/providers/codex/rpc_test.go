package codex

import (
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/providers"
	"net"
	"sync"
	"testing"
	"time"
)

func mockRPC(t *testing.T, handler func(message) any) *Connection {
	t.Helper()
	a, b := net.Pipe()
	rpc := NewRPC(a, a)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer b.Close()
		decoder := json.NewDecoder(b)
		encoder := json.NewEncoder(b)
		for {
			var m message
			if decoder.Decode(&m) != nil {
				return
			}
			if len(m.ID) == 0 {
				continue
			}
			value := handler(m)
			if encoder.Encode(map[string]any{"id": m.ID, "result": value}) != nil {
				return
			}
		}
	}()
	var once sync.Once
	c := &Connection{rpc, func() { once.Do(func() { rpc.Close(); b.Close(); <-done }) }}
	t.Cleanup(c.Close)
	return c
}
func TestRPCHandshakeAndNotifications(t *testing.T) {
	a, b := net.Pipe()
	rpc := NewRPC(a, a)
	defer rpc.Close()
	defer b.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		decoder := json.NewDecoder(b)
		encoder := json.NewEncoder(b)
		var m message
		if decoder.Decode(&m) != nil {
			return
		}
		if m.Method != "initialize" {
			t.Error("handshake missing")
		}
		encoder.Encode(map[string]any{"id": m.ID, "result": map[string]string{"userAgent": "codex/test"}})
		decoder.Decode(&m)
		if m.Method != "initialized" {
			t.Error("initialized missing")
		}
		decoder.Decode(&m)
		encoder.Encode(map[string]any{"method": "account/rateLimits/updated", "params": map[string]any{"rateLimits": nil}})
		encoder.Encode(map[string]any{"id": m.ID, "result": map[string]int{"ok": 1}})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	version, err := rpc.Initialize(ctx)
	if err != nil || version != "codex/test" {
		t.Fatalf("handshake: %s %v", version, err)
	}
	var result struct {
		OK int `json:"ok"`
	}
	if err := rpc.Call(ctx, "account/rateLimits/read", nil, &result); err != nil || result.OK != 1 {
		t.Fatal(err)
	}
	select {
	case n := <-rpc.Events():
		if n.Method != "account/rateLimits/updated" {
			t.Fatal(n)
		}
	case <-ctx.Done():
		t.Fatal("event missing")
	}
	<-done
}
func TestRPCCancellationClosesBlockedTransport(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	rpc := NewRPC(a, a)
	defer rpc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := rpc.Call(ctx, "initialize", nil, nil); err == nil {
		t.Fatal("blocked write ignored cancellation")
	}
	select {
	case <-rpc.Done():
	case <-time.After(time.Second):
		t.Fatal("transport leaked")
	}
}
func TestMalformedOrOverflowStreamDisconnects(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		a, b := net.Pipe()
		rpc := NewRPC(a, a)
		go func() {
			defer b.Close()
			if !overflow {
				b.Write([]byte("garbage\n"))
				return
			}
			enc := json.NewEncoder(b)
			for range 130 {
				if enc.Encode(map[string]any{"method": "thread/status/changed", "params": map[string]any{}}) != nil {
					return
				}
			}
		}()
		select {
		case <-rpc.Done():
		case <-time.After(time.Second):
			t.Fatal("invalid event stream did not fail closed")
		}
		rpc.Close()
	}
}
func TestProviderQuotaAndDesktopScope(t *testing.T) {
	now := time.Now()
	p := New(Options{Enabled: true, Now: func() time.Time { return now }})
	calls := 0
	p.connect = func(context.Context, string) (*Connection, error) {
		calls++
		return mockRPC(t, func(m message) any {
			switch m.Method {
			case "initialize":
				return map[string]string{"userAgent": "codex/test"}
			case "account/read":
				return map[string]any{"account": map[string]string{"type": "chatgpt", "email": "private@example.com"}}
			case "account/rateLimits/read":
				return map[string]any{"rateLimits": map[string]any{"primary": map[string]any{"usedPercent": 10, "windowDurationMins": 300}}}
			default:
				t.Errorf("unexpected mutating/unsupported method %s", m.Method)
				return nil
			}
		}), nil
	}
	s := p.Refresh(context.Background())
	if s.Quota.Health.State != providers.Healthy || *s.Quota.Windows[0].RemainingPercent != 90 || s.Tasks.State != Unknown || s.Tasks.Health.State != providers.Disabled {
		t.Fatalf("bad quota/scope %+v", s)
	}
	s.Quota.Windows[0].RemainingPercent = nil
	if p.Snapshot().Quota.Windows[0].RemainingPercent == nil {
		t.Fatal("aliased quota")
	}
	p.Refresh(context.Background())
	if calls != 1 {
		t.Fatal("quota polling too frequent")
	}
	now = now.Add(time.Minute)
	p.connect = func(context.Context, string) (*Connection, error) { return nil, errors.New("private failure") }
	s = p.Refresh(context.Background())
	if len(s.Quota.Windows) != 0 || s.Quota.Health.ErrorCode != "codex_unavailable" || s.Quota.Health.LastSuccessAt == nil {
		t.Fatal("failed refresh retained current quota")
	}
}
func TestProviderUnauthenticatedAndAPIKey(t *testing.T) {
	for _, accountType := range []string{"", "apiKey"} {
		t.Run(accountType, func(t *testing.T) {
			p := New(Options{Enabled: true})
			p.connect = func(context.Context, string) (*Connection, error) {
				return mockRPC(t, func(m message) any {
					if m.Method == "initialize" {
						return map[string]string{"userAgent": "test"}
					}
					if m.Method != "account/read" {
						t.Error("queried quota without ChatGPT account")
					}
					if accountType == "" {
						return map[string]any{"account": nil}
					}
					return map[string]any{"account": map[string]string{"type": accountType}}
				}), nil
			}
			s := p.Refresh(context.Background())
			want := providers.AuthRequired
			if accountType != "" {
				want = providers.Unsupported
			}
			if s.Quota.Health.State != want {
				t.Fatal(s)
			}
		})
	}
}
