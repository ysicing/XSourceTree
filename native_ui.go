// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/internal/workspace"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

var nativeIcons = map[string]*ui.SVG{
	"back":     nativeSymbol(`<path d="m12 5-7 7 7 7M5 12h14"/>`),
	"folder":   nativeSymbol(`<path d="M3 7V5a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>`),
	"file":     nativeSymbol(`<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"/><path d="M14 2v6h6M8 13h8M8 17h5"/>`),
	"branch":   nativeSymbol(`<circle cx="6" cy="5" r="3"/><circle cx="6" cy="19" r="3"/><circle cx="18" cy="5" r="3"/><path d="M6 8v8M18 8a10 10 0 0 1-12 8"/>`),
	"merge":    nativeSymbol(`<circle cx="12" cy="5" r="3"/><circle cx="6" cy="19" r="3"/><circle cx="18" cy="19" r="3"/><path d="M6 16v-1c0-4 6-2 6-7M18 16v-1c0-4-6-2-6-7"/>`),
	"history":  nativeSymbol(`<path d="M3 11a9 9 0 1 1 2.5 7M3 4v7h7M12 7v5l3 2"/>`),
	"stash":    nativeSymbol(`<path d="M3 3h18v5H3ZM5 8v13h14V8M10 12h4"/>`),
	"tag":      nativeSymbol(`<path d="m20 13-7 7a2 2 0 0 1-3 0l-8-8V2h10l8 8a2 2 0 0 1 0 3Z"/><circle cx="7" cy="7" r="1"/>`),
	"cloud":    nativeSymbol(`<path d="M6 17a4 4 0 0 1-.6-8A6 6 0 0 1 17 9h1a4 4 0 0 1 0 8Z"/>`),
	"settings": nativeSymbol(`<path d="M10 3h4l.6 3 1.4.8 2.9-1 2 3.4-2.3 2v1.6l2.3 2-2 3.4-2.9-1-1.4.8L14 21h-4l-.6-3-1.4-.8-2.9 1-2-3.4 2.3-2v-1.6l-2.3-2 2-3.4 2.9 1 1.4-.8Z"/><circle cx="12" cy="12" r="3"/>`),
	"check":    nativeSymbol(`<path d="m5 12 4 4L19 6"/>`),
	"chevron":  nativeSymbol(`<path d="m9 5 7 7-7 7"/>`),
	"plus":     nativeSymbol(`<path d="M12 5v14M5 12h14"/>`),
	"more":     nativeSymbol(`<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>`),
	"refresh":  nativeSymbol(`<path d="M20 7a9 9 0 0 0-15-2L2 8m0-6v6h6M4 17a9 9 0 0 0 15 2l3-3m0 6v-6h-6"/>`),
	"fetch":    nativeSymbol(`<path d="M7 17H6a4 4 0 0 1-.6-8A6 6 0 0 1 17 9h1a4 4 0 0 1 0 8h-1M12 12v9m-3-3 3 3 3-3"/>`),
	"pull":     nativeSymbol(`<path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/>`),
	"push":     nativeSymbol(`<path d="M12 16V3m-5 5 5-5 5 5M4 16v5h16v-5"/>`),
}

