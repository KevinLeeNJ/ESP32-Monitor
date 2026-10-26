package transport

import (
	"context"
	"errors"
	"esp32-monitor/client/internal/transport/wire"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
)

var dnsHost = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]{0,189}[a-zA-Z0-9])?$`)
var tokenChars = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

func validateConfig(c wire.Config, allowHTTP func(string) bool) error {
	bad := func() error { return errors.New("invalid_config") }
	if c.ProtocolVersion != 1 || !wire.UUID(c.RequestID) || !wire.UUID(c.DeviceID) || !wire.UUID(c.SessionID) || !wire.UUID(c.Epoch) || c.Revision == 0 || !c.Time.Enabled || c.Time.Offset < -720 || c.Time.Offset > 840 {
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
		if w.Host == nil || !validDNS(*w.Host) || w.Key == nil || *w.Key == "" || !wire.Text(*w.Key, 127) {
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
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || (u.Scheme != "https" && (u.Scheme != "http" || allowHTTP == nil || !allowHTTP(*d.URL))) {
			return bad()
		}
		if ip, e := netip.ParseAddr(u.Hostname()); e == nil && forbidden(ip) {
			return bad()
		}
	}
	return nil
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
func forbidden(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsValid() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip.String() == "255.255.255.255" || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("0.0.0.0/8").Contains(ip)
}

// Resolve and validate at dial time, not only at config submission (DNS rebinding).
// Private Device endpoints require an exact local user authorization.
func guardedClient(allowPrivate func(string) bool) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DisableKeepAlives = false
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, errors.New("endpoint_rejected")
		}
		ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if e != nil {
			return nil, errors.New("upstream_unavailable")
		}
		if len(ips) == 0 {
			return nil, errors.New("endpoint_rejected")
		}
		for _, ip := range ips {
			if forbidden(ip) || (ip.IsPrivate() && (allowPrivate == nil || !allowPrivate(host))) {
				return nil, errors.New("endpoint_rejected")
			}
		}
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	return &http.Client{Transport: t, Timeout: 4 * 1e9, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
