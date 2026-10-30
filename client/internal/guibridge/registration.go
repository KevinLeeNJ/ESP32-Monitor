package guibridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ServerRegistration is what the WebUI binds to a six-digit code. The bearer
// token stays in a private 0600 file and never crosses the WebView boundary.
type ServerRegistration struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	ServerURL  string `json:"server_url"`
}

var (
	registrationCodePattern = regexp.MustCompile(`^[0-9]{6}$`)
	serverTokenPattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	uploadEndpointSuffixes  = []string{"/v2/collector/samples"}
)

// TokenFilePath is the fixed private location the GUI writes the Server-issued
// upload token to; the Agent reads it on every request.
func TokenFilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ESP32-Monitor", "server-token"), nil
}

// ServerBase strips the collector endpoint so the GUI can show and reuse the
// base address the user entered.
func ServerBase(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	for _, suffix := range uploadEndpointSuffixes {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimRight(strings.TrimSuffix(base, suffix), "/")
			break
		}
	}
	return base
}

func normalizeServerBase(raw string) (string, error) {
	base := ServerBase(raw)
	if strings.HasSuffix(base, "/v1/ingest/telegraf") {
		return "", errors.New("v1_server_unsupported")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid_server_url")
	}
	switch parsed.Scheme {
	case "https", "http":
		return base, nil
	}
	return "", errors.New("invalid_server_url")
}

type registrationResponse struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	Token    string `json:"token"`
}

func exchangeRegistrationCode(ctx context.Context, base, code string) (registrationResponse, error) {
	body, _ := json.Marshal(map[string]string{"code": code})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v2/collector/register", strings.NewReader(string(body)))
	if err != nil {
		return registrationResponse{}, errors.New("invalid_server_url")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return registrationResponse{}, errors.New("server_unreachable")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(data) > 8192 {
		return registrationResponse{}, errors.New("invalid_server_response")
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		switch {
		case response.StatusCode == http.StatusNotFound && failure.Error == "invalid_code":
			return registrationResponse{}, errors.New("invalid_code")
		case response.StatusCode == http.StatusNotFound:
			return registrationResponse{}, errors.New("registration_unsupported")
		case response.StatusCode == http.StatusGone || failure.Error == "code_expired":
			return registrationResponse{}, errors.New("code_expired")
		case response.StatusCode == http.StatusTooManyRequests:
			return registrationResponse{}, errors.New("rate_limited")
		case response.StatusCode >= 500:
			return registrationResponse{}, errors.New("server_unreachable")
		default:
			return registrationResponse{}, fmt.Errorf("registration_failed_%d", response.StatusCode)
		}
	}
	var result registrationResponse
	if json.Unmarshal(data, &result) != nil || result.DeviceID == "" || result.Name == "" || !serverTokenPattern.MatchString(result.Token) {
		return registrationResponse{}, errors.New("invalid_server_response")
	}
	return result, nil
}

func writePrivateToken(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.New("token_store_failed")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".server-token-*")
	if err != nil {
		return errors.New("token_store_failed")
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err = file.Chmod(0o600); err == nil {
		_, err = file.WriteString(token + "\n")
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp, path)
	}
	if err != nil {
		return errors.New("token_store_failed")
	}
	return nil
}

func applyRegistration(current Settings, base string, registered registrationResponse, tokenPath string) Settings {
	current.Collector.UploadURL = base + "/v2/collector/samples"
	current.Collector.UploadTokenFile = tokenPath
	current.Collector.DeviceID = registered.DeviceID
	current.Collector.DeviceName = registered.Name
	return current
}

// RegisterServer exchanges the WebUI verification code for a Server token,
// stores the token privately and restarts the Agent with the new endpoint.
func (s *AppService) RegisterServer(ctx context.Context, baseURL, code string) (ServerRegistration, error) {
	base, err := normalizeServerBase(baseURL)
	if err != nil {
		return ServerRegistration{}, err
	}
	code = strings.TrimSpace(code)
	if !registrationCodePattern.MatchString(code) {
		return ServerRegistration{}, errors.New("invalid_code")
	}
	registered, err := exchangeRegistrationCode(ctx, base, code)
	if err != nil {
		return ServerRegistration{}, err
	}
	tokenPath, err := TokenFilePath()
	if err != nil {
		return ServerRegistration{}, errors.New("token_store_failed")
	}
	if err := writePrivateToken(tokenPath, registered.Token); err != nil {
		return ServerRegistration{}, err
	}
	next := applyRegistration(s.sup.CurrentSettings(), base, registered, tokenPath)
	if err := SaveSettings(next); err != nil {
		_ = os.Remove(tokenPath)
		return ServerRegistration{}, errors.New("settings_save_failed")
	}
	if err := s.sup.UpdateSettings(ctx, next); err != nil {
		return ServerRegistration{DeviceID: registered.DeviceID, DeviceName: registered.Name, ServerURL: base}, errors.New("agent_restart_failed")
	}
	return ServerRegistration{DeviceID: registered.DeviceID, DeviceName: registered.Name, ServerURL: base}, nil
}
