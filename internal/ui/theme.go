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

// Obsidian-like palette: near-black background, slightly lighter panels,
// muted text and a single purple accent.
var (
	colorBackground   = color.NRGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}
	colorSidebar      = color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}
	colorPanel        = color.NRGBA{R: 0x26, G: 0x26, B: 0x26, A: 0xff}
	colorPanelHover   = color.NRGBA{R: 0x2f, G: 0x2f, B: 0x2f, A: 0xff}
	colorBorder       = color.NRGBA{R: 0x33, G: 0x33, B: 0x33, A: 0xff}
	colorForeground   = color.NRGBA{R: 0xdc, G: 0xdd, B: 0xde, A: 0xff}
	colorMuted        = color.NRGBA{R: 0x8a, G: 0x8a, B: 0x8a, A: 0xff}
	colorAccent       = color.NRGBA{R: 0x7f, G: 0x6d, B: 0xf2, A: 0xff}
	colorAccentHover  = color.NRGBA{R: 0x93, G: 0x84, B: 0xf6, A: 0xff}
	colorSelection    = color.NRGBA{R: 0x3a, G: 0x35, B: 0x66, A: 0xff}
	colorError        = color.NRGBA{R: 0xe5, G: 0x53, B: 0x4b, A: 0xff}
	colorSuccess      = color.NRGBA{R: 0x57, G: 0xab, B: 0x5a, A: 0xff}
	colorWarning      = color.NRGBA{R: 0xd2, G: 0x99, B: 0x22, A: 0xff}
	colorScrollBar    = color.NRGBA{R: 0x4a, G: 0x4a, B: 0x4a, A: 0xff}
	colorShadow       = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x66}
	colorOverlay      = color.NRGBA{R: 0x22, G: 0x22, B: 0x26, A: 0x90}
	colorTextOnAccent = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// palette overrides the Fyne colours with the Obsidian-like scheme. The
// interface is dark regardless of the system appearance, which is what the
// look is about.
var palette = map[fyne.ThemeColorName]color.Color{
	theme.ColorNameBackground:                colorBackground,
	theme.ColorNameHeaderBackground:          colorSidebar,
	theme.ColorNameMenuBackground:            colorPanel,
	theme.ColorNameOverlayBackground:         colorOverlay,
	theme.ColorNameInputBackground:           colorPanel,
	theme.ColorNameInputBorder:               colorBorder,
	theme.ColorNameButton:                    colorPanel,
	theme.ColorNameHover:                     colorPanelHover,
	theme.ColorNamePressed:                   colorPanelHover,
	theme.ColorNameSelection:                 colorSelection,
	theme.ColorNameSeparator:                 colorBorder,
	theme.ColorNameForeground:                colorForeground,
	theme.ColorNamePlaceHolder:               colorMuted,
	theme.ColorNameDisabled:                  colorMuted,
	theme.ColorNameDisabledButton:            colorPanel,
	theme.ColorNamePrimary:                   colorAccent,
	theme.ColorNameForegroundOnPrimary:       colorTextOnAccent,
	theme.ColorNameFocus:                     colorAccent,
	theme.ColorNameScrollBar:                 colorScrollBar,
	theme.ColorNameScrollBarBackground:       colorBackground,
	theme.ColorNameShadow:                    colorShadow,
	theme.ColorNameError:                     colorError,
	theme.ColorNameSuccess:                   colorSuccess,
	theme.ColorNameWarning:                   colorWarning,
	theme.ColorNameHyperlink:                 colorAccentHover,
	theme.ColorNameForegroundOnError:         colorTextOnAccent,
	theme.ColorNameForegroundOnSuccess:       colorTextOnAccent,
	theme.ColorNameForegroundOnWarning:       colorTextOnAccent,
	theme.ColorNameInnerWindowBorder:         colorBorder,
	theme.ColorNameInnerWindowBorderInactive: colorPanel,
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

// appTheme is the application look: the Obsidian-like palette plus compact
// metrics.
type appTheme struct {
	base  fyne.Theme
	scale float32
}

func (t *appTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if c, ok := palette[name]; ok {
		return c
	}
	return t.base.Color(name, variant)
}

func (t *appTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *appTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *appTheme) Size(name fyne.ThemeSizeName) float32 {
	base := t.base.Size(name)
	if !scaledSizes[name] || t.scale == 1 {
		return base
	}
	return float32(math.Round(float64(base * t.scale)))
}

// applyTheme installs the application theme. A scale of 1 keeps stock Fyne
// metrics while the colours stay the same.
func applyTheme(app fyne.App, scale float32) {
	if scale <= 0 {
		scale = DefaultUIScale
	}
	base := app.Settings().Theme()
	if base == nil {
		base = theme.DefaultTheme()
	}
	app.Settings().SetTheme(&appTheme{base: base, scale: scale})
}
