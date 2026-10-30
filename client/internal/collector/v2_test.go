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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func requireNative(t *testing.T, roles ...string) {
	t.Helper()
	for _, role := range roles {
		if nativeSourceID(role) == "" {
			t.Skipf("native source %q unavailable on %s", role, runtime.GOOS)
		}
	}
}

type v2Fixture struct {
	fixedReads *atomic.Int32
	missing    *atomic.Bool
}

func fixtureFixed() Batch {
	metrics := []Metric{
		{Name: "cpu", Tags: map[string]string{"model": "Fixture CPU"}, Fields: map[string]float64{"logical_cores": 2, "physical_cores": 2}},
		{Name: "mem", Fields: map[string]float64{"total": 8192}},
		{Name: "system", Tags: map[string]string{"os_platform": "fixture", "os_version": "1"}},
		{Name: "net", Tags: map[string]string{"interface": "en0"}},
		{Name: "diskio", Tags: map[string]string{"name": "disk0"}},
		{Name: "disk", Tags: map[string]string{"device": "disk0s1", "path": "/", "fstype": "apfs"}, Fields: map[string]float64{"total": 32768}},
	}
	add := func(role string, tags map[string]string, fields map[string]float64) {
		if name := nativeSourceID(role); name != "" {
			metrics = append(metrics, Metric{Name: name, Tags: tags, Fields: fields})
		}
	}
	add("network", map[string]string{"interface": "en0", "type": "wifi"}, nil)
	add("disk", map[string]string{"device": "disk0", "model": "Fixture SSD"}, map[string]float64{"total": 65536})
	add("gpu", map[string]string{"device": "fixture-gpu"}, nil)
	add("battery", map[string]string{}, nil)
	return Batch{Metrics: metrics}
}

func TestInventoryExcludesVirtualNetworkCounters(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("physical interface filtering is platform-specific")
	}
	b := fixtureFixed()
	b.Metrics = append(b.Metrics, Metric{Name: "net", Tags: map[string]string{"interface": "utun0"}})
	s := &v2State{IDs: map[string]uint16{}, Next: 1}
	if err := s.buildInventory(b); err != nil {
		t.Fatal(err)
	}
	if len(s.inventory.Interfaces) != 1 || s.IDs["interface:utun0"] != 0 {
		t.Fatalf("virtual interface included: %+v", s.inventory.Interfaces)
	}
	// IDs from an older version remain reserved, but must not reappear in
	// capabilities or preview samples after their inventory entry is removed.
	s.IDs["interface:utun0"] = 500
	m := &Manager{v2: s}
	b.Metrics = append(b.Metrics, Metric{Name: "net", Tags: map[string]string{"interface": "utun0"}, Fields: map[string]float64{"upload_bytes_per_second": 1}})
	for _, metric := range m.v2Capabilities(b).Metrics {
		if metric.Metric == "interface.upload_rate" && slices.Contains(metric.Objects, 500) {
			t.Fatalf("stale virtual ID in capabilities: %+v", metric)
		}
	}
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
			add(id, map[string]string{"cpu": "cpu1"}, map[string]float64{"usage_active": 33})
		case "mem":
			if !f.missing.Load() {
				add(id, map[string]string{}, map[string]float64{"used_percent": 62, "used": 0})
			}
		case "system":
			add(id, map[string]string{}, map[string]float64{"uptime": 86400, "load1": 1.25})
		case "swap":
			add(id, map[string]string{}, map[string]float64{"total": 0, "used": 0})
		case "net":
			add(id, map[string]string{"interface": "en0"}, map[string]float64{"download_bytes_per_second": 0, "upload_bytes_per_second": 1024})
		case "diskio":
			add(id, map[string]string{"name": "disk0"}, map[string]float64{"read_bytes_per_second": 0, "write_bytes_per_second": 0})
		case "disk":
			add(id, map[string]string{"device": "disk0s1", "path": "/", "fstype": "apfs"}, map[string]float64{"used": 0})
		case nativeSourceID("cpu"):
			add(id, map[string]string{}, map[string]float64{"temperature_celsius": 42.3, "load1": 1.25, "load5": 1.1, "load15": 0.9})
		case nativeSourceID("memory"):
			add(id, map[string]string{}, map[string]float64{"app_bytes": 0, "wired_bytes": 0, "compressed_bytes": 0, "pressure_level": 1})
		case nativeSourceID("network"):
			add(id, map[string]string{"interface": "en0", "type": "wifi", "ipv4": "192.168.1.2"}, map[string]float64{"connected": 1, "present": 1})
		case nativeSourceID("gpu"):
			add(id, map[string]string{"device": "fixture-gpu"}, map[string]float64{"usage_percent": 0, "temperature_celsius": 0})
		case nativeSourceID("disk"):
			add(id, map[string]string{"device": "disk0"}, map[string]float64{"smart_healthy": 0})
		case nativeSourceID("disk_usage"):
			add(id, map[string]string{"device": "disk0"}, map[string]float64{"used": 1234})
		case nativeSourceID("public_ip"):
			add(id, map[string]string{"ipv4": "203.0.113.7"}, map[string]float64{"present": 1})
		case nativeSourceID("battery"):
			add(id, map[string]string{}, map[string]float64{"charge_percent": 0, "charging": 0})
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
	m, err := New(Options{StateDirectory: dir, UploadURL: endpoint, UploadToken: func(context.Context) (string, error) { return strings.Repeat("a", 64), nil }, CoreFactory: func() Collector { return &v2Fixture{reads, missing} }, NativeFactory: func() Collector { return &v2Fixture{new(atomic.Int32), missing} }, ApplyTimeout: time.Second})
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
	c.Upload.Optional = append(c.Upload.Optional, v2.UploadSelection{Metric: "cpu.load1"})
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

