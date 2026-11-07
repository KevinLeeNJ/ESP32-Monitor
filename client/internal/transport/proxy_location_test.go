package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"esp32-monitor/client/internal/transport/wire"
)

type locationDoer struct {
	mu          sync.Mutex
	ipCalls     int
	failFirst   int
	weatherHits int
	upstream    http.Handler
}

func (d *locationDoer) Do(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "ipwho.is" {
		d.mu.Lock()
		d.ipCalls++
		fail := d.ipCalls <= d.failFirst
		d.mu.Unlock()
		if fail {
			return nil, errors.New("network not ready")
		}
		return &http.Response{
			StatusCode: 200,
			Body: io.NopCloser(strings.NewReader(
				`{"success":true,"latitude":31.2,"longitude":121.5,"city":"Beijing"}`)),
			Header: http.Header{},
		}, nil
	}
	d.mu.Lock()
	d.weatherHits++
	d.mu.Unlock()
	rec := httptest.NewRecorder()
	d.upstream.ServeHTTP(rec, r)
	return rec.Result(), nil
}

func newLocationProxy(d *locationDoer) *proxyWorker {
	return &proxyWorker{
		config: wire.Config{Weather: wire.WeatherConfig{
			Enabled: true, LocationMode: "ip",
			Host: wire.Ptr("https://qweather.test"), Key: wire.Ptr("k"),
		}},
		client:       d,
		now:          time.Now,
		successTimes: map[*wire.Section]time.Time{},
		data: wire.ProxyData{
			Weather: health("starting", 1800000, 7200000),
			AQI:     health("starting", 1800000, 7200000),
		},
	}
}

func qweatherHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v7/weather/") {
			_, _ = io.WriteString(w, `{"code":"200","now":{"icon":"100"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"indexes":[{"aqiDisplay":"42"}]}`)
	})
}

// IP geolocation gates weather/AQI. After transient failures the loop must
// back off and retry, then forward the resolved city once location succeeds.
func TestProxyIPLocationBackoffAndCity(t *testing.T) {
	d := &locationDoer{failFirst: 2, upstream: qweatherHandler()}
	p := newLocationProxy(d)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); p.weatherLoop(ctx) }()
	deadline := time.Now().Add(8 * time.Second)
	for {
		p.mu.Lock()
		state := p.data.Weather.State
		p.mu.Unlock()
		if state == "healthy" || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	p.mu.Lock()
	data, _ := p.data.Weather.Data.(map[string]any)
	p.mu.Unlock()
	d.mu.Lock()
	ipCalls, weatherHits := d.ipCalls, d.weatherHits
	d.mu.Unlock()
	if p.data.Weather.State != "healthy" {
		t.Fatalf("weather never recovered: state=%s ipCalls=%d", p.data.Weather.State, ipCalls)
	}
	if ipCalls != 3 {
		t.Fatalf("ipwho.is calls = %d, want 3 (two failures + success)", ipCalls)
	}
	if weatherHits != 2 {
		t.Fatalf("qweather hits = %d, want 2 (weather + AQI after location)", weatherHits)
	}
	if data["city"] != "Beijing" {
		t.Fatalf("city = %v, want Beijing", data["city"])
	}
}

// The device reports the remaining time to its next refresh slot; a companion
// takeover must wait for it instead of fetching on connect.
func TestProxyWaitsForDeviceSlotBeforeFirstFetch(t *testing.T) {
	d := &locationDoer{upstream: qweatherHandler()}
	p := newLocationProxy(d)
	p.config.WeatherDelayMS = wire.Ptr(uint32(200))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	start := time.Now()
	go func() { defer close(done); p.weatherLoop(ctx) }()
	time.Sleep(80 * time.Millisecond)
	d.mu.Lock()
	early := d.ipCalls + d.weatherHits
	d.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		d.mu.Lock()
		started := d.ipCalls+d.weatherHits > 0
		d.mu.Unlock()
		if started || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	elapsed := time.Since(start)
	cancel()
	<-done
	if early != 0 {
		t.Fatalf("fetched before the booked slot: %d calls", early)
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("first fetch after %v, want the 200ms hint honoured", elapsed)
	}
}

// Refresh slots follow the device's wall clock, not the session start time.
func TestProxyRefreshSlotsAlignToWallClock(t *testing.T) {
	base := time.Date(2026, 11, 7, 12, 0, 10, 0, time.UTC)
	if got := nextSlot(base, 30*time.Second); !got.Equal(base.Truncate(30 * time.Second).Add(30 * time.Second)) ||
		!got.Equal(time.Date(2026, 11, 7, 12, 0, 30, 0, time.UTC)) {
		t.Fatalf("30s slot = %v", got)
	}
	if got := nextSlot(base, 15*time.Minute); !got.Equal(time.Date(2026, 11, 7, 12, 15, 0, 0, time.UTC)) {
		t.Fatalf("15m slot = %v", got)
	}
	at := time.Date(2026, 11, 7, 12, 15, 0, 0, time.UTC)
	if got := nextSlot(at, 15*time.Minute); !got.Equal(at.Add(15 * time.Minute)) {
		t.Fatalf("boundary slot = %v", got)
	}
}

// While geolocation keeps failing no weather/AQI request may be issued.
func TestProxyIPLocationFailureGatesWeather(t *testing.T) {
	d := &locationDoer{failFirst: 1 << 30, upstream: qweatherHandler()}
	p := newLocationProxy(d)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); p.weatherLoop(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		d.mu.Lock()
		calls := d.ipCalls
		d.mu.Unlock()
		if calls >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	d.mu.Lock()
	calls, weatherHits := d.ipCalls, d.weatherHits
	d.mu.Unlock()
	if calls < 2 {
		t.Fatalf("no backoff retry observed: ipCalls=%d", calls)
	}
	if weatherHits != 0 {
		t.Fatalf("weather fetched without location: hits=%d", weatherHits)
	}
	if p.data.Weather.State != "error" || p.data.Weather.Data != nil {
		t.Fatalf("weather section = %s data=%v, want error without data", p.data.Weather.State, p.data.Weather.Data)
	}
}