package adapters

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// A system Perl host is needed on macOS 15.4+. A standalone executable calling
// MediaRemote may receive empty data even when the system has an active source.
const perlLoader = `use strict; use warnings; use DynaLoader;
my $h = DynaLoader::dl_load_file($ARGV[0], 0) or die "bridge load failed";
my $s = DynaLoader::dl_find_symbol($h, "monitor_media_run") or die "bridge symbol missing";
DynaLoader::dl_install_xsub("main::run", $s); run();`

func platformCommand(o Options) (string, string, func(context.Context) (*exec.Cmd, error)) {
	return "mediaremote", "system_current_source", func(ctx context.Context) (*exec.Cmd, error) {
		library := o.NativeLibrary
		if library == "" {
			executable, err := os.Executable()
			if err != nil {
				return nil, errors.New("music_bridge_missing")
			}
			library = filepath.Join(filepath.Dir(executable), "libmonitor-media.dylib")
		}
		if !filepath.IsAbs(library) {
			return nil, errors.New("music_bridge_missing")
		}
		if _, err := os.Stat(library); err != nil {
			return nil, errors.New("music_bridge_missing")
		}
		return exec.CommandContext(ctx, "/usr/bin/perl", "-e", perlLoader, library), nil
	}
}
