//go:build windows

package transport

import (
	"net"
	"strings"
)

// PreferredLANIPv4 selects a private address on an active LAN interface.
func PreferredLANIPv4(defaultIP string) string {
	interfaces, _ := net.Interfaces()
	var fallback string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		if strings.Contains(name, "tun") || strings.Contains(name, "tap") || strings.Contains(name, "vpn") || strings.Contains(name, "virtual") || strings.Contains(name, "veth") || strings.Contains(name, "wsl") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			n, ok := addr.(*net.IPNet)
			if !ok || n.IP.To4() == nil || !n.IP.IsPrivate() {
				continue
			}
			ip := n.IP.String()
			if ip == defaultIP {
				return ip
			}
			if fallback == "" {
				fallback = ip
			}
		}
	}
	return fallback
}
