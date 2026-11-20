package pass

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeExecutable writes a shell script with the given body and 0755 mode.
func fakeExecutable(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLookupBinaryOverride(t *testing.T) {
	bin := fakeExecutable(t, `echo "pass 1.7"`)

	got, err := LookupBinary(bin)
	if err != nil {
		t.Fatalf("LookupBinary: %v", err)
	}
	if got != bin {
		t.Errorf("LookupBinary = %q, want %q", got, bin)
	}

	if _, err := LookupBinary(filepath.Join(t.TempDir(), "nope")); !errors.Is(err, ErrNoBinary) {
		t.Errorf("несуществующий путь: err = %v, want ErrNoBinary", err)
	}
}

func TestValidateBinary(t *testing.T) {
	good := fakeExecutable(t, `[ "$1" = "--version" ] && echo "pass v1.7.4"`)

	if err := ValidateBinary(good); err != nil {
		t.Errorf("ValidateBinary(валидный) = %v", err)
	}

	broken := fakeExecutable(t, `exit 3`)
	if err := ValidateBinary(broken); err == nil {
		t.Error("ожидалась ошибка для скрипта, который не отвечает --version")
	}

	dir := t.TempDir()
	if err := ValidateBinary(dir); err == nil {
		t.Error("ожидалась ошибка для каталога")
	}

	notExec := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(notExec, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBinary(notExec); err == nil {
		t.Error("ожидалась ошибка для файла без бита исполнения")
	}

	if err := ValidateBinary(""); err == nil {
		t.Error("ожидалась ошибка для пустого пути")
	}
}

func TestStoreWithoutBinary(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if s.HasBinary() {
		t.Fatal("HasBinary должен быть false")
	}
	if _, err := s.Show(context.Background(), "a/b"); !errors.Is(err, ErrNoBinary) {
		t.Errorf("Show без pass: err = %v, want ErrNoBinary", err)
	}
	if _, err := s.List(); err != nil {
		t.Errorf("List не должен требовать pass: %v", err)
	}
}

func TestGitStateOf(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git не установлен")
	}

	dir := t.TempDir()
	if state := GitStateOf(dir); !state.Available || state.Repository {
		t.Errorf("до git init: %+v", state)
	}

	cmd := exec.Command("git", "-C", dir, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if state := GitStateOf(dir); !state.Available || !state.Repository {
		t.Errorf("после git init: %+v", state)
	}
}

// TestInitGit проверяет `pass git init` через заглушку pass, которая
// делегирует подкоманду git настоящему git.
func TestInitGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git не установлен")
	}
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	storeDir := t.TempDir()
	fake := fakeExecutable(t, `[ "$1" = "git" ] && [ "$2" = "init" ] || exit 1
git -C "$PASSWORD_STORE_DIR" init -q`)
	if err := os.WriteFile(filepath.Join(storeDir, ".gpg-id"), []byte("ABC\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &Store{Binary: fake, Dir: storeDir, Timeout: 10 * time.Second}
	warning, err := store.InitGit(context.Background())
	if err != nil {
		t.Fatalf("InitGit: %v", err)
	}
	if warning != "" {
		t.Errorf("неожиданное предупреждение: %q", warning)
	}

	if _, err := os.Stat(filepath.Join(storeDir, ".git")); err != nil {
		t.Fatalf("репозиторий не создан: %v", err)
	}
	logCmd := exec.Command("git", "-C", storeDir, "log", "--oneline")
	out, err := logCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Initialize password store") {
		t.Errorf("нет первого коммита: %q", out)
	}
	if state := GitStateOf(storeDir); !state.Repository {
		t.Error("GitStateOf не видит созданный репозиторий")
	}
}