func nativeSymbol(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

func iconButton(c *ui.Context, label string, icon *ui.SVG) ui.Element {
	button := ui.ButtonBase(c).Label(label).Tooltip(label).Padding(6).Gap(6).Radius(c.Theme().Radius).AlignItems(ui.Center)
	if button.Pressed() {
		button.Background(c.Theme().SurfacePressed)
	} else if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	return button.Children(func() { ui.Icon(c, icon).FontSize(16) })
}

func toolButton(c *ui.Context, label string, icon *ui.SVG) ui.Element {
	return ui.Button(c, "").Label(label).Children(func() {
		ui.Icon(c, icon).FontSize(16)
		ui.Text(c, label).SingleLine()
	})
}

func listRow(c *ui.Context, label string, selected bool) ui.Element {
	row := ui.ButtonBase(c).Label(label).FillWidth().MinWidth(0).Padding(7, 10).Gap(8).Radius(c.Theme().Radius).AlignItems(ui.Center).AccessSelected(selected)
	if selected {
		row.Background(c.Theme().Accent.Alpha(0.12))
	} else if row.Hovered() {
		row.Background(c.Theme().SurfaceHover)
	}
	return row
}

func (app *nativeApp) view(c *ui.Context) {
	switch app.prefs.Appearance {
	case appearanceLight:
		c.SetTheme(ui.LightTheme())
	case appearanceDark:
		c.SetTheme(ui.DarkTheme())
	}
	ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
		ui.Row(c).Height(52).Padding(0, 18, 0, 100).AlignItems(ui.Center).Gap(12).DragWindow().Children(func() {
			title := "本地仓库"
			if app.repository != nil && app.screen != "library" {
				title = app.repository.snapshot.Name
			}
			ui.Text(c, title).Bold().FontSize(14)
			ui.Spacer(c)
			ui.Toolbar(c, func() {
				if iconButton(c, "设置", nativeIcons["settings"]).Tooltip("设置（⌘,）").Disabled(app.busy || app.dialog != nil).Clicked() {
					app.settingsDialog()
				}
			}).Label("应用操作").Padding(0)
		})
		ui.Divider(c)
		ui.Column(c).Grow(1).MinHeight(0).Disabled(app.busy).Children(func() {
			ui.Split(c, &app.prefs.Sidebar, func() { app.sidebar(c) }, func() {
				ui.Column(c).Fill().MinWidth(0).Children(func() {
					app.toolbar(c)
					ui.Divider(c)
					switch app.screen {
					case "library":
						app.libraryView(c)
					case "history":
						app.historyWorkspace(c)
					default:
						ui.Split(c, &app.prefs.Files, func() { app.repositoryPanel(c) }, func() { app.diffPanel(c) }).Grow(1).MinHeight(0).FillWidth()
					}
				})
			}).Fill().SplitReset(226)
		})
		ui.Divider(c)
		ui.Row(c).Height(34).Padding(0, 14).AlignItems(ui.Center).Gap(10).Children(func() {
			if app.busy {
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, app.status).FontSize(12).TextColor(c.Theme().TextMuted).MinWidth(0).MaxLines(1).Shrink(1)
				if strings.Contains(app.status, "扫描") || strings.Contains(app.status, "搜索") {
					if ui.Button(c, "取消").Clicked() {
						app.service.CancelScan()
						app.service.CancelHistory()
					}
				}
			} else if app.problem != "" {
				summary := strings.SplitN(app.problem, "\n", 2)[0]
				if strings.HasPrefix(summary, "git ") || strings.HasPrefix(summary, "fatal:") || strings.HasPrefix(summary, "error:") || strings.HasPrefix(summary, "ssh:") {
					summary = "Git 操作失败，请查看详情"
				}
				ui.Text(c, summary).FontSize(12).TextColor(c.Theme().Danger).MaxLines(1).Grow(1).Selectable().Tooltip(app.problem)
				if ui.Button(c, "查看详情").Clicked() {
					problem := app.problem
					app.dialog = &nativeDialog{
						title: "操作失败",
						body: func(c *ui.Context) {
							ui.Scroll(c).Height(280).FillWidth().Children(func() {
								ui.Text(c, problem).FontSize(12).Selectable().FillWidth()
							})
						},
					}
				}
				if ui.Button(c, "关闭提示").Clicked() {
					app.problem = ""
				}
			} else if app.repository != nil && app.screen != "library" {
				ui.Text(c, app.repository.snapshot.Path).FontSize(12).TextColor(c.Theme().TextMuted).MinWidth(0).MaxLines(1).Shrink(1).Selectable()
			} else {
				ui.Text(c, fmt.Sprintf("%d 个本地仓库", len(app.library.Repositories))).FontSize(12).TextColor(c.Theme().TextMuted)
			}
			ui.Spacer(c)
			version := ui.ButtonBase(c).Label("关于 XSourceTree").Tooltip(strings.TrimSpace("关于 XSourceTree\n"+buildDetails())).
				Padding(4, 6).Radius(c.Theme().Radius).Shrink(0).TextColor(c.Theme().TextMuted)
			if version.Pressed() {
				version.Background(c.Theme().SurfacePressed)
			} else if version.Hovered() {
				version.Background(c.Theme().SurfaceHover)
			}
			if version.Children(func() {
				ui.Text(c, buildVersionLabel()).FontSize(11).SingleLine()
			}).Clicked() {
				showNativeAbout()
			}
		})
	})
	app.dialogView(c)
}

