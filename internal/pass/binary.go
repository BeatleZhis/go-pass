package pass

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNoBinary is returned when the pass CLI cannot be located or run.
var ErrNoBinary = errors.New("не найден CLI pass")

// LookupBinary finds the pass CLI. An explicit override wins, then
// $PASS_BINARY, then PATH; finally the usual install locations are probed,
// because GUI applications started from Finder get a minimal PATH that does
// not include Homebrew.
func LookupBinary(override string) (string, error) {
	if override == "" {
		override = os.Getenv("PASS_BINARY")
	}
	if override != "" {
		p, err := exec.LookPath(override)
		if err != nil {
			return "", fmt.Errorf("%w: %q не найден или не исполняемый", ErrNoBinary, override)
		}
		return p, nil
	}

	if p, err := exec.LookPath("pass"); err == nil {
		return p, nil
	}

	candidates := []string{
		"/opt/homebrew/bin/pass",
		"/usr/local/bin/pass",
		"/usr/bin/pass",
		"/home/linuxbrew/.linuxbrew/bin/pass",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "bin", "pass"))
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return c, nil
		}
	}

	return "", fmt.Errorf("%w: установите password-store или укажите путь к pass", ErrNoBinary)
}

// ValidateBinary checks that path points to an executable pass CLI and that it
// answers `--version`. It is used when the user picks the binary manually.
func ValidateBinary(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("путь к pass не указан")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("некорректный путь %q: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("файл %s не найден", abs)
	}
	if info.IsDir() {
		return fmt.Errorf("%s — это каталог, а не исполняемый файл", abs)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s не является исполняемым файлом", abs)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, abs, "--version").CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%s не ответил за 5 секунд", abs)
		}
		return fmt.Errorf("%s не запускается как pass: %v (%s)", abs, err, firstLine(out))
	}
	return nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// HasBinary reports whether the store has a pass CLI path.
func (s *Store) HasBinary() bool {
	return s != nil && s.Binary != ""
}

// SetBinary validates the given path and assigns it to the store.
func (s *Store) SetBinary(path string) error {
	if err := ValidateBinary(path); err != nil {
		return err
	}
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	s.Binary = abs
	return nil
}

// GitState describes git availability for a store directory.
type GitState struct {
	// Available reports whether the git binary is installed.
	Available bool
	// Repository reports whether the directory is inside a git work tree.
	Repository bool
}

// GitStateOf inspects dir. It is intentionally read-only and bounded by a
// short timeout, so it can be called from the UI.
func GitStateOf(dir string) GitState {
	if _, err := exec.LookPath("git"); err != nil {
		return GitState{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return GitState{Available: true}
	}
	return GitState{Available: true, Repository: strings.TrimSpace(string(out)) == "true"}
}

// InitGit creates a git repository in the store through `pass git init` (that
// also writes .gitattributes for gpg diffs) and commits the current files with
// git directly: `pass git add/commit` needs a secure ramdisk, which is fragile
// and unavailable in some environments. A failing initial commit (for example
// when git has no user identity configured) is reported as a warning: the
// repository itself is already usable.
func (s *Store) InitGit(ctx context.Context) (warning string, err error) {
	if !s.HasBinary() {
		return "", ErrNoBinary
	}
	if _, lookErr := exec.LookPath("git"); lookErr != nil {
		return "", errors.New("git не установлен: инициализировать репозиторий невозможно")
	}
	if _, stderr, err := s.run(ctx, nil, "git", "init"); err != nil {
		return "", s.cliError(err, stderr, "git init")
	}

	status, err := s.git(ctx, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) == "" {
		return "", nil // pass git init уже всё закоммитил
	}
	if _, err := s.git(ctx, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := s.git(ctx, "commit", "-m", "Initialize password store"); err != nil {
		return "git-репозиторий создан, но первый коммит не удался: " + strings.TrimSpace(err.Error()), nil
	}
	return "", nil
}

// git runs a git command inside the store directory. Unlike `pass git` it does
// not create a secure ramdisk, which keeps initialization working everywhere.
func (s *Store) git(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", s.Dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %v (%s)", strings.Join(args, " "), err, firstLine(out))
	}
	return string(out), nil
}
