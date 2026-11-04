package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"esp32-monitor/client/internal/collector"
	v2 "esp32-monitor/client/internal/collector/uploadv2"
)

type fakeUI struct {
	answers     []string
	metricCalls int
}

func (u *fakeUI) text(_ string) (string, error) {
	if len(u.answers) == 0 {
		return "", fmt.Errorf("missing answer")
	}
	s := u.answers[0]
	u.answers = u.answers[1:]
	return s, nil
}
func (u *fakeUI) metrics(_ *collector.V2Capabilities, previous *v2.UploadConfig) (*v2.UploadConfig, error) {
	u.metricCalls++
	if previous != nil {
		return previous, nil
	}
	return &v2.UploadConfig{Enabled: true, IntervalSeconds: 10}, nil
}

func TestSetupAndReuse(t *testing.T) {
	old := http.DefaultTransport
	samples := make(chan v2.SampleBatch, 1)
	var mu sync.Mutex
	var calls []string
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			req, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				return
			}
			defer req.Body.Close()
			mu.Lock()
			calls = append(calls, req.Method+" "+req.URL.Path)
			mu.Unlock()
			requestBody, _ := io.ReadAll(req.Body)
			body := `{}`
			status := "200 OK"
			if strings.HasSuffix(req.URL.Path, "/profile") && req.Method == "GET" {
				status = "404 Not Found"
			}
			if strings.HasSuffix(req.URL.Path, "/profile") && req.Method == "PUT" {
				body = `{"r":1}`
			}
			if strings.HasSuffix(req.URL.Path, "/samples") && req.Method == "POST" {
				select {
				case samples <- func() v2.SampleBatch { var batch v2.SampleBatch; _ = json.Unmarshal(requestBody, &batch); return batch }():
				default:
				}
			}
			if req.URL.Path == "/v2/collector/register" {
				body = `{"device_id":"d","name":"test","token":"` + strings.Repeat("a", 64) + `"}`
			}
			fmt.Fprintf(server, "HTTP/1.1 %s\r\nContent-Length: %d\r\nETag: \"1\"\r\nConnection: close\r\n\r\n%s", status, len(body), body)
		}()
		return client, nil
	}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = old }()
	dir := filepath.Join(t.TempDir(), "headless")
	var out bytes.Buffer
	ui := &fakeUI{answers: []string{"http://example.test", "123456", ""}}
	if err := runWithUI(context.Background(), dir, true, &out, ui); err != nil {
		t.Fatal(err)
	}
	if ui.metricCalls != 1 {
		t.Fatalf("metric menu shown %d times", ui.metricCalls)
	}
	collectorData, err := os.ReadFile(filepath.Join(dir, "collector", "collector.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Config struct {
			Selections []struct {
				ID string `json:"id"`
			} `json:"selections"`
		} `json:"config"`
	}
	if err = json.Unmarshal(collectorData, &persisted); err != nil || len(persisted.Config.Selections) != 0 {
		t.Fatalf("unexpected local selections: %s, %v", collectorData, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s settings
	if err = json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(s.TokenFile)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("token mode: %v %v", st, err)
	}
	if err = runWithUI(context.Background(), dir, true, &out, &fakeUI{answers: []string{"", ""}}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	var after settings
	if err = json.Unmarshal(data, &after); err != nil || after != s {
		t.Fatalf("settings changed: %+v, %v", after, err)
	}
	select {
	case <-samples:
	default:
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, dir, false, strings.NewReader(""), io.Discard) }()
	select {
	case batch := <-samples:
		if len(batch.Samples) == 0 || batch.Samples[0].CPUUsage == nil || batch.Samples[0].MemoryUsage == nil || batch.Samples[0].Uptime == nil {
			t.Fatalf("missing required uploaded metrics: %+v", batch)
		}
	case <-time.After(20 * time.Second):
		cancel()
		<-done
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("no sample uploaded; requests: %v", calls)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

}
