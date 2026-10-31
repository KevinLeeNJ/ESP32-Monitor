//go:build linux

package collector

var nativeSources = []source{
	{"linux_cpu", "cpu", "native"}, {"linux_gpu", "gpu", "native"},
	{"linux_disk", "disk", "native"}, {"linux_disk_usage", "disk", "native"}, {"linux_network", "network", "native"},
	{"linux_battery", "battery", "native"}, {"linux_public_ip", "network", "native"},
	{"linux_system", "system", "native"},
}

func nativeLoadSource() string { return "system" }

func nativeSourceID(role string) string {
	switch role {
	case "cpu", "gpu", "disk", "disk_usage", "network", "battery", "public_ip", "system":
		return "linux_" + role
	}
	return ""
}
