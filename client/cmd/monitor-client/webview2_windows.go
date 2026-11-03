//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const webView2ClientGUID = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
const webView2BootstrapperURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

var messageBoxW = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

func ensureWebView2() error {
	return ensureWebView2With(webView2Installed, func() bool {
		return pawnMessage("未检测到 Microsoft Edge WebView2 运行时，客户端界面需要它才能启动。\n\n是否从 Microsoft 下载并安装？安装需要联网，可能需要几分钟。", 0x24) == 6
	}, installWebView2, func(message string) { pawnMessage(message, 0x30) })
}

// Keep the decision separate from installation so decline, failure and success
// are testable without changing the machine's runtime installation.
func ensureWebView2With(installed func() bool, ask func() bool, install func() error, warn func(string)) error {
	if installed() {
		return nil
	}
	if !ask() {
		return errors.New("webview2_runtime_missing")
	}
	err := install()
	if err == nil && installed() {
		return nil
	}
	warn("WebView2 安装未完成，客户端无法启动。请检查网络连接并重试，或从 Microsoft 手动安装后重新打开客户端：\nhttps://developer.microsoft.com/microsoft-edge/webview2/")
	if err != nil {
		return fmt.Errorf("webview2_install_failed: %w", err)
	}
	return errors.New("webview2_runtime_missing_after_install")
}

func installWebView2() error {
	dir, err := os.MkdirTemp("", "esp32-monitor-webview2-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "MicrosoftEdgeWebview2Setup.exe")
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 10 {
			return errors.New("invalid download redirect")
		}
		return nil
	}}
	resp, err := client.Get(webView2BootstrapperURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bootstrapper download: HTTP %d", resp.StatusCode)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	const maxSize = 20 << 20
	n, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n == 0 || n > maxSize {
		return errors.New("invalid bootstrapper size")
	}

	// Verify the Authenticode chain and Microsoft publisher before executing
	// the downloaded bootstrapper. The installer itself downloads the runtime.
	script := `$sig = Get-AuthenticodeSignature -LiteralPath $env:MONITOR_WEBVIEW_INSTALLER; if ($sig.Status -ne 'Valid' -or $sig.SignerCertificate.Subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)') { exit 1 }; $p = Start-Process -FilePath $env:MONITOR_WEBVIEW_INSTALLER -ArgumentList '/silent','/install' -Wait -PassThru; exit $p.ExitCode`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "MONITOR_WEBVIEW_INSTALLER="+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func webView2Installed() bool {
	paths := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + webView2ClientGUID},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + webView2ClientGUID},
		{registry.CURRENT_USER, `Software\Microsoft\EdgeUpdate\Clients\` + webView2ClientGUID},
	}
	for _, candidate := range paths {
		key, err := registry.OpenKey(candidate.root, candidate.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		version, _, err := key.GetStringValue("pv")
		key.Close()
		if err == nil && version != "" && version != "0.0.0.0" {
			return true
		}
	}
	return false
}
