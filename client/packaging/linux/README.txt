ESP32 Monitor Linux

On Debian 13 or Ubuntu 24.04 with a desktop environment, install GTK4,
WebKitGTK 6.0, and the AppImage's host tools and services:

  sudo apt install libgtk-4-1 libwebkitgtk-6.0-4 xdg-utils libglib2.0-bin udisks2 upower bubblewrap xdg-dbus-proxy

On Fedora, install the GUI runtime libraries with:

  sudo dnf install gtk4 webkitgtk6.0

The metrics and WebKit sandbox also require the corresponding host tools
and services, including xdg-utils, udisks2, upower, bubblewrap and
xdg-dbus-proxy.

GPU metrics are optional; install nvtop separately if needed. The installed
version must support --snapshot:

  sudo apt install nvtop

Other distributions use corresponding package names. GitHub credentials
require an unlocked Secret Service-compatible desktop keyring. Codex quota
requires a signed-in Codex CLI or supported Codex Desktop. Hardware, drivers
and permissions determine which metrics are available. Physical disk capacity
covers mounted partitions/whole disks plus RAID member partitions; unmounted
non-member partitions are excluded, and RAID volume usage is apportioned to
its member disks by capacity. SMART health comes from the UDisks2 cache.

The AppImage bundles the GUI, Agent and frontend assets. GTK4, WebKitGTK 6.0,
their dependencies, graphics drivers and the tools above are provided by the
host. Project, asset and third-party license texts are inside the AppImage
under /usr/share/doc/esp32-monitor/.

The distributed build is made on Debian 13 with glibc 2.41. Compatibility
with older host runtimes and other GTK/WebKit versions is not guaranteed.
The AppImage uses the host's default GTK backend; set GDK_BACKEND explicitly
to override it.

Make the AppImage executable and run it. If FUSE mounting is unavailable:
  APPIMAGE_EXTRACT_AND_RUN=1 ./ESP32-Monitor-x86_64.AppImage

For startup diagnostics, run it from a terminal:
  ./ESP32-Monitor-x86_64.AppImage 2>&1 | tee esp32-monitor-startup.log

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

To build the AppImage, install Go, Node.js, rsync, pkg-config, GTK4 and
WebKitGTK 6.0 development packages, and appimagetool. The Go module pins
Wails v3.0.0. Run `make build-appimage-linux` in client/. appimagetool may
download the AppImage runtime during packaging.
