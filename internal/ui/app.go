// Package ui renders the desktop front-end of the password manager.
package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/beatlezhis/go-pass/internal/clipboard"
	"github.com/beatlezhis/go-pass/internal/pass"
)

// Options configures the application window.
type Options struct {
	// Store is the password store backend (required).
	Store *pass.Store
	// Clip copies secrets to the system clipboard (required).
	Clip *clipboard.Manager
	// AppID is the Fyne application id, e.g. "com.github.beatlezhis.gopass".
	AppID string
	// Sync runs store operations and UI updates inline instead of in
	// background goroutines. Tests use it to stay deterministic.
	Sync bool
}

// App holds the widgets and state of the main window.
type App struct {
	store *pass.Store
	clip  *clipboard.Manager
	app   fyne.App
	win   fyne.Window

	entries []pass.Entry             // all entries of the store
	visible []pass.Entry             // entries matching the current filter
	nodes   map[string]*treeNodeInfo // uid -> folder/entry node of the tree

	selected string       // name shown in the detail pane
	current  *pass.Secret // decrypted entry shown in the detail pane
	revealed bool         // password is displayed in clear text

	search      *widget.Entry
	tree        *widget.Tree
	listEmpty   *widget.Label
	title       *widget.Label
	passLabel   *widget.Label
	details     *widget.Label
	revealBtn   *widget.Button
	copyBtn     *widget.Button
	status      *widget.Label
	placeholder fyne.CanvasObject
	detail      fyne.CanvasObject

	synchronous bool

	mu    sync.Mutex
	cache map[string]*pass.Secret

	clearAfter  time.Duration
	wasFiltered bool

	// savedOpen keeps the expanded folders while a search is active.
	savedOpen []string
	// noPersist suppresses saving of the expanded state (during filtering).
	noPersist bool
}

// New builds the application UI on top of the given Fyne application.
// It does not start the event loop, which keeps the window testable.
func New(fyneApp fyne.App, opts Options) (*App, error) {
	if opts.Store == nil || opts.Clip == nil {
		return nil, errors.New("ui: store и clipboard обязательны")
	}
	if fyneApp == nil {
		return nil, errors.New("ui: fyne.App обязателен")
	}

	a := &App{
		store:       opts.Store,
		clip:        opts.Clip,
		app:         fyneApp,
		cache:       make(map[string]*pass.Secret),
		synchronous: opts.Sync,
		clearAfter:  opts.Clip.Timeout,
	}

	a.clip.OnClear = func() {
		a.uiDo(func() { a.setStatus("Буфер обмена очищен") })
	}

	a.win = fyneApp.NewWindow("Go Pass — менеджер паролей")
	a.win.Resize(fyne.NewSize(1040, 680))
	a.win.SetMaster()
	a.build()
	a.restoreOpenBranches()
	a.registerShortcuts()
	a.win.SetOnClosed(func() { a.clip.Stop() })
	a.reload()
	return a, nil
}

// Window returns the main window.
func (a *App) Window() fyne.Window {
	return a.win
}

// Run creates the window and blocks until it is closed.
func Run(opts Options) error {
	fyneApp := app.NewWithID(appIDOr(opts.AppID))
	a, err := New(fyneApp, opts)
	if err != nil {
		return err
	}
	a.win.ShowAndRun()
	return nil
}

// uiDo runs fn on the UI goroutine. Everything that touches widgets from a
// background goroutine must go through it.
func (a *App) uiDo(fn func()) {
	if a.synchronous {
		fn()
		return
	}
	fyne.Do(fn)
}

// async runs fn in the background, or inline in synchronous (test) mode.
func (a *App) async(fn func()) {
	if a.synchronous {
		fn()
		return
	}
	go fn()
}

func appIDOr(id string) string {
	if id == "" {
		return "com.github.beatlezhis.gopass"
	}
	return id
}