func (app *nativeApp) sidebar(c *ui.Context) {
	ui.Column(c).Fill().Background(c.Theme().Surface).Children(func() {
		inLibrary := app.repository == nil || app.screen == "library"
		selected := "screen:" + app.screen
		if app.screen == "library" && app.group != "" {
			selected = "group:" + app.group
		}
		if ui.Sidebar(c, &selected, func() {
			if inLibrary {
				ui.SidebarItem(c, "screen:library", nativeIcons["folder"], "本地仓库")
				if len(app.library.Groups) > 0 {
					ui.SidebarSection(c, "仓库组", nil, func() {
						for _, group := range app.library.Groups {
							group := group
							ui.Row(c).Key(group.ID).FillWidth().AlignItems(ui.Center).Children(func() {
								ui.SidebarItem(c, "group:"+group.ID, nativeIcons["folder"], group.Name).Grow(1).MinWidth(0).Tooltip(group.Name).Label(group.Name).ContextMenu(func(menu *ui.Menu) {
									if menu.Item("重命名").Chosen() {
										app.groupDialog(group.ID)
									}
									if menu.Item("移除分组").Chosen() {
										app.removeGroup(group)
									}
								})
								iconButton(c, "管理仓库组 "+group.Name, nativeIcons["more"]).OnClick(func() { app.groupActions(group) })
							})
						}
					})
				}
				return
			}
			ui.Row(c).Height(30).Padding(0, 8).Gap(8).AlignItems(ui.Center).Label("仓库工作区").Children(func() {
				ui.Icon(c, nativeIcons["folder"]).FontSize(18).TextColor(c.Theme().TextMuted)
				ui.Text(c, "工作区").FontSize(12).FontWeight(500).TextColor(c.Theme().TextMuted)
			})
			ui.Column(c).FillWidth().Children(func() {
				ui.SidebarItem(c, "screen:changes", nil, "文件变更").Label("文件变更").
					Height(26).Padding(0, 8, 0, 40).FontSize(12).Children(func() {
					if count := len(app.repository.snapshot.Files); count > 0 {
						ui.Badge(c, fmt.Sprint(count))
					}
				})
				ui.SidebarItem(c, "screen:history", nil, "提交历史").Height(26).Padding(0, 8, 0, 40).FontSize(12)
			})
			app.localBranchSidebar(c)
			app.tagSidebar(c)
			app.remoteSidebar(c)
			ui.SidebarItem(c, "screen:stash", nativeIcons["stash"], "储藏").Label("储藏").
				Height(30).Margin(16, 0, 0, 0).FontSize(12).Children(func() {
				if count := len(app.repository.stashes); count > 0 {
					ui.Badge(c, fmt.Sprint(count))
				}
			})
		}).Label("仓库导航").Grow(1).MinHeight(0).FillWidth().Changed() {
			switch {
			case strings.HasPrefix(selected, "screen:"):
				screen := strings.TrimPrefix(selected, "screen:")
				app.leaveConflict(func() {
					app.screen, app.group = screen, ""
					app.refSelected = nativeRefKey{}
					app.clearDiff()
					app.selectedFile, app.commitHash, app.stashHash = "", "", ""
					if screen == "history" {
						app.searchHistory(false)
					}
				})
			case strings.HasPrefix(selected, "group:"):
				app.leaveConflict(func() { app.screen, app.group = "library", strings.TrimPrefix(selected, "group:") })
			}
		}
		if inLibrary {
			ui.Toolbar(c, func() {
				if toolButton(c, "新建仓库组", nativeIcons["plus"]).Clicked() {
					app.groupDialog("")
				}
			}).FillWidth().Padding(8, 10).Label("仓库组操作")
		} else {
			ui.Divider(c)
			ui.Column(c).FillWidth().Padding(6, 10).Children(func() {
				button := ui.ButtonBase(c).Label("返回仓库").FillWidth().Height(28).
					Padding(0, 8).Gap(8).AlignItems(ui.Center).Radius(5).TextColor(c.Theme().TextMuted)
				if button.Hovered() || button.Pressed() {
					button.Background(c.Theme().SurfaceHover)
				}
				button.Children(func() {
					ui.Icon(c, nativeIcons["back"]).FontSize(15)
					ui.Text(c, "返回仓库").FontSize(12)
				})
				if button.Clicked() {
					app.showLibrary()
				}
			})
		}
	})
}

