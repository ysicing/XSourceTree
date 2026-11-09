package main

import (
	"log"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/ysicing/XSourceTree/internal/workspace"
)

var addRequested = mygo.NewEvent[bool]("repository:add")
var scanRequested = mygo.NewEvent[bool]("repository:scan")
var groupRequested = mygo.NewEvent[bool]("repository:group")
var libraryRequested = mygo.NewEvent[bool]("repository:library")
var refreshRequested = mygo.NewEvent[bool]("repository:refresh")

func main() {
	app := mygo.App
	dir, err := app.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	manager, err := workspace.New(filepath.Join(dir, "recent.json"))
	if err != nil {
		log.Fatal(err)
	}
	service := &RepositoryService{manager: manager}
	mygo.Bind(service)
	app.WhenReady(func() {
		app.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Label: "文件", Submenu: []*mygo.MenuItem{
				{Label: "本地仓库", Accelerator: "CmdOrCtrl+Shift+L", Click: func(_ *mygo.MenuItem, win *mygo.Window) {
					if win != nil {
						_ = libraryRequested.Emit(win, true)
					}
				}},
				{Label: "添加已有的本地仓库…", Accelerator: "CmdOrCtrl+O", Click: func(_ *mygo.MenuItem, win *mygo.Window) {
					if win != nil {
						_ = addRequested.Emit(win, true)
					}
				}},
				{Label: "扫描文件夹…", Accelerator: "CmdOrCtrl+Shift+O", Click: func(_ *mygo.MenuItem, win *mygo.Window) {
					if win != nil {
						_ = scanRequested.Emit(win, true)
					}
				}},
				{Label: "新建仓库组…", Click: func(_ *mygo.MenuItem, win *mygo.Window) {
					if win != nil {
						_ = groupRequested.Emit(win, true)
					}
				}},
				mygo.Separator(),
				{Role: mygo.RoleClose},
			}},
			{Role: mygo.RoleEditMenu},
			{Label: "仓库", Submenu: []*mygo.MenuItem{
				{Label: "刷新", Accelerator: "CmdOrCtrl+R", Click: func(_ *mygo.MenuItem, win *mygo.Window) {
					if win != nil {
						_ = refreshRequested.Emit(win, true)
					}
				}},
			}},
			{Role: mygo.RoleViewMenu},
			{Role: mygo.RoleWindowMenu},
		}))
		openWindow()
	})
	app.OnWindowAllClosed(func() {})
	app.OnActivate(func(visible bool) {
		if !visible {
			openWindow()
		}
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func openWindow() {
	mygo.NewWindow(mygo.WindowOptions{
		Title: "XSourceTree", Width: 1320, Height: 860, MinWidth: 960, MinHeight: 640,
		TitleBarStyle: mygo.TitleBarHiddenInset, TitleBarHeight: 52,
		BackgroundColor: "light-dark(#ffffff, #191b20)", StateKey: "main", URL: "/",
	})
}
