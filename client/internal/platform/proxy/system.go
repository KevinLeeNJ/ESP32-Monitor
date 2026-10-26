package proxy

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func readSystem(ctx context.Context) (Settings, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "darwin":
		data, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output()
		if err != nil {
			return Settings{}, ErrDetection
		}
		return parseMac(string(data))
	case "windows":
		// Read the logged-in user's WinINET configuration; never modify it.
		script := `$ErrorActionPreference='Stop'; $p=Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings'; @{enabled=[bool]$p.ProxyEnable;server=[string]$p.ProxyServer;bypass=[string]$p.ProxyOverride;pac=[string]$p.AutoConfigURL}|ConvertTo-Json -Compress`
		data, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
		if err != nil {
			return Settings{}, ErrDetection
		}
		return parseWindows(data)
	case "linux":
		// Headless Linux uses the environment. GNOME desktops also expose per-user settings.
		if _, err := exec.LookPath("gsettings"); err != nil {
			return Settings{}, nil
		}
		mode, err := exec.CommandContext(ctx, "gsettings", "get", "org.gnome.system.proxy", "mode").Output()
		if err != nil {
			return Settings{}, nil
		} // Schema absent on non-GNOME systems.
		switch strings.Trim(strings.TrimSpace(string(mode)), "'") {
		case "none":
			return Settings{}, nil
		case "auto":
			return Settings{Automatic: true}, nil
		case "manual":
			data, err := exec.CommandContext(ctx, "gsettings", "list-recursively", "org.gnome.system.proxy").Output()
			if err != nil {
				return Settings{}, ErrDetection
			}
			return parseGnome(string(data))
		default:
			return Settings{}, ErrDetection
		}
	default:
		return Settings{}, nil
	}
}
func address(host, port, scheme string) (string, error) {
	n, err := strconv.Atoi(port)
	if host == "" || err != nil || n < 1 || n > 65535 {
		return "", ErrInvalid
	}
	return scheme + "://" + net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(n)), nil
}
func parseMac(text string) (Settings, error) {
	values := map[string]string{}
	// Read only the top-level dictionary, not scoped interface overrides.
	inExceptions := false
	var exceptions []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "__SCOPED__") {
			break
		}
		pair := strings.SplitN(strings.TrimSpace(line), " : ", 2)
		if len(pair) != 2 {
			if strings.TrimSpace(line) == "}" {
				inExceptions = false
			}
			continue
		}
		if pair[0] == "ExceptionsList" {
			inExceptions = true
			continue
		}
		if inExceptions {
			exceptions = append(exceptions, pair[1])
			continue
		}
		values[pair[0]] = pair[1]
	}
	s := Settings{Bypass: exceptions, Automatic: values["ProxyAutoConfigEnable"] == "1" || values["ProxyAutoDiscoveryEnable"] == "1"}
	for _, entry := range []struct {
		key, scheme string
		dst         *string
	}{{"HTTP", "http", &s.HTTP}, {"HTTPS", "http", &s.HTTPS}, {"SOCKS", "socks5h", &s.SOCKS}} {
		if values[entry.key+"Enable"] == "1" {
			value, err := address(values[entry.key+"Proxy"], values[entry.key+"Port"], entry.scheme)
			if err != nil {
				return Settings{}, err
			}
			*entry.dst = value
		}
	}
	if values["ExcludeSimpleHostnames"] == "1" {
		s.Bypass = append(s.Bypass, "<local>")
	}
	return s, nil
}
func parseWindows(data []byte) (Settings, error) {
	var v struct {
		Enabled bool   `json:"enabled"`
		Server  string `json:"server"`
		Bypass  string `json:"bypass"`
		PAC     string `json:"pac"`
	}
	if json.Unmarshal(data, &v) != nil {
		return Settings{}, ErrDetection
	}
	s := Settings{Automatic: v.PAC != "", Bypass: strings.Split(v.Bypass, ";")}
	if !v.Enabled {
		return s, nil
	}
	for _, part := range strings.Split(v.Server, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			if key != "" {
				s.HTTP = key
				s.HTTPS = key
			}
			continue
		}
		switch strings.ToLower(key) {
		case "http":
			s.HTTP = value
		case "https":
			s.HTTPS = value
		case "socks":
			if !strings.Contains(value, "://") {
				value = "socks5h://" + value
			}
			s.SOCKS = value
		}
	}
	if !s.enabled() {
		return Settings{}, ErrInvalid
	}
	return s, nil
}

var quoted = regexp.MustCompile(`'([^']*)'`)

func parseGnome(text string) (Settings, error) {
	s := Settings{}
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(line, " ", 3)
		if len(parts) != 3 {
			continue
		}
		values[parts[0]+"."+parts[1]] = strings.Trim(parts[2], "'")
		if parts[1] == "ignore-hosts" {
			for _, m := range quoted.FindAllStringSubmatch(parts[2], -1) {
				s.Bypass = append(s.Bypass, m[1])
			}
		}
	}
	for _, entry := range []struct {
		key, scheme string
		dst         *string
	}{{"http", "http", &s.HTTP}, {"https", "http", &s.HTTPS}, {"socks", "socks5h", &s.SOCKS}} {
		prefix := "org.gnome.system.proxy." + entry.key
		host := values[prefix+".host"]
		if host != "" {
			value, err := address(host, values[prefix+".port"], entry.scheme)
			if err != nil {
				return Settings{}, err
			}
			*entry.dst = value
		}
	}
	if values["org.gnome.system.proxy.use-same-proxy"] == "true" {
		s.HTTPS = s.HTTP
	}
	if !s.enabled() {
		return Settings{}, ErrInvalid
	}
	return s, nil
}
