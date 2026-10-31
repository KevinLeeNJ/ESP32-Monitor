//go:build !linux

package adapters

import "context"

func runNative(context.Context, *streamAdapter) {}
