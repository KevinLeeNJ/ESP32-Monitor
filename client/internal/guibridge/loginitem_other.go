//go:build !darwin && !windows && !linux

package guibridge

import "errors"

func setLaunchAtLogin(bool) error { return errors.New("launch_at_login_unsupported") }
