package ui

import (
	"strings"
	"testing"
)

func TestGeneratePassword(t *testing.T) {
	const length = 24
	pwd, err := GeneratePassword(length)
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	if len([]rune(pwd)) != length {
		t.Fatalf("длина = %d, want %d (%q)", len([]rune(pwd)), length, pwd)
	}

	classes := map[string]func(rune) bool{
		"lower":  func(r rune) bool { return strings.ContainsRune(lowerChars, r) },
		"upper":  func(r rune) bool { return strings.ContainsRune(upperChars, r) },
		"digit":  func(r rune) bool { return strings.ContainsRune(digitChars, r) },
		"symbol": func(r rune) bool { return strings.ContainsRune(symbolChars, r) },
	}
	for name, has := range classes {
		found := false
		for _, r := range pwd {
			if has(r) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("в пароле %q нет символов класса %s", pwd, name)
		}
	}

	seen := make(map[string]struct{}, 32)
	for i := 0; i < 32; i++ {
		p, err := GeneratePassword(length)
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[p]; dup {
			t.Fatalf("сгенерирован повтор: %q", p)
		}
		seen[p] = struct{}{}
	}
}

func TestGeneratePasswordTooShort(t *testing.T) {
	if _, err := GeneratePassword(4); err == nil {
		t.Fatal("ожидалась ошибка для слишком короткого пароля")
	}
}

func TestMaskPassword(t *testing.T) {
	if got := maskPassword("abc"); got != "•••" {
		t.Errorf("maskPassword = %q", got)
	}
	if got := maskPassword(""); got != "—" {
		t.Errorf("maskPassword(\"\") = %q", got)
	}
	// Многобайтовые символы маскируются по рунам, а не по байтам.
	if got := maskPassword("аб"); got != "••" {
		t.Errorf("maskPassword(аб) = %q", got)
	}
}

func TestDescriptionText(t *testing.T) {
	s := secretFixture()
	got := descriptionText(s)
	want := "login: user\nurl: https://example.com\n\nnote line"
	if got != want {
		t.Errorf("descriptionText =\n%q\nwant\n%q", got, want)
	}
}