// build assembles the widget tree.
func (a *App) build() {
	a.search = widget.NewEntry()
	a.search.SetPlaceHolder("Поиск: github, vpn, …")
	a.search.OnChanged = func(string) { a.applyFilter() }
	a.search.OnSubmitted = func(string) { a.selectFirst() }

	a.tree = widget.NewTree(
		func(uid widget.TreeNodeID) []widget.TreeNodeID {
			if n := a.nodes[uid]; n != nil {
				return n.children
			}
			return nil
		},
		func(uid widget.TreeNodeID) bool {
			n := a.nodes[uid]
			return n != nil && len(n.children) > 0
		},
		a.nodeCreate,
		a.nodeUpdate,
	)
	a.tree.OnSelected = a.treeSelected
	a.tree.OnBranchOpened = func(string) { a.onBranchChanged() }
	a.tree.OnBranchClosed = func(string) { a.onBranchChanged() }

	a.listEmpty = widget.NewLabel("Хранилище пустое")
	a.listEmpty.Alignment = fyne.TextAlignCenter
	a.listEmpty.TextStyle = fyne.TextStyle{Italic: true}
	a.listEmpty.Hide()

	a.nodes = buildTreeNodes(nil)

	left := container.NewBorder(a.search, nil, nil, nil,
		container.NewStack(a.tree, container.NewCenter(a.listEmpty)))

	a.title = widget.NewLabel("")
	a.title.TextStyle = fyne.TextStyle{Bold: true}
	a.title.Truncation = fyne.TextTruncateEllipsis

	a.passLabel = widget.NewLabel("")
	a.passLabel.TextStyle = fyne.TextStyle{Monospace: true}
	a.passLabel.Wrapping = fyne.TextWrapBreak
	a.passLabel.Truncation = fyne.TextTruncateOff

	a.revealBtn = widget.NewButtonWithIcon("Показать", theme.VisibilityIcon(), a.toggleReveal)
	a.revealBtn.Importance = widget.LowImportance
	a.revealBtn.Disable()

	a.copyBtn = widget.NewButtonWithIcon("Копировать пароль", theme.ContentCopyIcon(), a.copyPassword)
	a.copyBtn.Importance = widget.HighImportance
	a.copyBtn.Disable()

	copyAllBtn := widget.NewButtonWithIcon("Копировать всё", theme.ContentCopyIcon(), a.copyAll)
	copyAllBtn.Importance = widget.LowImportance

	a.details = widget.NewLabel("")
	a.details.TextStyle = fyne.TextStyle{Monospace: true}
	a.details.Wrapping = fyne.TextWrapWord
	a.details.Truncation = fyne.TextTruncateOff

	passwordBox := container.NewVBox(
		widget.NewLabelWithStyle("Пароль", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewBorder(nil, nil, nil, container.NewHBox(a.revealBtn, copyAllBtn), a.passLabel),
		container.NewHBox(a.copyBtn),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Метаданные и заметки", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	detailBody := container.NewBorder(
		container.NewVBox(a.title, passwordBox),
		nil, nil, nil,
		container.NewVScroll(a.details),
	)
	a.detail = container.NewPadded(detailBody)

	placeholderLabel := widget.NewLabel("Выберите запись в дереве слева —\nпароль будет расшифрован через pass")
	placeholderLabel.Alignment = fyne.TextAlignCenter
	a.placeholder = container.NewCenter(placeholderLabel)

	a.status = widget.NewLabel("Готово")
	a.status.Truncation = fyne.TextTruncateEllipsis

	storeLabel := widget.NewLabel("хранилище: " + abbreviateHome(a.store.Dir))
	storeLabel.TextStyle = fyne.TextStyle{Italic: true}
	storeLabel.Alignment = fyne.TextAlignTrailing

	statusBar := container.NewBorder(nil, nil,
		container.NewHBox(widget.NewIcon(theme.InfoIcon())),
		storeLabel,
		a.status,
	)

	split := container.NewHSplit(left, container.NewStack(a.placeholder, a.detail))
	split.SetOffset(0.32)

	a.win.SetContent(container.NewBorder(a.toolbar(), statusBar, nil, nil, split))
	a.showDetail(false)
}

// toolbar builds the top toolbar.
func (a *App) toolbar() fyne.CanvasObject {
	addBtn := widget.NewButtonWithIcon("Добавить", theme.ContentAddIcon(), a.showAddDialog)
	addBtn.Importance = widget.HighImportance

	refreshBtn := widget.NewButtonWithIcon("Обновить", theme.ViewRefreshIcon(), func() {
		a.cacheReset()
		a.reload()
	})

	expandBtn := widget.NewButtonWithIcon("Развернуть", theme.MenuExpandIcon(), a.expandAllBranches)
	expandBtn.Importance = widget.LowImportance

	collapseBtn := widget.NewButtonWithIcon("Свернуть", theme.MenuDropDownIcon(), a.collapseAllBranches)
	collapseBtn.Importance = widget.LowImportance

	bar := container.NewBorder(nil, nil,
		container.NewHBox(addBtn, refreshBtn, expandBtn, collapseBtn), nil)
	return container.NewVBox(bar, widget.NewSeparator())
}

// registerShortcuts wires Cmd/Ctrl+C and Cmd/Ctrl+N.
func (a *App) registerShortcuts() {
	ctrl := fyne.KeyModifierControl
	if isMac() {
		ctrl = fyne.KeyModifierSuper
	}
	add := &desktop.CustomShortcut{KeyName: fyne.KeyN, Modifier: ctrl}
	a.win.Canvas().AddShortcut(add, func(fyne.Shortcut) { a.showAddDialog() })

	copyShortcut := &desktop.CustomShortcut{KeyName: fyne.KeyC, Modifier: ctrl}
	a.win.Canvas().AddShortcut(copyShortcut, func(fyne.Shortcut) {
		focused := a.win.Canvas().Focused()
		if focused == nil || focused == a.tree {
			a.copyPassword()
		}
	})

	find := &desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: ctrl}
	a.win.Canvas().AddShortcut(find, func(fyne.Shortcut) { a.win.Canvas().Focus(a.search) })
}