func (app *nativeApp) toolbar(c *ui.Context) {
	ui.Toolbar(c, func() {
		if app.screen == "library" {
			if ui.PrimaryButton(c, "添加仓库").Clicked() {
				app.chooseLocal()
			}
			if ui.Button(c, "输入路径").Clicked() {
				app.pathDialog()
			}
			if toolButton(c, "扫描文件夹", nativeIcons["folder"]).Clicked() {
				app.chooseScan()
			}
		} else if app.repository != nil {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(0, 8, 0, 2).Children(func() {
				ui.Icon(c, nativeIcons["branch"]).TextColor(c.Theme().TextMuted)
				ui.Text(c, app.repository.snapshot.Branch).Bold().MaxLines(1).MaxWidth(140).Tooltip(app.repository.snapshot.Branch)
				if app.repository.snapshot.Ahead > 0 || app.repository.snapshot.Behind > 0 {
					ui.Text(c, fmt.Sprintf("↑%d ↓%d", app.repository.snapshot.Ahead, app.repository.snapshot.Behind)).TextColor(c.Theme().TextMuted).FontSize(12)
				}
			})
			ui.Row(c).Children(func() {
				for _, remote := range []struct{ id, title string }{{"fetch", "获取"}, {"pull", "拉取"}, {"push", "推送"}} {
					remote := remote
					if toolButton(c, remote.title, nativeIcons[remote.id]).Disabled(!app.writable).Clicked() {
						app.remoteAction(remote.id)
					}
				}
			})
			if toolButton(c, "分支", nativeIcons["branch"]).Disabled(!app.writable).Clicked() {
				app.branchDialog()
			}
			if toolButton(c, "合并", nativeIcons["merge"]).Disabled(!app.writable).Clicked() {
				app.mergeDialog()
			}
			if toolButton(c, "仓库设置", nativeIcons["settings"]).Clicked() {
				app.repositorySettingsDialog()
			}
			if iconButton(c, "更多", nativeIcons["more"]).Clicked() {
				app.repositoryActions()
			}
		}
		ui.Spacer(c)
		if iconButton(c, "刷新", nativeIcons["refresh"]).Clicked() {
			app.refresh()
		}
	}).Label("仓库操作").FillWidth().Height(48).Padding(6, 12)
}

