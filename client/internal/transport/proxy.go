package transport

import (
	"context"
	"encoding/json"
	"errors"
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
	go p.run(ctx)
	return p
}
func (p *proxyWorker) stop() { p.cancel(); <-p.done }
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
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		var d wire.DeviceSummary
		e := p.fetch(ctx, strings.TrimRight(*p.config.Devices.URL, "/")+"/v1/devices/summary", *p.config.Devices.Token, &d, 4096)
		if e == nil {
			e = validateSummary(d)
		}
		p.mu.Lock()
		p.update(&p.data.Devices, d, e)
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
func (p *proxyWorker) fetch(ctx context.Context, endpoint, token string, out any, limit int) error {
	r, e := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if e != nil {
		return errors.New("upstream_unavailable")
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Accept-Encoding", "identity")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := p.client.Do(r)
	if e != nil {
		return errors.New("upstream_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return errors.New("auth_required")
	}
	if resp.StatusCode != 200 {
		return errors.New("upstream_error")
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "" && ce != "identity" {
		return errors.New("invalid_upstream")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, int64(limit+1)))
	if e != nil || wire.Decode(b, out, limit) != nil {
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
	p.successTimes[sKey(s)] = now
}

// successTimes uses time.Time's monotonic component; failures never reset age.
func sKey(s *wire.Section) *wire.Section { return s }
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
	var locationErr error
	if p.config.Weather.LocationMode == "ip" {
		var geo struct {
			Success   bool    `json:"success"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		}
		locationErr = p.fetch(ctx, "https://ipwho.is/", "", &geo, 4096)
		if locationErr == nil && (!geo.Success || geo.Latitude < -90 || geo.Latitude > 90 || geo.Longitude < -180 || geo.Longitude > 180) {
			locationErr = errors.New("invalid_upstream")
		}
		lat = &geo.Latitude
		lon = &geo.Longitude
	}
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		if locationErr != nil {
			p.mu.Lock()
			p.update(&p.data.Weather, nil, locationErr)
			p.update(&p.data.AQI, nil, locationErr)
			p.mu.Unlock()
		} else {
			p.weather(ctx, *lat, *lon)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (p *proxyWorker) weather(ctx context.Context, lat, lon float64) {
	host := *p.config.Weather.Host
	location := url.QueryEscape(fmt.Sprintf("%.4f,%.4f", lon, lat))
	var weather struct {
		Code string `json:"code"`
		Now  struct {
			Icon string `json:"icon"`
		} `json:"now"`
	}
	e := p.fetchWeather(ctx, "https://"+host+"/v7/weather/now?location="+location, &weather)
	condition := -1
	if e == nil {
		if weather.Code != "200" {
			e = errors.New("upstream_error")
		} else {
			condition = weatherCondition(weather.Now.Icon)
		}
	}
	p.mu.Lock()
	p.update(&p.data.Weather, map[string]any{"condition": condition, "latitude": lat, "longitude": lon, "location_source": p.config.Weather.LocationMode}, e)
	p.mu.Unlock()
	var air struct {
		Indexes []struct {
			Display *json.RawMessage `json:"aqiDisplay,omitempty"`
			AQI     *json.RawMessage `json:"aqi,omitempty"`
		} `json:"indexes"`
	}
	e = p.fetchWeather(ctx, fmt.Sprintf("https://%s/airquality/v1/current/%.5f/%.5f?lang=zh", host, lat, lon), &air)
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
func (p *proxyWorker) fetchWeather(ctx context.Context, endpoint string, out any) error {
	client := p.client // client is immutable; header wrapper is local
	r, e := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if e != nil {
		return errors.New("upstream_unavailable")
	}
	r.Header.Set("X-QW-Api-Key", *p.config.Weather.Key)
	r.Header.Set("Accept-Encoding", "identity")
	resp, e := client.Do(r)
	if e != nil {
		return errors.New("upstream_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return errors.New("auth_required")
	}
	if resp.StatusCode != 200 {
		return errors.New("upstream_error")
	}
	if resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
		return errors.New("invalid_upstream")
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if e != nil || wire.Decode(data, out, 16384) != nil {
		return errors.New("invalid_upstream")
	}
	return nil
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
