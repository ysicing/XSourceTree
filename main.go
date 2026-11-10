// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"log"
	"path/filepath"

	"github.com/ysicing/XSourceTree/internal/workspace"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

func main() {
	app := mygo.App
	if !app.RequestSingleInstanceLock() {
		return
	}
	mygo.Use(native.New(updater.Options{Language: "zh-Hans"}))
	dir, err := app.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	manager, err := workspace.New(filepath.Join(dir, "recent.json"))
	if err != nil {
		log.Fatal(err)
	}
	service := &RepositoryService{manager: manager}
	var desktop *nativeApp
	open := func() {
		if desktop == nil {
			desktop = newNativeApp(service, filepath.Join(dir, "native-state.json"))
			desktop.open()
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
	app.WhenReady(func() {
		app.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Label: "XSourceTree", Submenu: []*mygo.MenuItem{
				{Label: "关于 XSourceTree", Click: func(_ *mygo.MenuItem, _ *mygo.Window) {
					app.ShowAboutPanel(mygo.AboutPanelOptions{
						ApplicationVersion: app.Version(),
						Version:            buildNumber,
						Credits:            buildDetails(),
					})
				}},
				updater.MenuItem(),
				mygo.Separator(),
				{Role: mygo.RoleServices},
				mygo.Separator(),
				{Role: mygo.RoleHide},
				{Role: mygo.RoleHideOthers},
				{Role: mygo.RoleUnhide},
				mygo.Separator(),
				{Role: mygo.RoleQuit},
			}},
			{Label: "文件", Submenu: []*mygo.MenuItem{
				{Label: "本地仓库", Accelerator: "CmdOrCtrl+Shift+L", Click: command(func(desktop *nativeApp) { desktop.showLibrary() })},
				{Label: "添加已有的本地仓库…", Accelerator: "CmdOrCtrl+O", Click: command(func(desktop *nativeApp) { desktop.chooseLocal() })},
				{Label: "扫描文件夹…", Accelerator: "CmdOrCtrl+Shift+O", Click: command(func(desktop *nativeApp) { desktop.chooseScan() })},
				{Label: "新建仓库组…", Click: command(func(desktop *nativeApp) { desktop.groupDialog("") })},
				mygo.Separator(),
				{Role: mygo.RoleClose},
			}},
			{Role: mygo.RoleEditMenu},
			{Label: "仓库", Submenu: []*mygo.MenuItem{
				{Label: "刷新", Accelerator: "CmdOrCtrl+R", Click: command(func(desktop *nativeApp) { desktop.refresh() })},
				{Label: "提交", Accelerator: "CmdOrCtrl+Enter", Click: command(func(desktop *nativeApp) { desktop.commit() })},
			}},
			{Role: mygo.RoleViewMenu},
			{Role: mygo.RoleWindowMenu},
		}))
		open()
	})
	app.OnWindowAllClosed(func() {})
	app.OnActivate(func(visible bool) {
		if !visible {
			open()
		}
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
