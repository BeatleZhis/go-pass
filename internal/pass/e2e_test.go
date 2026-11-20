package pass

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestE2EPassAndGit is an end-to-end check against the real pass and gpg
// binaries. It is enabled by GOPASS_E2E=1 (CI job "e2e") and creates a
// throwaway GNUPGHOME and password store.
func TestE2EPassAndGit(t *testing.T) {
	if os.Getenv("GOPASS_E2E") == "" {
		t.Skip("GOPASS_E2E не задан")
	}
	requireBinary(t, "gpg")
	requireBinary(t, "git")

	passBin, err := LookupBinary("")
	if err != nil {
		t.Skipf("pass не установлен: %v", err)
	}

	// gpg-agent на macOS не умеет сокеты с длинным путём, поэтому home короткий.
	gpgHome, err := os.MkdirTemp("/tmp", "gpe2e")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(gpgHome) })
	t.Setenv("GNUPGHOME", gpgHome)

	gpg := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("gpg", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gpg %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	// Агент нужен для операций с приватным ключом (best-effort).
	_ = exec.Command("gpgconf", "--launch", "gpg-agent").Run()

	gpg("--batch", "--passphrase", "", "--quick-gen-key",
		"E2E User <e2e@example.com>", "default", "default", "never")

	fingerprint := ""
	for _, line := range strings.Split(gpg("--with-colons", "--list-keys", "e2e@example.com"), "\n") {
		if strings.HasPrefix(line, "fpr:") {
			fingerprint = strings.Split(line, ":")[9]
			break
		}
	}
	if fingerprint == "" {
		t.Fatal("не удалось получить fingerprint ключа")
	}

	storeDir := filepath.Join(t.TempDir(), "store")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, ".gpg-id"), []byte(fingerprint+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PASSWORD_STORE_DIR", storeDir)

	// git-идентичность для первого коммита.
	t.Setenv("GIT_AUTHOR_NAME", "E2E")
	t.Setenv("GIT_AUTHOR_EMAIL", "e2e@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "E2E")
	t.Setenv("GIT_COMMITTER_EMAIL", "e2e@example.com")

	store := &Store{Binary: passBin, Dir: storeDir, Timeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	warning, err := store.InitGit(ctx)
	if err != nil {
		t.Fatalf("InitGit: %v", err)
	}
	if warning != "" {
		t.Logf("предупреждение InitGit: %s", warning)
	}

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", storeDir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	commitCount := func() int {
		n, err := strconv.Atoi(strings.TrimSpace(git("rev-list", "--count", "HEAD")))
		if err != nil {
			t.Fatalf("rev-list: %v", err)
		}
		return n
	}

	baseCommits := commitCount()

	// 1. Запись, созданная приложением, видна pass и закоммичена.
	if err := store.Insert(ctx, "work/vpn", "s3cret-from-app\nlogin: e2e"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	passShow := func(name string) (string, error) {
		cmd := exec.Command(passBin, "show", name)
		out, err := cmd.Output()
		return string(out), err
	}
	if got, err := passShow("work/vpn"); err != nil || !strings.Contains(got, "s3cret-from-app") {
		t.Fatalf("pass show после Insert: %q, err=%v", got, err)
	}
	if n := commitCount(); n != baseCommits+1 {
		t.Errorf("коммитов после Insert = %d, want %d", n, baseCommits+1)
	}
	if status := strings.TrimSpace(git("status", "--porcelain")); status != "" {
		t.Errorf("рабочее дерево грязное после Insert: %q", status)
	}

	// 2. Запись, созданная pass, читается приложением.
	cmd := exec.Command(passBin, "insert", "--multiline", "--force", "personal/mail")
	cmd.Stdin = strings.NewReader("from-pass-cli\nlogin: mail\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pass insert: %v\n%s", err, out)
	}
	sec, err := store.Show(ctx, "personal/mail")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if sec.Password != "from-pass-cli" {
		t.Errorf("пароль из pass = %q", sec.Password)
	}
	if sec.Extra() != "login: mail" {
		t.Errorf("метаданные из pass = %q", sec.Extra())
	}

	// 3. Удаление закоммичено и запись исчезает для pass.
	beforeDelete := commitCount()
	if err := store.Delete(ctx, "work/vpn"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := passShow("work/vpn"); err == nil {
		t.Error("pass show не должен находить удалённую запись")
	}
	if n := commitCount(); n != beforeDelete+1 {
		t.Errorf("коммитов после Delete = %d, want %d", n, beforeDelete+1)
	}
	if status := strings.TrimSpace(git("status", "--porcelain")); status != "" {
		t.Errorf("рабочее дерево грязное после Delete: %q", status)
	}
}

func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s не установлен", name)
	}
}
