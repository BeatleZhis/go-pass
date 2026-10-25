package ui

import (
	"bytes"
	"image/png"
	"testing"
)

func TestEmbeddedIcon(t *testing.T) {
	if len(appIconPNG) == 0 {
		t.Fatal("иконка не встроена")
	}

	img, err := png.Decode(bytes.NewReader(appIconPNG))
	if err != nil {
		t.Fatalf("иконка не декодируется: %v", err)
	}
	if got := img.Bounds().Dx(); got != 256 {
		t.Errorf("ширина иконки = %d, want 256", got)
	}
	if got := img.Bounds().Dy(); got != 256 {
		t.Errorf("высота иконки = %d, want 256", got)
	}

	// Угол должен быть прозрачным: иконка — скруглённый квадрат.
	if _, _, _, a := img.At(1, 1).RGBA(); a != 0 {
		t.Errorf("угол иконки не прозрачный: alpha=%d", a)
	}

	// В центре — фиолетовый замок, а не фон.
	r, g, b, a := img.At(128, 128).RGBA()
	if a == 0 {
		t.Fatal("центр иконки прозрачный")
	}
	if !(b > r && b > g) {
		t.Errorf("центр иконки не фиолетовый: r=%d g=%d b=%d", r>>8, g>>8, b>>8)
	}

	if res := appIcon(); res == nil || res.Name() != "icon.png" {
		t.Errorf("appIcon() = %v", res)
	}
}
