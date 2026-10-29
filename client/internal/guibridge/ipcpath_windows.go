//go:build windows

package guibridge

import (
	"golang.org/x/sys/windows"
)

// defaultIPCPath names the pipe after the current user SID so two accounts
// never share an endpoint.
func defaultIPCPath() (string, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return "", err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return `\\.\pipe\ESP32-Monitor-` + user.User.Sid.String(), nil
}
