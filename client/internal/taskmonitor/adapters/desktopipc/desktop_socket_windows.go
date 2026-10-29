//go:build windows

package desktopipc

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const desktopPipe = `\\.\pipe\codex-ipc`

func desktopEndpoint(string) (string, string) { return desktopPipe, "" }

func connectDesktop(ctx context.Context, path string) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := winio.DialPipeContext(dialCtx, path)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, errors.New("desktop_socket_permission_denied")
		}
		return nil, errors.New("desktop_socket_unavailable")
	}
	if err := verifyDesktopPeer(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// The pipe namespace is machine-wide, so a same-named pipe from another user
// must be rejected before any state is trusted.
func verifyDesktopPeer(conn net.Conn) error {
	handle, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("desktop_socket_unavailable")
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(handle.Fd()), &pid); err != nil {
		return errors.New("desktop_socket_unavailable")
	}
	own, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return errors.New("desktop_socket_permission_denied")
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return errors.New("desktop_socket_permission_denied")
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return errors.New("desktop_socket_permission_denied")
	}
	defer token.Close()
	server, err := token.GetTokenUser()
	if err != nil || !windows.EqualSid(server.User.Sid, own.User.Sid) {
		return errors.New("desktop_socket_permission_denied")
	}
	return nil
}
