// Package clipboard copies text to the system clipboard and wipes it again
// after a timeout, the way `pass -c` does.
package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout is how long a copied secret stays in the clipboard.
const DefaultTimeout = 45 * time.Second

// Clipboard is the minimal system clipboard interface. It is an interface so
// the clearing logic can be tested without touching the real clipboard.
type Clipboard interface {
	Set(text string) error
	Get() (string, error)
}

// Manager copies secrets and schedules their removal.
type Manager struct {
	// Timeout is the delay before the clipboard is wiped. Zero disables the
	// automatic clearing entirely.
	Timeout time.Duration
	// OnClear is called after the clipboard has been wiped.
	OnClear func()

	clip Clipboard

	mu       sync.Mutex
	gen      uint64
	timer    *time.Timer
	lastText string
}

// New returns a manager for the current platform. An error means no clipboard
// backend (pbcopy, wl-copy, xclip, xsel or clip.exe) could be found.
func New(timeout time.Duration) (*Manager, error) {
	clip, err := System()
	if err != nil {
		return nil, err
	}
	return NewWithClipboard(clip, timeout), nil
}

// NewUnavailable returns a manager that is always ready but reports err on
// every copy. It keeps the rest of the application usable on systems without a
// clipboard backend.
func NewUnavailable(err error) *Manager {
	return &Manager{clip: unavailable{err: err}}
}

type unavailable struct{ err error }

func (u unavailable) Set(string) error { return u.err }
func (u unavailable) Get() (string, error) {
	return "", u.err
}

// NewWithClipboard returns a manager backed by an explicit clipboard.
func NewWithClipboard(clip Clipboard, timeout time.Duration) *Manager {
	return &Manager{Timeout: timeout, clip: clip}
}

// Copy puts text into the clipboard and arms the auto-clear timer.
func (m *Manager) Copy(text string) error {
	if text == "" {
		return errors.New("нечего копировать")
	}
	if err := m.clip.Set(text); err != nil {
		return fmt.Errorf("не удалось скопировать в буфер обмена: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.lastText = text
	if m.Timeout <= 0 {
		return nil
	}

	m.gen++
	gen := m.gen
	m.timer = time.AfterFunc(m.Timeout, func() { m.clear(gen) })
	return nil
}

// clear wipes the clipboard if it still holds the text we put there and no
// newer copy happened in the meantime.
func (m *Manager) clear(gen uint64) {
	m.mu.Lock()
	if gen != m.gen {
		m.mu.Unlock()
		return
	}
	want := m.lastText
	m.timer = nil
	m.lastText = ""
	m.mu.Unlock()

	if current, err := m.clip.Get(); err == nil && current != want {
		// Someone copied something else in the meantime: leave it alone.
		return
	}
	if err := m.clip.Set(""); err != nil {
		return
	}
	if m.OnClear != nil {
		m.OnClear()
	}
}

// Stop cancels a pending clear, e.g. when the application quits.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.gen++
	m.lastText = ""
}

// System returns a clipboard implementation for the running platform.
func System() (Clipboard, error) {
	switch runtime.GOOS {
	case "darwin":
		return newCommandClipboard("pbcopy", "pbpaste"), nil
	case "windows":
		return windowsClipboard{}, nil
	case "linux":
		return systemLinux()
	default:
		return nil, fmt.Errorf("буфер обмена не поддерживается на %s", runtime.GOOS)
	}
}

// systemLinux prefers wl-copy on Wayland and falls back to X11 helpers.
func systemLinux() (Clipboard, error) {
	wayland := hasBinary("wl-copy") && hasBinary("wl-paste")
	xclip := hasBinary("xclip")
	xsel := hasBinary("xsel")

	switch {
	case isWaylandSession() && wayland:
		return waylandClipboard{}, nil
	case xclip:
		return xclipClipboard{}, nil
	case wayland:
		return waylandClipboard{}, nil
	case xsel:
		return xselClipboard{}, nil
	}
	return nil, errors.New("не найден wl-copy, xclip или xsel для работы с буфером обмена")
}

func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func isWaylandSession() bool {
	return strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != "" ||
		strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland")
}

// commandClipboard talks to a copy command and a paste command.
type commandClipboard struct {
	copyBin, pasteBin string
}

func newCommandClipboard(copyBin, pasteBin string) Clipboard {
	return &commandClipboard{copyBin: copyBin, pasteBin: pasteBin}
}

func (c *commandClipboard) Set(text string) error {
	cmd := exec.Command(c.copyBin)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %v %s", c.copyBin, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (c *commandClipboard) Get() (string, error) {
	cmd := exec.Command(c.pasteBin)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// waylandClipboard uses wl-copy/wl-paste.
type waylandClipboard struct{}

func (waylandClipboard) Set(text string) error {
	if text == "" {
		// wl-copy needs --clear to drop the selection.
		cmd := exec.Command("wl-copy", "--clear")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("wl-copy --clear: %w", err)
		}
		return nil
	}
	cmd := exec.Command("wl-copy")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("wl-copy: %w", err)
	}
	return nil
}

func (waylandClipboard) Get() (string, error) {
	out, err := exec.Command("wl-paste", "--no-newline").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// xclipClipboard reads and writes the X11 CLIPBOARD selection.
type xclipClipboard struct{}

func (xclipClipboard) Set(text string) error {
	cmd := exec.Command("xclip", "-selection", "clipboard")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xclip: %w", err)
	}
	return nil
}

func (xclipClipboard) Get() (string, error) {
	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// xselClipboard reads and writes the X11 CLIPBOARD selection.
type xselClipboard struct{}

func (xselClipboard) Set(text string) error {
	cmd := exec.Command("xsel", "--clipboard", "--input")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xsel: %w", err)
	}
	return nil
}

func (xselClipboard) Get() (string, error) {
	out, err := exec.Command("xsel", "--clipboard", "--output").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// windowsClipboard uses clip.exe for writing and PowerShell for reading.
type windowsClipboard struct{}

func (windowsClipboard) Set(text string) error {
	cmd := exec.Command("clip.exe")
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clip.exe: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (windowsClipboard) Get() (string, error) {
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard -Raw").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}
