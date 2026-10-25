package ui

import (
	"image/color"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/beatlezhis/go-pass/internal/clipboard"
)

func TestAppThemeScalesMetrics(t *testing.T) {
	base := theme.DefaultTheme()
	compact := &appTheme{base: base, scale: 0.8}

	// Шрифт и отступы уменьшаются.
	if got, want := compact.Size(theme.SizeNameText), float32(11); got != want {
		t.Errorf("text = %v, want %v", got, want)
	}
	if compact.Size(theme.SizeNamePadding) >= base.Size(theme.SizeNamePadding) {
		t.Error("padding не уменьшился")
	}
	if compact.Size(theme.SizeNameInnerPadding) >= base.Size(theme.SizeNameInnerPadding) {
		t.Error("innerPadding не уменьшился")
	}
	if compact.Size(theme.SizeNameInlineIcon) >= base.Size(theme.SizeNameInlineIcon) {
		t.Error("inline icon не уменьшился")
	}

	// Размеры, которые не должны меняться, остаются как есть.
	for _, name := range []fyne.ThemeSizeName{
		theme.SizeNameSeparatorThickness,
		theme.SizeNameDialogRadius,
		theme.SizeNameButtonRadius,
	} {
		if got, want := compact.Size(name), base.Size(name); got != want {
			t.Errorf("%v = %v, want %v", name, got, want)
		}
	}

	// Масштаб 1 оставляет тему нетронутой.
	stock := &appTheme{base: base, scale: 1}
	if got, want := stock.Size(theme.SizeNameText), base.Size(theme.SizeNameText); got != want {
		t.Errorf("при scale=1 text = %v, want %v", got, want)
	}
}

func TestCompactTreeRowsAreDense(t *testing.T) {
	a, _ := newTestApp(t)

	row := a.nodeCreate(true)
	a.nodeUpdate("personal", true, row)

	// Высота строки не должна превышать текст с небольшим запасом: раньше
	// widget.Label добавлял паддинги и строка была вдвое выше текста.
	lineHeight := fyne.MeasureText("Ag", theme.TextSize(), fyne.TextStyle{}).Height
	if got := row.MinSize().Height; got > lineHeight+2 {
		t.Errorf("высота строки = %v, ожидалось около %v", got, lineHeight)
	}

	// Итоговый шаг строк (высота + отступ между ними) должен быть компактным.
	pitch := row.MinSize().Height + theme.Padding()
	if pitch > 22 {
		t.Errorf("шаг строк дерева = %v, хочется не больше 22", pitch)
	}

	// В строке должна использоваться компактная подпись, а не widget.Label.
	if _, label := rowParts(row); label == nil {
		t.Fatal("в строке нет компактной подписи")
	}
}

func TestFitText(t *testing.T) {
	style := fyne.TextStyle{}
	size := theme.TextSize()

	if got := fitText("personal", 500, size, style); got != "personal" {
		t.Errorf("короткий текст изменился: %q", got)
	}
	if got := fitText("", 100, size, style); got != "" {
		t.Errorf("пустой текст = %q", got)
	}
	if got := fitText("personal", 0, size, style); got != "" {
		t.Errorf("при нулевой ширине = %q", got)
	}

	long := "papajohns.gitlab.yandexcloud.net (3)"
	const width float32 = 90
	got := fitText(long, width, size, style)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("ожидалось многоточие: %q", got)
	}
	if got == long {
		t.Error("текст не обрезан")
	}
	if w := fyne.MeasureText(got, size, style).Width; w > width {
		t.Errorf("обрезанный текст шире строки: %v > %v", w, width)
	}
}

func TestWindowSizeRestored(t *testing.T) {
	fyneApp := test.NewTempApp(t)
	fyneApp.Preferences().SetString(prefWindowSize, "777x555")

	store, _ := fakeStore(t, map[string]string{"personal/mail": "x\n"})
	a, err := New(fyneApp, Options{
		Store: store,
		Clip:  clipboard.NewWithClipboard(&memClipboard{}, 0),
		Sync:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Window().Canvas().Size(); got.Width != 777 || got.Height != 555 {
		t.Errorf("размер окна = %v, want 777x555", got)
	}
}

func TestWindowSizeDefaultsToCompact(t *testing.T) {
	store, _ := fakeStore(t, map[string]string{"personal/mail": "x\n"})
	a, err := New(test.NewTempApp(t), Options{
		Store: store,
		Clip:  clipboard.NewWithClipboard(&memClipboard{}, 0),
		Sync:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Window().Canvas().Size(); got != defaultWindowSize {
		t.Errorf("размер окна = %v, want %v", got, defaultWindowSize)
	}
}

func TestAppThemePalette(t *testing.T) {
	th := &appTheme{base: theme.DefaultTheme(), scale: DefaultUIScale}

	checks := map[fyne.ThemeColorName]color.NRGBA{
		theme.ColorNameBackground: colorBackground,
		theme.ColorNameForeground: colorForeground,
		theme.ColorNamePrimary:    colorAccent,
		theme.ColorNameSelection:  colorSelection,
		theme.ColorNameSeparator:  colorBorder,
		theme.ColorNameButton:     colorPanel,
	}
	for name, want := range checks {
		// Схема тёмная независимо от системной темы.
		for _, variant := range []fyne.ThemeVariant{theme.VariantDark, theme.VariantLight} {
			got, ok := th.Color(name, variant).(color.NRGBA)
			if !ok || got != want {
				t.Errorf("Color(%v, %v) = %v, want %v", name, variant, got, want)
			}
		}
	}

	// Акцент Obsidian — фиолетовый, текст на нём белый.
	if got := th.Color(theme.ColorNamePrimary, theme.VariantDark).(color.NRGBA); got.B <= got.R {
		t.Errorf("акцент должен быть фиолетовым: %v", got)
	}
}
