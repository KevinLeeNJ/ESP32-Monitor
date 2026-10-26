// Package config holds non-secret application settings. GUI persistence and
// transactional updates will be added separately; OAuth/PAT values never belong here.
package config

type Config struct {
	GitHub GitHub `json:"github"`
	Codex  Codex  `json:"codex"`
	Music  Music  `json:"music"`
}
type GitHub struct {
	Enabled  bool   `json:"enabled"`
	Login    string `json:"login"`
	ClientID string `json:"client_id,omitempty"`
}
type Codex struct {
	Enabled        bool     `json:"enabled"`
	Executable     string   `json:"executable"`
	TaskAdapter    string   `json:"task_adapter,omitempty"`
	DesktopEnabled bool     `json:"desktop_enabled"`
	Home           string   `json:"home,omitempty"`
	ThreadIDs      []string `json:"thread_ids,omitempty"`
}

type Music struct {
	Enabled       bool   `json:"enabled"`
	Adapter       string `json:"adapter,omitempty"`
	NativeLibrary string `json:"native_library,omitempty"`
}
