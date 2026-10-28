package guibridge

import _ "embed"

//go:embed assets/menu-bar@2x.png
var menuBarIcon []byte

// MenuBarIcon returns the template PNG used by the macOS status bar.
func MenuBarIcon() []byte { return menuBarIcon }
