package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/beatlezhis/go-pass/internal/clipboard"
	"github.com/beatlezhis/go-pass/internal/pass"
)

func TestApplySavedBinary(t *testing.T) {
	store, _ := fakeStore(t, map[string]string{"personal/mail": "x\n"})
	bin := store.Binary
	store.Binary = "" // как будто pass не найден в PATH

	fyneApp := test.NewTempApp(t)
	fyneApp.Preferences().SetString(prefPassBinary, bin)

	a, err := New(fyneApp, Options{
		Store: store,
		Clip:  clipboard.NewWithClipboard(&memClipboard{}, 0),
		Sync:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !a.store.HasBinary() {
		t.Fatal("сохранённый путь к pass не восстановлен")
	}
	if a.store.Binary != bin {
		t.Errorf("Binary = %q, want %q", a.store.Binary, bin)
	}
}

func TestApplySavedBinaryDropsInvalidPath(t *testing.T) {
	store, _ := fakeStore(t, map[string]string{"personal/mail": "x\n"})
	store.Binary = ""

	fyneApp := test.NewTempApp(t)
	fyneApp.Preferences().SetString(prefPassBinary, "/nope/pass")

	a, err := New(fyneApp, Options{
		Store: store,
		Clip:  clipboard.NewWithClipboard(&memClipboard{}, 0),
		Sync:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.store.HasBinary() {
		t.Error("невалидный сохранённый путь не должен применяться")
	}
	if got := fyneApp.Preferences().String(prefPassBinary); got != "" {
		t.Errorf("невалидный путь не очищен из preferences: %q", got)
	}
}

func TestApplyGitState(t *testing.T) {
	a, _ := newTestApp(t)

	a.applyGitState(pass.GitState{})
	if got := a.gitBtn.Text; got != "git: нет" {
		t.Errorf("без git: подпись %q", got)
	}
	if !a.gitBtn.Disabled() {
		t.Error("без git кнопка должна быть неактивна")
	}

	a.applyGitState(pass.GitState{Available: true})
	if got := a.gitBtn.Text; got != "git: нет" {
		t.Errorf("без репозитория: подпись %q", got)
	}
	if a.gitBtn.Disabled() {
		t.Error("без репозитория кнопка должна быть кликабельной")
	}

	a.applyGitState(pass.GitState{Available: true, Repository: true})
	if got := a.gitBtn.Text; got != "git ✓" {
		t.Errorf("с репозиторием: подпись %q", got)
	}
	if !a.gitBtn.Disabled() {
		t.Error("с репозиторием кнопка должна быть неактивна")
	}
}

func TestCheckEnvironmentAsksForPass(t *testing.T) {
	store, _ := fakeStore(t, map[string]string{"personal/mail": "x\n"})
	bin := store.Binary
	store.Binary = ""

	a, err := New(test.NewTempApp(t), Options{
		Store: store,
		Clip:  clipboard.NewWithClipboard(&memClipboard{}, 0),
		Sync:  true,
	})
	if err != nil {
		t.Fatal(err)
	}

	a.checkEnvironment()
	if a.missingDlg == nil {
		t.Fatal("ожидался диалог про отсутствующий pass")
	}
	if !strings.Contains(a.status.Text, "pass") {
		t.Errorf("статус = %q, ожидалось сообщение про pass", a.status.Text)
	}

	// Путь найден — диалог закрывается, режим снова pass.
	a.store.Binary = bin
	a.afterBinaryReady("pass найден")
	if a.missingDlg != nil {
		t.Error("диалог должен закрыться после выбора pass")
	}
	if strings.Contains(a.status.Text, "не найден") {
		t.Errorf("статус после выбора pass = %q", a.status.Text)
	}
}
