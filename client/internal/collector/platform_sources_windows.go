//go:build windows

package collector

var nativeSources = []source{
	{"windows_cpu", "cpu", "native"}, {"windows_gpu", "gpu", "native"},
	{"windows_disk", "disk", "native"}, {"windows_disk_usage", "disk", "native"}, {"windows_network", "network", "native"},
	{"windows_public_ip", "network", "native"}, {"windows_battery", "battery", "native"}, {"windows_system", "system", "native"},
}

func nativeLoadSource() string { return "windows_cpu" }

func nativeSourceID(role string) string {
	switch role {
	case "cpu":
		return "windows_cpu"
	case "gpu":
		return "windows_gpu"
	case "disk":
		return "windows_disk"
	case "disk_usage":
		return "windows_disk_usage"
	case "network":
		return "windows_network"
	case "battery":
		return "windows_battery"
	case "public_ip":
		return "windows_public_ip"
	case "system":
		return "windows_system"
	}
	return ""
}
