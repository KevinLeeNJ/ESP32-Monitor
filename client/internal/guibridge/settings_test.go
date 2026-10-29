package guibridge

import "testing"

func TestLoadSettingsUsesEmbeddedGitHubClientID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", t.TempDir())
	previous := DefaultGitHubClientID
	DefaultGitHubClientID = "Iv1.embedded"
	t.Cleanup(func() { DefaultGitHubClientID = previous })

	if err := SaveSettings(Settings{SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.GitHub.ClientID != "Iv1.embedded" {
		t.Fatalf("client id = %q, want embedded default", settings.GitHub.ClientID)
	}

	custom := Settings{SchemaVersion: 1, GitHub: GitHubSettings{ClientID: "Ov23li.custom"}}
	if err := SaveSettings(custom); err != nil {
		t.Fatal(err)
	}
	settings, err = LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.GitHub.ClientID != "Ov23li.custom" {
		t.Fatalf("client id = %q, want persisted override", settings.GitHub.ClientID)
	}
}
