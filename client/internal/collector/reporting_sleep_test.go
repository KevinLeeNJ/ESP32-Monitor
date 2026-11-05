package collector

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type reportTransport func(*http.Request) (*http.Response, error)

func (f reportTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSleepPausesUploadUntilWake(t *testing.T) {
	paused := true
	requests := 0
	m := &Manager{
		reportingPaused: func() bool { return paused },
		opts: Options{HTTPClient: &http.Client{Transport: reportTransport(func(*http.Request) (*http.Response, error) {
			requests++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})}},
		queue: []queued{{at: time.Now()}},
	}
	target := &uploadTarget{token: func(context.Context) (string, error) { return "token", nil }}
	m.deliver()
	if len(m.queue) != 1 || requests != 0 {
		t.Fatal("sleep drained the queue or sent a request")
	}
	if _, _, _, err := m.requestV2(context.Background(), target, http.MethodGet, "http://example.invalid/profile", nil, ""); !errors.Is(err, errReportingPaused) {
		t.Fatalf("request during sleep: %v", err)
	}
	paused = false
	if status, _, _, err := m.requestV2(context.Background(), target, http.MethodGet, "http://example.invalid/profile", nil, ""); err != nil || status != http.StatusOK || requests != 1 {
		t.Fatalf("request after wake: status=%d requests=%d err=%v", status, requests, err)
	}
}
