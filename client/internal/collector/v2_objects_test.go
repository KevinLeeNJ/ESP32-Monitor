package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v2 "esp32-monitor/client/internal/collector/uploadv2"
)

func TestObjectDirectoryPlatformNames(t *testing.T) {
	requireNative(t, "network", "gpu")
	for _, tc := range []struct{ name, nic, gpu string }{
		{"macOS", "en0", "Apple M4 Pro"},
		{"Windows", "以太网 2", "NVIDIA GeForce RTX 5080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := fixtureFixed()
			for i := range b.Metrics {
				m := &b.Metrics[i]
				if m.Tags["interface"] != "" {
					m.Tags["interface"] = tc.nic
				}
				if m.Name == nativeSourceID("gpu") {
					m.Tags["device"] = `PCI\PRIVATE-ID`
					m.Tags["model"] = tc.gpu
				}
				if m.Name == nativeSourceID("network") {
					m.Tags["model"] = "Intel Ethernet Controller"
				}
			}
			s := &v2State{IDs: map[string]uint16{}, Next: 1}
			if err := s.buildInventory(b); err != nil {
				t.Fatal(err)
			}
			var foundGPU, foundNIC, foundDrive, foundCore, foundBattery bool
			for _, o := range s.inventory.Objects {
				switch o.Kind {
				case "gpu":
					foundGPU = true
					if o.Name != tc.gpu || o.Model == nil || *o.Model != tc.gpu {
						t.Fatalf("GPU mapping %+v", o)
					}
				case "interface":
					foundNIC = true
					if o.Name != tc.nic || o.Model == nil || *o.Model != "Intel Ethernet Controller" {
						t.Fatalf("NIC mapping %+v", o)
					}
				case "drive":
					foundDrive = true
					if o.Name != "Fixture SSD" {
						t.Fatalf("drive mapping %+v", o)
					}
				case "core":
					foundCore = true
					if !strings.HasPrefix(o.Name, "逻辑核 ") {
						t.Fatalf("core mapping %+v", o)
					}
				case "battery":
					foundBattery = true
					if o.Name != "系统电池" {
						t.Fatalf("battery mapping %+v", o)
					}
				}
			}
			if !foundGPU || !foundNIC || !foundDrive || !foundCore || !foundBattery {
				t.Fatal("directory missing object class")
			}
			raw, _ := json.Marshal(s.inventory.Objects)
			if strings.Contains(string(raw), "PRIVATE-ID") {
				t.Fatal("raw PNP identity leaked as display name")
			}
			// Name-only metadata is present even before opting in to dynamic metrics.
			manager := &Manager{v2: s}
			p := manager.profileV2(DefaultConfig())
			if !reflect.DeepEqual(p.Inventory.Objects, s.inventory.Objects) || len(p.Enabled) != 0 {
				t.Fatal("startup profile omitted directory")
			}
			if err := validateV2Profile(p); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestObjectDirectoryRetiresIDsAndUsesFreshNames(t *testing.T) {
	requireNative(t, "gpu")
	old := &v2State{IDs: map[string]uint16{}, Next: 1}
	if err := old.buildInventory(fixtureFixed()); err != nil {
		t.Fatal(err)
	}
	id := old.IDs["gpu:fixture-gpu"]
	next := &v2State{IDs: detached(old.IDs), Names: detached(old.Names), Next: old.Next}
	next.IDs["interface:retired"] = uint16(next.Next)
	next.Next++
	b := fixtureFixed()
	for i := range b.Metrics {
		if b.Metrics[i].Name == nativeSourceID("gpu") {
			b.Metrics[i].Tags["model"] = "Updated GPU"
		}
	}
	if err := next.buildInventory(b); err != nil {
		t.Fatal(err)
	}
	for _, o := range next.inventory.Objects {
		if o.Name == "retired" {
			t.Fatal("retired directory entry")
		}
		if o.Kind == "gpu" && (o.ID != id || o.Name != "Updated GPU") {
			t.Fatal("display name changed identity")
		}
	}
	before := v2.Profile{Version: 2, IntervalSeconds: 10, Enabled: []v2.UploadSelection{}, Inventory: v2.Inventory{Objects: old.inventory.Objects}}
	after := before
	after.Inventory.Objects = next.inventory.Objects
	ops := profileDiff(before, after)
	found := false
	for _, op := range ops {
		if strings.HasPrefix(op.Path, "/inventory/objects/") {
			found = true
		} else {
			t.Fatalf("unexpected metadata patch %+v", op)
		}
	}
	if !found {
		t.Fatal("name change not patched")
	}
}

func TestFirstRestoreUploadsDirectoryWithoutSamplesAndScopesCacheToToken(t *testing.T) {
	var mu sync.Mutex
	profiles := map[string]*v2.Profile{}
	puts, posts := 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		token := r.Header.Get("Authorization")
		switch r.Method + " " + r.URL.Path {
		case "GET /v2/collector/profile":
			if profiles[token] == nil {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("ETag", `"1"`)
			_ = json.NewEncoder(w).Encode(profiles[token])
		case "PUT /v2/collector/profile":
			var p v2.Profile
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Error(err)
			}
			profiles[token] = &p
			puts++
			w.Header().Set("ETag", `"1"`)
			_, _ = io.WriteString(w, `{"r":1}`)
		case "POST /v2/collector/samples":
			posts++
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected %s", r.Method+" "+r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	defer upstream.Close()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var token atomic.Value
	token.Store(strings.Repeat("a", 64))
	create := func() *Manager {
		t.Helper()
		m, err := New(Options{
			StateDirectory: dir, UploadURL: upstream.URL + "/v2/collector/samples",
			UploadToken:   func(context.Context) (string, error) { return token.Load().(string), nil },
			CoreFactory:   func() Collector { return &v2Fixture{new(atomic.Int32), new(atomic.Bool)} },
			NativeFactory: func() Collector { return &v2Fixture{new(atomic.Int32), new(atomic.Bool)} },
		})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := create()
	if err := m.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitV2(t, func() bool { mu.Lock(); defer mu.Unlock(); return puts == 1 })
	if m.Snapshot().LastUploadAt != nil {
		t.Fatal("metadata marked as sample upload")
	}
	mu.Lock()
	p := profiles["Bearer "+token.Load().(string)]
	if p == nil || len(p.Inventory.Objects) == 0 || posts != 0 {
		t.Fatal("first connection lacked metadata or sent samples")
	}
	mu.Unlock()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	m = create()
	if err := m.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitV2(t, func() bool { return m.Snapshot().ProfileRevision == 1 })
	mu.Lock()
	if puts != 1 {
		t.Fatal("unchanged startup metadata reuploaded")
	}
	mu.Unlock()
	m.Close()
	// Same endpoint, different registered device credential: old confirmation is invalid.
	token.Store(strings.Repeat("b", 64))
	m = create()
	defer m.Close()
	if err := m.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitV2(t, func() bool { mu.Lock(); defer mu.Unlock(); return puts == 2 })
	mu.Lock()
	defer mu.Unlock()
	if profiles["Bearer "+token.Load().(string)] == nil || posts != 0 {
		t.Fatal("new registration skipped initial metadata")
	}
}

func TestDirectoryUTF8DisplayBounds(t *testing.T) {
	name := catalogText("\x00\n " + strings.Repeat("显卡", 300) + " \t")
	if len(name) > 512 || strings.ContainsAny(name, "\x00\n\t") {
		t.Fatal("unsafe display label")
	}
	var roundtrip string
	raw, _ := json.Marshal(name)
	if err := json.Unmarshal(raw, &roundtrip); err != nil || roundtrip != name {
		t.Fatal("UTF-8 label split")
	}
}

func TestChangingServerPreservesObjectIDsAndDropsConfirmation(t *testing.T) {
	dir := t.TempDir()
	m := newV2Fixture(t, "http://127.0.0.1:18080/v2/collector/samples", dir, new(atomic.Int32), new(atomic.Bool))
	ids := detached(m.v2.IDs)
	m.v2.Profile = &v2.Profile{Version: 2}
	m.v2.Revision = 9
	m.v2.AuthHash = "old"
	if err := m.persistV2(); err != nil {
		t.Fatal(err)
	}
	m.Close()
	changed := newV2Fixture(t, "http://127.0.0.1:18081/v2/collector/samples", dir, new(atomic.Int32), new(atomic.Bool))
	if !reflect.DeepEqual(ids, changed.v2.IDs) || changed.v2.Profile != nil || changed.v2.Revision != 0 || changed.v2.AuthHash != "" {
		t.Fatal("server switch reused remote identity or changed local IDs")
	}
}

func TestLatestMetadataSurvivesOfflineQueueExpiry(t *testing.T) {
	now := time.Now()
	profile := &v2.Profile{Version: 2, IntervalSeconds: 10, Enabled: []v2.UploadSelection{}}
	m := &Manager{queue: []queued{{v2: &v2.SampleBatch{Samples: []v2.Sample{}}, profile: profile, at: now.Add(-time.Hour)}}}
	m.trimQueueLocked(now)
	if len(m.queue) != 1 {
		t.Fatal("initial metadata lost during offline period")
	}
	m.queue = append(m.queue, queued{v2: &v2.SampleBatch{Samples: []v2.Sample{{Timestamp: now.Unix()}}}, profile: profile, at: now})
	m.trimQueueLocked(now)
	if len(m.queue) != 1 || len(m.queue[0].v2.Samples) != 1 {
		t.Fatal("superseded metadata blocked newer sample")
	}
}
func TestLargeNamingCacheCanRestart(t *testing.T) {
	dir := t.TempDir()
	m := newV2Fixture(t, "http://127.0.0.1:18080/v2/collector/samples", dir, new(atomic.Int32), new(atomic.Bool))
	if m.v2.Names == nil {
		m.v2.Names = map[string]string{}
	}
	objects := []v2.ObjectDescriptor{}
	for i := 0; i < 512; i++ {
		kind := "gpu"
		if i >= 256 {
			kind = "drive"
		}
		key := fmt.Sprintf("%s:retired-%d", kind, i)
		id := uint16(m.v2.Next)
		m.v2.Next++
		m.v2.IDs[key] = id
		m.v2.Names[key] = strings.Repeat("x", 256)
		objects = append(objects, v2.ObjectDescriptor{ID: id, Kind: kind, Name: strings.Repeat("x", 256)})
	}
	m.v2.Profile = &v2.Profile{Version: 2, IntervalSeconds: 10, Enabled: []v2.UploadSelection{}, Inventory: v2.Inventory{Objects: objects}}
	if err := validateV2Profile(*m.v2.Profile); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(m.v2)
	if len(raw) <= maxBody {
		t.Fatal("fixture does not exercise combined cache budget")
	}
	if err := m.persistV2(); err != nil {
		t.Fatal(err)
	}
	m.Close()
	restored := newV2Fixture(t, "http://127.0.0.1:18080/v2/collector/samples", dir, new(atomic.Int32), new(atomic.Bool))
	if restored.v2.Profile == nil || len(restored.v2.Profile.Inventory.Objects) != 512 {
		t.Fatal("valid large profile cache could not restart")
	}
	for _, o := range restored.v2.inventory.Objects {
		if strings.Contains(o.Name, "retired-") {
			t.Fatal("retired name published as current hardware")
		}
	}
}
func TestSamplesDoNotRepeatObjectDirectory(t *testing.T) {
	m := newV2Fixture(t, "http://127.0.0.1:18080/v2/collector/samples", "", new(atomic.Int32), new(atomic.Bool))
	b, _ := m.opts.CoreFactory().Gather(context.Background(), []string{"cpu", "mem", "system"})
	sm, err := m.sampleV2(v2Config(), b)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sm)
	for _, field := range []string{`"objects"`, `"name"`, `"model"`} {
		if strings.Contains(string(raw), field) {
			t.Fatal("long-term metadata repeated in dynamic sample")
		}
	}
}

func TestDirectoryClassLimitOnClient(t *testing.T) {
	p := v2.Profile{Version: 2, IntervalSeconds: 10, Enabled: []v2.UploadSelection{}}
	for i := 1; i <= 257; i++ {
		p.Inventory.Objects = append(p.Inventory.Objects, v2.ObjectDescriptor{ID: uint16(i), Kind: "core", Name: fmt.Sprintf("逻辑核 %d", i-1)})
	}
	if err := validateV2Profile(p); err == nil {
		t.Fatal("oversized core directory was queued for Server rejection")
	}
}