// reload reads the store listing in the background.
func (a *App) reload() {
	a.setStatus("Чтение хранилища…")
	previous := a.selected
	a.async(func() {
		entries, err := a.store.List()
		a.uiDo(func() {
			if err != nil {
				a.report(err)
				return
			}
			a.setEntries(entries)
			if previous != "" {
				a.selectByName(previous)
			}
		})
	})
}

// setEntries replaces the entry list and reapplies the filter.
func (a *App) setEntries(entries []pass.Entry) {
	a.entries = entries
	a.applyFilter()
}

// applyFilter rebuilds the filtered entry list and the tree that displays it.
func (a *App) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(a.search.Text))

	// Раскрытые папки запоминаем до перестройки дерева: в отфильтрованном
	// наборе нужных веток может не оказаться.
	if q != "" && !a.wasFiltered {
		a.savedOpen = a.collectOpenBranches()
	}

	a.visible = a.visible[:0]
	for _, e := range a.entries {
		if q == "" || strings.Contains(strings.ToLower(e.Name), q) {
			a.visible = append(a.visible, e)
		}
	}
	a.nodes = buildTreeNodes(a.visible)
	a.tree.Refresh()

	// Пока идёт поиск, раскрываем всё найденное; при сбросе поиска возвращаем
	// дерево к тому виду, который пользователь настроил сам.
	if q != "" {
		a.withoutPersist(func() { a.tree.OpenAllBranches() })
	} else if a.wasFiltered {
		a.withoutPersist(func() { a.tree.CloseAllBranches() })
		a.openBranches(a.savedOpen)
	}
	a.wasFiltered = q != ""

	if len(a.visible) == 0 {
		if q == "" {
			a.listEmpty.SetText("Хранилище пустое")
		} else {
			a.listEmpty.SetText("Ничего не найдено")
		}
		a.listEmpty.Show()
	} else {
		a.listEmpty.Hide()
	}

	a.updateCountStatus()
}

// updateCountStatus shows how many entries are in the store and in the filter.
func (a *App) updateCountStatus() {
	if strings.TrimSpace(a.search.Text) == "" {
		a.setStatus(fmt.Sprintf("Записей: %d", len(a.entries)))
		return
	}
	a.setStatus(fmt.Sprintf("Найдено: %d из %d", len(a.visible), len(a.entries)))
}

// selectFirst selects the first entry matching the current filter.
func (a *App) selectFirst() {
	if len(a.visible) == 0 {
		return
	}
	a.selectByName(a.visible[0].Name)
}

// selectByName opens the parent folders of name and selects the entry.
func (a *App) selectByName(name string) {
	if _, ok := a.nodes[name]; !ok {
		return
	}
	parts := strings.Split(name, "/")
	for i := 1; i < len(parts); i++ {
		parent := strings.Join(parts[:i], "/")
		if n := a.nodes[parent]; n != nil && len(n.children) > 0 {
			a.tree.OpenBranch(parent)
		}
	}
	a.tree.Select(name)
	a.tree.ScrollTo(name)
}

