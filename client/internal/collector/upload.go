package collector

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"esp32-monitor/client/internal/platform/proxy"
)

type queued struct {
	batch    Batch
	revision uint64
	at       time.Time
	attempt  int
	next     time.Time
}

func (m *Manager) initUploader() error {
	if m.opts.UploadURL != "" {
		u, err := url.Parse(m.opts.UploadURL)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" || u.Path != "/v1/ingest/telegraf" {
			return errors.New("invalid_upload_url")
		}
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "https" && (u.Scheme != "http" || ip == nil || !ip.IsLoopback()) {
			return errors.New("upload_requires_https_or_loopback")
		}
		if m.opts.UploadToken == nil {
			return errors.New("upload_token_required")
		}
	}
	if m.opts.HTTPClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = proxy.Default().Proxy
		m.opts.HTTPClient = &http.Client{Transport: transport, Timeout: 8 * time.Second}
	}
	client := *m.opts.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	m.opts.HTTPClient = &client
	return nil
}
func queueMetrics(q []queued) int {
	n := 0
	for _, b := range q {
		n += len(b.batch.Metrics)
	}
	return n
}
func (m *Manager) trimQueueLocked(now time.Time) {
	for len(m.queue) > 0 && (now.Sub(m.queue[0].at) > 5*time.Minute || queueMetrics(m.queue) > 1000) {
		m.snapshot.DroppedMetrics += uint64(len(m.queue[0].batch.Metrics))
		m.queue = m.queue[1:]
	}
}
func (m *Manager) enqueueLocked(b Batch, rev uint64, at time.Time) {
	// Collection never waits for transport; this bounded Agent queue retries.
	m.queue = append(m.queue, queued{batch: detached(b), revision: rev, at: at})
	m.trimQueueLocked(at)
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) uploadLoop() {
	defer close(m.workerDone)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
		}
		m.deliver()
	}
}
func (m *Manager) deliver() {
	m.delivery.Lock()
	defer m.delivery.Unlock()
	m.mu.Lock()
	m.trimQueueLocked(time.Now())
	if len(m.queue) == 0 || time.Now().Before(m.queue[0].next) || m.snapshot.UploadError == "upload_auth_required" {
		m.mu.Unlock()
		return
	}
	q := m.queue[0]
	if q.revision != m.snapshot.Revision {
		m.queue = m.queue[1:]
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	body, _ := json.Marshal(q.batch)
	if len(body) > maxBody || len(q.batch.Metrics) > 100 {
		m.finishDelivery(q, 413, nil)
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
	defer cancel()
	token, err := m.opts.UploadToken(ctx)
	if err != nil || token == "" {
		m.finishDelivery(q, 401, nil)
		return
	}
	req, err := http.NewRequestWithContext(ctx, "POST", m.opts.UploadURL, bytes.NewReader(body))
	if err != nil {
		m.finishDelivery(q, 400, nil)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.opts.HTTPClient.Do(req)
	if err != nil {
		m.finishDelivery(q, 0, nil)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	m.finishDelivery(q, resp.StatusCode, resp.Header)
}
func (m *Manager) finishDelivery(q queued, status int, headers http.Header) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A bounded queue can drop the in-flight head when newer batches arrive.
	if len(m.queue) == 0 || m.queue[0].revision != q.revision || !m.queue[0].at.Equal(q.at) {
		return
	}
	switch {
	case status >= 200 && status < 300:
		now := time.Now()
		m.snapshot.LastUploadAt = &now
		m.snapshot.UploadError = ""
		m.queue = m.queue[1:]
	case status == 401 || status == 403:
		m.snapshot.UploadError = "upload_auth_required"
	case status == 413 && len(q.batch.Metrics) > 1:
		half := len(q.batch.Metrics) / 2
		a, b := q, q
		a.batch = Batch{q.batch.Metrics[:half]}
		b.batch = Batch{q.batch.Metrics[half:]}
		b.at = b.at.Add(time.Nanosecond)
		m.queue = append([]queued{a, b}, m.queue[1:]...)
		m.snapshot.UploadError = "upload_batch_split"
	case status == 0 || status == 429 || status >= 500:
		m.queue[0].attempt++
		attempt := min(m.queue[0].attempt, 6)
		delay := time.Duration(1<<attempt)*time.Second + time.Duration(rand.IntN(500))*time.Millisecond
		if status == 429 {
			if seconds, err := strconv.Atoi(headers.Get("Retry-After")); err == nil && seconds >= 0 {
				delay = max(delay, time.Duration(min(seconds, 86400))*time.Second)
			} else if t, err := http.ParseTime(headers.Get("Retry-After")); err == nil {
				delay = max(delay, min(time.Until(t), 24*time.Hour))
			}
		}
		m.queue[0].next = time.Now().Add(delay)
		m.snapshot.UploadError = "upload_retrying"
	default:
		m.snapshot.UploadError = "upload_rejected_" + strconv.Itoa(status)
		if status == 422 {
			m.snapshot.UploadError = "upload_invalid_sample_or_clock"
		}
		m.snapshot.DroppedMetrics += uint64(len(q.batch.Metrics))
		m.queue = m.queue[1:]
	}
}

// TokenFile resolves credentials only inside the Agent. File permissions and
// symlink checks use the same contract as persisted collector configuration.
func TokenFile(path string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		b, err := readPrivateFile(path, 4096)
		if err != nil {
			return "", errors.New("upload_credential_unavailable")
		}
		token := strings.TrimSpace(string(b))
		if _, err := hex.DecodeString(token); len(token) != 64 || err != nil {
			return "", errors.New("upload_credential_invalid")
		}
		return token, nil
	}
}