func (app *nativeApp) libraryView(c *ui.Context) {
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(22).Gap(16).Children(func() {
		title := "本地仓库"
		for _, group := range app.library.Groups {
			if group.ID == app.group {
				title = group.Name
			}
		}
		ui.Text(c, title).FontSize(24).Bold()
		ui.TextInput(c, &app.query).Search().Placeholder("搜索仓库名称、路径或分支").FillWidth()
		var entries []workspace.LocalRepository
		for _, entry := range app.library.Repositories {
			if app.group != "" && entry.GroupID != app.group {
				continue
			}
			if strings.Contains(strings.ToLower(entry.Name+"\n"+entry.Path+"\n"+entry.Branch), strings.ToLower(app.query)) {
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			ui.Column(c).Grow(1).FillWidth().Center().Gap(10).Children(func() {
				message := "尚未添加仓库"
				if app.query != "" {
					message = "没有匹配的仓库"
				} else if app.group != "" {
					message = "此分组暂无仓库"
				}
				ui.Text(c, message).FontSize(17).Bold()
				if app.query != "" {
					if ui.Button(c, "清除搜索").Clicked() {
						app.query = ""
					}
				}
			})
			return
		}
		ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			for _, entry := range entries {
				entry := entry
				ui.Row(c).Key(entry.Path).FillWidth().Padding(4, 0).Gap(8).AlignItems(ui.Center).Children(func() {
					listRow(c, entry.Name, false).Grow(1).Padding(14, 12).Children(func() {
						ui.Icon(c, nativeIcons["folder"]).FontSize(24).TextColor(c.Theme().TextMuted)
						ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
							ui.Text(c, entry.Name).FontSize(14).Bold().MaxLines(1)
							ui.Text(c, entry.Path).TextColor(c.Theme().TextMuted).FontSize(12).MaxLines(1)
							if entry.Problem != "" {
								ui.Text(c, entry.Problem).TextColor(c.Theme().Danger).FontSize(12).MaxLines(1).Tooltip(entry.Problem)
							}
						})
						ui.Icon(c, nativeIcons["branch"]).TextColor(c.Theme().TextMuted)
						ui.Text(c, entry.Branch).FontSize(12).MaxWidth(160).MaxLines(1)
						if entry.Changes > 0 {
							ui.Badge(c, fmt.Sprintf("%d", entry.Changes))
						}
					}).OnClick(func() { app.openRepository(entry.Path) })
					if iconButton(c, "管理", nativeIcons["more"]).Clicked() {
						app.repositoryEntryDialog(entry)
					}
				})
				ui.Divider(c)
			}
		})
	})
}

func (app *nativeApp) repositoryPanel(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		if app.repository == nil {
			return
		}
		app.operationBar(c)
		switch app.screen {
		case "stash":
			app.stashPanel(c)
		default:
			app.changesPanel(c)
		}
	})
}

func (app *nativeApp) operationBar(c *ui.Context) {
	operation := app.repository.snapshot.Operation
	if operation.Kind == "" {
		return
	}
	ui.Column(c).FillWidth().Padding(12).Gap(8).Background(c.Theme().Surface).Children(func() {
		ui.Text(c, "正在进行"+operationName(operation.Kind)).Bold()
		ui.Row(c).Gap(6).Children(func() {
			conflicts := slices.ContainsFunc(app.repository.snapshot.Files, func(file gitrepo.File) bool { return file.Conflict })
			if ui.Button(c, "继续").Disabled(!app.writable || conflicts).Clicked() {
				app.operationAction("continue")
			}
			if operation.Kind != "merge" {
				if ui.Button(c, "跳过").Disabled(!app.writable).Clicked() {
					app.operationAction("skip")
				}
			}
			if ui.Button(c, "中止").Disabled(!app.writable).Clicked() {
				app.operationAction("abort")
			}
		})
	})
}