// prefOpenBranches is the preferences key that stores expanded folders.
const prefOpenBranches = "tree.openBranches"

// onBranchChanged persists the expanded state after a user action.
func (a *App) onBranchChanged() {
	if a.noPersist {
		return
	}
	a.persistOpenBranches()
}

// withoutPersist runs fn without recording branch changes (used while filtering).
func (a *App) withoutPersist(fn func()) {
	previous := a.noPersist
	a.noPersist = true
	fn()
	a.noPersist = previous
}

// collectOpenBranches returns the uids of the currently expanded folders.
func (a *App) collectOpenBranches() []string {
	open := make([]string, 0, len(a.nodes))
	for uid, n := range a.nodes {
		if len(n.children) > 0 && a.tree.IsBranchOpen(uid) {
			open = append(open, uid)
		}
	}
	sort.Strings(open)
	return open
}

// persistOpenBranches stores the expanded folders in the app preferences, so
// the tree opens the same way next time.
func (a *App) persistOpenBranches() {
	a.app.Preferences().SetString(prefOpenBranches, strings.Join(a.collectOpenBranches(), "\n"))
}

// restoreOpenBranches expands the folders remembered from the previous run.
func (a *App) restoreOpenBranches() {
	a.openBranches(strings.Split(a.app.Preferences().String(prefOpenBranches), "\n"))
}

// openBranches expands the given folders without touching the stored state.
func (a *App) openBranches(uids []string) {
	a.withoutPersist(func() {
		for _, uid := range uids {
			if uid != "" {
				a.tree.OpenBranch(uid)
			}
		}
	})
}

// expandAllBranches opens every folder and remembers that.
func (a *App) expandAllBranches() {
	a.withoutPersist(func() { a.tree.OpenAllBranches() })
	a.persistOpenBranches()
}

// collapseAllBranches closes every folder and remembers that.
func (a *App) collapseAllBranches() {
	a.withoutPersist(func() { a.tree.CloseAllBranches() })
	a.persistOpenBranches()
}

// treeSelected handles a click on a tree row: folders expand, entries decrypt.
func (a *App) treeSelected(uid widget.TreeNodeID) {
	n := a.nodes[uid]
	if n == nil {
		return
	}
	if len(n.children) > 0 {
		// Папки только раскрываются: чтобы повторный клик снова переключал
		// ветку, выделение с папки снимается (Select не срабатывает дважды).
		a.tree.ToggleBranch(uid)
		a.tree.Unselect(uid)
		return
	}
	if n.secret {
		a.showEntry(uid)
	}
}

// treeNodeInfo is one node of the folder tree: either a folder (children) or a
// stored entry (secret).
type treeNodeInfo struct {
	children []string
	secret   bool
	// count is the number of stored entries in this subtree.
	count int
}

// buildTreeNodes turns a flat entry list into a folder tree. The empty uid is
// the invisible root, so its children are the top-level folders.
func buildTreeNodes(entries []pass.Entry) map[string]*treeNodeInfo {
	nodes := map[string]*treeNodeInfo{"": {}}

	for _, e := range entries {
		parts := strings.Split(e.Name, "/")
		parent := ""
		for i, part := range parts {
			_ = part
			uid := strings.Join(parts[:i+1], "/")
			if nodes[uid] == nil {
				nodes[uid] = &treeNodeInfo{}
				nodes[parent].children = append(nodes[parent].children, uid)
			}
			if i == len(parts)-1 {
				nodes[uid].secret = true
			}
			parent = uid
		}
	}

	var countSecrets func(uid string) int
	countSecrets = func(uid string) int {
		n := nodes[uid]
		if n == nil {
			return 0
		}
		c := 0
		if n.secret {
			c = 1
		}
		for _, child := range n.children {
			c += countSecrets(child)
		}
		n.count = c
		return c
	}
	countSecrets("")

	for _, n := range nodes {
		sort.Slice(n.children, func(i, j int) bool {
			bi := len(nodes[n.children[i]].children) > 0
			bj := len(nodes[n.children[j]].children) > 0
			if bi != bj {
				return bi // папки идут раньше записей
			}
			return n.children[i] < n.children[j]
		})
	}
	return nodes
}

