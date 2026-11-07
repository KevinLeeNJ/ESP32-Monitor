package transport

import (
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/logging"
	"esp32-monitor/client/internal/transport/wire"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HTTPDoer permits deterministic local upstreams in tests. Production always
// uses guardedClient; no arbitrary URL/method/header forwarding is exposed.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type proxyWorker struct {
	mu           sync.Mutex
	config       wire.Config
	data         wire.ProxyData
	client       HTTPDoer
	cancel       context.CancelFunc
	done         chan struct{}
	now          func() time.Time
	successTimes map[*wire.Section]time.Time
}

func health(state string, stale, expire int64) wire.Section {
	return wire.Section{Health: wire.Health{State: state, StaleAfterMS: stale, ExpireAfterMS: expire}}
}
func newProxy(ctx context.Context, c wire.Config, client HTTPDoer) *proxyWorker {
	ctx, cancel := context.WithCancel(ctx)
	p := &proxyWorker{config: c, client: client, cancel: cancel, done: make(chan struct{}), now: time.Now, successTimes: map[*wire.Section]time.Time{}}
	p.data = wire.ProxyData{Epoch: c.Epoch, Revision: c.Revision, Weather: health("disabled", 1800000, 7200000), AQI: health("disabled", 1800000, 7200000), Devices: health("disabled", 45000, 90000)}
	if c.Weather.Enabled {
		p.data.Weather.State = "starting"
		p.data.AQI.State = "starting"
	}
	if c.Devices.Enabled {
		p.data.Devices.State = "starting"
	}
	logging.L("transport").Info("proxy_worker_start", "weather_enabled", c.Weather.Enabled,
		"weather_location", c.Weather.LocationMode, "devices_enabled", c.Devices.Enabled)
	go p.run(ctx)
	return p
}
func (p *proxyWorker) stop() { p.cancel(); <-p.done }
const (
	weatherRefreshInterval = 15 * time.Minute
	devicesRefreshInterval = 30 * time.Second
)

