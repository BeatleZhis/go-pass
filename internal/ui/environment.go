package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/beatlezhis/go-pass/internal/pass"
)

// applySavedBinary restores a previously chosen pass path when the automatic
// lookup failed (for example when the app reopens with a limited PATH).
func (a *App) applySavedBinary() {
	if a.store == nil || a.store.HasBinary() {
		return
	}
	saved := a.app.Preferences().String(prefPassBinary)
	if saved == "" {
		return
	}
	if err := a.store.SetBinary(saved); err != nil {
		a.app.Preferences().SetString(prefPassBinary, "")
	}
}

// checkEnvironment runs once the window is visible: it asks for a missing pass
// CLI and warns about a store without git.
func (a *App) checkEnvironment() {
	if a.store == nil {
		return
	}
	if !a.store.HasBinary() {
		a.promptMissingPass()
		return
	}
	a.refreshGitStatus()
	a.maybeWarnNoGit()
}

// promptMissingPass offers to locate or install the pass CLI. Without it the
// app can only list entry names, so the dialog is the primary next step.
func (a *App) promptMissingPass() {
	a.setStatus("pass не найден")

	content := container.NewVBox(
		widget.NewLabel("CLI pass не найден: без него нельзя расшифровывать\nи сохранять записи."),
		sectionLabel("УСТАНОВИТЬ"),
		widget.NewLabel("macOS:  brew install pass\nLinux:  sudo apt install pass"),
		sectionLabel("ИЛИ УКАЗАТЬ ПУТЬ"),
		widget.NewLabel("Например: /opt/homebrew/bin/pass"),
	)

	buttons := container.NewHBox(
		widget.NewButtonWithIcon("Найти снова", theme.ViewRefreshIcon(), a.retryBinary),
		widget.NewButtonWithIcon("Указать путь…", theme.FolderOpenIcon(), a.pickPassBinary),
		layout.NewSpacer(),
		widget.NewButton("Выход", func() { a.app.Quit() }),
	)

	d := dialog.NewCustomWithoutButtons("Требуется pass",
		container.NewBorder(nil, container.NewPadded(buttons), nil, nil, content), a.win)
	a.missingDlg = d
	d.Show()
}

func (a *App) closeMissingDialog() {
	if a.missingDlg == nil {
		return
	}
	a.missingDlg.Hide()
	a.missingDlg = nil
}

// retryBinary tries the automatic lookup again, e.g. after the user installed
// pass with a package manager.
func (a *App) retryBinary() {
	bin, err := pass.LookupBinary("")
	if err != nil {
		dialog.ShowError(err, a.win)
		return
	}
	a.store.Binary = bin
	a.afterBinaryReady("pass найден: " + bin)
}

// pickPassBinary lets the user select the executable manually and remembers it.
func (a *App) pickPassBinary() {
	dialog.ShowFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		path := rc.URI().Path()
		_ = rc.Close()

		if err := a.store.SetBinary(path); err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		a.app.Preferences().SetString(prefPassBinary, a.store.Binary)
		a.afterBinaryReady("pass: " + a.store.Binary)
	}, a.win)
}

// afterBinaryReady updates the UI once a usable pass binary is known.
func (a *App) afterBinaryReady(message string) {
	a.closeMissingDialog()
	a.setStatus(message)
	a.reload()
	a.refreshGitStatus()
	a.maybeWarnNoGit()
}

// refreshGitStatus inspects the store repository in the background so the
// status bar always reflects the real state.
func (a *App) refreshGitStatus() {
	if a.store == nil || a.store.Dir == "" {
		return
	}
	dir := a.store.Dir
	a.async(func() {
		state := pass.GitStateOf(dir)
		a.uiDo(func() { a.applyGitState(state) })
	})
}

// applyGitState renders the git indicator. A clickable "git: нет" opens the
// initialization dialog.
func (a *App) applyGitState(state pass.GitState) {
	switch {
	case !state.Available:
		a.gitBtn.SetText("git: нет")
		a.gitBtn.Importance = widget.LowImportance
		a.gitBtn.Disable()
	case state.Repository:
		a.gitBtn.SetText("git ✓")
		a.gitBtn.Importance = widget.LowImportance
		a.gitBtn.Disable()
	default:
		a.gitBtn.SetText("git: нет")
		a.gitBtn.Importance = widget.WarningImportance
		a.gitBtn.Enable()
	}
	a.gitBtn.Refresh()
}

// confirmInitGit asks before creating a repository in the store.
func (a *App) confirmInitGit() {
	content := widget.NewLabel("Создать git-репозиторий в хранилище?\n" +
		"Появится история изменений и возможность синхронизации через git.")
	content.Wrapping = fyne.TextWrapWord

	d := dialog.NewCustomConfirm("Инициализировать git?", "Инициализировать", "Отмена",
		content, func(ok bool) {
			if ok {
				a.initGit()
			}
		}, a.win)
	d.Show()
}

// initGit runs `pass git init` and commits the current store contents.
func (a *App) initGit() {
	a.setStatus("Инициализация git…")
	a.async(func() {
		ctx, cancel := context.WithTimeout(context.Background(), pass.DefaultTimeout)
		defer cancel()
		warning, err := a.store.InitGit(ctx)

		a.uiDo(func() {
			a.refreshGitStatus()
			if err != nil {
				a.report(err)
				return
			}
			if warning != "" {
				a.setStatus(warning)
				dialog.ShowError(errors.New(warning), a.win)
				return
			}
			a.setStatus("git-репозиторий инициализирован")
		})
	})
}

// maybeWarnNoGit shows a one-time warning when the store is not a git
// repository, with an option to initialize it and to stop reminding.
func (a *App) maybeWarnNoGit() {
	if a.store == nil || a.store.Dir == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(a.store.Dir, ".gpg-id")); err != nil {
		return // хранилище ещё не инициализировано
	}
	if a.app.Preferences().Bool(prefNoGitWarn) {
		return
	}
	state := pass.GitStateOf(a.store.Dir)
	if !state.Available || state.Repository {
		return
	}

	check := widget.NewCheck("Больше не напоминать", nil)
	content := container.NewVBox(
		widget.NewLabel("Хранилище не находится под git: изменения не будут\n"+
			"коммититься, история и синхронизация недоступны."),
		check,
	)

	d := dialog.NewCustomConfirm("Хранилище без git", "Инициализировать", "Не сейчас",
		content, func(ok bool) {
			if check.Checked {
				a.app.Preferences().SetBool(prefNoGitWarn, true)
			}
			if ok {
				a.initGit()
			}
		}, a.win)
	d.Show()
}