// nodeCreate builds the template used for a tree row.
func (a *App) nodeCreate(branch bool) fyne.CanvasObject {
	icon := widget.NewIcon(theme.FolderIcon())
	if !branch {
		icon.SetResource(theme.DocumentIcon())
	}
	label := widget.NewLabel("")
	label.Truncation = fyne.TextTruncateEllipsis
	label.TextStyle = fyne.TextStyle{Bold: branch}
	// Небольшой отступ между иконкой и подписью.
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(fyne.NewSize(4, 1))

	// Border даёт подписи всю оставшуюся ширину строки, поэтому длинные имена
	// обрезаются многоточием, а не сжимаются в ноль.
	return container.NewBorder(nil, nil, container.NewHBox(icon, gap), nil, label)
}

// nodeUpdate fills a tree row with the node's name and icon.
func (a *App) nodeUpdate(uid widget.TreeNodeID, branch bool, o fyne.CanvasObject) {
	icon, label := rowParts(o)
	if icon == nil || label == nil {
		return
	}

	name := baseName(uid)
	if branch {
		if n := a.nodes[uid]; n != nil {
			name = fmt.Sprintf("%s (%d)", name, n.count)
		}
	}
	label.SetText(name)
	label.TextStyle = fyne.TextStyle{Bold: branch}
	if branch {
		icon.SetResource(theme.FolderIcon())
	} else {
		icon.SetResource(theme.DocumentIcon())
	}
}

// rowParts finds the icon and label of a tree row template, which nests them in
// a Border container.
func rowParts(o fyne.CanvasObject) (*widget.Icon, *widget.Label) {
	var icon *widget.Icon
	var label *widget.Label

	var walk func(fyne.CanvasObject)
	walk = func(obj fyne.CanvasObject) {
		switch v := obj.(type) {
		case *widget.Icon:
			icon = v
		case *widget.Label:
			label = v
		case *fyne.Container:
			for _, child := range v.Objects {
				walk(child)
			}
		}
	}
	walk(o)
	return icon, label
}

// baseName returns the last path segment of a tree uid.
func baseName(uid string) string {
	if i := strings.LastIndexByte(uid, '/'); i >= 0 {
		return uid[i+1:]
	}
	return uid
}

// showEntry decrypts and displays the entry with the given name.
func (a *App) showEntry(name string) {
	if name == "" {
		return
	}
	a.selected = name
	a.current = nil
	a.revealed = false

	a.title.SetText(name)
	a.passLabel.SetText("")
	a.details.SetText("")
	a.revealBtn.SetText("Показать")
	a.revealBtn.Disable()
	a.copyBtn.Disable()
	a.showDetail(true)
	a.setStatus("Расшифровка " + name + "…")

	a.async(func() {
		secret, err := a.secret(name)
		a.uiDo(func() {
			if a.selected != name {
				return // a newer selection won the race
			}
			if err != nil {
				a.current = nil
				a.passLabel.SetText("—")
				a.details.SetText("")
				a.setStatus("Ошибка расшифровки " + name)
				a.report(err)
				return
			}
			a.render(secret)
		})
	})
}

// render fills the detail pane with a decrypted secret.
func (a *App) render(secret *pass.Secret) {
	a.current = secret
	a.revealed = false
	a.revealBtn.SetText("Показать")
	a.passLabel.SetText(maskPassword(secret.Password))
	a.details.SetText(descriptionText(secret))
	a.revealBtn.Enable()
	a.copyBtn.Enable()
	a.showDetail(true)
	a.setStatus(fmt.Sprintf("%s: пароль из %d символов", secret.Name, len([]rune(secret.Password))))
}

// maskPassword hides every character of the password.
func maskPassword(pwd string) string {
	if pwd == "" {
		return "—"
	}
	return strings.Repeat("•", len([]rune(pwd)))
}

// descriptionText renders metadata and notes for the read-only area.
func descriptionText(s *pass.Secret) string {
	var b strings.Builder
	for _, f := range s.Fields {
		fmt.Fprintf(&b, "%s: %s\n", f.Key, f.Value)
	}
	if s.Notes != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s.Notes)
	}
	if b.Len() == 0 {
		return "—"
	}
	return strings.TrimRight(b.String(), "\n")
}

