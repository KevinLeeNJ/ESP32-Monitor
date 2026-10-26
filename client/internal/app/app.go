// Package app is the composition root shared by the user-session Agent and future GUI bindings.
package app

import (
	githubauth "esp32-monitor/client/internal/auth/github"
	"esp32-monitor/client/internal/companion"
	"esp32-monitor/client/internal/config"
	"esp32-monitor/client/internal/nowplaying"
	"esp32-monitor/client/internal/nowplaying/adapters"
	"esp32-monitor/client/internal/platform"
	"esp32-monitor/client/internal/providers/codex"
	"esp32-monitor/client/internal/providers/github"
	"path/filepath"
)

type Application struct {
	Companion  *companion.Service
	GitHubAuth *githubauth.Client
}

func New(c config.Config, cacheDir string) (*Application, error) {
	cachePath := ""
	if cacheDir != "" && c.GitHub.Enabled {
		cachePath = filepath.Join(cacheDir, "github-"+c.GitHub.Login+".json")
	}
	auth := githubauth.New(githubauth.Options{ClientID: c.GitHub.ClientID, Store: platform.GitHubCredentials{}, OpenBrowser: platform.OpenBrowser})
	githubProvider, err := github.New(github.Options{Enabled: c.GitHub.Enabled, Login: c.GitHub.Login, Token: auth.Token, CachePath: cachePath})
	if err != nil {
		return nil, err
	}
	music, err := adapters.New(adapters.Options{Enabled: c.Music.Enabled, Adapter: c.Music.Adapter, NativeLibrary: c.Music.NativeLibrary})
	if err != nil {
		return nil, err
	}
	return &Application{GitHubAuth: auth, Companion: &companion.Service{Music: music, GitHub: githubProvider, Codex: codex.New(codex.Options{Enabled: c.Codex.Enabled, Executable: c.Codex.Executable, DesktopEnabled: c.Codex.DesktopEnabled, TaskAdapter: c.Codex.TaskAdapter, Home: c.Codex.Home, ThreadIDs: c.Codex.ThreadIDs})}}, nil
}

// GetNowPlayingSnapshot is a nonblocking, detached DTO for future GUI bindings.
// The user-session Agent owns Companion.Music.Run(ctx); this getter never starts
// a process, reads native APIs, changes settings, or uploads media data.
func (a *Application) GetNowPlayingSnapshot() nowplaying.Snapshot {
	return a.Companion.Music.Snapshot()
}
