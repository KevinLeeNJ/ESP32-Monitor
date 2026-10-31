//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// linuxMDHealth derives array health from md sysfs. The array must be clean or
// active, no member may be degraded, and observed member error counters must be
// zero. A cached SMART failure fails the array; missing SMART data is unknown
// and does not by itself mark a healthy array as failed.
func linuxMDHealth(blockRoot string, smart map[string]float64) map[string]float64 {
	paths, err := filepath.Glob(filepath.Join(blockRoot, "md*"))
	if err != nil {
		return nil
	}
	out := map[string]float64{}
	for _, path := range paths {
		md := filepath.Join(path, "md")
		if st, err := os.Stat(md); err != nil || !st.IsDir() {
			continue
		}
		state, err := os.ReadFile(filepath.Join(md, "array_state"))
		if err != nil {
			continue
		}
		degraded, err := os.ReadFile(filepath.Join(md, "degraded"))
		if err != nil {
			continue
		}
		missing, err := strconv.ParseUint(strings.TrimSpace(string(degraded)), 10, 32)
		if err != nil {
			continue
		}
		members, _ := filepath.Glob(filepath.Join(md, "dev-*"))
		if len(members) == 0 {
			continue
		}
		healthy := missing == 0 && (strings.TrimSpace(string(state)) == "clean" || strings.TrimSpace(string(state)) == "active")
		for _, member := range members {
			state, err := os.ReadFile(filepath.Join(member, "state"))
			if err != nil || strings.TrimSpace(string(state)) != "in_sync" {
				healthy = false
			}
			for _, counter := range []string{"errors", "bad_blocks"} {
				data, err := os.ReadFile(filepath.Join(member, counter))
				if err != nil {
					continue
				}
				text := strings.TrimSpace(string(data))
				if text == "" {
					// Some kernels expose bad_blocks without the journal feature.
					continue
				}
				n, err := strconv.ParseUint(text, 10, 64)
				if err != nil || n != 0 {
					healthy = false
				}
			}
			if disk := linuxMDParentDevice(member); disk != "" {
				if h, ok := smart[disk]; ok && h == 0 {
					healthy = false
				}
			}
		}
		if healthy {
			out[filepath.Base(path)] = 1
		} else {
			out[filepath.Base(path)] = 0
		}
	}
	return out
}

// member block links end in .../block/sdc/sdc1; the parent disk is sdc.
func linuxMDParentDevice(member string) string {
	target, err := os.Readlink(filepath.Join(member, "block"))
	if err != nil {
		return ""
	}
	_, rest, ok := strings.Cut(target, "/block/")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	return name
}
