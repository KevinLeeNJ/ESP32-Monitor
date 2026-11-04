package guibridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"esp32-monitor/client/internal/platform"
	"esp32-monitor/client/internal/registration"
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

type registrationResponse = registration.Response

func normalizeServerBase(raw string) (string, error) { return registration.NormalizeServerBase(raw) }
func exchangeRegistrationCode(ctx context.Context, base, code string) (registrationResponse, error) {
	return registration.ExchangeRegistrationCode(ctx, base, code)
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

func stageRegistration(current Settings, base string, registered registrationResponse, defaultPath string) (Settings, string, error) {
	dir := filepath.Dir(defaultPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return current, "", errors.New("token_store_failed")
	}
	f, err := os.CreateTemp(dir, "server-token-*")
	if err != nil {
		return current, "", errors.New("token_store_failed")
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(path)
		return current, "", errors.New("token_store_failed")
	}
	if err := writePrivateToken(path, registered.Token); err != nil {
		os.Remove(path)
		return current, "", err
	}
	return applyRegistration(current, base, registered, path), path, nil
}

func removeReplacedToken(current Settings, defaultPath string) {
	old := current.Collector.UploadTokenFile
	dir := filepath.Dir(defaultPath)
	if filepath.Dir(old) == dir && (old == defaultPath || strings.HasPrefix(filepath.Base(old), "server-token-")) {
		_ = os.Remove(old)
	}
}

// RegisterServer exchanges the WebUI verification code for a Server token,
// applies the new endpoint to the running Agent and stores the token privately.
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
	current := s.sup.CurrentSettings()
	next, newToken, err := stageRegistration(current, base, registered, tokenPath)
	if err != nil {
		return ServerRegistration{}, err
	}
	result := ServerRegistration{DeviceID: registered.DeviceID, DeviceName: registered.Name, ServerURL: base}
	if err := s.sup.UpdateSettings(ctx, next); err != nil {
		_ = os.Remove(newToken)
		_ = s.sup.UpdateSettings(ctx, current)
		return result, errors.New("settings_apply_failed")
	}
	if err := SaveSettings(next); err != nil {
		_ = os.Remove(newToken)
		_ = s.sup.UpdateSettings(ctx, current)
		return result, errors.New("settings_save_failed")
	}
	removeReplacedToken(current, tokenPath)
	if s.onSettings != nil {
		s.onSettings(next)
	}
	return result, nil
}
