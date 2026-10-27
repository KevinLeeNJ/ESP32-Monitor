package collector

import (
	"context"
	"encoding/json"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type v2Fixture struct {
	fixedReads *atomic.Int32
	missing    *atomic.Bool
}

func fixtureFixed() Batch {
	return Batch{Metrics: []Metric{
		{Name: "cpu", Tags: map[string]string{"model": "Fixture CPU"}, Fields: map[string]float64{"logical_cores": 2, "physical_cores": 2}},
		{Name: "mem", Fields: map[string]float64{"total": 8192}},
		{Name: "system", Tags: map[string]string{"os_platform": "fixture", "os_version": "1"}},
		{Name: "net", Tags: map[string]string{"interface": "en0"}},
		{Name: "darwin_network", Tags: map[string]string{"interface": "en0", "type": "wifi"}},
		{Name: "diskio", Tags: map[string]string{"name": "disk0"}},
		{Name: "darwin_disk", Tags: map[string]string{"device": "disk0", "model": "Fixture SSD"}, Fields: map[string]float64{"total": 65536, "smart_media_errors": 0}},
		{Name: "disk", Tags: map[string]string{"device": "disk0s1", "path": "/", "fstype": "apfs"}, Fields: map[string]float64{"total": 32768}},
		{Name: "darwin_gpu", Tags: map[string]string{"device": "fixture-gpu"}},
		{Name: "darwin_battery", Tags: map[string]string{}},
		{Name: "darwin_fan", Tags: map[string]string{"fan": "0"}},
	}}
}
func (f *v2Fixture) Fixed(context.Context) Batch { f.fixedReads.Add(1); return fixtureFixed() }
func (f *v2Fixture) Fork() Collector             { return &v2Fixture{fixedReads: f.fixedReads, missing: f.missing} }
func (f *v2Fixture) Gather(_ context.Context, ids []string) (Batch, map[string]string) {
	b := Batch{Metrics: []Metric{}}
	add := func(src string, tags map[string]string, fields map[string]float64) {
		b.Metrics = append(b.Metrics, Metric{Name: src, Tags: tags, Fields: fields, Timestamp: time.Now().Unix()})
	}
	for _, id := range ids {
		switch id {
		case "cpu":
			add(id, map[string]string{"cpu": "cpu-total"}, map[string]float64{"usage_active": 12.5})
			add(id, map[string]string{"cpu": "cpu0"}, map[string]float64{"usage_active": 0})
		case "mem":
			if !f.missing.Load() {
				add(id, map[string]string{}, map[string]float64{"used_percent": 62, "used": 0})
			}
		case "system":
			add(id, map[string]string{}, map[string]float64{"uptime": 86400, "load1": 1.25})
		case "darwin_cpu":
			add(id, map[string]string{}, map[string]float64{"temperature_celsius": 42.3})
		case "swap":
			add(id, map[string]string{}, map[string]float64{"total": 0, "used": 0})
		case "darwin_memory":
			add(id, map[string]string{}, map[string]float64{"app_bytes": 0, "wired_bytes": 0, "compressed_bytes": 0, "pressure_level": 1})
		case "darwin_network":
			add(id, map[string]string{"interface": "en0", "type": "wifi", "ipv4": "192.168.1.2"}, map[string]float64{"connected": 1})
		case "net":
			add(id, map[string]string{"interface": "en0"}, map[string]float64{"download_bytes_per_second": 0, "upload_bytes_per_second": 1024})
		case "darwin_gpu":
			add(id, map[string]string{"device": "fixture-gpu"}, map[string]float64{"usage_percent": 0, "temperature_celsius": 0})
		case "diskio":
			add(id, map[string]string{"name": "disk0"}, map[string]float64{"read_bytes_per_second": 0, "write_bytes_per_second": 0})
		case "darwin_disk":
			add(id, map[string]string{"device": "disk0"}, map[string]float64{"smart_healthy": 0, "smart_media_errors": 0})
		case "disk":
			add(id, map[string]string{"device": "disk0s1", "path": "/", "fstype": "apfs"}, map[string]float64{"used": 0})
		case "darwin_battery":
			add(id, map[string]string{}, map[string]float64{"charge_percent": 0, "charging": 0})
		case "darwin_fan":
			add(id, map[string]string{"fan": "0"}, map[string]float64{"speed_rpm": 0})
		}
	}
	return b, map[string]string{}
}
func newV2Fixture(t *testing.T, endpoint, dir string, reads *atomic.Int32, missing *atomic.Bool) *Manager {
	t.Helper()
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{StateDirectory: dir, UploadURL: endpoint, UploadToken: func(context.Context) (string, error) { return strings.Repeat("a", 64), nil }, CoreFactory: func() Collector { return &v2Fixture{reads, missing} }, NativeFactory: func() Collector { return &NativeCollector{} }, ApplyTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}
func v2Config(optional ...v2.UploadSelection) Config {
	return Config{IntervalSeconds: 5, Selections: []Selection{}, Upload: &v2.UploadConfig{Enabled: true, IntervalSeconds: 5, Optional: optional}}
}
func awaitV2(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for v2 uploader")
}

func TestV2ProfileRetrySelectionAndStartupCache(t *testing.T) {
	var lock sync.Mutex
	var profile *v2.Profile
	revision := uint64(0)
	writes, patches, posts := 0, 0, 0
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
			t.Error("upstream auth")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v2/collector/profile":
			if profile == nil {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("ETag", `"7"`)
			_ = json.NewEncoder(w).Encode(profile)
		case "PUT /v2/collector/profile":
			if r.Header.Get("If-None-Match") != "*" {
				t.Error("missing creation condition")
			}
			var p v2.Profile
			_ = json.NewDecoder(r.Body).Decode(&p)
			profile = &p
			revision = 7
			writes++
			w.Header().Set("ETag", `"7"`)
			_, _ = io.WriteString(w, `{"r":7}`)
		case "PATCH /v2/collector/profile":
			var ops v2.ProfilePatch
			_ = json.NewDecoder(r.Body).Decode(&ops)
			for _, op := range ops {
				if op.Path != "/enabled" {
					t.Errorf("unchanged fixed fields repeated: %+v", op)
				}
				_ = json.Unmarshal(op.Value, &profile.Enabled)
			}
			patches++
			w.Header().Set("ETag", `"7"`)
			_, _ = io.WriteString(w, `{"r":7}`)
		case "POST /v2/collector/samples":
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, body)
			var batch v2.SampleBatch
			if json.Unmarshal(body, &batch) != nil || batch.Revision != revision || len(batch.Samples) != 1 {
				t.Errorf("invalid batch %s", body)
			}
			posts++
			if posts == 1 {
				w.WriteHeader(503)
			} else {
				w.WriteHeader(204)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	reads, missing := &atomic.Int32{}, &atomic.Bool{}
	m := newV2Fixture(t, server.URL+"/v2/collector/samples", "", reads, missing)
	c := v2Config(v2.UploadSelection{Metric: "cpu.model"})
	if _, e := m.Apply(context.Background(), c); e != nil {
		t.Fatal(e)
	}
	awaitV2(t, func() bool { return m.Snapshot().LastUploadAt != nil })
	lock.Lock()
	if len(bodies) < 2 || string(bodies[0]) != string(bodies[1]) {
		t.Error("retry changed payload or batch ID")
	}
	if strings.Contains(string(bodies[0]), "Fixture") {
		t.Error("fixed data in samples")
	}
	if writes != 1 {
		t.Error("profile repeated")
	}
	lock.Unlock()
	c.Upload.Optional = append(c.Upload.Optional, v2.UploadSelection{Metric: "memory.used"})
	if _, e := m.Apply(context.Background(), c); e != nil {
		t.Fatal(e)
	}
	awaitV2(t, func() bool { return m.Snapshot().LastUploadAt != nil })
	if _, e := m.Discover(context.Background()); e != nil {
		t.Fatal(e)
	}
	if reads.Load() != 1 {
		t.Fatalf("fixed probe count %d", reads.Load())
	}
	lock.Lock()
	if patches != 1 {
		t.Errorf("selection changes should PATCH once: %d", patches)
	}
	lock.Unlock()
	dir := m.opts.StateDirectory
	_ = m.Close()
	restored := newV2Fixture(t, server.URL+"/v2/collector/samples", dir, &atomic.Int32{}, missing)
	if e := restored.Restore(context.Background()); e != nil {
		t.Fatal(e)
	}
	awaitV2(t, func() bool { return restored.Snapshot().LastUploadAt != nil })
	lock.Lock()
	defer lock.Unlock()
	if writes != 1 || patches != 1 {
		t.Errorf("restart retransmitted fixed profile: %d/%d", writes, patches)
	}
}
func TestV2SevenCategoriesAndMissingRequired(t *testing.T) {
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	c := v2Config()
	for _, key := range []string{"cpu.temperature", "cpu.load1", "memory.used", "memory.app", "memory.wired", "memory.compressed", "memory.swap_total", "memory.swap_used", "memory.pressure"} {
		c.Upload.Optional = append(c.Upload.Optional, v2.UploadSelection{Metric: key})
	}
	ids := map[string]uint16{}
	for _, kind := range []string{"core", "gpu", "drive", "volume", "interface", "battery", "fan"} {
		for id := range m.v2.objectsOfKind(kind) {
			ids[kind] = id
			break
		}
	}
	for key, kind := range map[string]string{"gpu.usage": "gpu", "gpu.temperature": "gpu", "drive.read_rate": "drive", "drive.write_rate": "drive", "drive.smart_health": "drive", "drive.smart_attribute": "drive", "volume.used": "volume", "interface.download_rate": "interface", "interface.ipv4": "interface", "battery.level": "battery", "battery.charging": "battery", "fan.rpm": "fan"} {
		s := v2.UploadSelection{Metric: key, Objects: []uint16{ids[kind]}}
		if key == "drive.smart_attribute" {
			s.Detail = []uint16{4}
		}
		c.Upload.Optional = append(c.Upload.Optional, s)
	}
	var e error
	c, e = m.prepareV2(c)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := m.opts.CoreFactory().Gather(context.Background(), allSources())
	s, e := m.sampleV2(c, b)
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(s)
	for _, fragment := range []string{`"c":1250`, `"m":6200`, `"t":423`, `"u":"0"`, `"p":0`, `"h":false`, `"c":false`, `"r":0`} {
		if !strings.Contains(string(encoded), fragment) {
			t.Errorf("missing %s in %s", fragment, encoded)
		}
	}
	p := m.profileV2(c)
	if p.Inventory.CPU != nil || p.Inventory.OS != nil || p.Inventory.MemoryTotal != nil || p.Inventory.Drives[0].Model != nil {
		t.Fatal("unselected fixed info leaked")
	}
	for n := range b.Metrics {
		if b.Metrics[n].Name == "mem" {
			delete(b.Metrics[n].Fields, "used_percent")
		}
	}
	if _, e = m.sampleV2(c, b); e == nil {
		t.Fatal("missing required value invented")
	}
	c.Upload.Optional = []v2.UploadSelection{{Metric: "interface.ipv4"}}
	if _, e = m.prepareV2(c); e == nil {
		t.Fatal("implicit future objects allowed")
	}
}
func TestV2MinimalAndPatchDoesNotRepeatMetadata(t *testing.T) {
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	c := v2Config()
	b, _ := m.opts.CoreFactory().Gather(context.Background(), []string{"cpu", "mem", "system"})
	s, e := m.sampleV2(c, b)
	if e != nil {
		t.Fatal(e)
	}
	s.Timestamp = 1793059200
	body, _ := json.Marshal(v2.SampleBatch{Revision: 12, ID: "0123456789abcdef0123456789abcdef", Samples: []v2.Sample{s}})
	if len(body) != 99 {
		t.Fatalf("minimum batch grew: %s", body)
	}
	before := v2.Profile{Version: 2, IntervalSeconds: 5, Enabled: []v2.UploadSelection{}, Inventory: v2.Inventory{CPU: &v2.CPUInfo{Model: ptr("cpu")}, Drives: []v2.DriveInfo{{ID: 2, Model: ptr("ssd"), TotalBytes: ptr(uint64(100))}}}}
	after := detached(before)
	after.Inventory.Drives[0].TotalBytes = ptr(uint64(200))
	ops := profileDiff(before, after)
	if len(ops) != 1 || ops[0].Path != "/inventory/drives/0/total" || string(ops[0].Value) != `"200"` {
		t.Fatalf("expected changed leaf only: %+v", ops)
	}
	after = detached(before)
	after.Inventory.Drives = append(after.Inventory.Drives, v2.DriveInfo{ID: 9})
	ops = profileDiff(before, after)
	if len(ops) != 1 || ops[0].Path != "/inventory/drives/-" {
		t.Fatalf("existing object retransmitted: %+v", ops)
	}
	if !reflect.DeepEqual(m.profileV2(c).Inventory, v2.Inventory{}) {
		t.Fatal("minimum profile has fixed info")
	}
}

func TestV2ExactValuesAndAddressRemoval(t *testing.T) {
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	drive := m.v2.inventory.Drives[0].ID
	iface := m.v2.inventory.Interfaces[0].ID
	c := v2Config(v2.UploadSelection{Metric: "memory.used"}, v2.UploadSelection{Metric: "drive.smart_attribute", Objects: []uint16{drive}, Detail: []uint16{4}}, v2.UploadSelection{Metric: "interface.ipv4", Objects: []uint16{iface}})
	b, _ := m.opts.CoreFactory().Gather(context.Background(), allSources())
	for n := range b.Metrics {
		v := &b.Metrics[n]
		switch v.Name {
		case "mem":
			v.Exact = map[string]string{"used": "18446744073709551615"}
		case "darwin_disk":
			delete(v.Fields, "smart_media_errors")
			v.Exact = map[string]string{"smart_media_errors": "340282366920938463463374607431768211455"}
		case "darwin_network":
			v.Tags["ipv4"] = ""
		}
	}
	sample, err := m.sampleV2(c, b)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(sample)
	for _, want := range []string{`"u":"18446744073709551615"`, `"4":"340282366920938463463374607431768211455"`, `"ip":[]`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("missing %s: %s", want, body)
		}
	}
	for _, v := range b.Metrics {
		if v.Name == "darwin_disk" {
			v.Exact["smart_media_errors"] = "340282366920938463463374607431768211456"
		}
	}
	if _, ok := smartInteger(fixedMetric(b, "darwin_disk", "", ""), "smart_media_errors"); ok {
		t.Fatal("SMART overflow accepted")
	}
}

func TestV2MissingServerProfileReconcilesBeforeNewBatch(t *testing.T) {
	var puts, posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v2/collector/profile":
			w.WriteHeader(404)
		case "PUT /v2/collector/profile":
			n := puts.Add(1)
			w.Header().Set("ETag", `"`+strconv.Itoa(int(n))+`"`)
			_, _ = fmt.Fprintf(w, `{"r":%d}`, n)
		case "POST /v2/collector/samples":
			if posts.Add(1) == 1 {
				w.WriteHeader(409)
				_, _ = io.WriteString(w, `{"error":"profile_required"}`)
			} else {
				w.WriteHeader(204)
			}
		}
	}))
	defer server.Close()
	m := newV2Fixture(t, server.URL+"/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	m.delivery.Lock()
	defer m.delivery.Unlock()
	c := v2Config()
	p := m.profileV2(c)
	batch, _ := m.opts.CoreFactory().Gather(context.Background(), allSources())
	sample, e := m.sampleV2(c, batch)
	if e != nil {
		t.Fatal(e)
	}
	q := queued{revision: 1, at: time.Now(), profile: &p, v2: &v2.SampleBatch{ID: strings.Repeat("a", 32), Samples: []v2.Sample{sample}}}
	m.mu.Lock()
	m.snapshot.Revision = 1
	m.queue = []queued{q}
	m.mu.Unlock()
	m.deliverV2(q)
	m.mu.Lock()
	next := m.queue[0]
	m.mu.Unlock()
	if next.v2.ID == q.v2.ID || next.v2.Revision != 0 {
		t.Fatal("new profile requires a fresh batch after explicit rejection")
	}
	m.deliverV2(next)
	if puts.Load() != 2 || m.Snapshot().QueueDepth != 0 || m.Snapshot().LastUploadAt == nil {
		t.Fatalf("recovery failed: %+v", m.Snapshot())
	}
}

func TestV2OptionalFailureDoesNotSuppressMandatorySample(t *testing.T) {
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	c := v2Config(v2.UploadSelection{Metric: "cpu.temperature"}, v2.UploadSelection{Metric: "memory.pressure"})
	batch, _ := m.opts.CoreFactory().Gather(context.Background(), allSources())
	for n := range batch.Metrics {
		switch batch.Metrics[n].Name {
		case "darwin_cpu":
			batch.Metrics[n].Fields["temperature_celsius"] = math.NaN()
		case "darwin_memory":
			batch.Metrics[n].Fields["pressure_level"] = 3
		}
	}
	sample, err := m.sampleV2(c, batch)
	if err != nil || sample.CPUUsage == nil || sample.CPU != nil || sample.Memory != nil {
		t.Fatalf("optional failure blocked core data: %+v %v", sample, err)
	}
}
