package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

// appIconPNG is the generated application icon (see tools/genicon). The
// 256 px version is enough for the dock and window icons on every platform.
//
//go:embed assets/icon.png
var appIconPNG []byte

// appIcon returns the icon as a Fyne resource.
func appIcon() fyne.Resource {
	return fyne.NewStaticResource("icon.png", appIconPNG)
}
