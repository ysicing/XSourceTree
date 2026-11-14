// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"github.com/ysicing/51code/internal/gitrepo"
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type nativeFileViewer struct {
	full           bool
	side           int
	sources        [2]*gitrepo.FileContent
	states         [2]editor.State
	contextChanged bool
}

func (app *nativeApp) canApplyPatch() bool {
	return app.patch.Supported && !app.fileViewer.contextChanged
}

func (viewer *nativeFileViewer) setSources(diff gitrepo.Diff) {
	sources := [2]*gitrepo.FileContent{diff.Old, diff.New}
	sameFile := viewer.sources[1] != nil && diff.New != nil &&
		viewer.sources[1].Path == diff.New.Path && viewer.sources[1].Revision == diff.New.Revision
	for index, source := range sources {
		previous := viewer.sources[index]
		if source != nil && previous != nil && *source == *previous {
			continue
		}
		viewer.states[index] = editor.State{}
		if source != nil && source.Present && !source.Binary && !source.Truncated && !source.Unsupported {
			buffer := editor.NewBuffer(source.Text)
			buffer.SetLanguage(editor.LanguageForText(source.Path, source.Text))
			viewer.states[index] = editor.State{
				Buffer: buffer, ReadOnly: true, Label: locales.FileContentLabel(), NoScrollPastEnd: true,
			}
		}
	}
	viewer.sources = sources
	if !sameFile {
		viewer.side = 1
		if diff.New != nil && !diff.New.Present && diff.Old != nil && diff.Old.Present {
			viewer.side = 0
		}
	}
	viewer.contextChanged = false
}

func fileVersionLabel(source *gitrepo.FileContent) string {
	if source == nil {
		return ""
	}
	switch source.Revision {
	case "WORKTREE":
		return locales.WorkingVersion()
	case "INDEX":
		return locales.IndexVersion()
	case "HEAD":
		return "HEAD"
	case "":
		return locales.AbsentVersion()
	default:
		return locales.CommitVersion(shortHash(source.Revision))
	}
}

func (app *nativeApp) fileVersionMenu(menu *ui.Menu) {
	for index, source := range app.fileViewer.sources {
		if source != nil && menu.Item(fileVersionLabel(source)).Radio(app.fileViewer.side == index).Chosen() {
			app.fileViewer.side = index
		}
	}
}

func (app *nativeApp) diffLayoutControl(c *ui.Context) {
	layouts := []struct{ value, label string }{
		{"auto", locales.AutomaticDiff()}, {"unified", locales.UnifiedDiff()}, {"split", locales.SplitDiff()},
	}
	labels := []string{layouts[0].label, layouts[1].label, layouts[2].label}
	selected := app.diffLayoutLabel()
	ui.Select(c, &selected, labels).Label(locales.DiffLayout()).Width(92).MinWidth(72).Shrink(0).OnChange(func() {
		for _, layout := range layouts {
			if selected == layout.label {
				app.prefs.DiffLayout = layout.value
				app.prefs.Split = layout.value == "split"
			}
		}
	})
}

func (app *nativeApp) diffLayoutLabel() string {
	switch app.prefs.DiffLayout {
	case "split":
		return locales.SplitDiff()
	case "unified":
		return locales.UnifiedDiff()
	default:
		return locales.AutomaticDiff()
	}
}

func (app *nativeApp) fileHistoryAction() {
	file := app.selectedFile
	if (app.screen == "history" || app.screen == "search") && !app.historyWorking {
		file = app.commitFile
	} else if app.screen == "compare" && app.comparison != nil {
		file = app.comparison.file
	}
	if app.fileViewer.full {
		if source := app.fileViewer.sources[app.fileViewer.side]; source != nil {
			file = source.Path
		}
	}
	app.switchWorkspace("search")
	app.historyFile, app.historyQuery, app.historyFilter = file, "", gitrepo.HistoryFilter{}
	app.searchDateEnabled = false
	app.executeSearch()
}

