package registration

import (
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/platform"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var serverTokenPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func NormalizeServerBase(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	base = strings.TrimRight(strings.TrimSuffix(base, "/v2/collector/samples"), "/")
	if strings.HasSuffix(base, "/v1/ingest/telegraf") {
		return "", errors.New("v1_server_unsupported")
	}
	if _, err := platform.ParseHTTPURL(base); err != nil {
		return "", errors.New("invalid_server_url")
	}
	return base, nil
}

type Response struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	Token    string `json:"token"`
}

func ExchangeRegistrationCode(ctx context.Context, base, code string) (Response, error) {
	body, _ := json.Marshal(map[string]string{"code": code})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v2/collector/register", strings.NewReader(string(body)))
	if err != nil {
		return Response{}, errors.New("invalid_server_url")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return Response{}, errors.New("server_unreachable")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(data) > 8192 {
		return Response{}, errors.New("invalid_server_response")
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		switch {
		case response.StatusCode == http.StatusNotFound && failure.Error == "invalid_code":
			return Response{}, errors.New("invalid_code")
		case response.StatusCode == http.StatusNotFound:
			return Response{}, errors.New("registration_unsupported")
		case response.StatusCode == http.StatusGone || failure.Error == "code_expired":
			return Response{}, errors.New("code_expired")
		case response.StatusCode == http.StatusTooManyRequests:
			return Response{}, errors.New("rate_limited")
		case response.StatusCode >= 500:
			return Response{}, errors.New("server_unreachable")
		default:
			return Response{}, fmt.Errorf("registration_failed_%d", response.StatusCode)
		}
	}
	var result Response
	if json.Unmarshal(data, &result) != nil || result.DeviceID == "" || result.Name == "" || !serverTokenPattern.MatchString(result.Token) {
		return Response{}, errors.New("invalid_server_response")
	}
	return result, nil
}
