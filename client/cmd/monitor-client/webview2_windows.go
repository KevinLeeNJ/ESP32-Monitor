//go:build windows

package main

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const webView2ClientGUID = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

var messageBoxW = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

func ensureWebView2() error {
	if webView2Installed() {
		return nil
	}
	text, _ := syscall.UTF16PtrFromString("未检测到 Microsoft Edge WebView2 运行时，界面无法启动。\n\n" +
		"请安装 Microsoft Edge WebView2 Runtime（Evergreen）后重新打开 ESP32 Monitor。\n" +
		"下载地址：https://developer.microsoft.com/microsoft-edge/webview2/")
	caption, _ := syscall.UTF16PtrFromString("ESP32-Monitor")
	messageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(caption)), 0x10)
	return errors.New("webview2_runtime_missing")
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
