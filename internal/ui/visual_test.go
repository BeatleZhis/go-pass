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
// Это ручной инструмент для визуальной проверки вёрстки; все данные вымышлены.
func TestVisualStates(t *testing.T) {
	dir := os.Getenv("GOPASS_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("GOPASS_SNAPSHOT_DIR не задан")
	}

	store, _ := fakeStore(t, map[string]string{
		"personal/mail":      "Kf7$mQ2pLx9!aR4z\nlogin: alice@example.com\nurl: https://mail.example.com\n",
		"personal/messenger": "Wq3#nB8vT1@sY6dH\nlogin: +15550000000\n",
		"work/vpn":           "Zr5%tG6kP2&uJ8wE\nlogin: vpn-user\nurl: https://vpn.example.com\n",
		"cloud/aws/root":     "Yn4!cV7bN1@mK5xS\nlogin: root\nurl: https://console.example.com\n\nДоступ только через bastion\n",
		"cloud/aws/deploy":   "Hj8&dF3gL6^pR2tY\nlogin: deploy-bot\n",
		"dev/github":         "Pl9@kM4nB7#qW3zX\nlogin: octocat\nurl: https://github.com\n",
		"dev/gitlab/runner":  "Cd2$fH6jK8%nS1vG\nlogin: runner\n",
		"homelab/nas/admin":  "Bt6^gJ3mN9&pQ4wR\nlogin: admin\n\nПароль меняется раз в квартал\n",
		"social/mastodon":    "Vx1!zC5vB8@nM2kJ\nlogin: alice@example.social\n",
	})

	newApp := func() *App {
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
		return a
	}

	save := func(a *App, name string) {
		f, err := os.Create(dir + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, a.Window().Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}

	a := newApp()
	save(a, "01-tree.png")

	a.selectByName("cloud/aws/root")
	if a.current == nil {
		t.Fatal("пароль не расшифрован")
	}
	save(a, "02-masked.png")

	a.toggleReveal()
	save(a, "03-revealed.png")
	a.toggleReveal()

	a.search.SetText("aws")
	save(a, "04-search.png")

	a.search.SetText("нет-такого")
	save(a, "05-noresults.png")
	a.search.SetText("")

	a.showAddDialog()
	save(a, "06-add-dialog.png")

	// Диалоги живут в оверлее, поэтому редактор снимаем в отдельном окне.
	b := newApp()
	b.selectByName("cloud/aws/root")
	b.showEditDialog()
	save(b, "07-edit-dialog.png")
}
