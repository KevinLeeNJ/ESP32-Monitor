//go:build !darwin && !linux

package desktopipc

import "errors"

func validateDesktopSocket(string) error { return errors.New("desktop_platform_unsupported") }
