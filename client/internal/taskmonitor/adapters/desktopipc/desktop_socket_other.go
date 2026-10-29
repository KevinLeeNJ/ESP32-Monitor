//go:build !darwin && !linux && !windows

package desktopipc

import (
	"context"
	"errors"
	"net"
)

func desktopEndpoint(string) (string, string) { return "", "desktop_platform_unsupported" }

func connectDesktop(context.Context, string) (net.Conn, error) {
	return nil, errors.New("desktop_platform_unsupported")
}
