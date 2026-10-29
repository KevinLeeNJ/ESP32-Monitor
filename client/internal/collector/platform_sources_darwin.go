//go:build darwin

package collector

var nativeSources = []source{
	{"darwin_cpu", "cpu", "native"}, {"darwin_memory", "memory", "native"}, {"darwin_gpu", "gpu", "native"},
	{"darwin_disk", "disk", "native"}, {"darwin_disk_usage", "disk", "native"}, {"darwin_network", "network", "native"},
	{"darwin_battery", "battery", "native"}, {"darwin_public_ip", "network", "native"}, {"darwin_system", "system", "native"},
}

func nativeLoadSource() string { return "system" }

func nativeSourceID(role string) string {
	switch role {
	case "cpu":
		return "darwin_cpu"
	case "memory":
		return "darwin_memory"
	case "gpu":
		return "darwin_gpu"
	case "disk":
		return "darwin_disk"
	case "disk_usage":
		return "darwin_disk_usage"
	case "network":
		return "darwin_network"
	case "battery":
		return "darwin_battery"
	case "public_ip":
		return "darwin_public_ip"
	case "system":
		return "darwin_system"
	}
	return ""
}
