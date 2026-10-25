package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// elidedText is a single line of text without the padding a widget.Label adds.
// It is used for tree rows, where every pixel of height costs a whole entry of
// the list. Text that does not fit is cut with an ellipsis instead of
// stretching the tree.
type elidedText struct {
	widget.BaseWidget

	text     string
	bold     bool
	mono     bool
	textSize float32
	color    fyne.ThemeColorName
}

func newElidedText() *elidedText {
	t := &elidedText{color: theme.ColorNameForeground}
	t.ExtendBaseWidget(t)
	return t
}

// SetText updates the displayed text.
func (t *elidedText) SetText(text string) {
	if t.text == text {
		return
	}
	t.text = text
	t.Refresh()
}

// SetBold makes the text bold, which marks folders in the tree.
func (t *elidedText) SetBold(bold bool) {
	if t.bold == bold {
		return
	}
	t.bold = bold
	t.Refresh()
}

// SetTextSize overrides the font size; zero means the theme text size.
func (t *elidedText) SetTextSize(size float32) {
	if t.textSize == size {
		return
	}
	t.textSize = size
	t.Refresh()
}

// Text returns the current text.
func (t *elidedText) Text() string {
	return t.text
}

func (t *elidedText) CreateRenderer() fyne.WidgetRenderer {
	text := canvas.NewText("", theme.Color(t.color))
	text.TextSize = theme.TextSize()
	return &elidedTextRenderer{label: t, text: text}
}

type elidedTextRenderer struct {
	label *elidedText
	text  *canvas.Text
}

func (r *elidedTextRenderer) style() fyne.TextStyle {
	return fyne.TextStyle{Bold: r.label.bold, Monospace: r.label.mono}
}

// size is the font size used for measuring and drawing.
func (r *elidedTextRenderer) size() float32 {
	if r.label.textSize > 0 {
		return r.label.textSize
	}
	return theme.TextSize()
}

// MinSize reports the height of one line but a deliberately small width: the
// tree should be able to shrink and elide long names instead of demanding space.
func (r *elidedTextRenderer) MinSize() fyne.Size {
	size := r.size()
	height := fyne.MeasureText("Ag", size, r.style()).Height
	return fyne.NewSize(size*2, height)
}

func (r *elidedTextRenderer) Layout(size fyne.Size) {
	r.text.TextSize = r.size()
	r.text.TextStyle = r.style()
	r.text.Text = fitText(r.label.text, size.Width, r.text.TextSize, r.text.TextStyle)
	r.text.Move(fyne.NewPos(0, 0))
	r.text.Resize(fyne.MeasureText(r.text.Text, r.text.TextSize, r.text.TextStyle))
}

func (r *elidedTextRenderer) Refresh() {
	r.text.Color = theme.Color(r.label.color)
	r.Layout(r.label.Size())
	r.text.Refresh()
}

func (r *elidedTextRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.text}
}

func (r *elidedTextRenderer) Destroy() {}

// fitText trims text to fit width, adding an ellipsis when it was shortened.
func fitText(text string, width, size float32, style fyne.TextStyle) string {
	if width <= 0 || text == "" {
		return ""
	}
	if fyne.MeasureText(text, size, style).Width <= width {
		return text
	}

	ellipsis := "…"
	if fyne.MeasureText(ellipsis, size, style).Width > width {
		return ""
	}

	runes := []rune(text)
	// Binary search for the longest prefix that still fits with the ellipsis.
	low, high := 0, len(runes)
	for low < high {
		mid := (low + high + 1) / 2
		candidate := strings.TrimRight(string(runes[:mid]), " ") + ellipsis
		if fyne.MeasureText(candidate, size, style).Width <= width {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return strings.TrimRight(string(runes[:low]), " ") + ellipsis
}
