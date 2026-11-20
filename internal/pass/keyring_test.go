package pass

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStandaloneStoreEndToEnd(t *testing.T) {
	kr, err := OpenKeyring(t.TempDir())
	if err != nil {
		t.Fatalf("OpenKeyring: %v", err)
	}

	meta, err := kr.GenerateAndSave("Leonid", "leonid@example.com", "strong-master-pass")
	if err != nil {
		t.Fatalf("GenerateAndSave: %v", err)
	}

	storeDir := filepath.Join(t.TempDir(), "password-store")
	if err := InitStore(storeDir, meta.Fingerprint); err != nil {
		t.Fatalf("InitStore: %v", err)
	}

	// Обратите внимание: Binary намеренно пустой — CLI pass не используется вообще.
	store := &Store{
		Dir:     storeDir,
		Keyring: kr,
	}
	ctx := context.Background()

	// 1. Создание записи.
	if err := store.Insert(ctx, "work/github", "gh-secret-token\nlogin: deleo547"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// 2. Блокировка связки ключей должна запрещать расшифровку.
	kr.Lock()
	if _, err := store.Show(ctx, "work/github"); !errors.Is(err, ErrLocked) {
		t.Fatalf("ожидалась ErrLocked при заблокированном ключе, получено: %v", err)
	}

	// 3. Разблокировка мастер-паролем возвращает доступ к записи.
	if err := kr.Unlock("strong-master-pass"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	sec, err := store.Show(ctx, "work/github")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if sec.Password != "gh-secret-token" || sec.Extra() != "login: deleo547" {
		t.Errorf("неожиданное содержимое секрета: %+v", sec)
	}

	// 4. Удаление записи и пустой папки work.
	if err := store.Delete(ctx, "work/github"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "work")); !os.IsNotExist(err) {
		t.Error("опустевшая папка work должна быть удалена")
	}
}
