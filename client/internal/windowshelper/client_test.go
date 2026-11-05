package windowshelper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A real child process exercises pipe framing, EOF, cancellation and reaping.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("WINDOWS_HELPER_TEST_MODE")
	if mode == "" {
		return
	}
	enc := json.NewEncoder(os.Stdout)
	music := false
	var held string
	sensor := func(id string) {
		_ = enc.Encode(map[string]any{"channel": "sensors", "id": id, "data": map[string]any{"ok": true, "schema": 1, "id": id, "pid": os.Getpid(), "music": music}})
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var m struct{ Channel, ID, Method string }
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			os.Exit(2)
		}
		switch m.Method {
		case "music.subscribe":
			music = true
			if mode == "bad" {
				fmt.Println(`{"channel":"music","current":null}`)
				continue
			}
			for i := 0; i < 100; i++ {
				_ = enc.Encode(map[string]any{"channel": "music", "id": "", "current": nil, "error_code": "music_timeout"})
			}
		case "music.unsubscribe":
			music = false
		case "sensors.read":
			switch mode {
			case "crash":
				os.Exit(3)
			case "hang":
				continue
			case "timeout":
				_ = enc.Encode(map[string]any{"channel": "sensors", "id": m.ID, "error_code": "sensors_timeout"})
				continue
			case "reverse":
				if held == "" {
					held = m.ID
					_ = enc.Encode(map[string]any{"channel": "music", "id": "", "current": nil, "error_code": "test_request_held"})
					continue
				}
				sensor(m.ID)
				sensor(held)
				held = ""
				continue
			}
			sensor(m.ID)
		}
	}
	os.Exit(0)
}

func helperCommand(ctx context.Context, mode string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), "WINDOWS_HELPER_TEST_MODE="+mode, "GORACE=atexit_sleep_ms=0")
	return cmd, nil
}
func testClient(t *testing.T, mode string) (*Client, *atomic.Int32) {
	t.Helper()
	var starts atomic.Int32
	c := New(func(ctx context.Context) (*exec.Cmd, error) { starts.Add(1); return helperCommand(ctx, mode) })
	c.retryMin, c.retryMax = 10*time.Millisecond, 20*time.Millisecond
	t.Cleanup(c.Close)
	return c, &starts
}
func sensorRead(t *testing.T, c *Client) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, err := c.ReadSensors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func musicEvent(t *testing.T, events <-chan json.RawMessage) json.RawMessage {
	t.Helper()
	select {
	case data := <-events:
		return data
	case <-time.After(3 * time.Second):
		t.Fatal("music event timeout")
		return nil
	}
}

func TestSharedProcessMusicIsolationAndUnsubscribe(t *testing.T) {
	c, starts := testClient(t, "normal")
	events, unsubscribe := c.SubscribeMusic()
	line := musicEvent(t, events)
	var msg struct {
		Error string `json:"error_code"`
	}
	_ = json.Unmarshal(line, &msg)
	if msg.Error != "music_timeout" {
		t.Fatalf("unexpected event: %s", line)
	}
	// Do not drain the flooded music subscription. Sensors must still reply.
	first := sensorRead(t, c)
	if first["music"] != true {
		t.Fatal(first)
	}
	unsubscribe()
	second := sensorRead(t, c)
	if second["music"] != false || first["pid"] != second["pid"] || starts.Load() != 1 {
		t.Fatalf("process not shared: %v / %v, starts=%d", first, second, starts.Load())
	}
	_, unsub2 := c.SubscribeMusic()
	defer unsub2()
	third := sensorRead(t, c)
	if third["music"] != true || starts.Load() != 1 {
		t.Fatal(third)
	}
}

func TestConcurrentResponsesCorrelatedByID(t *testing.T) {
	c, starts := testClient(t, "reverse")
	const count = 12
	results := make(chan string, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			data, err := c.ReadSensors(ctx)
			if err != nil {
				results <- "error:" + err.Error()
				return
			}
			var m struct{ ID string }
			_ = json.Unmarshal(data, &m)
			results <- m.ID
		}()
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for id := range results {
		if id == "" || seen[id] || len(id) > 3 {
			t.Fatalf("wrong/duplicate response %q", id)
		}
		seen[id] = true
	}
	if len(seen) != count || starts.Load() != 1 {
		t.Fatalf("responses=%v starts=%d", seen, starts.Load())
	}
}