// A commit with upload enabled must sync the profile ahead of the first
// sample round, and a 401 must clear by itself once the Agent picks up a
// re-registered token — no restart required.
func TestV2CommitSyncsProfileAndRecoversFromAuthError(t *testing.T) {
	var lock sync.Mutex
	var first string
	var puts int
	good := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		if first == "" {
			first = r.Method + " " + r.URL.Path
		}
		accept := good
		lock.Unlock()
		if !accept {
			w.WriteHeader(401)
			return
		}
		lock.Lock()
		defer lock.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /v2/collector/profile":
			w.WriteHeader(404)
		case "PUT /v2/collector/profile":
			if r.Header.Get("If-None-Match") != "*" {
				t.Error("missing creation condition")
			}
			puts++
			w.Header().Set("ETag", `"3"`)
			_, _ = io.WriteString(w, `{"r":3}`)
		case "POST /v2/collector/samples":
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	m := newV2Fixture(t, server.URL+"/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	if _, e := m.Apply(context.Background(), v2Config()); e != nil {
		t.Fatal(e)
	}
	awaitV2(t, func() bool { return m.Snapshot().UploadError == "upload_auth_required" })
	lock.Lock()
	good = true
	lock.Unlock()
	awaitV2(t, func() bool {
		s := m.Snapshot()
		lock.Lock()
		defer lock.Unlock()
		return puts > 0 && s.UploadError == "" && s.LastUploadAt != nil
	})
	lock.Lock()
	defer lock.Unlock()
	if first != "GET /v2/collector/profile" {
		t.Fatalf("profile did not lead delivery: %s", first)
	}
	if puts != 1 {
		t.Fatalf("profile PUT count %d", puts)
	}
}
func TestV2SevenCategoriesAndMissingRequired(t *testing.T) {
	requireNative(t, "cpu", "gpu", "disk", "disk_usage", "network", "battery")
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	c := v2Config()
	optional := []string{"cpu.temperature", "cpu.load1", "memory.used", "memory.swap_total", "memory.swap_used"}
	if nativeSourceID("memory") != "" {
		optional = append(optional, "memory.app", "memory.wired", "memory.compressed", "memory.pressure")
	}
	for _, key := range optional {
		c.Upload.Optional = append(c.Upload.Optional, v2.UploadSelection{Metric: key})
	}
	ids := map[string]uint16{}
	for _, kind := range []string{"core", "gpu", "drive", "interface", "battery"} {
		for id := range m.v2.objectsOfKind(kind) {
			ids[kind] = id
			break
		}
	}
	for key, kind := range map[string]string{"gpu.usage": "gpu", "gpu.temperature": "gpu", "drive.read_rate": "drive", "drive.write_rate": "drive", "drive.smart_health": "drive", "drive.used": "drive", "interface.download_rate": "interface", "interface.ipv4": "interface", "battery.level": "battery", "battery.charging": "battery"} {
		c.Upload.Optional = append(c.Upload.Optional, v2.UploadSelection{Metric: key, Objects: []uint16{ids[kind]}})
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
	fragments := []string{`"c":1250`, `"m":6200`, `"u":"0"`, `"h":false`, `"c":false`}
	if nativeSourceID("cpu") != "" {
		fragments = append(fragments, `"t":423`)
	}
	if nativeSourceID("memory") != "" {
		fragments = append(fragments, `"p":0`)
	}
	for _, fragment := range fragments {
		if !strings.Contains(string(encoded), fragment) {
			t.Errorf("missing %s in %s", fragment, encoded)
		}
	}
	p := m.profileV2(c)
	if p.Inventory.CPU != nil || p.Inventory.OS != nil || p.Inventory.MemoryTotal == nil || p.Inventory.Drives[0].Model != nil {
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
	if !strings.Contains(string(body), `"mem":{"u":"0"}`) {
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
	if !reflect.DeepEqual(m.profileV2(c).Inventory, v2.Inventory{MemoryTotal: ptr(uint64(8192)), Objects: detached(m.v2.inventory.Objects)}) {
		t.Fatal("minimum profile must contain memory total and object directory")
	}
}

// omitempty groups are absent on a fresh profile; the patch grammar only
// allows member-wise /-/ adds and indexed removes, never a whole-group op.
func TestV2ProfileDiffAbsentInventoryGroup(t *testing.T) {
	before := v2.Profile{Version: 2, IntervalSeconds: 5, Enabled: []v2.UploadSelection{}}
	after := detached(before)
	after.Inventory.GPUs = []v2.ObjectInfo{{ID: 7}}
	ops := profileDiff(before, after)
	if len(ops) != 1 || ops[0].Op != "add" || ops[0].Path != "/inventory/gpus/-" || string(ops[0].Value) != `{"id":7}` {
		t.Fatalf("absent group must add members: %+v", ops)
	}
	ops = profileDiff(after, before)
	if len(ops) != 1 || ops[0].Op != "remove" || ops[0].Path != "/inventory/gpus/0" {
		t.Fatalf("emptied group must remove members: %+v", ops)
	}
}

func TestV2ProfileDiffScalarGroupsUseLeaves(t *testing.T) {
	before := v2.Profile{Version: 2, IntervalSeconds: 5, Enabled: []v2.UploadSelection{}}
	after := detached(before)
	after.Inventory.OS = &v2.OSInfo{Family: "darwin", Name: "macOS", Version: "15"}
	after.Inventory.CPU = &v2.CPUInfo{Model: ptr("M4")}
	after.Inventory.Product = ptr("MacBook")
	ops := profileDiff(before, after)
	got := map[string]string{}
	for _, op := range ops {
		got[op.Path] = op.Op
	}
	for path, op := range map[string]string{
		"/inventory/os/family": "add", "/inventory/os/name": "add", "/inventory/os/version": "add",
		"/inventory/cpu/model": "add", "/inventory/product": "replace",
	} {
		if got[path] != op {
			t.Errorf("%s = %q, want %q (%+v)", path, got[path], op, ops)
		}
	}
	if _, ok := got["/inventory/os"]; ok {
		t.Fatal("whole OS group patched")
	}
	ops = profileDiff(after, before)
	for _, op := range ops {
		if op.Path == "/inventory/os" || op.Path == "/inventory/cpu" {
			t.Fatalf("whole scalar group removed: %+v", ops)
		}
	}
}

func TestV2ExactValuesAndAddressRemoval(t *testing.T) {
	requireNative(t, "disk", "network")
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	drive := m.v2.inventory.Drives[0].ID
	iface := m.v2.inventory.Interfaces[0].ID
	c := v2Config(v2.UploadSelection{Metric: "memory.used"}, v2.UploadSelection{Metric: "drive.smart_health", Objects: []uint16{drive}}, v2.UploadSelection{Metric: "interface.ipv4", Objects: []uint16{iface}})
	b, _ := m.opts.CoreFactory().Gather(context.Background(), allSources())
	for n := range b.Metrics {
		v := &b.Metrics[n]
		switch v.Name {
		case "mem":
			v.Exact = map[string]string{"used": "18446744073709551615"}
		case nativeSourceID("network"):
			v.Tags["ipv4"] = ""
		}
	}
	sample, err := m.sampleV2(c, b)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(sample)
	for _, want := range []string{`"u":"18446744073709551615"`, `"ip":[]`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("missing %s: %s", want, body)
		}
	}
	if strings.Contains(string(body), `"v":`) {
		t.Fatalf("SMART details must not be uploaded: %s", body)
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
	requireNative(t, "cpu")
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	optional := []v2.UploadSelection{{Metric: "cpu.temperature"}}
	if nativeSourceID("memory") != "" {
		optional = append(optional, v2.UploadSelection{Metric: "memory.pressure"})
	}
	c := v2Config(optional...)
	batch, _ := m.opts.CoreFactory().Gather(context.Background(), allSources())
	for n := range batch.Metrics {
		switch batch.Metrics[n].Name {
		case nativeSourceID("cpu"):
			batch.Metrics[n].Fields["temperature_celsius"] = math.NaN()
		case nativeSourceID("memory"):
			batch.Metrics[n].Fields["pressure_level"] = 3
		}
	}
	sample, err := m.sampleV2(c, batch)
	if err != nil || sample.CPUUsage == nil || sample.CPU != nil || sample.Memory == nil || sample.Memory.Used == nil || sample.Memory.Pressure != nil {
		t.Fatalf("optional failure blocked core data: %+v %v", sample, err)
	}
}

func TestV2BatteryUploadIsBundled(t *testing.T) {
	requireNative(t, "battery")
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	id := m.v2.inventory.Batteries[0].ID
	for _, key := range []string{"battery.level", "battery.charging"} {
		t.Run(key, func(t *testing.T) {
			c, err := m.prepareV2(v2Config(v2.UploadSelection{Metric: key, Objects: []uint16{id}}))
			if err != nil {
				t.Fatal(err)
			}
			want := []v2.UploadSelection{{Metric: "battery.charging", Objects: []uint16{id}}, {Metric: "battery.level", Objects: []uint16{id}}}
			if !reflect.DeepEqual(c.Upload.Optional, want) {
				t.Fatalf("battery upload split: %+v", c.Upload.Optional)
			}
			batch, _ := m.opts.CoreFactory().Gather(context.Background(), allSources())
			sample, err := m.sampleV2(c, batch)
			if err != nil || len(sample.Batteries) != 1 || sample.Batteries[0].Level == nil || sample.Batteries[0].Charging == nil {
				t.Fatalf("incomplete battery sample: %+v %v", sample, err)
			}
			if *sample.Batteries[0].Level != 0 || *sample.Batteries[0].Charging {
				t.Fatal("real zero/false changed")
			}
			for n := range batch.Metrics {
				if batch.Metrics[n].Name == nativeSourceID("battery") {
					delete(batch.Metrics[n].Fields, "charging")
				}
			}
			sample, err = m.sampleV2(c, batch)
			if err != nil || len(sample.Batteries) != 0 || sample.CPUUsage == nil {
				t.Fatalf("partial battery must be omitted without suppressing mandatory metrics: %+v %v", sample, err)
			}
		})
	}
	upload := &v2.UploadConfig{Optional: []v2.UploadSelection{{Metric: "battery.level", Objects: []uint16{2}}, {Metric: "battery.charging", Objects: []uint16{1}}}}
	bundleBatterySelections(upload)
	if !reflect.DeepEqual(upload.Optional[0].Objects, []uint16{1, 2}) || !reflect.DeepEqual(upload.Optional[0].Objects, upload.Optional[1].Objects) {
		t.Fatalf("paired objects differ: %+v", upload.Optional)
	}
}

func TestV2MetricBundlesExpandOnlyObservedFields(t *testing.T) {
	caps := &V2Capabilities{Metrics: []V2Metric{
		{Metric: "cpu.model", Available: true}, {Metric: "cpu.physical_cores", Available: true}, {Metric: "cpu.logical_cores", Available: true}, {Metric: "cpu.performance_cores", Available: true}, {Metric: "cpu.efficiency_cores", Available: false},
		{Metric: "cpu.load1", Available: true}, {Metric: "cpu.load5", Available: true}, {Metric: "cpu.load15", Available: true},
		{Metric: "memory.app", Available: true}, {Metric: "memory.wired", Available: true}, {Metric: "memory.compressed", Available: true},
		{Metric: "memory.swap_total", Available: true}, {Metric: "memory.swap_used", Available: true},
		{Metric: "drive.model", Available: true, Objects: []uint16{7}}, {Metric: "drive.total", Available: true, Objects: []uint16{7}}, {Metric: "drive.used", Available: true, Objects: []uint16{7}},
		{Metric: "drive.read_rate", Available: true}, {Metric: "drive.write_rate", Available: true},
		{Metric: "interface.medium", Available: true}, {Metric: "interface.up", Available: true}, {Metric: "interface.upload_rate", Available: true}, {Metric: "interface.download_rate", Available: true},
		{Metric: "interface.ipv4", Available: true}, {Metric: "network.public_ipv4", Available: true},
		{Metric: "system.os", Available: true}, {Metric: "system.product", Available: true},
	}}
	upload := &v2.UploadConfig{Enabled: true, Optional: []v2.UploadSelection{
		{Metric: "cpu.model"}, {Metric: "cpu.load5"}, {Metric: "memory.app"}, {Metric: "memory.swap_used"},
		{Metric: "interface.up", Objects: []uint16{4}}, {Metric: "interface.download_rate", Objects: []uint16{4}}, {Metric: "interface.ipv4", Objects: []uint16{4}},
		{Metric: "drive.used", Objects: []uint16{7}}, {Metric: "drive.write_rate", Objects: []uint16{7}},
		{Metric: "system.os"},
	}}
	bundleAvailableSelections(upload, caps)
	got := map[string]v2.UploadSelection{}
	for _, selection := range upload.Optional {
		if _, ok := got[selection.Metric]; ok {
			t.Fatalf("duplicate selection: %s", selection.Metric)
		}
		got[selection.Metric] = selection
	}
	for _, key := range []string{"cpu.model", "cpu.physical_cores", "cpu.logical_cores", "cpu.performance_cores", "cpu.load1", "cpu.load5", "cpu.load15", "memory.app", "memory.wired", "memory.compressed", "memory.swap_total", "memory.swap_used", "drive.model", "drive.total", "drive.used", "drive.read_rate", "drive.write_rate", "interface.medium", "interface.up", "interface.upload_rate", "interface.download_rate", "interface.ipv4", "network.public_ipv4", "system.os", "system.product"} {
		selection, ok := got[key]
		if !ok {
			t.Errorf("missing bundled member %s", key)
			continue
		}
		if strings.HasPrefix(key, "interface.") && !reflect.DeepEqual(selection.Objects, []uint16{4}) {
			t.Errorf("object mismatch for %s: %v", key, selection.Objects)
		}
	}
	if _, ok := got["cpu.efficiency_cores"]; ok {
		t.Fatal("unavailable field was invented")
	}
	for _, key := range []string{"drive.read_rate", "drive.write_rate"} {
		if !reflect.DeepEqual(got[key].Objects, []uint16{7}) {
			t.Errorf("drive object mismatch for %s", key)
		}
	}
	if len(got["network.public_ipv4"].Objects) != 0 {
		t.Fatal("global public IP was assigned an interface")
	}
}

func TestV2ApplyGroupsCPUInfoUsingDiscoveredCapabilities(t *testing.T) {
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	snapshot, err := m.Apply(context.Background(), v2Config(v2.UploadSelection{Metric: "cpu.model"}))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, selection := range snapshot.Config.Upload.Optional {
		names = append(names, selection.Metric)
	}
	if !reflect.DeepEqual(names, []string{"cpu.logical_cores", "cpu.model", "cpu.physical_cores"}) {
		t.Fatalf("group expansion did not use observed capabilities: %v", names)
	}
}

func TestV2PerCoreSelectionAlwaysIncludesEveryObservedCore(t *testing.T) {
	caps := &V2Capabilities{Inventory: v2.Inventory{Cores: []v2.CoreInfo{{ID: 2, Index: 1}, {ID: 1, Index: 0}}}, Metrics: []V2Metric{{Metric: "core.usage", Available: true, Objects: []uint16{1, 2}}}}
	upload := &v2.UploadConfig{Enabled: true, Optional: []v2.UploadSelection{{Metric: "core.usage", Objects: []uint16{1}}}}
	if err := selectAllAvailableCores(upload, caps); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(upload.Optional[0].Objects, []uint16{1, 2}) {
		t.Fatalf("subset selected: %+v", upload.Optional)
	}
	caps.Metrics[0].Objects = []uint16{1}
	if err := selectAllAvailableCores(upload, caps); err == nil {
		t.Fatal("unavailable core was silently omitted")
	}
}

func TestV2PairedRatesOmitPartialReadings(t *testing.T) {
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	drive := m.v2.inventory.Drives[0].ID
	iface := m.v2.inventory.Interfaces[0].ID
	c := v2Config(v2.UploadSelection{Metric: "drive.read_rate", Objects: []uint16{drive}}, v2.UploadSelection{Metric: "drive.write_rate", Objects: []uint16{drive}}, v2.UploadSelection{Metric: "interface.upload_rate", Objects: []uint16{iface}}, v2.UploadSelection{Metric: "interface.download_rate", Objects: []uint16{iface}}, v2.UploadSelection{Metric: "interface.ipv4", Objects: []uint16{iface}})
	batch, _ := m.opts.CoreFactory().Gather(context.Background(), allSources())
	for n := range batch.Metrics {
		switch batch.Metrics[n].Name {
		case "diskio":
			delete(batch.Metrics[n].Fields, "write_bytes_per_second")
		case "net":
			delete(batch.Metrics[n].Fields, "download_bytes_per_second")
		}
	}
	sample, err := m.sampleV2(c, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(sample.Drives) != 0 {
		t.Fatalf("partial disk pair uploaded: %+v", sample.Drives)
	}
	if len(sample.Interfaces) != 1 || sample.Interfaces[0].UploadRate != nil || sample.Interfaces[0].DownloadRate != nil || sample.Interfaces[0].IPv4 == nil {
		t.Fatalf("network pair or unrelated IP incorrect: %+v", sample.Interfaces)
	}
	if sample.CPUUsage == nil || sample.MemoryUsage == nil || sample.Uptime == nil {
		t.Fatal("partial optional rates suppressed required metrics")
	}
}

func TestPreviewFollowsGUIActivity(t *testing.T) {
	m := &Manager{}
	if m.previewActive() {
		t.Fatal("preview active without a GUI consumer")
	}
	m.MarkPreview()
	if !m.previewActive() {
		t.Fatal("preview inactive after a GUI poll")
	}
	m.previewAt.Store(time.Now().Add(-previewTTL).UnixNano())
	if m.previewActive() {
		t.Fatal("preview stayed active after the TTL")
	}
}

func TestV2PreviewWorksWithoutUploadEndpoint(t *testing.T) {
	reads := new(atomic.Int32)
	missing := new(atomic.Bool)
	m := newV2Fixture(t, "", "", reads, missing)
	caps, err := m.Discover(context.Background())
	if err != nil || caps.V2 == nil || len(caps.V2.Metrics) == 0 {
		t.Fatalf("local V2 capabilities unavailable without endpoint: %+v %v", caps.V2, err)
	}
	if m.Snapshot().QueueDepth != 0 {
		t.Fatal("local preview queued an upload without an endpoint")
	}
	m.MarkPreview()
	awaitV2(t, func() bool { return m.Snapshot().V2Sample != nil })
	snapshot := m.Snapshot()
	if snapshot.V2Sample.CPUUsage == nil || snapshot.V2Sample.MemoryUsage == nil {
		t.Fatalf("local preview missing live required metrics: %+v", snapshot.V2Sample)
	}
	if snapshot.Config.Upload != nil && snapshot.Config.Upload.Enabled {
		t.Fatalf("local preview enabled upload: %+v", snapshot.Config.Upload)
	}
	if snapshot.UploadError != "" || snapshot.QueueDepth != 0 {
		t.Fatalf("local preview attempted delivery: %+v", snapshot)
	}
}

func TestV2ApplyPerCoreCannotSelectSubset(t *testing.T) {
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	ids := []uint16{m.v2.inventory.Cores[0].ID, m.v2.inventory.Cores[1].ID}
	snapshot, err := m.Apply(context.Background(), v2Config(v2.UploadSelection{Metric: "core.usage", Objects: ids[:1]}))
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Config.Upload.Optional[0].Objects; !reflect.DeepEqual(got, ids) {
		t.Fatalf("partial configuration retained: %v", got)
	}
	// V2Sample is the GUI preview sample and covers every gathered core,
	// independent of the corrected upload selection asserted above.
	if snapshot.V2Sample == nil || len(snapshot.V2Sample.Cores) < len(m.v2.inventory.Cores) {
		t.Fatalf("sample does not include every inventory core: %+v", snapshot.V2Sample)
	}
}

func TestV2PreviewSampleCoversUnselectedMetrics(t *testing.T) {
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	preview := selectedSources(v2PreviewConfig(v2Config()))
	for _, src := range []string{"cpu", "mem", "swap", "system"} {
		if !slices.Contains(preview, src) {
			t.Fatalf("unselected source not previewed: %s", src)
		}
	}
	for _, role := range []string{"cpu", "memory", "gpu", "disk_usage", "network", "battery"} {
		if source := nativeSourceID(role); source != "" && !slices.Contains(preview, source) {
			t.Fatalf("unselected native source not previewed: %s", source)
		}
	}
	for _, role := range []string{"disk", "public_ip"} {
		if source := nativeSourceID(role); source != "" && !slices.Contains(preview, source) {
			t.Fatalf("unselected source not previewed: %s", source)
		}
	}
	batch, _ := m.opts.CoreFactory().Gather(context.Background(), preview)
	previewSample, err := m.previewSampleV2(batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(previewSample.Drives) == 0 || len(previewSample.Interfaces) == 0 {
		t.Fatalf("unselected devices missing from preview: %+v", previewSample)
	}
	for _, role := range []string{"gpu", "battery"} {
		if nativeSourceID(role) == "" {
			continue
		}
		var count int
		switch role {
		case "gpu":
			count = len(previewSample.GPUs)
		case "battery":
			count = len(previewSample.Batteries)
		}
		if count == 0 {
			t.Fatalf("unselected %s missing from preview: %+v", role, previewSample)
		}
	}
	if previewSample.CPU == nil || previewSample.CPU.Load1 == nil {
		t.Fatalf("unselected cpu metrics missing from preview: %+v", previewSample.CPU)
	}
	if nativeSourceID("cpu") != "" && previewSample.CPU.Temperature == nil {
		t.Fatalf("unselected cpu temperature missing from preview: %+v", previewSample.CPU)
	}
	if nativeSourceID("memory") != "" && (previewSample.Memory == nil || previewSample.Memory.Pressure == nil) {
		t.Fatalf("unselected memory metrics missing from preview: %+v", previewSample.Memory)
	}
	if nativeSourceID("disk") != "" {
		for _, drive := range previewSample.Drives {
			if drive.SMART == nil || drive.SMART.Healthy == nil {
				t.Fatalf("preview SMART health missing: %+v", drive)
			}
		}
	}
	if nativeSourceID("public_ip") != "" && previewSample.PublicIPv4 == nil {
		t.Fatal("unselected public IPv4 missing from preview")
	}
	uploadSample, err := m.sampleV2(v2Config(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(uploadSample.Drives) != 0 || len(uploadSample.Interfaces) != 0 || len(uploadSample.GPUs) != 0 || len(uploadSample.Batteries) != 0 {
		t.Fatalf("unselected metrics uploaded: %+v", uploadSample)
	}
}

func TestDeviceDependencyValidation(t *testing.T) {
	cases := []struct {
		name       string
		selections []v2.UploadSelection
		valid      bool
	}{
		{"network missing parent", []v2.UploadSelection{{Metric: "interface.ipv4", Objects: []uint16{1}}}, false},
		{"network wrong object", []v2.UploadSelection{{Metric: "interface.medium", Objects: []uint16{2}}, {Metric: "interface.upload_rate", Objects: []uint16{1}}}, false},
		{"network selected", []v2.UploadSelection{{Metric: "interface.up", Objects: []uint16{1}}, {Metric: "interface.ipv4", Objects: []uint16{1}}, {Metric: "network.public_ipv4"}}, true},
		{"public alone", []v2.UploadSelection{{Metric: "network.public_ipv4"}}, false},
		{"gpu missing parent", []v2.UploadSelection{{Metric: "gpu.temperature", Objects: []uint16{3}}}, false},
		{"gpu selected", []v2.UploadSelection{{Metric: "gpu.usage", Objects: []uint16{3}}, {Metric: "gpu.temperature", Objects: []uint16{3}}}, true},
		{"disk missing parent", []v2.UploadSelection{{Metric: "drive.smart_health", Objects: []uint16{4}}}, false},
		{"disk selected", []v2.UploadSelection{{Metric: "drive.model", Objects: []uint16{4}}, {Metric: "drive.write_rate", Objects: []uint16{4}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDeviceDependencies(&v2.UploadConfig{Enabled: true, Optional: tc.selections})
			if (err == nil) != tc.valid {
				t.Fatalf("dependency validation: %v", err)
			}
		})
	}
}

func TestV2DefaultMemoryAndPartialDiskBundle(t *testing.T) {
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	c, err := m.prepareV2(v2Config(v2.UploadSelection{Metric: "memory.total"}, v2.UploadSelection{Metric: "memory.used"}))
	if err != nil || len(c.Upload.Optional) != 0 {
		t.Fatalf("old selections not normalized: %+v %v", c.Upload, err)
	}
	profile := m.profileV2(c)
	if profile.Inventory.MemoryTotal == nil {
		t.Fatal("default memory total missing from profile")
	}
	batch, _ := m.opts.CoreFactory().Gather(context.Background(), []string{"cpu", "mem", "system"})
	sample, err := m.sampleV2(c, batch)
	if err != nil || sample.Memory == nil || sample.Memory.Used == nil || *sample.Memory.Used != 0 {
		t.Fatalf("default memory used missing: %+v %v", sample, err)
	}
	for n := range batch.Metrics {
		if batch.Metrics[n].Name == "mem" {
			delete(batch.Metrics[n].Fields, "used")
			delete(batch.Metrics[n].Exact, "used")
		}
	}
	sample, err = m.sampleV2(c, batch)
	if err != nil || (sample.Memory != nil && sample.Memory.Used != nil) {
		t.Fatalf("missing memory used must be omitted: %+v %v", sample, err)
	}
	upload := &v2.UploadConfig{Enabled: true, Optional: []v2.UploadSelection{{Metric: "drive.total", Objects: []uint16{1, 2}}}}
	caps := &V2Capabilities{Metrics: []V2Metric{{Metric: "drive.model", Available: true, Objects: []uint16{1}}, {Metric: "drive.total", Available: true, Objects: []uint16{1, 2}}}}
	bundleAvailableSelections(upload, caps)
	got := map[string][]uint16{}
	for _, item := range upload.Optional {
		got[item.Metric] = item.Objects
	}
	if !reflect.DeepEqual(got["drive.model"], []uint16{1}) || !reflect.DeepEqual(got["drive.total"], []uint16{1, 2}) {
		t.Fatalf("partial disk group: %+v", got)
	}
}

func TestV2DiskUsageSelectedAndLegacyVolumeRemoved(t *testing.T) {
	requireNative(t, "disk_usage")
	m := newV2Fixture(t, "http://127.0.0.1:1/v2/collector/samples", "", &atomic.Int32{}, &atomic.Bool{})
	drive := m.v2.inventory.Drives[0].ID
	c, err := m.prepareV2(v2Config(v2.UploadSelection{Metric: "drive.used", Objects: []uint16{drive}}, v2.UploadSelection{Metric: "volume.used", Objects: []uint16{123}}))
	if err != nil || len(c.Upload.Optional) != 1 || c.Upload.Optional[0].Metric != "drive.used" {
		t.Fatalf("migration: %+v %v", c.Upload, err)
	}
	batch, _ := m.opts.CoreFactory().Gather(context.Background(), allSources())
	sample, err := m.sampleV2(c, batch)
	if err != nil || len(sample.Drives) != 1 || sample.Drives[0].Used == nil || *sample.Drives[0].Used != 1234 || len(sample.Volumes) != 0 {
		t.Fatalf("disk usage missing: %+v %v", sample, err)
	}
	body, _ := json.Marshal(sample)
	if !strings.Contains(string(body), `"u":"1234"`) || strings.Contains(string(body), `"vol"`) {
		t.Fatalf("unexpected encoded disk sample: %s", body)
	}
}

func TestV2ObjectDisplayNames(t *testing.T) {
	s := &v2State{IDs: map[string]uint16{`gpu:PCI\VEN_10DE`: 7}, Next: 8}
	gpu := Metric{Tags: map[string]string{"device": `PCI\VEN_10DE`, "model": "NVIDIA GeForce RTX 5080"}}
	if id, err := s.allocate("gpu", gpu); err != nil || id != 7 {
		t.Fatalf("allocate existing gpu = %d, %v", id, err)
	}
	if got := s.Names[`gpu:PCI\VEN_10DE`]; got != "NVIDIA GeForce RTX 5080" {
		t.Fatalf("gpu display name = %q", got)
	}
	drive := Metric{Tags: map[string]string{"device": "PHYSICALDRIVE0", "model": "Samsung SSD 990 PRO"}}
	if id, err := s.allocate("drive", drive); err != nil || id != 8 {
		t.Fatalf("allocate drive = %d, %v", id, err)
	}
	if got := s.Names["drive:PHYSICALDRIVE0"]; got != "Samsung SSD 990 PRO" {
		t.Fatalf("drive display name = %q", got)
	}
}
