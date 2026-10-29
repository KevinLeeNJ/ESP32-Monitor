//go:build windows

package ipc

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const pipePrefix = `\\.\pipe\`

func validateIPCPath(path string) error {
	if !strings.HasPrefix(strings.ToLower(path), pipePrefix) {
		return errors.New("ipc_socket_path_must_be_named_pipe")
	}
	// A pipe name is one path component; nested paths would silently address
	// a different kernel namespace.
	name := path[len(pipePrefix):]
	if name == "" || strings.Contains(name, `\`) || len(path) > 200 {
		return errors.New("ipc_socket_path_invalid")
	}
	return nil
}

// listenIPC creates the first instance of a per-user named pipe. The DACL
// grants full access only to the current user SID, and remote clients are
// rejected by go-winio.
func listenIPC(path string) (net.Listener, error) {
	timeout := 200 * time.Millisecond
	if conn, err := winio.DialPipe(path, &timeout); err == nil {
		_ = conn.Close()
		return nil, ErrAlreadyRunning
	}
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	listener, err := winio.ListenPipe(path, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")"})
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return listener, nil
}

func closeIPCListener(listener net.Listener, path string) error { return listener.Close() }

func dialIPC(ctx context.Context, path string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return winio.DialPipeContext(ctx, path)
}

func ipcPeerOK(conn net.Conn) bool {
	return pipePeerMatches(conn, windows.GetNamedPipeClientProcessId)
}

// ipcServerPeerOK verifies the process listening on the other end of the pipe
// so the GUI cannot be tricked into talking to a squatted pipe name.
func ipcServerPeerOK(conn net.Conn) bool {
	return pipePeerMatches(conn, windows.GetNamedPipeServerProcessId)
}

func pipePeerMatches(conn net.Conn, lookup func(windows.Handle, *uint32) error) bool {
	fd, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	var pid uint32
	if lookup(windows.Handle(fd.Fd()), &pid) != nil || pid == 0 {
		return false
	}
	return processUserMatches(pid)
}

func processUserMatches(pid uint32) bool {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return false
	}
	current, err := currentUserSIDValue()
	if err != nil {
		return false
	}
	return windows.EqualSid(user.User.Sid, current)
}

var currentSID struct {
	once sync.Once
	sid  *windows.SID
	err  error
}

func currentUserSIDValue() (*windows.SID, error) {
	currentSID.once.Do(func() {
		token, err := windows.OpenCurrentProcessToken()
		if err != nil {
			currentSID.err = err
			return
		}
		defer token.Close()
		user, err := token.GetTokenUser()
		if err != nil {
			currentSID.err = err
			return
		}
		currentSID.sid = user.User.Sid
	})
	return currentSID.sid, currentSID.err
}

func currentUserSID() (string, error) {
	sid, err := currentUserSIDValue()
	if err != nil {
		return "", err
	}
	return sid.String(), nil
}
