// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"log"
	"path/filepath"
	"runtime"

	"github.com/ysicing/51code/internal/workspace"
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

// Run 启动原生应用，并接收根入口通过构建参数注入的信息。
func Run(info BuildInfo) {
	buildNumber, buildCommit, buildTime = info.Number, info.Commit, info.Time
	app := mygo.App
	displayName := app.Name()
	if err := preserveLegacyApplicationIdentity(app); err != nil {
		log.Fatal(err)
	}
	if !app.RequestSingleInstanceLock() {
		return
	}
	app.SetName(displayName)
	dir, err := app.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	manager, err := workspace.New(filepath.Join(dir, "recent.json"))
	if err != nil {
		log.Fatal(err)
	}
	service := &RepositoryService{manager: manager}
	desktop := newNativeApp(service, filepath.Join(dir, "native-state.json"))
	mygo.Use(native.New(updater.Options{
		Interval:               nativeUpdateCheckInterval(desktop.prefs.UpdateCheckFrequency),
		DisableAutomaticChecks: runtime.GOOS != "darwin",
	}))
	open := func() {
		if desktop == nil {
			desktop = newNativeApp(service, filepath.Join(dir, "native-state.json"))
		}
		if desktop.win == nil {
			desktop.open()
			applyNativeShortcuts(desktop.prefs.Shortcuts)
			desktop.win.OnClosed(func() { desktop = nil })
		} else {
			desktop.win.Show()
			desktop.win.Focus()
		}
	}
	command := func(action func(*nativeApp)) func(*mygo.MenuItem, *mygo.Window) {
		return func(_ *mygo.MenuItem, _ *mygo.Window) {
			if desktop == nil {
				open()
			}
			target := desktop
			target.win.Update(func() {
				if !target.closed && !target.busy && target.dialog == nil {
					action(target)
				}
			})
		}
	}
	app.OnSecondInstance(func(_ []string, _ string) { open() })
	setMenu := func() {
		updates := &mygo.MenuItem{
			Label: locales.DownloadRelease(),
			Click: command(func(desktop *nativeApp) {
				if err := mygo.Shell.OpenExternal(desktopReleasesURL); err != nil {
					desktop.problem = err.Error()
				}
			}),
		}
		if runtime.GOOS == "darwin" {
			updates = updater.MenuItem()
		}
		app.SetMenu(mygo.NewMenu(platformMenu([]*mygo.MenuItem{
			{Label: "51Code", Submenu: []*mygo.MenuItem{
				{Label: locales.About(), Click: func(_ *mygo.MenuItem, _ *mygo.Window) {
					showNativeAbout()
				}},
				updates,
				{ID: "settings", Label: locales.SettingsMenu(), Accelerator: "CmdOrCtrl+,", Click: command(func(desktop *nativeApp) { desktop.settingsDialog() })},
				mygo.Separator(),
				{Role: mygo.RoleServices},
				mygo.Separator(),
				{Role: mygo.RoleHide},
				{Role: mygo.RoleHideOthers},
				{Role: mygo.RoleUnhide},
				mygo.Separator(),
				{Role: mygo.RoleQuit},
			}},
			{Label: locales.FilesMenu(), Submenu: []*mygo.MenuItem{
				{ID: shortcutLibrary, Label: locales.LocalRepositories(), Accelerator: shortcutAccelerator(defaultNativeShortcuts().Library), Click: command(func(desktop *nativeApp) { desktop.showLibrary() })},
				{ID: shortcutAddRepo, Label: locales.AddExistingRepositoryMenu(), Accelerator: shortcutAccelerator(defaultNativeShortcuts().AddRepository), Click: command(func(desktop *nativeApp) { desktop.chooseLocal() })},
				{ID: shortcutScan, Label: locales.ScanFoldersMenu(), Accelerator: shortcutAccelerator(defaultNativeShortcuts().Scan), Click: command(func(desktop *nativeApp) { desktop.chooseScan() })},
				{Label: locales.NewGroupMenu(), Click: command(func(desktop *nativeApp) { desktop.groupDialog("") })},
				mygo.Separator(),
				{Role: mygo.RoleClose},
			}},
			{Role: mygo.RoleEditMenu},
			{Label: locales.Repository(), Submenu: []*mygo.MenuItem{
				{ID: shortcutRefresh, Label: locales.Refresh(), Accelerator: shortcutAccelerator(defaultNativeShortcuts().Refresh), Click: command(func(desktop *nativeApp) { desktop.refresh() })},
				{ID: shortcutCommit, Label: locales.Commit(), Accelerator: shortcutAccelerator(defaultNativeShortcuts().Commit), Click: command(func(desktop *nativeApp) { desktop.commit() })},
				mygo.Separator(),
				{Label: locales.RepositorySettingsMenu(), Click: command(func(desktop *nativeApp) { desktop.repositorySettingsDialog() })},
			}},
			{Role: mygo.RoleViewMenu},
			{Role: mygo.RoleWindowMenu},
		}, runtime.GOOS)))
		if desktop != nil {
			applyNativeShortcuts(desktop.prefs.Shortcuts)
		}
	}
	app.WhenReady(func() {
		setMenu()
		app.OnLocaleChanged(func(_ string) { setMenu() })
		open()
	})
	if runtime.GOOS == "darwin" {
		app.OnWindowAllClosed(func() {})
	}
	app.OnActivate(func(visible bool) {
		if !visible {
			open()
		}
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
