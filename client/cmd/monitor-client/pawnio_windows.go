//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

func pawnIOInstalled() bool {
	for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\PawnIO`, registry.QUERY_VALUE|view)
		if err != nil {
			continue
		}
		version, _, err := key.GetStringValue("DisplayVersion")
		key.Close()
		if err == nil && version != "" {
			return true
		}
	}
	return false
}

func pawnMessage(message string, flags uintptr) uintptr {
	text, _ := syscall.UTF16PtrFromString(message)
	caption, _ := syscall.UTF16PtrFromString("ESP32-Monitor")
	result, _, _ := messageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(caption)), flags)
	return result
}

func validPawnInstaller(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false
	}
	return hex.EncodeToString(hash.Sum(nil)) == "a3a46226c5e2824f4cdd42be0eecbabfc672c86f7889710f5ab1e6ad385b47a0"
}

func ensurePawnIO() {
	if pawnIOInstalled() {
		return
	}
	if pawnMessage("未检测到 PawnIO。它用于读取 CPU 等硬件传感器。是否安装？安装时 Windows 会请求管理员权限。", 0x24) != 6 {
		return
	}
	exe, err := os.Executable()
	if err == nil {
		installer := filepath.Join(filepath.Dir(exe), "PawnIO_setup.exe")
		if !validPawnInstaller(installer) {
			pawnMessage("PawnIO 安装程序缺失或校验失败，请重新安装客户端。", 0x30)
			return
		}
		cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `Start-Process -FilePath $env:MONITOR_PAWN_INSTALLER -ArgumentList '-install' -Verb RunAs -Wait -ErrorAction Stop`)
		cmd.Env = append(os.Environ(), "MONITOR_PAWN_INSTALLER="+installer)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		err = cmd.Run()
	}
	if err != nil || !pawnIOInstalled() {
		pawnMessage("PawnIO 未安装。您可以稍后重新启动客户端并重试；部分硬件传感器可能不可用。", 0x30)
	}
}