func (app *nativeApp) changesPanel(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(12).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.TextInput(c, &app.fileQuery).Label("筛选文件").Search().Placeholder("筛选文件").Grow(1).MinWidth(0)
		iconButton(c, "文件选项", nativeIcons["more"]).Menu(func(menu *ui.Menu) {
			if menu.Item("按文件名排序").Checked(app.prefs.ByName).Chosen() {
				app.prefs.ByName = !app.prefs.ByName
			}
			menu.Separator()
			if menu.Item("储藏修改…").Disabled(!app.writable || len(app.repository.snapshot.Files) == 0 || app.repository.snapshot.Operation.Kind != "").Chosen() {
				app.stashDialog()
			}
		})
	})
	ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Padding(0, 6, 8).Children(func() {
		for _, staged := range []bool{false, true} {
			staged := staged
			label := "工作区"
			if staged {
				label = "暂存区"
			}
			files := app.visibleFiles(staged)
			slices.SortFunc(files, func(first, second gitrepo.File) int {
				if app.prefs.ByName {
					if order := strings.Compare(filepath.Base(first.Path), filepath.Base(second.Path)); order != 0 {
						return order
					}
				}
				return strings.Compare(first.Path, second.Path)
			})
			ui.Toolbar(c, func() {
				ui.Text(c, label).FontSize(12).Bold().TextColor(c.Theme().TextMuted)
				ui.Badge(c, fmt.Sprint(len(files)))
				ui.Spacer(c)
				action := "全部暂存"
				if staged {
					action = "全部取消"
				}
				count := 0
				for _, file := range files {
					if !file.Conflict {
						count++
					}
				}
				if app.fileQuery != "" {
					action = fmt.Sprintf("暂存筛选结果 (%d)", count)
					if staged {
						action = fmt.Sprintf("取消筛选结果暂存 (%d)", count)
					}
				}
				if ui.Button(c, action).FontSize(12).Disabled(!app.writable || count == 0).Clicked() {
					app.stageFiles(staged, true)
				}
			}).Label(label+"操作").FillWidth().Padding(4, 6).Margin(4, 0, 2)
			for _, file := range files {
				file := file
				ui.Row(c).Key(fmt.Sprintf("%t:%s", staged, file.Path)).FillWidth().Gap(2).AlignItems(ui.Center).Children(func() {
					key := fmt.Sprintf("%t:%s", staged, file.Path)
					checked := app.selectedFiles[key]
					if ui.Checkbox(c, &checked, "").Label("选择 " + label + " " + file.Path).Disabled(file.Conflict || !app.writable).Changed() {
						app.selectedFiles[key] = checked
					}
					status := file.Working
					if staged {
						status = file.Index
					}
					if file.Conflict {
						status = "!"
					}
					color := c.Theme().Warning
					switch status {
					case "A", "?":
						color = c.Theme().Success
					case "D", "!":
						color = c.Theme().Danger
					}
					if listRow(c, file.Path, app.selectedFile == file.Path && app.staged == staged).Grow(1).Tooltip(file.Path).Children(func() {
						nativeFileIcon(c, file.Path)
						ui.Text(c, filepath.Base(file.Path)).Grow(1).MinWidth(0).MaxLines(1)
						if directory := filepath.Dir(file.Path); directory != "." {
							ui.Text(c, directory).FontSize(11).MaxWidth(90).MaxLines(1).TextColor(c.Theme().TextMuted)
						}
						ui.Text(c, status).Font("monospace").FontSize(11).TextColor(color)
					}).Clicked() {
						app.selectFile(file.Path, staged)
					}
				})
			}
			action := "暂存所选文件"
			if staged {
				action = "取消暂存所选文件"
			}
			if count := len(app.checkedFiles(staged)); count > 0 {
				ui.Toolbar(c, func() {
					if ui.Button(c, fmt.Sprintf("%s (%d)", action, count)).Label(action).Disabled(!app.writable).Clicked() {
						app.stageFiles(staged, false)
					}
				}).FillWidth().Label(label + "批量操作")
			}
		}
	})
	ui.Divider(c)
	ui.Column(c).FillWidth().Padding(12).Gap(8).Disabled(!app.writable).Children(func() {
		draft := app.prefs.Drafts[app.repository.snapshot.Path]
		if ui.TextArea(c, &draft).Label("提交说明").Placeholder("提交说明").Lines(3, 6).FillWidth().Changed() {
			app.prefs.Drafts[app.repository.snapshot.Path] = draft
		}
		count := 0
		conflicts := false
		for _, file := range app.repository.snapshot.Files {
			if isStaged(file) {
				count++
			}
			conflicts = conflicts || file.Conflict
		}
		if ui.PrimaryButton(c, fmt.Sprintf("提交 %d 个文件", count)).FillWidth().Disabled(count == 0 || conflicts || app.repository.snapshot.Operation.Kind != "" || strings.TrimSpace(draft) == "").Clicked() {
			app.commit()
		}
	})
}

