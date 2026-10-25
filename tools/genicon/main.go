//go:build ignore

// Command genicon рисует иконку приложения: тёмный скруглённый квадрат в
// палитре темы и фиолетовый замок. Генератор нужен, чтобы иконку можно было
// пересобрать без графических редакторов и внешних зависимостей:
//
//	go run tools/genicon/main.go -out assets/icon-1024.png
package main

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

// Дизайн задан в условной сетке 1024×1024, чтобы координаты читались.
const design = 1024.0

// Палитра совпадает с темой приложения (internal/ui/theme.go).
var (
	bgTop      = color.NRGBA{R: 0x2a, G: 0x2a, B: 0x33, A: 0xff}
	bgBottom   = color.NRGBA{R: 0x17, G: 0x17, B: 0x1b, A: 0xff}
	border     = color.NRGBA{R: 0x3f, G: 0x3f, B: 0x4d, A: 0xff}
	shackle    = color.NRGBA{R: 0xcf, G: 0xc8, B: 0xff, A: 0xff}
	bodyTop    = color.NRGBA{R: 0x9c, G: 0x8d, B: 0xff, A: 0xff}
	bodyBottom = color.NRGBA{R: 0x6c, G: 0x5c, B: 0xe7, A: 0xff}
	bodyRim    = color.NRGBA{R: 0xb6, G: 0xab, B: 0xff, A: 0xff}
	glow       = color.NRGBA{R: 0x7f, G: 0x6d, B: 0xf2, A: 0xff}
	keyhole    = color.NRGBA{R: 0x16, G: 0x16, B: 0x1b, A: 0xff}
)

func main() {
	out := flag.String("out", "assets/icon-1024.png", "куда сохранить PNG")
	size := flag.Int("size", 1024, "сторона изображения в пикселях")
	flag.Parse()

	img := render(*size)

	f, err := os.Create(*out)
	if err != nil {
		log.Fatalf("создать %s: %v", *out, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatalf("записать PNG: %v", err)
	}
	log.Printf("иконка %d×%d сохранена в %s", *size, *size, *out)
}

// render рисует иконку с суперсэмплингом: каждый пиксель усредняется по 4×4
// подвыборкам, поэтому края скруглений и кольца получаются гладкими.
func render(size int) *image.NRGBA {
	const ss = 4
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var sr, sg, sb float64
			var inside int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					u := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					v := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					c, ok := shade(u, v)
					if !ok {
						continue
					}
					sr += float64(c.R)
					sg += float64(c.G)
					sb += float64(c.B)
					inside++
				}
			}
			if inside == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(sr/float64(inside) + 0.5),
				G: uint8(sg/float64(inside) + 0.5),
				B: uint8(sb/float64(inside) + 0.5),
				A: uint8(255 * inside / (ss * ss)),
			})
		}
	}
	return img
}

// shade возвращает цвет точки в нормализованных координатах (0..1) или false,
// если точка вне иконки.
func shade(u, v float64) (color.NRGBA, bool) {
	x, y := u*design, v*design

	// Скруглённый квадрат: всё, что вне него, прозрачно.
	if !roundedRect(x, y, 0, 0, design, design, 232) {
		return color.NRGBA{}, false
	}

	// Фон с мягким вертикальным градиентом и тонкой рамкой по краю.
	c := mix(bgTop, bgBottom, v)
	if !roundedRect(x, y, 6, 6, design-6, design-6, 226) {
		return border, true
	}

	// Фиолетовое свечение вокруг замка — чтобы фон не выглядел пустым.
	if d := math.Hypot(x-512, y-560); d < 520 {
		g := 1 - d/520
		c = mix(c, glow, g*g*0.28)
	}

	// Дужка замка: половина кольца над корпусом.
	if annulus(x, y, 512, 430, 104, 212) && y <= 430 {
		c = mix(shackle, bodyTop, 0.25)
	}

	// Корпус замка с градиентом и светлым ободком.
	const bx0, by0, bx1, by1, br = 296, 420, 728, 806, 92
	if roundedRect(x, y, bx0, by0, bx1, by1, br) {
		t := (y - by0) / (by1 - by0)
		c = mix(bodyTop, bodyBottom, clamp01(t))
		if !roundedRect(x, y, bx0+10, by0+10, bx1-10, by1-10, br-8) {
			c = bodyRim
		}
	}

	// Замочная скважина: круг и ножка.
	if circle(x, y, 512, 566, 58) || roundedRect(x, y, 484, 566, 540, 688, 24) {
		c = keyhole
	}

	return c, true
}

// roundedRect сообщает, лежит ли точка внутри прямоугольника со скруглением r.
func roundedRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Min(math.Max(x, x0+r), x1-r)
	cy := math.Min(math.Max(y, y0+r), y1-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// circle сообщает, лежит ли точка внутри круга.
func circle(x, y, cx, cy, r float64) bool {
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// annulus сообщает, лежит ли точка внутри кольца.
func annulus(x, y, cx, cy, inner, outer float64) bool {
	dx, dy := x-cx, y-cy
	d2 := dx*dx + dy*dy
	return d2 >= inner*inner && d2 <= outer*outer
}

// mix линейно смешивает два цвета.
func mix(a, b color.NRGBA, t float64) color.NRGBA {
	t = clamp01(t)
	lerp := func(x, y uint8) uint8 {
		return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5)
	}
	return color.NRGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: 0xff}
}

func clamp01(v float64) float64 {
	return math.Min(1, math.Max(0, v))
}
