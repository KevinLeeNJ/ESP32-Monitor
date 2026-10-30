package collector

import (
	"context"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	v2 "esp32-monitor/client/internal/collector/uploadv2"
	"esp32-monitor/client/internal/logging"
	"esp32-monitor/client/internal/platform"
)

type queued struct {
	revision uint64
	at       time.Time
	attempt  int
	next     time.Time
	v2       *v2.SampleBatch
	profile  *v2.Profile
}

func (m *Manager) initUploader() error {
	if m.opts.UploadURL != "" {
		u, err := platform.ParseHTTPURL(m.opts.UploadURL)
		if err != nil || u.Path != "/v2/collector/samples" {
			return errors.New("invalid_upload_url")
		}
		if m.opts.UploadToken == nil {
			return errors.New("upload_token_required")
		}
	}
	if m.opts.HTTPClient == nil {
		m.opts.HTTPClient = platform.NewHTTPClient(8 * time.Second)
	}
	client := *m.opts.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	m.opts.HTTPClient = &client
	return nil
}
func queueMetrics(q []queued) int {
	n := 0
	for _, b := range q {
		if b.v2 != nil {
			n += len(b.v2.Samples)
		}
	}
	return n
}
func (m *Manager) trimQueueLocked(now time.Time) {
	for len(m.queue) > 0 {
		head := m.queue[0]
		// With samples disabled this is the only chance to publish startup metadata.
		// Keep the current profile retryable through a long first-connection outage.
		onlyMetadata := len(m.queue) == 1 && head.v2 != nil && len(head.v2.Samples) == 0
		expired := now.Sub(head.at) > 5*time.Minute && !onlyMetadata
		if !expired && queueMetrics(m.queue) <= 1000 && queueBytes(m.queue) <= maxBody {
			break
		}
		dropped := queueMetrics(m.queue[:1])
		m.snapshot.DroppedMetrics += uint64(dropped)
		m.queue = m.queue[1:]
		if dropped > 0 && now.Sub(m.dropLogAt) >= time.Minute {
			m.dropLogAt = now
			logging.L("collector").Warn("queue_overflow", "dropped_metrics", dropped, "dropped_total", m.snapshot.DroppedMetrics)
		}
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
	// A 401/403 only delays delivery: the token file is re-read on every
	// request, so a re-registration recovers on the next attempt.
	if len(m.queue) == 0 || time.Now().Before(m.queue[0].next) {
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
	m.deliverV2(q)
}
func (m *Manager) finishDelivery(q queued, status int, headers http.Header, profileSync ...bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A bounded queue can drop the in-flight head when newer batches arrive.
	if len(m.queue) == 0 || m.queue[0].revision != q.revision || !m.queue[0].at.Equal(q.at) {
		return
	}
	previousError := m.snapshot.UploadError
	switch {
	case status >= 200 && status < 300:
		now := time.Now()
		if q.v2 != nil && len(q.v2.Samples) > 0 {
			m.snapshot.LastUploadAt = &now
		}
		m.snapshot.UploadError = ""
		m.queue = m.queue[1:]
	case status == 401 || status == 403:
		// Retryable: the credential file is re-read per request, so a
		// re-registered token is picked up without an agent restart. The cap
		// keeps re-registration recovery under ~16s; healthy delivery runs at
		// the sample interval anyway.
		m.queue[0].attempt++
		attempt := min(m.queue[0].attempt, 4)
		m.queue[0].next = time.Now().Add(time.Duration(1<<attempt)*time.Second + time.Duration(rand.IntN(500))*time.Millisecond)
		m.snapshot.UploadError = "upload_auth_required"
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
			if len(profileSync) > 0 && profileSync[0] {
				m.snapshot.UploadError = "upload_invalid_profile"
			}
		}
		m.snapshot.DroppedMetrics += uint64(queueMetrics([]queued{q}))
		m.queue = m.queue[1:]
	}
	if previousError != m.snapshot.UploadError {
		if m.snapshot.UploadError == "" {
			logging.L("collector").Info("upload_recovered")
		} else {
			logging.L("collector").Warn("upload_state", "error", m.snapshot.UploadError, "status", status)
		}
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
