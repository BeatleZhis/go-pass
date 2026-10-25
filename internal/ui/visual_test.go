package ui

import (
	"image/png"
	"os"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/beatlezhis/go-pass/internal/clipboard"
)

// TestVisualStates сохраняет снимки интерфейса, если задан GOPASS_SNAPSHOT_DIR.
// Это ручной инструмент для визуальной проверки вёрстки.
func TestVisualStates(t *testing.T) {
	dir := os.Getenv("GOPASS_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("GOPASS_SNAPSHOT_DIR не задан")
	}

	store, _ := fakeStore(t, map[string]string{
		"personal/mail":          "alpha-pass\nlogin: max\n",
		"personal/messenger":     "tango-pass\nlogin: +79990000000\n",
		"dev/github":        "bravo-pass\nlogin: octo\nurl: https://github.com\n",
		"cloud/aws/root": "charlie-pass\nlogin: root\nurl: https://aws.example\n\nroot-доступ, менять только через панель\n",
		"cloud/aws/deploy":  "delta-pass\n",
		"dev/gitlab/app":     "echo-pass\nlogin: alice\n",
		"dev/gitlab/vpn":        "foxtrot-pass\nlogin: vpn-user\n",
		"work/vpn/alice":     "golf-pass\n",
		"bank/admin":       "hotel-pass\n",
	})

	a, err := New(test.NewTempApp(t), Options{
		Store: store,
		Clip:  clipboard.NewWithClipboard(&memClipboard{}, 0),
		AppID: "test.gopass",
		Sync:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	a.Window().Resize(fyne.NewSize(920, 600))
	a.Window().Show()
	if len(a.visible) != 9 {
		t.Fatalf("список: %+v", a.visible)
	}

	save := func(name string) {
		f, err := os.Create(dir + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, a.Window().Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}

	save("01-tree.png")

	a.selectByName("cloud/aws/root")
	if a.current == nil {
		t.Fatal("пароль не расшифрован")
	}
	save("02-masked.png")

	a.toggleReveal()
	save("03-revealed.png")
	a.toggleReveal()

	a.search.SetText("vpn")
	save("04-search-vpn.png")

	a.search.SetText("нет-такого")
	save("05-noresults.png")
	a.search.SetText("")

	a.showAddDialog()
	save("06-add-dialog.png")
}
