//go:build windows

package main

import (
	"errors"
	"testing"
)

func TestEnsureWebView2(t *testing.T) {
	cases := []struct {
		name                                                        string
		initiallyInstalled, accept, installSucceeds, installedAfter bool
		wantInstall, wantWarning, wantError                         bool
	}{
		{name: "already installed", initiallyInstalled: true},
		{name: "declined", wantError: true},
		{name: "installed", accept: true, installSucceeds: true, installedAfter: true, wantInstall: true},
		{name: "installer fails", accept: true, wantInstall: true, wantWarning: true, wantError: true},
		{name: "runtime still missing", accept: true, installSucceeds: true, wantInstall: true, wantWarning: true, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls, warned := 0, false
			err := ensureWebView2With(func() bool {
				if calls > 0 {
					return tc.installedAfter
				}
				return tc.initiallyInstalled
			},
				func() bool { return tc.accept }, func() error {
					calls++
					if !tc.installSucceeds {
						return errors.New("failure")
					}
					return nil
				},
				func(string) { warned = true })
			if (calls != 0) != tc.wantInstall || warned != tc.wantWarning || (err != nil) != tc.wantError {
				t.Fatalf("install calls=%d warning=%v error=%v", calls, warned, err)
			}
		})
	}
}
