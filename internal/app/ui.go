// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ysicing/51code/internal/gitrepo"
	"github.com/ysicing/51code/internal/workspace"
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/plugins/updater"
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
	"close":    nativeSymbol(`<path d="m6 6 12 12M6 18 18 6"/>`),
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

func (app *nativeApp) feedbackButton(c *ui.Context) {
	label := locales.Feedback()
	button := ui.ButtonBase(c).Key("feedback").Label(label).Tooltip(label).
		Padding(6).Radius(c.Theme().Radius).Shrink(0).AlignItems(ui.Center)
	color := c.Theme().TextMuted
	if button.Pressed() {
		button.Background(c.Theme().SurfacePressed)
		color = c.Theme().Accent
	} else if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
		color = c.Theme().Accent
	}
	if button.Children(func() {
		sfsymbols.Symbol(c, "bubble.left").Size(14).Color(color).Role(ui.RoleNone)
	}).Clicked() {
		c.OpenURLThen("https://github.com/ysicing/51code/issues/new", func(err error) {
			if err != nil {
				app.problem = locales.FeedbackOpenFailed(err.Error())
			}
		})
	}
}

func toolButton(c *ui.Context, label string, icon *ui.SVG) ui.Element {
	return ui.Button(c, "").Label(label).Children(func() {
		ui.Icon(c, icon).FontSize(16)
		ui.Text(c, label).SingleLine()
	})
}

