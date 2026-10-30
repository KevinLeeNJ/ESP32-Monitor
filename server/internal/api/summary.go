package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"

	"esp32-monitor/server/internal/panel"
)

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) error {
	if r.Method != "GET" {
		return errf(405, "method_not_allowed")
	}
	if subtle.ConstantTimeCompare([]byte(digest(bearer(r))), []byte(digest(h.cfg.ReadToken))) != 1 || bearer(r) == "" {
		return errf(401, "unauthorized")
	}
	rows, err := h.db.QueryContext(r.Context(), "SELECT state FROM v2_devices ORDER BY rowid")
	if err != nil {
		return err
	}
	defer rows.Close()
	out := panel.Summary{Devices: []panel.Device{}}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		var d Device
		if err = json.Unmarshal(raw, &d); err != nil {
			return err
		}
		if !d.Panel || d.Profile == nil || d.LastSeen == 0 || h.now().Unix()-d.LastSeen > maxStalenessSeconds(d.Profile) || len(out.Devices) >= 4 {
			continue
		}
		value := func(key string) (float64, bool) {
			v, ok := d.Latest[key]
			if !ok || !h.fresh(&d, key, v) {
				return 0, false
			}
			n, e := strconv.ParseFloat(string(v.Value), 64)
			return n, e == nil
		}
		cpu, cpuOK := value("cpu.usage")
		mem, memOK := value("memory.usage")
		up, upOK := value("system.uptime")
		disk, diskOK := 0.0, false
		// A physical disk is selected explicitly in the WebUI. No mount-path inference.
		if d.DriveID != 0 {
			key := "drive.used/" + strconv.Itoa(int(d.DriveID))
			v, ok := d.Latest[key]
			if ok && h.fresh(&d, key, v) {
				var used string
				if json.Unmarshal(v.Value, &used) == nil {
					u, e := strconv.ParseUint(used, 10, 64)
					if e == nil {
						for _, dr := range d.Profile.Inventory.Drives {
							if dr.ID == d.DriveID && dr.TotalBytes != nil && *dr.TotalBytes > 0 && u <= *dr.TotalBytes {
								disk = float64(u) / float64(*dr.TotalBytes) * 100
								diskOK = true
							}
						}
					}
				}
			}
		}
		out.Devices = append(out.Devices, panel.Device{Name: d.DisplayName, Uptime: panel.FormatUptime(up, upOK), MetricCount: 3, Metrics: []panel.Metric{panel.FormatPercent("CPU", cpu/100, cpuOK), panel.FormatPercent("MEM", mem/100, memOK), panel.FormatPercent("DISK", disk, diskOK)}})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	out.DeviceCount = len(out.Devices)
	writeJSON(w, 200, out)
	return nil
}
