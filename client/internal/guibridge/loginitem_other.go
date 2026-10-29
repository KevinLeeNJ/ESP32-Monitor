//go:build !darwin && !windows && !linux

package guibridge

import "errors"

func setLaunchAtLogin(bool, func(bool) error) error {
	return errors.New("launch_at_login_unsupported")
}
