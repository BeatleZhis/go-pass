package ui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/beatlezhis/go-pass/internal/clipboard"
	"github.com/beatlezhis/go-pass/internal/pass"
)

// memClipboard хранит скопированный текст для проверок.
type memClipboard struct {
	value string
}

func (m *memClipboard) Set(text string) error { m.value = text; return nil }
func (m *memClipboard) Get() (string, error)  { return m.value, nil }

// fakeStore создаёт временное хранилище и скрипт-заглушку pass, который умеет
// отдавать заранее заданное содержимое и записывать stdin команды insert.
func fakeStore(t *testing.T, entries map[string]string) (*pass.Store, string) {
	t.Helper()
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	inserted := filepath.Join(dir, "inserted")

	script := "#!/bin/sh\ncase \"$1\" in\nshow)\n  case \"$2\" in\n"
	i := 0
	for name, content := range entries {
		p := filepath.Join(storeDir, filepath.FromSlash(name)+".gpg")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("encrypted"), 0o600); err != nil {
			t.Fatal(err)
		}

		contentFile := filepath.Join(dir, fmt.Sprintf("content-%d", i))
		if err := os.WriteFile(contentFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		script += fmt.Sprintf("    '%s') cat '%s' ;;\n", name, contentFile)
		i++
	}
	script += "    *) printf 'temp-pass\\nlogin: temp\\n' ;;\n  esac ;;\n"
	script += "insert) cat > '" + inserted + "'; name=\"$4\"; mkdir -p '" + storeDir + "'/\"$(dirname \"$name\")\"; : > '" + storeDir + "'/\"$name.gpg\" ;;\n"
	script += "*) exit 1 ;;\nesac\n"

	bin := filepath.Join(dir, "pass")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &pass.Store{Binary: bin, Dir: storeDir, Timeout: 10 * time.Second}, inserted
}

func newTestApp(t *testing.T) (*App, *memClipboard) {
	t.Helper()

	store, _ := fakeStore(t, map[string]string{
		"personal/mail":          "alpha-pass\nlogin: max\n",
		"dev/github":        "bravo-pass\nlogin: octo\nurl: https://github.com\n",
		"cloud/aws/root": "charlie-pass\nlogin: root\n\nзаметка\n",
	})

	mem := &memClipboard{}
	clip := clipboard.NewWithClipboard(mem, time.Minute)

	fyneApp := test.NewTempApp(t)
	a, err := New(fyneApp, Options{Store: store, Clip: clip, AppID: "test.gopass", Sync: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Window().Resize(fyne.NewSize(1040, 680))
	a.Window().Show()
	if len(a.visible) != 3 {
		t.Fatalf("список записей не загрузился: %+v", a.visible)
	}
	return a, mem
}

func TestAppListsAndFilters(t *testing.T) {
	a, _ := newTestApp(t)

	if got := a.visible[0].Name; got != "personal/mail" {
		t.Errorf("первая запись = %q, want personal/mail", got)
	}
	if len(a.existingDirs()) == 0 {
		t.Errorf("папки хранилища не определены")
	}

	a.search.SetText("dev")
	if len(a.visible) != 1 || a.visible[0].Name != "dev/github" {
		t.Fatalf("фильтр по поиску: %+v", a.visible)
	}

	a.search.SetText("нет-такого")
	if len(a.visible) != 0 {
		t.Fatalf("пустой результат поиска: %+v", a.visible)
	}
}

func TestAppRevealsAndCopies(t *testing.T) {
	a, mem := newTestApp(t)

	a.selectByName("cloud/aws/root")
	if a.current == nil || a.current.Name != "cloud/aws/root" {
		t.Fatalf("запись не расшифрована: %+v", a.current)
	}

	if got, want := a.passLabel.Text, strings.Repeat("•", len("charlie-pass")); got != want {
		t.Errorf("пароль не замаскирован: %q", got)
	}
	if got := a.details.Text; !strings.Contains(got, "login: root") || !strings.Contains(got, "заметка") {
		t.Errorf("метаданные не показаны: %q", got)
	}

	a.toggleReveal()
	if got := a.passLabel.Text; got != "charlie-pass" {
		t.Errorf("после «Показать» = %q", got)
	}
	a.toggleReveal()

	a.copyPassword()
	if mem.value != "charlie-pass" {
		t.Errorf("в буфере %q, want charlie-pass", mem.value)
	}
	if !strings.Contains(a.status.Text, "буфере обмена") {
		t.Errorf("статус = %q", a.status.Text)
	}

	a.copyAll()
	if !strings.Contains(mem.value, "login: root") {
		t.Errorf("в буфере %q, want всю запись", mem.value)
	}

	// Без выбранной записи копировать нечего.
	a.current = nil
	mem.value = ""
	a.copyPassword()
	if mem.value != "" {
		t.Errorf("скопировалось без выбранной записи: %q", mem.value)
	}
}

func TestAppSaveStoresEntry(t *testing.T) {
	store, inserted := fakeStore(t, map[string]string{"personal/mail": "alpha-pass\n"})

	mem := &memClipboard{}
	a, err := New(test.NewTempApp(t), Options{
		Store: store,
		Clip:  clipboard.NewWithClipboard(mem, time.Minute),
		Sync:  true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Window().Resize(fyne.NewSize(1040, 680))
	a.Window().Show()
	if len(a.visible) != 1 {
		t.Fatalf("исходный список: %+v", a.visible)
	}

	a.save("github/new", "fresh-pass\nlogin: me")
	if len(a.visible) != 2 || a.selected != "github/new" {
		t.Fatalf("после сохранения список=%+v selected=%q", a.visible, a.selected)
	}

	b, err := os.ReadFile(inserted)
	if err != nil {
		t.Fatalf("pass не получил данные: %v", err)
	}
	if !strings.Contains(string(b), "fresh-pass") || !strings.Contains(string(b), "login: me") {
		t.Errorf("в pass ушло %q", string(b))
	}
}

func TestAppRendersSnapshot(t *testing.T) {
	a, _ := newTestApp(t)
	a.selectByName("dev/github")
	if a.current == nil {
		t.Fatal("пароль не расшифрован")
	}
	a.toggleReveal()

	img := a.Window().Canvas().Capture()
	if img.Bounds().Dx() < 600 || img.Bounds().Dy() < 400 {
		t.Fatalf("неожиданный размер снимка: %v", img.Bounds())
	}

	if path := os.Getenv("GOPASS_SNAPSHOT"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEntryExists(t *testing.T) {
	a, _ := newTestApp(t)

	if !a.entryExists("personal/mail") {
		t.Error("personal/mail должна существовать")
	}
	if a.entryExists("apps/nope") {
		t.Error("apps/nope не должна существовать")
	}
}