func (app *nativeApp) stashPanel(c *ui.Context) {
	ui.Column(c).Padding(12).Gap(8).FillWidth().Children(func() {
		ui.Text(c, "储藏").FontSize(18).Bold()
		if ui.Button(c, "新建储藏").FillWidth().Disabled(!app.writable || len(app.repository.snapshot.Files) == 0 || app.repository.snapshot.Operation.Kind != "").Clicked() {
			app.stashDialog()
		}
	})
	ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Padding(12).Children(func() {
		if len(app.repository.stashes) == 0 {
			ui.Text(c, "暂无储藏").TextColor(c.Theme().TextMuted)
		}
		for _, stash := range app.repository.stashes {
			stash := stash
			if listRow(c, stash.Message, app.stashHash == stash.Hash).Children(func() {
				ui.Icon(c, nativeIcons["stash"]).TextColor(c.Theme().TextMuted)
				ui.Text(c, stash.Message).Grow(1).MinWidth(0).MaxLines(2)
			}).Clicked() {
				app.selectStash(stash.Hash)
			}
		}
	})
	if app.stashHash != "" {
		ui.Row(c).Padding(12).Gap(6).Disabled(!app.writable).Children(func() {
			if ui.Button(c, "应用").Clicked() {
				app.applyStash(false)
			}
			if ui.Button(c, "应用并删除").Clicked() {
				app.applyStash(true)
			}
			if ui.Button(c, "删除").Clicked() {
				app.dropStash()
			}
		})
	}
}

func (app *nativeApp) diffPanel(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		if app.conflict != nil {
			app.conflictPanel(c)
			return
		}
		ui.Row(c).FillWidth().Padding(10, 14).Gap(8).AlignItems(ui.Center).Children(func() {
			title := app.selectedFile
			if app.screen == "history" {
				title = app.commitFile
				if title == "" {
					title = shortHash(app.commitHash)
				}
			} else if app.screen == "stash" {
				title = "储藏差异"
			}
			ui.Text(c, title).Bold().Grow(1).MinWidth(0).MaxLines(1).Tooltip(title)
			layout := 0
			if app.prefs.Split {
				layout = 1
			}
			if ui.Segmented(c, &layout, "统一", "并排").Label("差异布局").Changed() {
				app.prefs.Split = layout == 1
			}
			ui.Checkbox(c, &app.prefs.Wrap, "换行")
		})
		if app.screen == "changes" && app.selectedFile != "" {
			app.patchToolbar(c)
		}
		ui.Divider(c)
		if app.diff.Truncated {
			ui.Text(c, "预览已截断，请使用整文件操作").Padding(10).TextColor(c.Theme().Warning)
		}
		if len(app.diffViews) == 1 {
			app.configureDiff(app.diffViews[0])
			editor.DiffView(c, app.diffViews[0]).Grow(1).FillWidth().MinHeight(0)
		} else if len(app.diffViews) > 1 {
			ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
				for index, state := range app.diffViews {
					ui.Column(c).Key(index).FillWidth().Children(func() {
						ui.Text(c, app.diffNames[index]).Bold().Padding(10, 14)
						app.configureDiff(state)
						state.FitContent = true
						editor.DiffView(c, state).FillWidth()
					})
				}
			})
		} else if app.rawDiff.Buffer != nil && app.diff.Text != "" {
			app.rawDiff.SoftWrap = app.prefs.Wrap
			editor.View(c, &app.rawDiff).Grow(1).FillWidth().MinHeight(0)
		} else {
			ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
				message := "选择文件或提交查看差异"
				if app.screen == "stash" {
					message = "选择储藏查看差异"
					if len(app.repository.stashes) == 0 {
						message = "暂无储藏差异"
					}
				}
				ui.Text(c, message).TextColor(c.Theme().TextMuted)
			})
		}
	})
}