// waitDelay honours the device's handover hint before the first fetch. A nil
// hint keeps older firmware's immediate start.
func waitDelay(ctx context.Context, ms *uint32) bool {
	if ms == nil || *ms == 0 {
		return true
	}
	timer := time.NewTimer(time.Duration(*ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// nextSlot returns the next wall-clock multiple of interval, matching
// monitor_schedule_due's epoch slots.
func nextSlot(now time.Time, interval time.Duration) time.Time {
	return now.Truncate(interval).Add(interval)
}

// waitForSlot blocks until the next wall-clock multiple of interval so Page
// one keeps the standalone cadence after a companion takes over.
func waitForSlot(ctx context.Context, interval time.Duration) bool {
	now := time.Now()
	timer := time.NewTimer(nextSlot(now, interval).Sub(now))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (p *proxyWorker) run(ctx context.Context) {
	defer close(p.done)
	var wg sync.WaitGroup
	defer wg.Wait()
	if p.config.Weather.Enabled {
		wg.Add(1)
		go func() { defer wg.Done(); p.weatherLoop(ctx) }()
	}
	if p.config.Devices.Enabled {
		wg.Add(1)
		go func() { defer wg.Done(); p.devicesLoop(ctx) }()
	}
	<-ctx.Done()
}
func (p *proxyWorker) devicesLoop(ctx context.Context) {
	if !waitDelay(ctx, p.config.DevicesDelayMS) {
		return
	}
	for {
		var d wire.DeviceSummary
		e := p.fetch(ctx, strings.TrimRight(*p.config.Devices.URL, "/")+"/v2/devices/summary", &d, 4096, func(r *http.Request) {
			r.Header.Set("Accept", "application/json")
			if token := *p.config.Devices.Token; token != "" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
		})
		if e == nil {
			e = validateSummary(d)
		}
		p.mu.Lock()
		p.update(&p.data.Devices, d, e)
		p.mu.Unlock()
		if !waitForSlot(ctx, devicesRefreshInterval) {
			return
		}
	}
}
func validateSummary(d wire.DeviceSummary) error {
	if d.Count < 0 || d.Count > 4 || d.Count != len(d.Devices) {
		return errors.New("invalid_upstream")
	}
	for _, c := range d.Devices {
		if !wire.Text(c.Name, 8) || !wire.Text(c.Uptime, 8) || c.MetricCount != 3 || len(c.Metrics) != 3 {
			return errors.New("invalid_upstream")
		}
		for i, m := range c.Metrics {
			if m.Name != []string{"CPU", "MEM", "DISK"}[i] || !wire.Text(m.Value, 8) || (m.Percent != nil && (*m.Percent < 0 || *m.Percent > 100)) {
				return errors.New("invalid_upstream")
			}
		}
	}
	return nil
}
func (p *proxyWorker) fetch(ctx context.Context, endpoint string, out any, limit int, configure func(*http.Request)) error {
	r, e := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if e != nil {
		return errors.New("upstream_unavailable")
	}
	/* Do not pin Accept-Encoding here: QWeather answers gzip even when asked
	 * for identity, and Go only transparently decompresses when it owns the
	 * header. */
	r.Header.Set("Accept", "application/json")
	if configure != nil {
		configure(r)
	}
	resp, e := p.client.Do(r)
	if e != nil {
		logging.L("transport").Warn("proxy_fetch_failed", "host", r.URL.Host, "path", r.URL.Path, "error", e.Error())
		return errors.New("upstream_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		logging.L("transport").Warn("proxy_fetch_failed", "host", r.URL.Host, "path", r.URL.Path, "status", resp.StatusCode)
		return errors.New("auth_required")
	}
	if resp.StatusCode != 200 {
		logging.L("transport").Warn("proxy_fetch_failed", "host", r.URL.Host, "path", r.URL.Path, "status", resp.StatusCode)
		return errors.New("upstream_error")
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "" && ce != "identity" {
		logging.L("transport").Warn("proxy_fetch_failed", "host", r.URL.Host, "path", r.URL.Path, "content_encoding", ce)
		return errors.New("invalid_upstream")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, int64(limit+1)))
	if e != nil || wire.DecodeJSON(b, out, limit) != nil {
		logging.L("transport").Warn("proxy_fetch_failed", "host", r.URL.Host, "path", r.URL.Path, "read_error", e != nil, "bytes", len(b))
		return errors.New("invalid_upstream")
	}
	return nil
}
func (p *proxyWorker) update(s *wire.Section, data any, e error) {
	now := p.now()
	s.ObservedAt = wire.Ptr(now.Unix())
	if e != nil {
		s.ErrorCode = wire.Ptr(e.Error())
		s.State = "error"
		if s.Data != nil {
			s.State = "degraded"
		}
		if e.Error() == "auth_required" {
			s.State = "auth_required"
			s.Data = nil
		}
		return
	}
	s.Data = data
	s.State = "healthy"
	s.ErrorCode = nil
	s.LastSuccessAt = wire.Ptr(now.Unix())
	s.AgeMS = wire.Ptr(int64(0))
	s.SourceVersion = wire.Ptr("proxy-v1")
	// successTimes uses time.Time's monotonic component; failures never reset age.
	p.successTimes[s] = now
}
func (p *proxyWorker) snapshot(serial bool) wire.ProxyData {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := p.data
	now := p.now()
	for _, x := range []struct {
		src *wire.Section
		dst *wire.Section
	}{{&p.data.Weather, &d.Weather}, {&p.data.AQI, &d.AQI}, {&p.data.Devices, &d.Devices}} {
		if t, ok := p.successTimes[x.src]; ok {
			age := max(int64(0), now.Sub(t).Milliseconds())
			x.dst.AgeMS = &age
			if age >= x.dst.ExpireAfterMS {
				x.dst.Data = nil
				if x.dst.State == "healthy" {
					x.dst.State = "degraded"
				}
			} else if age >= x.dst.StaleAfterMS && x.dst.State == "healthy" {
				x.dst.State = "degraded"
			}
		}
	}
	stale := int64(45000)
	if serial {
		stale = 15000
	}
	d.Time = health("healthy", stale, 60000)
	d.Time.ObservedAt = wire.Ptr(now.Unix())
	d.Time.LastSuccessAt = wire.Ptr(now.Unix())
	d.Time.AgeMS = wire.Ptr(int64(0))
	d.Time.SourceVersion = wire.Ptr("system")
	d.Time.Data = wire.TimeData{UnixMS: now.UnixMilli(), Offset: p.config.Time.Offset, Trusted: false, Source: "system_clock"}
	return d
}
func (p *proxyWorker) weatherLoop(ctx context.Context) {
	lat, lon := p.config.Weather.Latitude, p.config.Weather.Longitude
	city := ""
	var locationErr error
	if p.config.Weather.LocationMode == "ip" {
		lat, lon = nil, nil
	}
	if !waitDelay(ctx, p.config.WeatherDelayMS) {
		return
	}
	backoff := time.Second
	for {
		if lat == nil {
			var geo struct {
				Success   bool    `json:"success"`
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
				City      string  `json:"city"`
			}
			locationErr = p.fetch(ctx, "https://ipwho.is/", &geo, 4096, func(r *http.Request) {
				r.Header.Set("Accept", "application/json")
			})
			if locationErr == nil && (!geo.Success || geo.Latitude < -90 || geo.Latitude > 90 || geo.Longitude < -180 || geo.Longitude > 180) {
				locationErr = errors.New("invalid_upstream")
			}
			if locationErr == nil {
				lat, lon = &geo.Latitude, &geo.Longitude
				city = wire.Clip(geo.City, 31)
				backoff = time.Second
			}
		}
		if locationErr != nil {
			p.mu.Lock()
			p.update(&p.data.Weather, nil, locationErr)
			p.update(&p.data.AQI, nil, locationErr)
			p.mu.Unlock()
			// Location gates weather/AQI; keep retrying with backoff instead
			// of staying broken until the next 15-minute tick.
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		p.weather(ctx, *lat, *lon, city)
		if !waitForSlot(ctx, weatherRefreshInterval) {
			return
		}
	}
}
func (p *proxyWorker) weather(ctx context.Context, lat, lon float64, city string) {
	host := *p.config.Weather.Host
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	host = strings.TrimRight(host, "/")
	location := url.QueryEscape(fmt.Sprintf("%.4f,%.4f", lon, lat))
	var weather struct {
		Code string `json:"code"`
		Now  struct {
			Icon string `json:"icon"`
		} `json:"now"`
	}
	e := p.fetch(ctx, host+"/v7/weather/now?location="+location, &weather, 16384, p.qweatherHeaders)
	condition := -1
	if e == nil {
		if weather.Code != "200" {
			e = errors.New("upstream_error")
		} else {
			condition = weatherCondition(weather.Now.Icon)
		}
	}
	p.mu.Lock()
	payload := map[string]any{"condition": condition, "latitude": lat, "longitude": lon, "location_source": p.config.Weather.LocationMode}
	if city != "" {
		payload["city"] = city
	}
	p.update(&p.data.Weather, payload, e)
	p.mu.Unlock()
	errorText := ""
	if e != nil {
		errorText = e.Error()
	}
	if e != nil || condition < 0 {
		logging.L("transport").Warn("proxy_weather_result", "code", weather.Code,
			"icon", weather.Now.Icon, "condition", condition, "error", errorText)
	} else {
		logging.L("transport").Info("proxy_weather_result", "code", weather.Code,
			"icon", weather.Now.Icon, "condition", condition)
	}
	var air struct {
		Indexes []struct {
			Display *json.RawMessage `json:"aqiDisplay,omitempty"`
			AQI     *json.RawMessage `json:"aqi,omitempty"`
		} `json:"indexes"`
	}
	e = p.fetch(ctx, fmt.Sprintf("%s/airquality/v1/current/%.5f/%.5f?lang=zh", host, lat, lon), &air, 16384, p.qweatherHeaders)
	v := -1
	if e == nil {
		if len(air.Indexes) == 0 {
			e = errors.New("invalid_upstream")
		} else {
			index := air.Indexes[0]
			for _, raw := range []*json.RawMessage{index.Display, index.AQI} {
				if raw == nil {
					continue
				}
				text := strings.Trim(string(*raw), "\"")
				n, err := strconv.ParseFloat(text, 64)
				if err == nil && n >= 0 && n <= 500 && n == float64(int(n)) {
					v = int(n)
					break
				}
			}
			if v < 0 {
				e = errors.New("invalid_upstream")
			}
		}
	}
	p.mu.Lock()
	p.update(&p.data.AQI, map[string]any{"value": v, "scale": "qweather_aqi"}, e)
	p.mu.Unlock()
}

// QWeather supports X-QW-Api-Key. Do not put credentials in URL query strings.
func (p *proxyWorker) qweatherHeaders(r *http.Request) {
	r.Header.Set("X-QW-Api-Key", *p.config.Weather.Key)
}

// Keep the firmware's condition families; an unrecognized code stays unknown.
func weatherCondition(icon string) int {
	n, e := strconv.Atoi(icon)
	if e != nil {
		return -1
	}
	switch {
	case n == 100 || n == 150:
		return 0
	case n == 101 || n == 102 || n == 151 || n == 152:
		return 1
	case n == 103 || n == 153:
		return 2
	case n == 104:
		return 3
	case n >= 300 && n <= 304:
		return 7
	case n == 305 || n == 309 || n == 399:
		return 4
	case n == 306 || n == 314:
		return 5
	case n >= 307 && n <= 312:
		return 6
	case n == 313 || n >= 404 && n <= 407:
		return 11
	case n == 400 || n == 408 || n == 499:
		return 8
	case n == 401 || n == 409:
		return 9
	case n == 402 || n == 403 || n == 410:
		return 10
	case n == 500 || n == 501:
		return 13
	case n == 502:
		return 14
	case n == 503 || n == 504 || n == 507 || n == 508:
		return 15
	}
	return -1
}
