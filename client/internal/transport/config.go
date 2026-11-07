package transport

import (
	"errors"
	"esp32-monitor/client/internal/transport/wire"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var dnsHost = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]{0,189}[a-zA-Z0-9])?$`)
var tokenChars = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

func validateConfig(c wire.Config) error {
	bad := func() error { return errors.New("invalid_config") }
	if c.ProtocolVersion != 1 || !wire.UUID(c.RequestID) || !wire.UUID(c.DeviceID) || !wire.UUID(c.SessionID) || !wire.UUID(c.Epoch) || c.Revision == 0 || !c.Time.Enabled || c.Time.Offset < -720 || c.Time.Offset > 840 {
		return bad()
	}
	if (c.WeatherDelayMS != nil && *c.WeatherDelayMS > 900000) ||
		(c.DevicesDelayMS != nil && *c.DevicesDelayMS > 30000) {
		return bad()
	}
	w := c.Weather
	if w.LocationMode != "coordinates" && w.LocationMode != "ip" {
		return bad()
	}
	if !w.Enabled {
		if w.Latitude != nil || w.Longitude != nil || w.Host != nil || w.Key != nil {
			return bad()
		}
	} else {
		if w.Host == nil || !validWeatherURL(*w.Host) || w.Key == nil || *w.Key == "" || !wire.Text(*w.Key, 127) {
			return bad()
		}
		if w.LocationMode == "coordinates" {
			if w.Latitude == nil || w.Longitude == nil || *w.Latitude < -90 || *w.Latitude > 90 || *w.Longitude < -180 || *w.Longitude > 180 {
				return bad()
			}
		} else if w.Latitude != nil || w.Longitude != nil {
			return bad()
		}
	}
	d := c.Devices
	if !d.Enabled {
		if d.URL != nil || d.Token != nil {
			return bad()
		}
	} else {
		if d.URL == nil || !wire.Text(*d.URL, 192) || d.Token == nil || len(*d.Token) > 128 || !tokenChars.MatchString(*d.Token) {
			return bad()
		}
		u, e := url.Parse(*d.URL)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
			return bad()
		}
		if u.Scheme != "https" && u.Scheme != "http" {
			return bad()
		}
	}
	return nil
}
func validWeatherURL(raw string) bool {
	if validDNS(raw) {
		return true
	} // Older firmware sends only the hostname.
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 191 && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == "" && u.RawPath == "" && (u.Path == "" || u.Path == "/")
}
func validDNS(s string) bool {
	if !dnsHost.MatchString(s) || len(s) > 191 || net.ParseIP(s) != nil {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if l == "" || len(l) > 63 || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return false
		}
	}
	return true
}
func guardedClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DisableKeepAlives = false
	return &http.Client{Transport: t, Timeout: 4 * 1e9, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
