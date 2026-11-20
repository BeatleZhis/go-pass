package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/beatlezhis/go-pass/internal/clipboard"
	"github.com/beatlezhis/go-pass/internal/pass"
)

func TestLockSessionClearsKeyCacheAndSelection(t *testing.T) {
	kr, err := pass.OpenKeyring(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta, err := kr.GenerateAndSave("Test User", "test@example.com", "master-123")
	if err != nil {
		t.Fatal(err)
	}

	storeDir := filepath.Join(t.TempDir(), "store")
	if err := pass.InitStore(storeDir, meta.Fingerprint); err != nil {
		t.Fatal(err)
	}
	store := &pass.Store{Dir: storeDir, Keyring: kr}
	if err := store.Insert(t.Context(), "work/vpn", "secret-pass\n"); err != nil {
		t.Fatal(err)
	}

	fyneApp := test.NewTempApp(t)
	clip := clipboard.NewWithClipboard(&memClipboard{}, 0)
	a, err := New(fyneApp, Options{Store: store, Clip: clip, Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	a.Window().Resize(fyne.NewSize(900, 600))
	a.Window().Show()

	// Ключ разблокирован после GenerateAndSave — запись должна открыться.
	a.selectByName("work/vpn")
	if a.current == nil || a.current.Password != "secret-pass" {
		t.Fatalf("ожидалось открытие записи, current=%+v", a.current)
	}

	// Блокируем сессию.
	a.lockSession()

	if kr.IsUnlocked() {
		t.Error("ключ в keyring должен быть заблокирован после lockSession")
	}
	if len(a.cache) != 0 {
		t.Errorf("кеш секретов должен быть очищен, размер=%d", len(a.cache))
	}
	if a.selected != "" || a.current != nil {
		t.Errorf("выделение должно быть сброшено, selected=%q", a.selected)
	}
	if !strings.Contains(a.status.Text, "заблокировано") {
		t.Errorf("неожиданный статус после блокировки: %q", a.status.Text)
	}
}
