//go:build windows

package platform

import (
	"context"
	"strings"
	"syscall"
	"unsafe"
)

// credential mirrors CREDENTIALW from wincred.h. Only the explicitly named
// generic GitHub secret is requested; credentials are never enumerated.
type credential struct {
	Flags          uint32
	Type           uint32
	TargetName     *uint16
	Comment        *uint16
	LastWritten    syscall.Filetime
	BlobSize       uint32
	Blob           *byte
	Persist        uint32
	AttributeCount uint32
	Attributes     uintptr
	TargetAlias    *uint16
	UserName       *uint16
}

func windowsGitHubToken(ctx context.Context, account string) (string, error) {
	if ctx.Err() != nil {
		return "", ErrCredentialUnavailable
	}
	target, err := syscall.UTF16PtrFromString("esp32-monitor.github/" + account)
	if err != nil {
		return "", ErrCredentialUnavailable
	}
	dll := syscall.NewLazyDLL("advapi32.dll")
	read := dll.NewProc("CredReadW")
	free := dll.NewProc("CredFree")
	var value *credential
	ok, _, _ := read.Call(uintptr(unsafe.Pointer(target)), 1, 0, uintptr(unsafe.Pointer(&value)))
	if ok == 0 || value == nil {
		return "", ErrCredentialUnavailable
	}
	defer free.Call(uintptr(unsafe.Pointer(value)))
	if value.Blob == nil || value.BlobSize == 0 || value.BlobSize > 16384 || value.BlobSize%2 != 0 {
		return "", ErrCredentialUnavailable
	}
	token := strings.TrimSpace(syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(value.Blob)), int(value.BlobSize/2))))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", ErrCredentialUnavailable
	}
	return token, nil
}

func windowsSaveCredential(account, secret string) error {
	target, err := syscall.UTF16PtrFromString("esp32-monitor.github/" + account)
	if err != nil {
		return ErrCredentialUnavailable
	}
	user, err := syscall.UTF16PtrFromString(account)
	if err != nil {
		return ErrCredentialUnavailable
	}
	blob, err := syscall.UTF16FromString(secret)
	if err != nil {
		return ErrCredentialUnavailable
	}
	value := credential{Type: 1, TargetName: target, UserName: user, Persist: 2, BlobSize: uint32((len(blob) - 1) * 2), Blob: (*byte)(unsafe.Pointer(&blob[0]))}
	write := syscall.NewLazyDLL("advapi32.dll").NewProc("CredWriteW")
	ok, _, _ := write.Call(uintptr(unsafe.Pointer(&value)), 0)
	if ok == 0 {
		return ErrCredentialUnavailable
	}
	return nil
}
func windowsDeleteCredential(account string) error {
	target, err := syscall.UTF16PtrFromString("esp32-monitor.github/" + account)
	if err != nil {
		return ErrCredentialUnavailable
	}
	remove := syscall.NewLazyDLL("advapi32.dll").NewProc("CredDeleteW")
	ok, _, code := remove.Call(uintptr(unsafe.Pointer(target)), 1, 0)
	if ok == 0 && code != syscall.Errno(1168) {
		return ErrCredentialUnavailable
	}
	return nil
}