func (app *nativeApp) fileDiffPanel(c *ui.Context) {
	panel := ui.Column(c).Fill().MinWidth(0).Disabled(app.readTask != nil)
	panel.Children(func() {
		if app.conflict != nil {
			app.conflictPanel(c)
			return
		}
		if width := panel.LayoutBounds().W; width != app.diffWidth {
			app.diffWidth = width
			c.AnimationFrame()
		}
		if app.diffWidth >= 780 {
			app.diffAutoSplit = true
		} else if app.diffWidth < 740 {
			app.diffAutoSplit = false
		}
		hasSources := app.diff.Old != nil && app.diff.New != nil
		full := 0
		if hasSources && app.fileViewer.full {
			full = 1
		}
		ui.Row(c).FillWidth().Padding(8, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			title := app.selectedFile
			if (app.screen == "history" || app.screen == "search") && !app.historyWorking {
				title = app.commitFile
				if title == "" {
					title = shortHash(app.commitHash)
				}
			} else if app.screen == "stash" {
				title = locales.StashDiff()
			} else if app.screen == "compare" && app.comparison != nil {
				title = app.comparison.file
			}
			if full == 1 {
				if source := app.fileViewer.sources[app.fileViewer.side]; source != nil {
					title = source.Path
				}
			}
			if app.readTask != nil && app.readTask.kind != nativeReadHistory && app.readTask.kind != nativeReadHistoryPage {
				ui.Spinner(c).Size(14, 14)
				title = app.readTask.label
			}
			ui.Text(c, title).Bold().Grow(1).MinWidth(0).MaxLines(1).Tooltip(title)
			ui.Segmented(c, &full, locales.FileDiff(), locales.FullFile()).
				Label(locales.FileViewMode()).Disabled(!hasSources).Shrink(0).
				OnChange(func() { app.fileViewer.full = full == 1 })
		})
		if full == 1 {
			app.fullFilePanel(c)
			return
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Padding(0, 10).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				if app.screen == "changes" && app.selectedFile != "" {
					app.patchToolbar(c)
				}
			})
			app.diffLayoutControl(c)
			ui.Checkbox(c, &app.prefs.Wrap, locales.Wrap()).Shrink(0)
		})
		ui.Divider(c)
		if hasSources {
			ui.Text(c, fileVersionLabel(app.diff.Old)+" → "+fileVersionLabel(app.diff.New)).
				FontSize(11).TextColor(c.Theme().TextMuted).Padding(4, 12).FillWidth()
		}
		if app.fileViewer.contextChanged {
			ui.Text(c, locales.DiffContextChanged()).Padding(8, 12).TextColor(c.Theme().Warning).FillWidth()
		}
		if app.diff.Truncated {
			ui.Text(c, locales.TruncatedPreview()).Padding(10).TextColor(c.Theme().Warning)
		}
		switch {
		case len(app.diffViews) == 1:
			app.configureDiff(app.diffViews[0])
			editor.DiffView(c, app.diffViews[0]).Grow(1).FillWidth().MinHeight(0)
		case len(app.diffViews) > 1:
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
		case app.rawDiff.Buffer != nil && app.diff.Text != "":
			app.rawDiff.SoftWrap = app.prefs.Wrap
			editor.View(c, &app.rawDiff).Grow(1).FillWidth().MinHeight(0)
		default:
			ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
				message := locales.SelectFileOrCommit()
				if app.screen == "stash" {
					message = locales.SelectStash()
					if len(app.repository.stashes) == 0 {
						message = locales.EmptyStashDiff()
					}
				}
				ui.Text(c, message).TextColor(c.Theme().TextMuted)
			})
		}
	})
}

func (app *nativeApp) fullFilePanel(c *ui.Context) {
	viewer := &app.fileViewer
	source := viewer.sources[viewer.side]
	state := &viewer.states[viewer.side]
	ui.Row(c).FillWidth().Padding(0, 12, 6).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.MenuButton(c, fileVersionLabel(source), app.fileVersionMenu).Label(locales.FileVersion())
		ui.Button(c, locales.FileHistory()).OnClick(app.fileHistoryAction)
		ui.Column(c).Grow(1)
		ui.Button(c, locales.FindInFile()).Disabled(state.Buffer == nil).
			OnClick(func() { state.Run(editor.Find) })
		ui.Checkbox(c, &app.prefs.Wrap, locales.Wrap())
	})
	ui.Divider(c)
	if state.Buffer != nil {
		state.SoftWrap = app.prefs.Wrap
		state.Label = locales.FileContentLabel()
		editor.View(c, state).Grow(1).FillWidth().MinHeight(0)
		return
	}
	message := locales.AbsentVersion()
	switch {
	case source != nil && source.Unsupported:
		message = locales.FileContentUnsupported()
	case source != nil && source.Truncated:
		message = locales.FileContentTooLarge()
	case source != nil && source.Binary:
		message = locales.BinaryFile()
	}
	ui.Column(c).Grow(1).FillWidth().Center().Padding(20).Children(func() {
		ui.Text(c, message).TextColor(c.Theme().TextMuted)
	})
}
