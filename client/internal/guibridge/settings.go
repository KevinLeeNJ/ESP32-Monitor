// Package guibridge hosts the Wails GUI process: settings, the Agent
// supervisor and the bound services that proxy calls over the IPC socket.
package guibridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"esp32-monitor/client/internal/platform"
)

// Settings is the persisted, non-sensitive GUI configuration. It decides the
// Agent spawn flags; upload tokens stay in 0600 token files and never here.
type Settings struct {
	SchemaVersion int               `json:"schema_version"`
	Collector     CollectorSettings `json:"collector"`
	Companion     CompanionSettings `json:"companion"`
	GitHub        GitHubSettings    `json:"github"`
	Codex         CodexSettings     `json:"codex"`
	Music         MusicSettings     `json:"music"`
	Launch        LaunchSettings    `json:"launch"`
}

// CollectorSettings controls metric collection and remote upload.
type CollectorSettings struct {
	Enabled         bool   `json:"enabled"`
	UploadURL       string `json:"upload_url,omitempty"`
	UploadTokenFile string `json:"upload_token_file,omitempty"`
	DeviceID        string `json:"device_id,omitempty"`
	DeviceName      string `json:"device_name,omitempty"`
}

// CompanionSettings controls the ESP32 Companion backend.
type CompanionSettings struct {
	Enabled bool   `json:"enabled"`
	BindIP  string `json:"bind_ip,omitempty"`
}

// GitHubSettings selects the contribution provider account.
type GitHubSettings struct {
	Login    string `json:"login,omitempty"`
	ClientID string `json:"client_id,omitempty"`
}

// CodexSettings enables the official App Server quota reader.
type CodexSettings struct {
	Enabled bool `json:"enabled"`
}

// MusicSettings enables the system playback adapter.
type MusicSettings struct {
	Enabled bool `json:"enabled"`
}

// LaunchSettings keeps user decisions about background behaviour.
type LaunchSettings struct {
	// LaunchAtLogin registers the app with the platform login item mechanism.
	LaunchAtLogin bool `json:"launch_at_login"`
	// CloseToBackground keeps the Agent running when the window closes.
	CloseToBackground bool `json:"close_to_background"`
	// ShowInStatusBar keeps a status bar icon with open and quit actions.
	ShowInStatusBar bool `json:"show_in_status_bar"`
}

// DefaultGitHubClientID is this application's public GitHub OAuth App client
// ID for the Device Flow. Client IDs are public and device flow needs no
// secret, so release builds embed it. Override with:
//
//	go build -ldflags "-X esp32-monitor/client/internal/guibridge.DefaultGitHubClientID=<id>"
var DefaultGitHubClientID = "Ov23li0SFaR9q2YJr4dq"

// DefaultSettings is used for first launch: collection and status bar icon on.
func DefaultSettings() Settings {
	return Settings{
		SchemaVersion: 1,
		Collector:     CollectorSettings{Enabled: true},
		GitHub:        GitHubSettings{ClientID: DefaultGitHubClientID},
		Launch:        LaunchSettings{CloseToBackground: true, ShowInStatusBar: true},
	}
}

// SettingsPath returns the private settings file path.
func SettingsPath() (string, error) {
	base, err := platform.AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "gui-settings.json"), nil
}

// DefaultSocketPath returns the well-known Agent IPC endpoint. macOS/Linux
// use a user-config Unix socket; Windows uses a per-user named pipe.
func DefaultSocketPath() (string, error) {
	return defaultIPCPath()
}

// LoadSettings reads the settings file, falling back to defaults.
func LoadSettings() (Settings, error) {
	path, err := SettingsPath()
	if err != nil {
		return DefaultSettings(), err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return DefaultSettings(), err
	}
	settings := DefaultSettings()
	if err := json.Unmarshal(data, &settings); err != nil {
		return DefaultSettings(), errors.New("invalid_gui_settings")
	}
	if settings.SchemaVersion != 1 {
		return DefaultSettings(), errors.New("unsupported_gui_settings_version")
	}
	// Existing settings predate the embedded client ID; adopt it on upgrade.
	if settings.GitHub.ClientID == "" {
		settings.GitHub.ClientID = DefaultGitHubClientID
	}
	return settings, nil
}

// SaveSettings writes the settings file with private permissions.
func SaveSettings(settings Settings) error {
	if settings.SchemaVersion != 1 {
		return errors.New("unsupported_gui_settings_version")
	}
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(filepath.Dir(path), ".gui-settings-*", filepath.Base(path), data, 0o600)
}
