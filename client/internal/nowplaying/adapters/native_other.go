//go:build !linux && !windows

package adapters

import "context"

func runNative(context.Context, *streamAdapter) {}
