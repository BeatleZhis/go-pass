package pass

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakePass writes a shell script that behaves like `pass` for the two commands
// the wrapper uses. It records arguments and stdin so tests can assert on them.
func fakePass(t *testing.T, script string) (bin, argsFile, stdinFile string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "pass")
	argsFile = filepath.Join(dir, "args")
	stdinFile = filepath.Join(dir, "stdin")

	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\n" + script + "\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake pass: %v", err)
	}
	t.Setenv("FAKE_STDIN_FILE", stdinFile)
	return bin, argsFile, stdinFile
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func TestNewResolvesStoreFromEnv(t *testing.T) {
	store := t.TempDir()
	t.Setenv("PASSWORD_STORE_DIR", store)

	bin, _, _ := fakePass(t, "exit 0")
	t.Setenv("PASS_BINARY", bin) // явный путь важнее того, что найдётся в PATH

	s, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.Dir != store {
		t.Errorf("Dir = %q, want %q", s.Dir, store)
	}
	if s.Binary != bin {
		t.Errorf("Binary = %q, want %q", s.Binary, bin)
	}
}

func TestNewFailsOnMissingStore(t *testing.T) {
	t.Setenv("PASSWORD_STORE_DIR", filepath.Join(t.TempDir(), "does-not-exist"))
	bin, _, _ := fakePass(t, "exit 0")
	t.Setenv("PASS_BINARY", bin)

	if _, err := New(); err == nil {
		t.Fatal("ожидалась ошибка о отсутствующем хранилище")
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("dev/github.gpg")
	write("personal/mail.gpg")
	write("cloud/aws/root.gpg")
	write(".hidden.gpg") // скрытые файлы pass не показывает
	write(".git/config") // служебный каталог
	write("notes.txt")   // не .gpg
	write("weird.GPG")   // регистрозависимое расширение

	s := &Store{Binary: "/bin/true", Dir: root}
	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	want := []string{"personal/mail", "dev/github", "cloud/aws/root"}
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("List = %v, want %v", names, want)
	}
}

func TestShow(t *testing.T) {
	bin, argsFile, _ := fakePass(t, `cat <<'EOF'
s3cret-password
login: user@example.com
url: https://example.com

Заметка в две строки
вторая строка
EOF`)

	s := &Store{Binary: bin, Dir: t.TempDir()}
	secret, err := s.Show(context.Background(), "dev/github")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}

	if secret.Password != "s3cret-password" {
		t.Errorf("Password = %q", secret.Password)
	}
	if len(secret.Fields) != 2 {
		t.Fatalf("Fields = %+v, want 2", secret.Fields)
	}
	if secret.Fields[0].Key != "login" || secret.Fields[0].Value != "user@example.com" {
		t.Errorf("поле login разобрано неверно: %+v", secret.Fields[0])
	}
	if secret.Fields[1].Key != "url" || secret.Fields[1].Value != "https://example.com" {
		t.Errorf("поле url разобрано неверно: %+v", secret.Fields[1])
	}
	if want := "Заметка в две строки\nвторая строка"; secret.Notes != want {
		t.Errorf("Notes = %q, want %q", secret.Notes, want)
	}

	args := strings.Fields(readFile(t, argsFile))
	if len(args) != 2 || args[0] != "show" || args[1] != "dev/github" {
		t.Errorf("аргументы pass = %v", args)
	}
}

