ESP32 Monitor Linux

On Ubuntu 24.04 or Debian 13 with a desktop environment, install the
AppImage's host tools and services:

  sudo apt install util-linux xdg-utils libglib2.0-bin udisks2 upower bubblewrap xdg-dbus-proxy

GPU metrics are optional; install nvtop separately if needed. The installed
version must support --snapshot:

  sudo apt install nvtop

Other distributions use corresponding package names. GitHub credentials
require an unlocked Secret Service-compatible desktop keyring. Codex quota
requires a signed-in Codex CLI or supported Codex Desktop. Hardware, drivers
and permissions determine which metrics are available. Disk usage includes
mounted filesystems only; SMART health comes from the UDisks2 cache.

The AppImage bundles the GUI, Agent, GTK 4 and WebKitGTK 6.0. Graphics drivers
and the tools above remain host-provided. Project, asset and third-party
license texts are inside the AppImage under /usr/share/doc/esp32-monitor/.
Supported systems are Ubuntu 24.04+
and Debian 13+ on x86_64; this release is built and tested on Debian 13
(glibc 2.41). The minimum runtime is glibc 2.39 and GLIBCXX 3.4.32, so Fedora
40+ meets it as well. Fedora 45's container image has also been smoke-tested
with Wayland and X11, software rendering and the WebKit sandbox. Ubuntu 22.04,
Debian 12 and older releases are not supported.
The tested versions are GTK 4.18.6, WebKitGTK 2.54.0, GLib 2.84.4,
util-linux 2.41.5, UDisks2 2.10.1, UPower 1.90.9, xdg-utils 1.2.1,
bubblewrap 0.12.0,
xdg-dbus-proxy 0.1.6 and nvtop 3.2.0.
These are tested versions, not minimum supported versions. lsblk must support
--json, --bytes and the FSUSED column.

Make the AppImage executable and run it. If FUSE mounting is unavailable:
  APPIMAGE_EXTRACT_AND_RUN=1 ./ESP32-Monitor-x86_64.AppImage

At startup, the AppImage uses the host's ldd to check the GUI and essential
WebKit processes for missing libraries and incompatible symbol versions.
Errors name the affected file and dependency on standard error. To capture
them, run the AppImage from a terminal. On a graphical desktop, the error is
also shown in a dialog if zenity or kdialog is installed:
  ./ESP32-Monitor-x86_64.AppImage 2>&1 | tee esp32-monitor-startup.log
Libraries loaded later by graphics drivers or optional plugins are not covered
by this check; their own runtime errors are still written to standard error.

On first launch the client installs a desktop entry and icon under
~/.local/share so KDE and other Wayland compositors can resolve the window
icon. Set ESP32_MONITOR_SKIP_DESKTOP_INTEGRATION=1 to skip this. To undo it,
delete ~/.local/share/applications/esp32-monitor.desktop and
~/.local/share/icons/hicolor/256x256/apps/esp32-monitor.png.

WebKitGTK sandboxes its web process with bubblewrap and xdg-dbus-proxy, which
must come from the host. If either tool is missing, or if the kernel or a
container disables unprivileged user namespaces, the sandbox cannot start.
The GUI then exits or never shows a window; bwrap reports errors such as
"setting up uid map: Permission denied". Install bubblewrap and
xdg-dbus-proxy; in containers, allow unprivileged user namespaces or run the
AppImage on the host desktop. As a last resort only, sandboxing can be
disabled:

  WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1 ./ESP32-Monitor-x86_64.AppImage

This removes the sandbox's process isolation; treat it as a security
downgrade and do not use it by default. WEBKIT_DISABLE_SANDBOX is not
honored.
