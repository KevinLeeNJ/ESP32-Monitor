package guibridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"esp32-monitor/client/internal/platform"
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
	base, err := platform.AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "server-token"), nil
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
	if _, err := platform.ParseHTTPURL(base); err != nil {
		return "", errors.New("invalid_server_url")
	}
	return base, nil
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("token_store_failed")
	}
	if err := platform.WriteFileAtomic(dir, ".server-token-*", filepath.Base(path), []byte(token+"\n"), 0o600); err != nil {
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

func persistRegistration(current Settings, base string, registered registrationResponse, defaultPath string, save func(Settings) error) (Settings, error) {
	dir := filepath.Dir(defaultPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return current, errors.New("token_store_failed")
	}
	f, err := os.CreateTemp(dir, "server-token-*")
	if err != nil {
		return current, errors.New("token_store_failed")
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(path)
		return current, errors.New("token_store_failed")
	}
	if err := writePrivateToken(path, registered.Token); err != nil {
		os.Remove(path)
		return current, err
	}
	next := applyRegistration(current, base, registered, path)
	if err := save(next); err != nil {
		os.Remove(path)
		return current, errors.New("settings_save_failed")
	}
	old := current.Collector.UploadTokenFile
	if filepath.Dir(old) == dir && (old == defaultPath || strings.HasPrefix(filepath.Base(old), "server-token-")) {
		_ = os.Remove(old)
	}
	return next, nil
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
	next, err := persistRegistration(s.sup.CurrentSettings(), base, registered, tokenPath, SaveSettings)
	if err != nil {
		return ServerRegistration{}, err
	}
	if err := s.sup.UpdateSettings(ctx, next); err != nil {
		return ServerRegistration{DeviceID: registered.DeviceID, DeviceName: registered.Name, ServerURL: base}, errors.New("agent_restart_failed")
	}
	return ServerRegistration{DeviceID: registered.DeviceID, DeviceName: registered.Name, ServerURL: base}, nil
}
