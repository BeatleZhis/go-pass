package clipboard

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// memClipboard is an in-memory Clipboard for tests.
type memClipboard struct {
	mu    sync.Mutex
	value string
	sets  int
	err   error
}

func (m *memClipboard) Set(text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.value = text
	m.sets++
	return nil
}

func (m *memClipboard) Get() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.value, m.err
}

func (m *memClipboard) text() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.value
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("условие не выполнилось за отведённое время")
}

func TestCopyThenClear(t *testing.T) {
	mem := &memClipboard{}
	cleared := make(chan struct{}, 1)

	m := NewWithClipboard(mem, 30*time.Millisecond)
	m.OnClear = func() { cleared <- struct{}{} }

	if err := m.Copy("s3cret"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if got := mem.text(); got != "s3cret" {
		t.Fatalf("clipboard = %q", got)
	}

	select {
	case <-cleared:
	case <-time.After(time.Second):
		t.Fatal("буфер обмена не был очищен")
	}
	if got := mem.text(); got != "" {
		t.Errorf("clipboard = %q, want empty", got)
	}
}

func TestClearKeepsForeignContent(t *testing.T) {
	mem := &memClipboard{}
	m := NewWithClipboard(mem, 40*time.Millisecond)

	if err := m.Copy("our-secret"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	// Пользователь скопировал что-то своё — затирать это нельзя.
	if err := mem.Set("user-data"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(120 * time.Millisecond)
	if got := mem.text(); got != "user-data" {
		t.Errorf("clipboard = %q, want user-data", got)
	}
}

func TestCopyRestartsTimer(t *testing.T) {
	mem := &memClipboard{}
	m := NewWithClipboard(mem, 60*time.Millisecond)

	if err := m.Copy("first"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := m.Copy("second"); err != nil {
		t.Fatal(err)
	}

	// Первый таймер не должен очистить значение, скопированное позже.
	time.Sleep(30 * time.Millisecond)
	if got := mem.text(); got != "second" {
		t.Fatalf("clipboard = %q, want second", got)
	}

	waitFor(t, time.Second, func() bool { return mem.text() == "" })
}

func TestZeroTimeoutKeepsValue(t *testing.T) {
	mem := &memClipboard{}
	m := NewWithClipboard(mem, 0)

	if err := m.Copy("stays"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := mem.text(); got != "stays" {
		t.Errorf("clipboard = %q, want stays", got)
	}
}

func TestCopyErrors(t *testing.T) {
	mem := &memClipboard{err: errors.New("boom")}
	m := NewWithClipboard(mem, 0)

	if err := m.Copy("x"); err == nil {
		t.Fatal("ожидалась ошибка буфера обмена")
	}
	if err := m.Copy(""); err == nil {
		t.Fatal("ожидалась ошибка на пустом тексте")
	}
}

func TestUnavailableClipboard(t *testing.T) {
	m := NewUnavailable(errors.New("нет pbcopy"))
	if err := m.Copy("x"); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if m.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0", m.Timeout)
	}
}
