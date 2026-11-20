package ui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/beatlezhis/go-pass/internal/clipboard"
	"github.com/beatlezhis/go-pass/internal/pass"
)

// memClipboard хранит скопированный текст для проверок.
type memClipboard struct {
	value string
}

func (m *memClipboard) Set(text string) error { m.value = text; return nil }
func (m *memClipboard) Get() (string, error)  { return m.value, nil }

// fakeStore создаёт временное хранилище и скрипт-заглушку pass, который умеет
// отдавать заранее заданное содержимое и записывать stdin команды insert.
func fakeStore(t *testing.T, entries map[string]string) (*pass.Store, string) {
	t.Helper()
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	inserted := filepath.Join(dir, "inserted")

	// show отдаёт содержимое .gpg, если оно уже записано (после insert), иначе
	// заранее заданный текст. insert записывает stdin и в файл хранилища, и в
	// файл inserted, чтобы тесты могли проверить оба сценария.
	script := "#!/bin/sh\ncase \"$1\" in\nshow)\n  f='" + storeDir + "'/\"$2.gpg\"\n  if [ -s \"$f\" ]; then cat \"$f\"; else\n    case \"$2\" in\n"
	i := 0
	for name, content := range entries {
		p := filepath.Join(storeDir, filepath.FromSlash(name)+".gpg")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		contentFile := filepath.Join(dir, fmt.Sprintf("content-%d", i))
		if err := os.WriteFile(contentFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		script += fmt.Sprintf("      '%s') cat '%s' ;;\n", name, contentFile)
		i++
	}
	script += "      *) printf 'temp-pass\\nlogin: temp\\n' ;;\n    esac\n  fi ;;\n"
	script += "insert) cat > '" + inserted + "'; name=\"$4\"; mkdir -p '" + storeDir + "'/\"$(dirname \"$name\")\"; cp '" + inserted + "' '" + storeDir + "'/\"$name.gpg\" ;;\n"
	script += "rm) if [ \"$2\" = \"--force\" ] && [ -n \"$3\" ]; then rm -f '" + storeDir + "'/\"$3.gpg\"; else exit 1; fi ;;\n"
	script += "*) exit 1 ;;\nesac\n"

	bin := filepath.Join(dir, "pass")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &pass.Store{Binary: bin, Dir: storeDir, Timeout: 10 * time.Second}, inserted
}

func newTestApp(t *testing.T) (*App, *memClipboard) {
	t.Helper()

	store, _ := fakeStore(t, map[string]string{
		"personal/mail":  "alpha-pass\nlogin: max\n",
		"dev/github":     "bravo-pass\nlogin: octo\nurl: https://example.com\n",
		"cloud/aws/root": "charlie-pass\nlogin: root\n\nзаметка\n",
	})

	mem := &memClipboard{}
	clip := clipboard.NewWithClipboard(mem, time.Minute)

	fyneApp := test.NewTempApp(t)
	a, err := New(fyneApp, Options{Store: store, Clip: clip, AppID: "test.gopass", Sync: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Window().Resize(fyne.NewSize(1040, 680))
	a.Window().Show()
	if len(a.visible) != 3 {
		t.Fatalf("список записей не загрузился: %+v", a.visible)
	}
	return a, mem
}

func TestAppListsAndFilters(t *testing.T) {
	a, _ := newTestApp(t)

	if got := a.visible[0].Name; got != "cloud/aws/root" {
		t.Errorf("первая запись = %q, want cloud/aws/root", got)
	}
	if len(a.existingDirs()) == 0 {
		t.Errorf("папки хранилища не определены")
	}

	a.search.SetText("dev")
	if len(a.visible) != 1 || a.visible[0].Name != "dev/github" {
		t.Fatalf("фильтр по поиску: %+v", a.visible)
	}

	a.search.SetText("нет-такого")
	if len(a.visible) != 0 {
		t.Fatalf("пустой результат поиска: %+v", a.visible)
	}
}

func TestAppRevealsAndCopies(t *testing.T) {
	a, mem := newTestApp(t)

	a.selectByName("cloud/aws/root")
	if a.current == nil || a.current.Name != "cloud/aws/root" {
		t.Fatalf("запись не расшифрована: %+v", a.current)
	}

	if got, want := a.passLabel.Text, strings.Repeat("•", len("charlie-pass")); got != want {
		t.Errorf("пароль не замаскирован: %q", got)
	}
	if got := a.details.Text; !strings.Contains(got, "login: root") || !strings.Contains(got, "заметка") {
		t.Errorf("метаданные не показаны: %q", got)
	}

	a.toggleReveal()
	if got := a.passLabel.Text; got != "charlie-pass" {
		t.Errorf("после «Показать» = %q", got)
	}
	a.toggleReveal()

	a.copyPassword()
	if mem.value != "charlie-pass" {
		t.Errorf("в буфере %q, want charlie-pass", mem.value)
	}
	if !strings.Contains(a.status.Text, "буфере обмена") {
		t.Errorf("статус = %q", a.status.Text)
	}

	a.copyAll()
	if !strings.Contains(mem.value, "login: root") {
		t.Errorf("в буфере %q, want всю запись", mem.value)
	}

	// Без выбранной записи копировать нечего.
	a.current = nil
	mem.value = ""
	a.copyPassword()
	if mem.value != "" {
		t.Errorf("скопировалось без выбранной записи: %q", mem.value)
	}
}

func TestAppSaveStoresEntry(t *testing.T) {
	store, inserted := fakeStore(t, map[string]string{"personal/mail": "alpha-pass\n"})

	mem := &memClipboard{}
	a, err := New(test.NewTempApp(t), Options{
		Store: store,
		Clip:  clipboard.NewWithClipboard(mem, time.Minute),
		Sync:  true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Window().Resize(fyne.NewSize(1040, 680))
	a.Window().Show()
	if len(a.visible) != 1 {
		t.Fatalf("исходный список: %+v", a.visible)
	}

	a.save("dev/new", "fresh-pass\nlogin: me")
	if len(a.visible) != 2 || a.selected != "dev/new" {
		t.Fatalf("после сохранения список=%+v selected=%q", a.visible, a.selected)
	}

	b, err := os.ReadFile(inserted)
	if err != nil {
		t.Fatalf("pass не получил данные: %v", err)
	}
	if !strings.Contains(string(b), "fresh-pass") || !strings.Contains(string(b), "login: me") {
		t.Errorf("в pass ушло %q", string(b))
	}
}

func TestAppRendersSnapshot(t *testing.T) {
	a, _ := newTestApp(t)
	a.selectByName("dev/github")
	if a.current == nil {
		t.Fatal("пароль не расшифрован")
	}
	a.toggleReveal()

	img := a.Window().Canvas().Capture()
	if img.Bounds().Dx() < 600 || img.Bounds().Dy() < 400 {
		t.Fatalf("неожиданный размер снимка: %v", img.Bounds())
	}

	if path := os.Getenv("GOPASS_SNAPSHOT"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEntryExists(t *testing.T) {
	a, _ := newTestApp(t)

	if !a.entryExists("personal/mail") {
		t.Error("personal/mail должна существовать")
	}
	if a.entryExists("personal/nope") {
		t.Error("personal/nope не должна существовать")
	}
}

func TestBuildTreeNodes(t *testing.T) {
	entries := []pass.Entry{
		{Name: "personal/mail"},
		{Name: "personal/messenger"},
		{Name: "dev/github"},
		{Name: "cloud/aws/root"},
		{Name: "cloud/aws/deploy"},
		{Name: "single"},
	}
	nodes := buildTreeNodes(entries)

	root := nodes[""]
	if root == nil {
		t.Fatal("нет корневого узла")
	}
	// Папки идут раньше записей, внутри группы — по алфавиту.
	wantRoot := "cloud,dev,personal,single"
	if got := strings.Join(root.children, ","); got != wantRoot {
		t.Errorf("верхний уровень = %q, want %q", got, wantRoot)
	}

	if got := strings.Join(nodes["personal"].children, ","); got != "personal/mail,personal/messenger" {
		t.Errorf("personal = %q", got)
	}
	if !nodes["personal/mail"].secret {
		t.Error("personal/mail должна быть записью")
	}
	if nodes["personal"].secret {
		t.Error("personal — папка, а не запись")
	}
	if got := strings.Join(nodes["cloud"].children, ","); got != "cloud/aws" {
		t.Errorf("cloud = %q", got)
	}
	if got := strings.Join(nodes["cloud/aws"].children, ","); got != "cloud/aws/deploy,cloud/aws/root" {
		t.Errorf("cloud/aws = %q", got)
	}
	if !nodes["single"].secret || len(nodes["single"].children) != 0 {
		t.Error("single должна быть записью без детей")
	}

	// Запись может быть одновременно папкой (pass это позволяет).
	both := buildTreeNodes([]pass.Entry{{Name: "cloud"}, {Name: "cloud/root"}})
	if !both["cloud"].secret {
		t.Error("cloud должна быть и записью")
	}
	if got := strings.Join(both["cloud"].children, ","); got != "cloud/root" {
		t.Errorf("дети cloud = %q", got)
	}

	if len(buildTreeNodes(nil)[""].children) != 0 {
		t.Error("пустой store должен давать пустой корень")
	}
}

func TestTreeFoldersToggleAndEntriesOpen(t *testing.T) {
	a, _ := newTestApp(t)

	if got := strings.Join(a.nodes[""].children, ","); got != "cloud,dev,personal" {
		t.Fatalf("верхний уровень = %q", got)
	}

	// Клик по папке раскрывает её и не выбирает запись.
	a.tree.Select("cloud")
	if !a.tree.IsBranchOpen("cloud") {
		t.Error("папка не раскрылась")
	}
	if a.selected != "" {
		t.Errorf("папка не должна открывать запись: %q", a.selected)
	}

	// Повторный клик сворачивает.
	a.tree.Select("cloud")
	if a.tree.IsBranchOpen("cloud") {
		t.Error("папка не свернулась")
	}

	// Клик по записи расшифровывает её.
	a.tree.Select("personal/mail")
	if a.current == nil || a.current.Name != "personal/mail" {
		t.Fatalf("запись не открылась: %+v", a.current)
	}
}

func TestTreeFilterExpandsMatches(t *testing.T) {
	a, _ := newTestApp(t)

	a.search.SetText("aws")
	if len(a.visible) != 1 || a.visible[0].Name != "cloud/aws/root" {
		t.Fatalf("фильтр: %+v", a.visible)
	}
	if !a.tree.IsBranchOpen("cloud") || !a.tree.IsBranchOpen("cloud/aws") {
		t.Error("ветки с совпадениями должны раскрыться")
	}
	if _, ok := a.nodes["personal"]; ok {
		t.Error("в отфильтрованном дереве не должно быть personal")
	}

	a.search.SetText("")
	if a.tree.IsBranchOpen("cloud") {
		t.Error("после сброса поиска дерево должно свернуться")
	}
	if len(a.nodes[""].children) != 3 {
		t.Errorf("после сброса поиска верхний уровень = %v", a.nodes[""].children)
	}
}

func TestSelectByNameOpensAncestors(t *testing.T) {
	a, _ := newTestApp(t)

	a.selectByName("cloud/aws/root")
	if !a.tree.IsBranchOpen("cloud") || !a.tree.IsBranchOpen("cloud/aws") {
		t.Error("родительские папки не раскрылись")
	}
	if a.current == nil || a.current.Name != "cloud/aws/root" {
		t.Fatalf("запись не открылась: %+v", a.current)
	}
}

func TestTreeFolderCounts(t *testing.T) {
	entries := []pass.Entry{
		{Name: "personal/mail"},
		{Name: "personal/messenger"},
		{Name: "dev/github"},
		{Name: "cloud/aws/root"},
		{Name: "cloud/aws/deploy"},
		{Name: "single"},
	}
	nodes := buildTreeNodes(entries)

	tests := map[string]int{
		"":               6,
		"personal":       2,
		"personal/mail":  1,
		"dev":            1,
		"cloud":          2,
		"cloud/aws":      2,
		"cloud/aws/root": 1,
		"single":         1,
	}
	for uid, want := range tests {
		if got := nodes[uid].count; got != want {
			t.Errorf("count(%q) = %d, want %d", uid, got, want)
		}
	}
}

func TestTreeRowShowsCount(t *testing.T) {
	a, _ := newTestApp(t)

	obj := a.nodeCreate(true)
	a.nodeUpdate("personal", true, obj)
	_, label := rowParts(obj)
	if label == nil {
		t.Fatal("в строке нет подписи")
	}
	if got := label.Text(); got != "personal (1)" {
		t.Errorf("подпись папки = %q, want %q", got, "personal (1)")
	}

	leaf := a.nodeCreate(false)
	a.nodeUpdate("personal/mail", false, leaf)
	_, leafLabel := rowParts(leaf)
	if got := leafLabel.Text(); got != "mail" {
		t.Errorf("подпись записи = %q, want mail", got)
	}
}

func TestExpandAndCollapseAll(t *testing.T) {
	a, _ := newTestApp(t)

	a.expandAllBranches()
	for _, uid := range []string{"personal", "dev", "cloud", "cloud/aws"} {
		if !a.tree.IsBranchOpen(uid) {
			t.Errorf("ветка %q не раскрыта", uid)
		}
	}

	a.collapseAllBranches()
	for _, uid := range []string{"personal", "dev", "cloud"} {
		if a.tree.IsBranchOpen(uid) {
			t.Errorf("ветка %q не свернута", uid)
		}
	}
}

func TestExpandedBranchesPersist(t *testing.T) {
	fyneApp := test.NewTempApp(t)
	store, _ := fakeStore(t, map[string]string{
		"personal/mail":      "alpha-pass\n",
		"personal/messenger": "tango-pass\n",
		"dev/github":         "bravo-pass\n",
		"cloud/aws/root":     "charlie-pass\n",
	})
	clip := clipboard.NewWithClipboard(&memClipboard{}, 0)

	a, err := New(fyneApp, Options{Store: store, Clip: clip, Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	a.Window().Resize(fyne.NewSize(800, 600))
	a.Window().Show()

	// Пользователь раскрыл «cloud» — состояние должно сохраниться.
	a.tree.Select("cloud")
	if !a.tree.IsBranchOpen("cloud") {
		t.Fatal("ветка не раскрылась")
	}
	stored := fyneApp.Preferences().String(prefOpenBranches)
	if !strings.Contains(stored, "cloud") {
		t.Fatalf("состояние не сохранено: %q", stored)
	}

	// Поиск раскрывает всё, но не должен затирать сохранённое состояние.
	a.search.SetText("aws")
	if got := fyneApp.Preferences().String(prefOpenBranches); got != stored {
		t.Errorf("поиск изменил сохранённое состояние: %q -> %q", stored, got)
	}
	a.search.SetText("")
	if !a.tree.IsBranchOpen("cloud") {
		t.Error("после сброса поиска раскрытая папка потерялась")
	}

	// Новый запуск с теми же preferences открывает ту же папку.
	b, err := New(fyneApp, Options{Store: store, Clip: clip, Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	if !b.tree.IsBranchOpen("cloud") {
		t.Error("состояние дерева не восстановилось при запуске")
	}
}

func TestFilterKeepsUnrelatedOpenFolders(t *testing.T) {
	a, _ := newTestApp(t)

	a.tree.Select("personal")
	a.tree.Select("cloud")
	if !a.tree.IsBranchOpen("personal") || !a.tree.IsBranchOpen("cloud") {
		t.Fatal("папки не раскрылись")
	}

	// В результатах поиска нет открытых веток — состояние всё равно должно
	// пережить поиск.
	a.search.SetText("dev")
	if !a.tree.IsBranchOpen("dev") {
		t.Error("ветка с совпадением не раскрылась")
	}
	a.search.SetText("")
	if !a.tree.IsBranchOpen("personal") || !a.tree.IsBranchOpen("cloud") {
		t.Error("после сброса поиска раскрытые ранее папки потерялись")
	}
	if a.tree.IsBranchOpen("dev") {
		t.Error("ветка, раскрытая только поиском, должна свернуться")
	}
}

func TestTreeToggleButton(t *testing.T) {
	a, _ := newTestApp(t)

	// Изначально дерево свёрнуто — кнопка предлагает раскрыть.
	if got := a.toggleBtn.Text; got != "Развернуть" {
		t.Fatalf("начальная подпись кнопки = %q, want Развернуть", got)
	}

	test.Tap(a.toggleBtn)
	if !a.tree.IsBranchOpen("personal") || !a.tree.IsBranchOpen("cloud/aws") {
		t.Error("кнопка не раскрыла дерево")
	}
	if got := a.toggleBtn.Text; got != "Свернуть" {
		t.Errorf("после раскрытия подпись = %q, want Свернуть", got)
	}

	test.Tap(a.toggleBtn)
	if a.tree.IsBranchOpen("personal") || a.tree.IsBranchOpen("cloud") {
		t.Error("кнопка не свернула дерево")
	}
	if got := a.toggleBtn.Text; got != "Развернуть" {
		t.Errorf("после сворачивания подпись = %q, want Развернуть", got)
	}
}

func TestTreeToggleFollowsManualExpansion(t *testing.T) {
	a, _ := newTestApp(t)

	// Раскрываем все папки вручную — кнопка должна предложить свернуть.
	for _, uid := range []string{"personal", "dev", "cloud", "cloud/aws"} {
		a.tree.Select(uid)
	}
	if !a.allBranchesOpen() {
		t.Fatal("не все папки раскрылись")
	}
	if got := a.toggleBtn.Text; got != "Свернуть" {
		t.Errorf("подпись = %q, want Свернуть", got)
	}

	// Свернули одну папку — снова предлагаем раскрыть.
	a.tree.Select("dev")
	if got := a.toggleBtn.Text; got != "Развернуть" {
		t.Errorf("подпись = %q, want Развернуть", got)
	}
}

func TestEditEntryRoundTrip(t *testing.T) {
	store, inserted := fakeStore(t, map[string]string{
		"personal/mail": "old-pass\nlogin: old\n",
		"dev/github":    "two-pass\n",
	})
	a, err := New(test.NewTempApp(t), Options{
		Store: store,
		Clip:  clipboard.NewWithClipboard(&memClipboard{}, time.Minute),
		Sync:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	a.Window().Resize(fyne.NewSize(900, 600))
	a.Window().Show()

	// Без выбранной записи редактирование недоступно.
	if !a.editBtn.Disabled() {
		t.Error("кнопка редактирования должна быть неактивна без выбора")
	}

	a.selectByName("personal/mail")
	if a.current == nil {
		t.Fatal("запись не расшифрована")
	}
	if a.editBtn.Disabled() {
		t.Error("кнопка редактирования должна быть активна для выбранной записи")
	}
	if got := a.current.Extra(); got != "login: old" {
		t.Fatalf("Extra() = %q", got)
	}

	if err := a.applyEdit("personal/mail", "new-Pass-42", "login: edited\nurl: https://example.org"); err != nil {
		t.Fatalf("applyEdit: %v", err)
	}

	// В pass ушло новое содержимое: пароль + метаданные.
	body, err := os.ReadFile(inserted)
	if err != nil {
		t.Fatal(err)
	}
	want := "new-Pass-42\nlogin: edited\nurl: https://example.org\n"
	if string(body) != want {
		t.Errorf("в pass ушло %q, want %q", string(body), want)
	}

	// Запись перечитана: показывается новый пароль и новые метаданные.
	if a.current == nil || a.current.Password != "new-Pass-42" {
		t.Fatalf("после правки current = %+v", a.current)
	}
	if got := a.details.Text; !strings.Contains(got, "login: edited") {
		t.Errorf("метаданные не обновились: %q", got)
	}
	if len(a.visible) != 2 {
		t.Errorf("список изменился после правки: %+v", a.visible)
	}
}

func TestApplyEditValidates(t *testing.T) {
	a, _ := newTestApp(t)

	for _, tc := range []struct {
		name     string
		password string
	}{
		{"", "secret"},
		{"personal/mail", ""},
		{"personal/mail", "   "},
	} {
		if err := a.applyEdit(tc.name, tc.password, ""); err == nil {
			t.Errorf("applyEdit(%q, %q) не вернул ошибку", tc.name, tc.password)
		}
	}
}

func TestEditKeepsSearchFilter(t *testing.T) {
	a, _ := newTestApp(t)

	a.search.SetText("mail")
	if len(a.visible) != 1 || a.visible[0].Name != "personal/mail" {
		t.Fatalf("фильтр: %+v", a.visible)
	}
	a.selectByName("personal/mail")

	if err := a.applyEdit("personal/mail", "brand-new", ""); err != nil {
		t.Fatalf("applyEdit: %v", err)
	}

	// Правка не должна сбрасывать поиск: запись остаётся видимой и выбранной.
	if a.search.Text != "mail" {
		t.Errorf("поиск сброшен: %q", a.search.Text)
	}
	if len(a.visible) != 1 || a.visible[0].Name != "personal/mail" {
		t.Errorf("после правки видимые записи = %+v", a.visible)
	}
	if a.current == nil || a.current.Password != "brand-new" {
		t.Errorf("после правки current = %+v", a.current)
	}
}

func TestDeleteEntryUpdatesTreeAndCache(t *testing.T) {
	a, _ := newTestApp(t)

	if !a.deleteBtn.Disabled() {
		t.Error("кнопка удаления должна быть неактивна без выбора")
	}

	a.selectByName("dev/github")
	if a.current == nil || a.selected != "dev/github" {
		t.Fatal("запись не выбрана")
	}
	if a.deleteBtn.Disabled() {
		t.Error("кнопка удаления должна быть активна для выбранной записи")
	}
	if _, ok := a.cache["dev/github"]; !ok {
		t.Fatal("запись должна находиться в кеше")
	}

	a.deleteEntry("dev/github")

	if a.selected != "" || a.current != nil {
		t.Errorf("выделение должно быть сброшено: selected=%q", a.selected)
	}
	if _, ok := a.cache["dev/github"]; ok {
		t.Error("удалённая запись должна быть очищена из кеша")
	}
	if a.entryExists("dev/github") {
		t.Error("удалённая запись не должна оставаться в хранилище")
	}
	if len(a.visible) != 2 {
		t.Errorf("ожидалось 2 записи после удаления, получено %d", len(a.visible))
	}
	if !a.deleteBtn.Disabled() {
		t.Error("кнопка удаления должна снова заблокироваться после сброса выбора")
	}
}

func TestDeleteNonExistentEntryReportsError(t *testing.T) {
	a, _ := newTestApp(t)

	a.deleteEntry("dev/missing")
	if !strings.Contains(a.status.Text, "не найдена") {
		t.Errorf("ожидалась ошибка в статусе, получено: %q", a.status.Text)
	}
}

func TestDeleteLastEntryInFolderPrunesTreeAndPrefs(t *testing.T) {
	fyneApp := test.NewTempApp(t)
	store, _ := fakeStore(t, map[string]string{
		"personal/mail": "alpha-pass\n",
		"dev/github":    "bravo-pass\n",
	})
	clip := clipboard.NewWithClipboard(&memClipboard{}, 0)

	a, err := New(fyneApp, Options{Store: store, Clip: clip, Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	a.Window().Resize(fyne.NewSize(900, 600))
	a.Window().Show()

	a.tree.Select("personal")
	a.tree.Select("dev")
	prefsBefore := fyneApp.Preferences().String(prefOpenBranches)
	if !strings.Contains(prefsBefore, "personal") || !strings.Contains(prefsBefore, "dev") {
		t.Fatalf("ожидались обе папки в preferences, получено: %q", prefsBefore)
	}

	a.selectByName("dev/github")
	a.deleteEntry("dev/github")

	if _, exists := a.nodes["dev"]; exists {
		t.Error("опустевшая папка dev должна исчезнуть из дерева")
	}
	if a.tree.IsBranchOpen("dev") {
		t.Error("ветка dev не должна оставаться открытой в виджете дерева")
	}
	prefsAfter := fyneApp.Preferences().String(prefOpenBranches)
	if strings.Contains(prefsAfter, "dev") {
		t.Errorf("удалённая папка dev не должна оставаться в preferences: %q", prefsAfter)
	}
	if !strings.Contains(prefsAfter, "personal") {
		t.Errorf("папка personal должна сохраниться в preferences: %q", prefsAfter)
	}
	if !strings.Contains(a.status.Text, "dev/github") {
		t.Errorf("статус-бар должен сообщать об удалении записи, получено: %q", a.status.Text)
	}
}
