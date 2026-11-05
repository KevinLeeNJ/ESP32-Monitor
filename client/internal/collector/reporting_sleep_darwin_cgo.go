//go:build darwin && cgo

package collector

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CGDisplayConfiguration.h>

static int monitor_display_asleep(void) {
    return CGDisplayIsAsleep(CGMainDisplayID());
}
*/
import "C"

// A dark wake can run the Agent while the Mac's display remains asleep.
func macReportingPaused() bool { return C.monitor_display_asleep() != 0 }