func TestShowNotFound(t *testing.T) {
	bin, _, _ := fakePass(t, `echo "Error: github/nope is not in the password store." >&2; exit 1`)
	s := &Store{Binary: bin, Dir: t.TempDir()}

	_, err := s.Show(context.Background(), "github/nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestInsertPassesContentOnStdin(t *testing.T) {
	bin, argsFile, stdinFile := fakePass(t, `cat > "$FAKE_STDIN_FILE"`)
	s := &Store{Binary: bin, Dir: t.TempDir()}

	if err := s.Insert(context.Background(), "cloud/aws/root", "p@ss\nlogin: root\r\n"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	args := strings.Fields(readFile(t, argsFile))
	want := []string{"insert", "--multiline", "--force", "cloud/aws/root"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("аргументы pass = %v, want %v", args, want)
	}
	if got, want := readFile(t, stdinFile), "p@ss\nlogin: root\n"; got != want {
		t.Errorf("stdin = %q, want %q", got, want)
	}
}

func TestInsertReportsCLIError(t *testing.T) {
	bin, _, _ := fakePass(t, `echo "gpg: decryption failed: No secret key" >&2; exit 2`)
	s := &Store{Binary: bin, Dir: t.TempDir()}

	err := s.Insert(context.Background(), "any", "p@ss")
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if !strings.Contains(err.Error(), "gpg") {
		t.Errorf("в ошибке нет деталей gpg: %v", err)
	}
}

func TestInsertSucceedsWhenOnlyGitCommitFails(t *testing.T) {
	root := t.TempDir()
	// Эмулируем pass: файл записан, но git commit упал.
	script := `name="$4"; mkdir -p "$PASSWORD_STORE_DIR/$(dirname "$name")"; printf 'x' > "$PASSWORD_STORE_DIR/$name.gpg"; echo "fatal: unable to auto-detect email address" >&2; exit 128`
	bin, _, _ := fakePass(t, script)

	s := &Store{Binary: bin, Dir: root}
	if err := s.Insert(context.Background(), "new/entry", "p@ss"); err != nil {
		t.Fatalf("Insert должен считать запись сохранённой: %v", err)
	}
}

func TestParseSecretEmptyPasswordLine(t *testing.T) {
	sec := ParseSecret("x", "\n\nlogin: u\n")
	if sec.Password != "" {
		t.Errorf("Password = %q, want empty", sec.Password)
	}
	if len(sec.Fields) != 0 {
		t.Errorf("Fields = %+v, want none", sec.Fields)
	}
}

func TestParseSecretWithoutMetadata(t *testing.T) {
	sec := ParseSecret("x", "just-a-password\n")
	if sec.Password != "just-a-password" {
		t.Errorf("Password = %q", sec.Password)
	}
	if sec.Notes != "" {
		t.Errorf("Notes = %q, want empty", sec.Notes)
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"dev/github", "personal/mail", "a", "cloud/aws/root", "пробел в имени", "dots.in.name"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{"", "   ", "/abs/path", "~/home", "a//b", "a/../b", "..", ".", ".hidden/x", "a/b/", "with\nnewline", "x\x00y"}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", name)
		}
	}
}

func TestSecretExtra(t *testing.T) {
	raw := "p@ss\nlogin: user\nurl: https://example.com\n\nзаметка\nвторая строка\n"
	sec := ParseSecret("demo", raw)

	want := "login: user\nurl: https://example.com\n\nзаметка\nвторая строка"
	if got := sec.Extra(); got != want {
		t.Errorf("Extra() = %q, want %q", got, want)
	}

	// Запись без метаданных: остаток пустой.
	if got := ParseSecret("x", "only-pass\n").Extra(); got != "" {
		t.Errorf("Extra() = %q, want empty", got)
	}

	// CRLF нормализуется, хвостовые переводы строк отбрасываются.
	if got := ParseSecret("x", "p\r\nlogin: u\r\n").Extra(); got != "login: u" {
		t.Errorf("Extra() с CRLF = %q", got)
	}
}

func TestStoreDelete(t *testing.T) {
	storeDir := t.TempDir()
	entryPath := filepath.Join(storeDir, "personal", "mail.gpg")
	if err := os.MkdirAll(filepath.Dir(entryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryPath, []byte("ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}

	argsLog := filepath.Join(t.TempDir(), "args.txt")
	bin := filepath.Join(t.TempDir(), "pass")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" > '" + argsLog + "'\n" +
		"if [ \"$1\" = \"rm\" ] && [ \"$2\" = \"--force\" ] && [ -n \"$3\" ]; then\n" +
		"  rm -f \"$PASSWORD_STORE_DIR/$3.gpg\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	s := &Store{Binary: bin, Dir: storeDir, Timeout: 5 * time.Second}
	ctx := context.Background()

	for _, bad := range []string{"", "../secret", "/abs/path", "dir/.hidden"} {
		if err := s.Delete(ctx, bad); err == nil {
			t.Errorf("Delete(%q) не вернул ошибку валидации", bad)
		}
	}

	if err := s.Delete(ctx, "personal/mail"); err != nil {
		t.Fatalf("Delete(personal/mail): %v", err)
	}
	if _, err := os.Stat(entryPath); !os.IsNotExist(err) {
		t.Fatalf("файл записи должен быть удалён, stat err = %v", err)
	}
	gotArgs, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("не прочитан лог аргументов: %v", err)
	}
	if got := strings.TrimSpace(string(gotArgs)); got != "rm --force personal/mail" {
		t.Errorf("аргументы pass = %q, want %q", got, "rm --force personal/mail")
	}

	if err := s.Delete(ctx, "personal/mail"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete несуществующей записи = %v, want ErrNotFound", err)
	}
}

func TestStoreDeleteGitCommitFailure(t *testing.T) {
	t.Run("игнорирует ошибку git если файл уже удалён", func(t *testing.T) {
		storeDir := t.TempDir()
		entryPath := filepath.Join(storeDir, "dev", "token.gpg")
		if err := os.MkdirAll(filepath.Dir(entryPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(entryPath, []byte("ciphertext"), 0o600); err != nil {
			t.Fatal(err)
		}

		bin := filepath.Join(t.TempDir(), "pass")
		script := "#!/bin/sh\n" +
			"rm -f \"$PASSWORD_STORE_DIR/$3.gpg\"\n" +
			"echo \"fatal: unable to auto-detect email address\" >&2\n" +
			"exit 128\n"
		if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}

		s := &Store{Binary: bin, Dir: storeDir, Timeout: 5 * time.Second}
		if err := s.Delete(context.Background(), "dev/token"); err != nil {
			t.Fatalf("ошибка git после удаления файла должна игнорироваться, получено: %v", err)
		}
	})

	t.Run("возвращает ошибку если файл остался на диске", func(t *testing.T) {
		storeDir := t.TempDir()
		entryPath := filepath.Join(storeDir, "dev", "locked.gpg")
		if err := os.MkdirAll(filepath.Dir(entryPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(entryPath, []byte("ciphertext"), 0o600); err != nil {
			t.Fatal(err)
		}

		bin := filepath.Join(t.TempDir(), "pass")
		script := "#!/bin/sh\n" +
			"echo \"permission denied\" >&2\n" +
			"exit 1\n"
		if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}

		s := &Store{Binary: bin, Dir: storeDir, Timeout: 5 * time.Second}
		err := s.Delete(context.Background(), "dev/locked")
		if err == nil {
			t.Fatal("ожидалась ошибка, так как файл не был удалён")
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("неожиданное сообщение об ошибке: %v", err)
		}
	})
}

func TestStoreDeletePrunesEmptyParents(t *testing.T) {
	storeDir := t.TempDir()
	deepEntry := filepath.Join(storeDir, "cloud", "aws", "prod", "root.gpg")
	siblingEntry := filepath.Join(storeDir, "cloud", "gcp.gpg")

	for _, p := range []string{deepEntry, siblingEntry} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("ciphertext"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	bin := filepath.Join(t.TempDir(), "pass")
	script := "#!/bin/sh\nrm -f \"$PASSWORD_STORE_DIR/$3.gpg\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	s := &Store{Binary: bin, Dir: storeDir, Timeout: 5 * time.Second}
	ctx := context.Background()

	if err := s.Delete(ctx, "cloud/aws/prod/root"); err != nil {
		t.Fatalf("Delete(cloud/aws/prod/root): %v", err)
	}
	for _, removed := range []string{
		filepath.Join(storeDir, "cloud", "aws", "prod"),
		filepath.Join(storeDir, "cloud", "aws"),
	} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Errorf("пустая папка %q должна быть удалена", removed)
		}
	}
	if _, err := os.Stat(filepath.Join(storeDir, "cloud")); err != nil {
		t.Errorf("непустая папка cloud должна остаться: %v", err)
	}

	if err := s.Delete(ctx, "cloud/gcp"); err != nil {
		t.Fatalf("Delete(cloud/gcp): %v", err)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "cloud")); !os.IsNotExist(err) {
		t.Errorf("опустевшая папка cloud должна быть удалена")
	}
	if _, err := os.Stat(storeDir); err != nil {
		t.Fatalf("корневой каталог хранилища не должен удаляться: %v", err)
	}
}
