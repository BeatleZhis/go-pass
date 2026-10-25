package ui

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// DefaultUIScale is the density of the interface. 1 means the stock Fyne
// metrics, smaller values shrink text and paddings; 0.85 keeps a long folder
// tree readable without wasting vertical space.
const DefaultUIScale float32 = 0.85

// compactTheme wraps a Fyne theme and scales the metrics that make the UI feel
// bulky: font sizes, paddings, line spacing and inline icons.
type compactTheme struct {
	base  fyne.Theme
	scale float32
}

func (t *compactTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	return t.base.Color(name, variant)
}

func (t *compactTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *compactTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

// scaledSizes lists the metrics that follow the scale factor.
var scaledSizes = map[fyne.ThemeSizeName]bool{
	theme.SizeNameText:           true,
	theme.SizeNameHeadingText:    true,
	theme.SizeNameSubHeadingText: true,
	theme.SizeNameCaptionText:    true,
	theme.SizeNamePadding:        true,
	theme.SizeNameInnerPadding:   true,
	theme.SizeNameLineSpacing:    true,
	theme.SizeNameInlineIcon:     true,
	theme.SizeNameScrollBar:      true,
	theme.SizeNameScrollBarSmall: true,
	theme.SizeNameSplitThickness: true,
}

func (t *compactTheme) Size(name fyne.ThemeSizeName) float32 {
	base := t.base.Size(name)
	if !scaledSizes[name] || t.scale == 1 {
		return base
	}
	return float32(math.Round(float64(base * t.scale)))
}

// applyUIScale installs a compact theme on the application. A scale of 1 (or
// less than zero) keeps the stock look.
func applyUIScale(app fyne.App, scale float32) {
	if scale <= 0 {
		scale = DefaultUIScale
	}
	if scale == 1 {
		return
	}
	base := app.Settings().Theme()
	if base == nil {
		base = theme.DefaultTheme()
	}
	app.Settings().SetTheme(&compactTheme{base: base, scale: scale})
}
