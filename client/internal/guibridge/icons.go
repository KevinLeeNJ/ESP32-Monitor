package guibridge

import _ "embed"

//go:embed assets/menu-bar@2x.png
var menuBarIcon []byte

//go:embed assets/app-icon-32.png
var trayAppIcon []byte

//go:embed assets/app-icon-256.png
var appIcon []byte

// MenuBarIcon returns the template PNG used by the macOS status bar.
func MenuBarIcon() []byte { return menuBarIcon }

// TrayAppIcon returns the coloured app icon used by the Windows notification area.
func TrayAppIcon() []byte { return trayAppIcon }

// AppIcon returns the application icon used by native window chrome and dialogs.
func AppIcon() []byte { return appIcon }