// secret returns the decrypted entry, using an in-memory cache.
func (a *App) secret(name string) (*pass.Secret, error) {
	a.mu.Lock()
	if s, ok := a.cache[name]; ok {
		a.mu.Unlock()
		return s, nil
	}
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), pass.DefaultTimeout)
	defer cancel()

	s, err := a.store.Show(ctx, name)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	a.cache[name] = s
	a.mu.Unlock()
	return s, nil
}

func (a *App) cacheReset() {
	a.mu.Lock()
	a.cache = make(map[string]*pass.Secret)
	a.mu.Unlock()
}

// toggleReveal switches the password between masked and clear text.
func (a *App) toggleReveal() {
	if a.current == nil {
		return
	}
	a.revealed = !a.revealed
	if a.revealed {
		a.passLabel.SetText(a.current.Password)
		a.revealBtn.SetText("Скрыть")
	} else {
		a.passLabel.SetText(maskPassword(a.current.Password))
		a.revealBtn.SetText("Показать")
	}
}

// copyPassword copies only the first line of the entry.
func (a *App) copyPassword() {
	if a.current == nil || a.current.Name != a.selected {
		a.setStatus("Сначала выберите запись")
		return
	}
	a.copyToClipboard(a.current.Password, "Пароль")
}

// copyAll copies the whole decrypted entry.
func (a *App) copyAll() {
	if a.current == nil || a.current.Name != a.selected {
		a.setStatus("Сначала выберите запись")
		return
	}
	a.copyToClipboard(a.current.Raw, "Вся запись")
}

func (a *App) copyToClipboard(text, what string) {
	if err := a.clip.Copy(text); err != nil {
		a.report(err)
		return
	}
	if a.clearAfter > 0 {
		a.setStatus(fmt.Sprintf("%s в буфере обмена, очистка через %s", what, humanDuration(a.clearAfter)))
		return
	}
	a.setStatus(what + " в буфере обмена")
}

// addFormState keeps what the user typed, so a validation error does not throw
// the form away.
type addFormState struct {
	name     string
	password string
	notes    string
}

// showAddDialog asks for a name and content, then stores the entry.
func (a *App) showAddDialog() {
	a.showAddForm(addFormState{})
}

// showAddForm renders the "new entry" dialog, optionally prefilled.
func (a *App) showAddForm(state addFormState) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("например: cloud/aws/root")
	nameEntry.SetText(state.name)
	nameEntry.Validator = func(s string) error {
		if strings.TrimSpace(s) == "" {
			return nil // не ругаемся на пустое поле до сохранения
		}
		return pass.ValidateName(strings.TrimSuffix(strings.TrimSpace(s), ".gpg"))
	}

	dirs := a.existingDirs()
	namePart := container.NewVBox(widget.NewLabel("Имя записи"), nameEntry)
	if len(dirs) > 0 {
		dirSelect := widget.NewSelect(dirs, func(v string) {
			if v != "" {
				nameEntry.SetText(v + "/")
			}
		})
		dirSelect.PlaceHolder = "папка (необязательно)"
		namePart = container.NewVBox(
			widget.NewLabel("Имя записи"),
			nameEntry,
			widget.NewLabel("Существующие папки"),
			dirSelect,
		)
	}

	pwdEntry := widget.NewPasswordEntry()
	pwdEntry.TextStyle = fyne.TextStyle{Monospace: true}
	pwdEntry.SetPlaceHolder("пароль (первая строка записи)")
	pwdEntry.SetText(state.password)

	notesEntry := widget.NewMultiLineEntry()
	notesEntry.SetPlaceHolder("login: user@example.com\nurl: https://example.com\n\nлюбые заметки")
	notesEntry.SetMinRowsVisible(5)
	notesEntry.TextStyle = fyne.TextStyle{Monospace: true}
	notesEntry.SetText(state.notes)

	genBtn := widget.NewButtonWithIcon("Сгенерировать", theme.ContentRedoIcon(), func() {
		pwd, err := GeneratePassword(20)
		if err != nil {
			a.report(err)
			return
		}
		pwdEntry.Password = false // показать, чтобы пароль можно было проверить
		pwdEntry.SetText(pwd)
		pwdEntry.Refresh()
		a.setStatus("Сгенерирован пароль из 20 символов — нажмите «Сохранить»")
	})

	hint := widget.NewLabel("Пароль попадёт в первую строку, остальное — в метаданные записи.")
	hint.Wrapping = fyne.TextWrapWord
	hint.TextStyle = fyne.TextStyle{Italic: true}

	form := container.NewVBox(
		namePart,
		widget.NewLabel("Пароль"),
		container.NewBorder(nil, nil, nil, genBtn, pwdEntry),
		widget.NewLabel("Дополнительно"),
		notesEntry,
		hint,
	)

	// Keep the form open (by reopening it prefilled) when validation fails.
	reopen := func(err error) {
		a.setStatus(err.Error())
		a.showAddForm(addFormState{
			name:     nameEntry.Text,
			password: pwdEntry.Text,
			notes:    notesEntry.Text,
		})
	}

	d := dialog.NewCustomConfirm("Новая запись", "Сохранить", "Отмена", form, func(ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSuffix(strings.TrimSpace(nameEntry.Text), ".gpg")
		if err := pass.ValidateName(name); err != nil {
			reopen(err)
			return
		}
		if pwdEntry.Text == "" {
			reopen(errors.New("пароль не может быть пустым"))
			return
		}

		content := pwdEntry.Text
		if extra := strings.TrimSpace(notesEntry.Text); extra != "" {
			content += "\n" + extra
		}

		if a.entryExists(name) {
			dialog.ShowConfirm("Перезаписать запись?",
				fmt.Sprintf("Запись «%s» уже есть в хранилище. Заменить её содержимое?", name),
				func(yes bool) {
					if yes {
						a.save(name, content)
					}
				}, a.win)
			return
		}
		a.save(name, content)
	}, a.win)
	d.Resize(fyne.NewSize(580, 520))
	d.Show()
	a.win.Canvas().Focus(nameEntry)
}