func (app *nativeApp) configureDiff(state *editor.DiffState) {
	state.Wrap = app.prefs.Wrap
	state.Layout = editor.DiffUnified
	if app.prefs.Split {
		state.Layout = editor.DiffSplit
	}
}

func (app *nativeApp) patchToolbar(c *ui.Context) {
	ui.Toolbar(c, func() {
		action := "暂存文件"
		if app.staged {
			action = "取消暂存文件"
		}
		if ui.Button(c, action).Clicked() {
			app.stageSelectedFile()
		}
		if !app.staged {
			if ui.Button(c, "丢弃修改…").Clicked() {
				app.discardFile()
			}
		}
		if ui.Button(c, "文件历史").Clicked() {
			app.historyFile, app.historyQuery, app.screen = app.selectedFile, "", "history"
			app.searchHistory(false)
		}
		if !app.patch.Supported || len(app.patch.Hunks) == 0 {
			return
		}
		if ui.Button(c, "‹").Label("上一区块").Tooltip("上一区块").Disabled(app.hunk == 0).Clicked() {
			app.hunk--
			app.goToPatchHunk()
		}
		ui.Text(c, fmt.Sprintf("%d/%d", app.hunk+1, len(app.patch.Hunks))).FontSize(12)
		if ui.Button(c, "›").Label("下一区块").Tooltip("下一区块").Disabled(app.hunk+1 >= len(app.patch.Hunks)).Clicked() {
			app.hunk++
			app.goToPatchHunk()
		}
		label := "暂存此区块"
		if app.staged {
			label = "取消此区块暂存"
		}
		if ui.Button(c, label).Clicked() {
			app.applyHunk()
		}
		if ui.Button(c, "选择行…").Clicked() {
			app.linesDialog()
		}
	}).Label("差异操作").FillWidth().Padding(2, 10, 8).Disabled(!app.writable)
}

func (app *nativeApp) conflictPanel(c *ui.Context) {
	conflict := app.conflict
	ui.Column(c).FillWidth().Padding(12).Gap(8).Children(func() {
		ui.Text(c, conflict.Path).Bold().MaxLines(1)
		ui.Row(c).Gap(6).Wrap().Disabled(!app.writable).Children(func() {
			if ui.Button(c, "使用当前版本").Clicked() {
				app.resolveConflict("ours")
			}
			if ui.Button(c, "使用传入版本").Clicked() {
				app.resolveConflict("theirs")
			}
			if ui.Button(c, "删除文件").Clicked() {
				app.resolveConflict("delete")
			}
			if ui.PrimaryButton(c, "保存结果并暂存").Disabled(!conflict.Editable).Clicked() {
				app.resolveConflict("result")
			}
		})
	})
	if conflict.Editable {
		ui.Row(c).FillWidth().Height(190).Children(func() {
			for index, name := range []string{"基础版本", "当前版本", "传入版本"} {
				ui.Column(c).Key(name).Grow(1).MinWidth(0).FillHeight().Children(func() {
					ui.Text(c, name).FontSize(12).Padding(8)
					editor.View(c, &app.conflictBase[index]).Grow(1).FillWidth().MinHeight(0)
				})
			}
		})
		ui.Divider(c)
		editor.View(c, &app.conflictEdit).Grow(1).FillWidth().MinHeight(0)
	} else {
		ui.Text(c, "此冲突无法进行文本编辑，请选择完整版本或删除文件。").Padding(20)
	}
}