func titlebarButton(c *ui.Context, label string, icon *ui.SVG) ui.Element {
	return ui.Button(c, "").Label(label).Tooltip(label).Height(44).MinWidth(48).Padding(4, 8).Children(func() {
		ui.Column(c).AlignItems(ui.Center).Gap(2).Children(func() {
			ui.Icon(c, icon).FontSize(16)
			ui.Text(c, label).FontSize(11).SingleLine()
		})
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
		bar := c.TitleBar()
		ui.Row(c).Height(52).Shrink(0).Padding(0, bar.Right+12, 0, bar.Left+12).AlignItems(ui.Center).Gap(12).DragWindow().Children(func() {
			title := locales.Repository()
			if app.repository != nil && app.screen != "library" {
				title = app.repository.snapshot.Name
			}
			ui.Column(c).Width(220).MinWidth(0).Shrink(1).Gap(2).Children(func() {
				ui.Text(c, title).Bold().FontSize(14).MaxLines(1).FillWidth().Tooltip(title)
				if app.repository != nil && app.screen != "library" {
					snapshot := app.repository.snapshot
					ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
						ui.Icon(c, nativeIcons["branch"]).FontSize(12).TextColor(c.Theme().TextMuted)
						ui.Text(c, snapshot.Branch).FontSize(11).TextColor(c.Theme().TextMuted).MaxLines(1).MinWidth(0).Shrink(1).Tooltip(snapshot.Branch)
						if snapshot.Ahead > 0 || snapshot.Behind > 0 {
							ui.Text(c, fmt.Sprintf("↑%d ↓%d", snapshot.Ahead, snapshot.Behind)).FontSize(11).TextColor(c.Theme().TextMuted)
						}
					})
				}
			})
			app.toolbar(c)
			ui.Toolbar(c, func() {
				if iconButton(c, locales.Settings(), nativeIcons["settings"]).Tooltip(locales.Settings() + "（⌘,）").Disabled(app.busy || app.dialog != nil).Clicked() {
					app.settingsDialog()
				}
			}).Label(locales.ApplicationActions()).Padding(0).Shrink(0)
		})
		ui.Divider(c)
		ui.Column(c).Grow(1).MinHeight(0).Disabled(app.busy).Children(func() {
			ui.Split(c, &app.prefs.Sidebar, func() { app.sidebar(c) }, func() {
				ui.Column(c).Fill().MinWidth(0).Children(func() {
					switch app.screen {
					case "library":
						app.libraryView(c)
					case "history", "search":
						app.historyWorkspace(c)
					case "compare":
						app.comparisonWorkspace(c)
					case "worktrees":
						app.worktreePanel(c)
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
				if app.scanning {
					if ui.Button(c, locales.Cancel()).Clicked() {
						app.service.CancelScan()
						app.service.CancelHistory()
					}
				}
			} else if app.problem != "" {
				summary := strings.SplitN(app.problem, "\n", 2)[0]
				if strings.HasPrefix(summary, "git ") || strings.HasPrefix(summary, "fatal:") || strings.HasPrefix(summary, "error:") || strings.HasPrefix(summary, "ssh:") {
					summary = locales.GitOperationFailed()
				}
				ui.Text(c, summary).FontSize(12).TextColor(c.Theme().Danger).MaxLines(1).Grow(1).Selectable().Tooltip(app.problem)
				if ui.Button(c, locales.ShowDetails()).Clicked() {
					problem := app.problem
					app.dialog = &nativeDialog{
						title: locales.OperationFailed(),
						body: func(c *ui.Context) {
							ui.Scroll(c).Height(280).FillWidth().Children(func() {
								ui.Text(c, problem).FontSize(12).Selectable().FillWidth()
							})
						},
					}
				}
				if ui.Button(c, locales.DismissNotice()).Clicked() {
					app.problem = ""
				}
			} else if app.repository != nil && app.screen != "library" {
				ui.Text(c, app.repository.snapshot.Path).FontSize(12).TextColor(c.Theme().TextMuted).MinWidth(0).MaxLines(1).Shrink(1).Selectable()
			} else {
				ui.Text(c, locales.LocalRepositoryCount(len(app.library.Repositories))).FontSize(12).TextColor(c.Theme().TextMuted)
			}
			ui.Spacer(c)
			app.feedbackButton(c)
			if app.versionButton(c) {
				updater.CheckForUpdates()
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
				ui.SidebarItem(c, "screen:library", nativeIcons["folder"], locales.LocalRepositories())
				if len(app.library.Groups) > 0 {
					ui.SidebarSection(c, locales.RepositoryGroups(), nil, func() {
						for _, group := range app.library.Groups {
							group := group
							ui.Row(c).Key(group.ID).FillWidth().AlignItems(ui.Center).Children(func() {
								ui.SidebarItem(c, "group:"+group.ID, nativeIcons["folder"], group.Name).Grow(1).MinWidth(0).Tooltip(group.Name).Label(group.Name).ContextMenu(func(menu *ui.Menu) {
									if menu.Item(locales.Rename()).Chosen() {
										app.groupDialog(group.ID)
									}
									if menu.Item(locales.RemoveGroup()).Chosen() {
										app.removeGroup(group)
									}
								})
								iconButton(c, locales.ManageGroup(group.Name), nativeIcons["more"]).OnClick(func() { app.groupActions(group) })
							})
						}
					})
				}
				return
			}
			ui.Row(c).Height(30).Padding(0, 8).Gap(8).AlignItems(ui.Center).Label(locales.RepositoryWorkspace()).Children(func() {
				ui.Icon(c, nativeIcons["folder"]).FontSize(18).TextColor(c.Theme().TextMuted)
				ui.Text(c, locales.Workspace()).FontSize(12).FontWeight(500).TextColor(c.Theme().TextMuted)
			})
			ui.Column(c).FillWidth().Children(func() {
				ui.SidebarItem(c, "screen:changes", nil, locales.FileChanges()).Label(locales.FileChanges()).
					Height(26).Padding(0, 8, 0, 40).FontSize(12).Children(func() {
					if count := len(app.repository.snapshot.Files); count > 0 {
						ui.Badge(c, fmt.Sprint(count))
					}
				})
				ui.SidebarItem(c, "screen:history", nil, locales.CommitHistory()).Height(26).Padding(0, 8, 0, 40).FontSize(12)
				if app.prefs.SidebarItems.Search {
					ui.SidebarItem(c, "screen:search", nil, locales.Search()).Height(26).Padding(0, 8, 0, 40).FontSize(12)
				}
				if app.prefs.SidebarItems.Compare {
					ui.SidebarItem(c, "screen:compare", nil, locales.CompareVersions()).Height(26).Padding(0, 8, 0, 40).FontSize(12)
				}
				if app.prefs.SidebarItems.Stash {
					ui.SidebarItem(c, "screen:stash", nil, locales.Stash()).Label(locales.Stash()).
						Height(26).Padding(0, 8, 0, 40).FontSize(12).Children(func() {
						if count := len(app.repository.stashes); count > 0 {
							ui.Badge(c, fmt.Sprint(count))
						}
					})
				}
				if app.prefs.SidebarItems.Worktrees {
					ui.SidebarItem(c, "screen:worktrees", nil, locales.Worktrees()).Height(26).Padding(0, 8, 0, 40).FontSize(12)
				}
			})
			app.localBranchSidebar(c)
			app.tagSidebar(c)
			app.remoteSidebar(c)
		}).Label(locales.RepositoryNavigation()).Grow(1).MinHeight(0).FillWidth().Changed() {
			switch {
			case strings.HasPrefix(selected, "screen:"):
				screen := strings.TrimPrefix(selected, "screen:")
				app.leaveConflict(func() {
					app.switchWorkspace(screen)
					app.group = ""
					if screen == "worktrees" {
						app.loadWorktrees()
					} else if screen == "history" && !app.historyLoaded {
						app.searchHistory(false)
					} else if (screen == "history" || screen == "search" && app.historyLoaded) &&
						app.historyNeedsRefresh() {
						app.refresh()
					}
				})
			case strings.HasPrefix(selected, "group:"):
				app.leaveConflict(func() { app.screen, app.group = "library", strings.TrimPrefix(selected, "group:") })
			}
		}
		if inLibrary {
			ui.Toolbar(c, func() {
				if toolButton(c, locales.NewGroup(), nativeIcons["plus"]).Clicked() {
					app.groupDialog("")
				}
			}).FillWidth().Padding(8, 10).Label(locales.RepositoryGroupActions())
		} else {
			ui.Divider(c)
			ui.Column(c).FillWidth().Padding(6, 10).Children(func() {
				button := ui.ButtonBase(c).Label(locales.BackToRepositories()).FillWidth().Height(28).
					Padding(0, 8).Gap(8).AlignItems(ui.Center).Radius(5).TextColor(c.Theme().TextMuted)
				if button.Hovered() || button.Pressed() {
					button.Background(c.Theme().SurfaceHover)
				}
				button.Children(func() {
					ui.Icon(c, nativeIcons["back"]).FontSize(15)
					ui.Text(c, locales.BackToRepositories()).FontSize(12)
				})
				if button.Clicked() {
					app.showLibrary()
				}
			})
		}
	})
}

func (app *nativeApp) toolbar(c *ui.Context) {
	disabled := app.busy || app.dialog != nil
	ui.Toolbar(c, func() {
		ui.Spacer(c)
		if app.screen == "library" {
			if !app.libraryEmpty() {
				if ui.PrimaryButton(c, locales.AddRepository()).Disabled(disabled).Clicked() {
					app.chooseLocal()
				}
				if ui.Button(c, locales.EnterPath()).Disabled(disabled).Clicked() {
					app.pathDialog()
				}
				if toolButton(c, locales.ScanFolders(), nativeIcons["folder"]).Disabled(disabled).Clicked() {
					app.chooseScan()
				}
			}
		} else if app.repository != nil {
			ui.Row(c).Children(func() {
				for _, remote := range []struct{ id, title string }{{"fetch", locales.Fetch()}, {"pull", locales.Pull()}, {"push", locales.Push()}} {
					remote := remote
					if titlebarButton(c, remote.title, nativeIcons[remote.id]).Disabled(disabled || !app.writable).Clicked() {
						app.remoteAction(remote.id)
					}
				}
			})
			if titlebarButton(c, locales.Branch(), nativeIcons["branch"]).Disabled(disabled || !app.writable).Clicked() {
				app.branchDialog()
			}
			if titlebarButton(c, locales.Merge(), nativeIcons["merge"]).Disabled(disabled || !app.canMerge()).Clicked() {
				app.mergeDialog()
			}
			if titlebarButton(c, locales.RepositorySettings(), nativeIcons["settings"]).Disabled(disabled).Clicked() {
				app.repositorySettingsDialog()
			}
			iconButton(c, locales.More(), nativeIcons["more"]).Disabled(disabled).Menu(app.repositoryActions)
		}
		if iconButton(c, locales.Refresh(), nativeIcons["refresh"]).Disabled(disabled).Clicked() {
			app.refresh()
		}
	}).Label(locales.RepositoryActions()).Grow(1).MinWidth(0).Height(52).Padding(0)
}

func (app *nativeApp) libraryEmpty() bool {
	return app.query == "" && !slices.ContainsFunc(app.library.Repositories, func(entry workspace.LocalRepository) bool {
		return app.group == "" || entry.GroupID == app.group
	})
}

func (app *nativeApp) emptyLibraryActions(c *ui.Context) {
	ui.Column(c).Key("empty-library").Grow(1).FillWidth().Center().Gap(12).
		Disabled(app.busy || app.dialog != nil).Children(func() {
		ui.PrimaryButton(c, locales.AddRepository()).Width(168).Height(36).OnClick(app.chooseLocal)
		ui.Toolbar(c, func() {
			ui.Button(c, locales.EnterPath()).TextColor(c.Theme().TextMuted).OnClick(app.pathDialog)
			ui.Button(c, locales.ScanFolders()).TextColor(c.Theme().TextMuted).OnClick(app.chooseScan)
		}).Label(locales.AddExistingRepository()).Gap(24).Padding(0)
	})
}

func (app *nativeApp) libraryView(c *ui.Context) {
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(22).Gap(16).Children(func() {
		for _, group := range app.library.Groups {
			if group.ID == app.group {
				ui.Text(c, group.Name).FontSize(24).Bold()
				break
			}
		}
		if app.libraryEmpty() {
			app.emptyLibraryActions(c)
			return
		}
		ui.TextInput(c, &app.query).Search().Placeholder(locales.SearchRepositories()).FillWidth()
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
				message := locales.EmptyRepositories()
				if app.query != "" {
					message = locales.NoMatchingRepositories()
				} else if app.group != "" {
					message = locales.EmptyGroup()
				}
				ui.Text(c, message).FontSize(17).Bold()
				if app.query != "" {
					if ui.Button(c, locales.ClearSearch()).Clicked() {
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
					if iconButton(c, locales.Manage(), nativeIcons["more"]).Clicked() {
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
		ui.Text(c, locales.OperationInProgress(operationName(operation.Kind))).Bold()
		ui.Row(c).Gap(6).Children(func() {
			conflicts := slices.ContainsFunc(app.repository.snapshot.Files, func(file gitrepo.File) bool { return file.Conflict })
			if ui.Button(c, locales.Continue()).Disabled(!app.writable || conflicts).Clicked() {
				app.operationAction("continue")
			}
			if operation.Kind != "merge" {
				if ui.Button(c, locales.Skip()).Disabled(!app.writable).Clicked() {
					app.operationAction("skip")
				}
			}
			if ui.Button(c, locales.Abort()).Disabled(!app.writable).Clicked() {
				app.operationAction("abort")
			}
		})
	})
}

func (app *nativeApp) changesPanel(c *ui.Context) {
	app.fileControls(c)
	area := ui.Column(c).Grow(1).MinHeight(0).FillWidth()
	area.Children(func() {
		height := max(1, area.LayoutBounds().H-1)
		target := app.prefs.ChangesSplit
		if target == 0 {
			staged, working := app.changesSectionHeight(c, true), app.changesSectionHeight(c, false)
			target = 100 * min(staged, max(height/2, height-working)) / height
		}
		minimum := min(50, 100*72/height)
		target = max(minimum, min(100-minimum, target))
		// 仅在布局目标变化时同步绑定值，避免普通重绘覆盖刚拖动的位置。
		if target != app.changesSplitTarget {
			app.changesSplit, app.changesSplitTarget = target, target
			c.AnimationFrame()
		}
		app.changesSplit = max(minimum, min(100-minimum, app.changesSplit))
		if ui.SplitVerticalPercent(c, &app.changesSplit, func() {
			app.changesSection(c, true)
		}, func() {
			app.changesSection(c, false)
		}).Grow(1).MinHeight(0).FillWidth().Changed() {
			app.prefs.ChangesSplit = app.changesSplit
		}
	})
	ui.Divider(c)
	ui.Column(c).FillWidth().Padding(12).Gap(8).Disabled(!app.writable).Children(func() {
		draft := app.prefs.Drafts[app.repository.snapshot.Path]
		app.commitAIHeader(c)
		if ui.TextArea(c, &draft).Bind(&app.commitInput).Label(locales.CommitMessage()).Placeholder(locales.CommitMessageHint()).Lines(3, 6).FillWidth().Changed() {
			app.prefs.Drafts[app.repository.snapshot.Path] = draft
			app.draftRevision++
		}
		app.commitAIResult(c)
		count := 0
		conflicts := false
		for _, file := range app.repository.snapshot.Files {
			if isStaged(file) {
				count++
			}
			conflicts = conflicts || file.Conflict
		}
		if ui.PrimaryButton(c, locales.CommitFiles(count)).FillWidth().Disabled(count == 0 || conflicts || app.repository.snapshot.Operation.Kind != "" || strings.TrimSpace(draft) == "").Clicked() {
			app.commit()
		}
	})
}

func (app *nativeApp) changesSectionHeight(c *ui.Context, staged bool) float32 {
	files := app.visibleFiles(staged)
	if len(files) == 0 {
		return 72
	}
	rows := len(files)
	if app.prefs.FileTree {
		rows, _ = app.fileTreeSize(c, nativeFileNodes(files, app.prefs.FileSort, app.prefs.FileDescending, staged), staged, 0)
	}
	height := 44 + float32(rows)*28
	if len(app.checkedFiles(staged)) > 0 {
		height += 40
	}
	return height
}

func (app *nativeApp) changesSection(c *ui.Context, staged bool) {
	label, action, selectedAction := locales.UnstagedFiles(), locales.StageAll(), locales.StageSelected()
	if staged {
		label, action, selectedAction = locales.StagedFiles(), locales.UnstageAll(), locales.UnstageSelected()
	}
	files := app.visibleFiles(staged)
	slices.SortFunc(files, func(first, second gitrepo.File) int {
		return compareNativeFiles(first, second, app.prefs.FileSort, app.prefs.FileDescending, staged)
	})
	count := 0
	var pathWidth float32
	for _, file := range files {
		if !file.Conflict {
			count++
		}
		pathWidth = max(pathWidth, app.filePathWidth(c, file.Path))
	}
	rowCount, listWidth := len(files), pathWidth+88
	var nodes []*nativeFileNode
	if app.prefs.FileTree {
		nodes = nativeFileNodes(files, app.prefs.FileSort, app.prefs.FileDescending, staged)
		rowCount, listWidth = app.fileTreeSize(c, nodes, staged, 0)
	}
	if app.filesFiltered() {
		action = locales.StageFiltered(fmt.Sprint(count))
		if staged {
			action = locales.UnstageFiltered(fmt.Sprint(count))
		}
	}
	section := ui.Column(c).Key(staged).FillWidth().MinHeight(0)
	if len(files) == 0 {
		section.Height(72)
	} else {
		height := 44 + float32(rowCount)*28
		if len(app.checkedFiles(staged)) > 0 {
			height += 40
		}
		section.Grow(1).MaxHeight(height)
	}
	section.Children(func() {
		ui.Toolbar(c, func() {
			ui.Text(c, label).FontSize(12).Bold()
			badge := fmt.Sprint(len(files))
			if app.filesFiltered() {
				total := 0
				for _, file := range app.repository.snapshot.Files {
					if staged && isStaged(file) || !staged && isWorking(file) {
						total++
					}
				}
				badge = fmt.Sprintf("%d / %d", len(files), total)
			}
			ui.Badge(c, badge)
			ui.Spacer(c)
			if ui.Button(c, action).FontSize(12).Disabled(!app.writable || count == 0).Clicked() {
				app.stageFiles(staged, true)
			}
		}).Label(locales.SectionActions(label)).FillWidth().Padding(4, 12)
		if len(files) == 0 {
			message := locales.NoUnstagedFiles()
			if staged {
				message = locales.NoStagedFiles()
			}
			if app.filesFiltered() {
				message = locales.NoMatchingFiles()
			}
			ui.Text(c, message).FontSize(12).TextColor(c.Theme().TextMuted).Padding(6, 12)
			return
		}
		if app.prefs.FileTree {
			scroll := nativeFileScroll(ui.ScrollBoth(c).Label(locales.SectionList(label)).Grow(1).MinHeight(0).FillWidth().Padding(0, 6, 6))
			scroll.Children(func() {
				ui.Column(c).FillWidth().MinWidth(max(listWidth, scroll.LayoutBounds().W-12)).Children(func() {
					tree := ui.TreeBase(c)
					tree.Root.Label(locales.SectionTree(label)).FillWidth().Children(func() {
						app.fileTree(c, tree, nodes, staged, 0)
					})
				})
			})
		} else {
			list := ui.List(c, nil, len(files)).
				ItemKey(func(index int) any { return files[index].Path }).
				Label(locales.SectionList(label)).Grow(1).MinHeight(0).FillWidth().
				Padding(0, 6, 6).RowWidth(listWidth)
			nativeFileScroll(list).
				Rows(func(row ui.ListRow) { app.changeFileRow(row.Context, files[row.Index], staged, nil, 0) })
		}
		if count := len(app.checkedFiles(staged)); count > 0 {
			ui.Toolbar(c, func() {
				if ui.Button(c, fmt.Sprintf("%s (%d)", selectedAction, count)).Label(selectedAction).Disabled(!app.writable).Clicked() {
					app.stageFiles(staged, false)
				}
			}).FillWidth().Padding(4, 12).Label(locales.SectionBatchActions(label))
		}
	})
}

func (app *nativeApp) stashPanel(c *ui.Context) {
	ui.Column(c).Padding(12).Gap(8).FillWidth().Children(func() {
		ui.Text(c, locales.Stash()).FontSize(18).Bold()
		if ui.Button(c, locales.NewStash()).FillWidth().Disabled(!app.writable || len(app.repository.snapshot.Files) == 0 || app.repository.snapshot.Operation.Kind != "").Clicked() {
			app.stashDialog()
		}
	})
	ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Padding(12).Children(func() {
		if len(app.repository.stashes) == 0 {
			ui.Text(c, locales.EmptyStashes()).TextColor(c.Theme().TextMuted)
		}
		for _, stash := range app.repository.stashes {
			stash := stash
			if listRow(c, stash.Message, app.stashHash == stash.Hash).Children(func() {
				ui.Icon(c, nativeIcons["stash"]).TextColor(c.Theme().TextMuted)
				ui.Text(c, stash.Message).Grow(1).MinWidth(0).MaxLines(2)
			}).Clicked() && (app.stashHash != stash.Hash || app.readTask != nil || app.problem != "") {
				app.selectStash(stash.Hash)
			}
		}
	})
	if app.stashHash != "" {
		ui.Row(c).Padding(12).Gap(6).Disabled(!app.writable || app.readTask != nil).Children(func() {
			if ui.Button(c, locales.Apply()).Clicked() {
				app.applyStash(false)
			}
			if ui.Button(c, locales.ApplyAndDrop()).Clicked() {
				app.applyStash(true)
			}
			if ui.Button(c, locales.Delete()).Clicked() {
				app.dropStash()
			}
		})
	}
}

func (app *nativeApp) diffPanel(c *ui.Context) {
	app.fileDiffPanel(c)
}

func (app *nativeApp) configureDiff(state *editor.DiffState) {
	state.Wrap = app.prefs.Wrap
	state.Layout = editor.DiffUnified
	if app.prefs.DiffLayout == "split" || app.prefs.DiffLayout == "auto" && app.diffAutoSplit {
		state.Layout = editor.DiffSplit
	}
}

func (app *nativeApp) patchToolbar(c *ui.Context) {
	ui.Toolbar(c, func() {
		action := locales.StageFile()
		if app.staged {
			action = locales.UnstageFile()
		}
		if ui.Button(c, action).Disabled(!app.writable).Clicked() {
			app.stageSelectedFile()
		}
		if ui.Button(c, locales.FileHistory()).Clicked() {
			app.fileHistoryAction()
		}
		if !app.staged {
			if ui.Button(c, locales.DiscardChangesMenu()).Disabled(!app.writable).Clicked() {
				app.discardFile()
			}
		}
		if !app.patch.Supported || len(app.patch.Hunks) == 0 {
			return
		}
		if ui.Button(c, "‹").Label(locales.PreviousHunk()).Tooltip(locales.PreviousHunk()).Disabled(app.hunk == 0).Clicked() {
			app.hunk--
			app.goToPatchHunk()
		}
		ui.Text(c, fmt.Sprintf("%d/%d", app.hunk+1, len(app.patch.Hunks))).FontSize(12)
		if ui.Button(c, "›").Label(locales.NextHunk()).Tooltip(locales.NextHunk()).Disabled(app.hunk+1 >= len(app.patch.Hunks)).Clicked() {
			app.hunk++
			app.goToPatchHunk()
		}
		label := locales.StageHunk()
		if app.staged {
			label = locales.UnstageHunk()
		}
		if ui.Button(c, label).Disabled(!app.writable || !app.canApplyPatch()).Clicked() {
			app.applyHunk()
		}
		if ui.Button(c, locales.SelectLinesMenu()).Disabled(!app.writable || !app.canApplyPatch()).Clicked() {
			app.linesDialog()
		}
	}).Label(locales.DiffActions()).FillWidth().Padding(2, 0, 6)
}

func (app *nativeApp) conflictPanel(c *ui.Context) {
	conflict := app.conflict
	ui.Column(c).FillWidth().Padding(12).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			title := conflict.Path
			if app.readTask != nil {
				ui.Spinner(c).Size(14, 14)
				title = app.readTask.label
			}
			ui.Text(c, title).Bold().MinWidth(0).Grow(1).MaxLines(1)
		})
		ui.Row(c).Gap(6).Wrap().Disabled(!app.writable).Children(func() {
			if ui.Button(c, locales.UseOurs()).Clicked() {
				app.resolveConflict("ours")
			}
			if ui.Button(c, locales.UseTheirs()).Clicked() {
				app.resolveConflict("theirs")
			}
			if ui.Button(c, locales.DeleteFile()).Clicked() {
				app.resolveConflict("delete")
			}
			if ui.PrimaryButton(c, locales.SaveAndStage()).Disabled(!conflict.Editable).Clicked() {
				app.resolveConflict("result")
			}
		})
	})
	if conflict.Editable {
		ui.Row(c).FillWidth().Height(190).Children(func() {
			for index, name := range []string{locales.BaseVersion(), locales.CurrentVersionLabel(), locales.IncomingVersion()} {
				ui.Column(c).Key(index).Grow(1).MinWidth(0).FillHeight().Children(func() {
					ui.Text(c, name).FontSize(12).Padding(8)
					editor.View(c, &app.conflictBase[index]).Grow(1).FillWidth().MinHeight(0)
				})
			}
		})
		ui.Divider(c)
		editor.View(c, &app.conflictEdit).Grow(1).FillWidth().MinHeight(0)
	} else {
		ui.Text(c, locales.NonTextConflict()).Padding(20)
	}
}