// existingDirs returns the distinct top-level directories of the store, used as
// quick suggestions in the "new entry" dialog.
func (a *App) existingDirs() []string {
	seen := make(map[string]struct{})
	var dirs []string
	for _, e := range a.entries {
		if i := strings.IndexByte(e.Name, '/'); i > 0 {
			d := e.Name[:i]
			if _, ok := seen[d]; !ok {
				seen[d] = struct{}{}
				dirs = append(dirs, d)
			}
		}
	}
	sort.Strings(dirs)
	return dirs
}

// entryExists reports whether name is already present in the store listing.
func (a *App) entryExists(name string) bool {
	for _, e := range a.entries {
		if e.Name == name {
			return true
		}
	}
	return false
}

// save stores the entry in the background and refreshes the list afterwards.
func (a *App) save(name, content string) {
	progress := dialog.NewCustomWithoutButtons("Сохранение",
		container.NewVBox(
			widget.NewProgressBarInfinite(),
			widget.NewLabel("Шифрование через gpg. Если появится запрос парольной фразы — введите её."),
		), a.win)
	progress.Show()
	a.setStatus("Сохранение " + name + "…")

	a.async(func() {
		ctx, cancel := context.WithTimeout(context.Background(), pass.DefaultTimeout)
		defer cancel()
		err := a.store.Insert(ctx, name, content)

		a.uiDo(func() {
			progress.Hide()
			if err != nil {
				a.report(err)
				return
			}
			a.setStatus("Запись «" + name + "» сохранена")
			a.refreshAndSelect(name)
		})
	})
}

// refreshAndSelect reloads the listing and selects the given entry.
func (a *App) refreshAndSelect(name string) {
	a.async(func() {
		entries, err := a.store.List()
		a.uiDo(func() {
			if err != nil {
				a.report(err)
				return
			}
			a.search.SetText("")
			a.setEntries(entries)
			a.selectByName(name)
			a.setStatus(fmt.Sprintf("Записей: %d", len(entries)))
		})
	})
}

func (a *App) showDetail(visible bool) {
	if visible {
		a.placeholder.Hide()
		a.detail.Show()
		return
	}
	a.detail.Hide()
	a.placeholder.Show()
}

func (a *App) setStatus(msg string) {
	a.status.SetText(msg)
}

// report shows an error both in the status bar and in a dialog.
func (a *App) report(err error) {
	msg := err.Error()
	a.setStatus(msg)
	dialog.ShowError(errors.New(msg), a.win)
}

// abbreviateHome shortens a path below the user's home directory to "~/...".
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + strings.TrimPrefix(path, home)
}

func humanDuration(d time.Duration) string {
	if d%time.Second == 0 {
		return fmt.Sprintf("%d с", int(d.Seconds()))
	}
	return d.String()
}