func TestDispatchCorrelatesOutOfOrderAndIgnoresLateReplies(t *testing.T) {
	c := New(nil)
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &session{cancel: cancel, pending: make(map[string]pending)}
	c.session = s
	first, second := make(chan result, 1), make(chan result, 1)
	s.pending["1"] = pending{reply: first, timer: time.NewTimer(time.Hour)}
	s.pending["2"] = pending{reply: second, timer: time.NewTimer(time.Hour)}
	if !c.dispatch(s, []byte(`{"channel":"sensors","id":"2","data":{"value":22}}`)) ||
		!c.dispatch(s, []byte(`{"channel":"sensors","id":"1","data":{"value":11}}`)) {
		t.Fatal("valid reply rejected")
	}
	if got := <-first; string(got.data) != `{"value":11}` {
		t.Fatal(string(got.data))
	}
	if got := <-second; string(got.data) != `{"value":22}` {
		t.Fatal(string(got.data))
	}
	if !c.dispatch(s, []byte(`{"channel":"sensors","id":"1","data":{"value":99}}`)) {
		t.Fatal("late reply rejected")
	}
	if len(first) != 0 || len(s.pending) != 0 || ctx.Err() != nil {
		t.Fatal("late response changed request state")
	}
}

func TestCancelledCallerDoesNotRestartHealthyHelper(t *testing.T) {
	c, starts := testClient(t, "reverse")
	events, unsub := c.SubscribeMusic()
	defer unsub()
	musicEvent(t, events)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.ReadSensors(ctx); done <- err }()
	for {
		var msg struct {
			Error string `json:"error_code"`
		}
		_ = json.Unmarshal(musicEvent(t, events), &msg)
		if msg.Error == "test_request_held" {
			break
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	data := sensorRead(t, c) // releases both replies, including the abandoned one
	if data["ok"] != true || starts.Load() != 1 {
		t.Fatalf("%v starts=%d", data, starts.Load())
	}
}

func TestPendingCallerCancellationKeepsOperationDeadline(t *testing.T) {
	c, _ := testClient(t, "hang")
	c.requestTimeout = 300 * time.Millisecond
	events, unsub := c.SubscribeMusic()
	defer unsub()
	musicEvent(t, events)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := c.ReadSensors(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	c.mu.Lock()
	s := c.session
	n := len(s.pending)
	c.mu.Unlock()
	if n != 1 {
		t.Fatalf("pending=%d", n)
	}
	// Even after the caller leaves, the operation watchdog reaps the hung child.
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		c.mu.Lock()
		changed := c.changed
		replaced := c.session != s
		c.mu.Unlock()
		if replaced {
			break
		}
		select {
		case <-changed:
		case <-deadline.C:
			t.Fatal("hung child not replaced")
		}
	}
}

func TestCrashAndSensorTimeoutRestartWithMusicReplay(t *testing.T) {
	for _, mode := range []string{"crash", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			var starts atomic.Int32
			c := New(func(ctx context.Context) (*exec.Cmd, error) {
				if starts.Add(1) == 1 {
					return helperCommand(ctx, mode)
				}
				return helperCommand(ctx, "normal")
			})
			c.retryMin, c.retryMax = 10*time.Millisecond, 20*time.Millisecond
			defer c.Close()
			events, unsub := c.SubscribeMusic()
			defer unsub()
			musicEvent(t, events)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := c.ReadSensors(ctx); err == nil {
				t.Fatal("expected child failure")
			}
			for {
				c.mu.Lock()
				changed := c.changed
				ready := c.session != nil && starts.Load() >= 2
				c.mu.Unlock()
				if ready {
					break
				}
				select {
				case <-changed:
				case <-ctx.Done():
					t.Fatal("no restart")
				}
			}
			data := sensorRead(t, c)
			if data["music"] != true {
				t.Fatalf("subscription not replayed: %v", data)
			}
			if starts.Load() != 2 {
				t.Fatalf("starts=%d", starts.Load())
			}
		})
	}
}

func TestMissingAndCloseAreBounded(t *testing.T) {
	c := New(func(context.Context) (*exec.Cmd, error) { return nil, ErrMissing })
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.ReadSensors(ctx); !errors.Is(err, ErrMissing) {
		t.Fatal(err)
	}
	c.Close()
	if _, err := c.ReadSensors(ctx); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestMalformedEnvelopeRejectsMissingID(t *testing.T) {
	c, _ := testClient(t, "bad")
	events, unsub := c.SubscribeMusic()
	defer unsub()
	var msg struct {
		Error string `json:"error_code"`
	}
	_ = json.Unmarshal(musicEvent(t, events), &msg)
	if msg.Error != "music_protocol_error" {
		t.Fatal(msg)
	}
}
