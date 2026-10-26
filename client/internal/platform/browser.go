package platform

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"runtime"
)

// OpenBrowser delegates HTTPS navigation to the OS default browser. It never
// chooses a specific browser or embeds the user's GitHub authentication page.
func OpenBrowser(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return errors.New("invalid_browser_url")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "/usr/bin/open", rawURL)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32.exe", "url.dll,FileProtocolHandler", rawURL)
	case "linux":
		cmd = exec.CommandContext(ctx, "xdg-open", rawURL)
	default:
		return errors.New("browser_unsupported")
	}
	if cmd.Run() != nil {
		return errors.New("browser_open_failed")
	}
	return nil
}
