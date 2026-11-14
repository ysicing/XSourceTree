// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"

	"github.com/ysicing/51code/internal/gitrepo"
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo/ui"
)

func (app *nativeApp) historyWorkingOffset() int {
	if app.screen == "history" && app.repository != nil && len(app.repository.snapshot.Files) > 0 && app.historyOffset == 0 &&
		app.historyResult.query == "" && app.historyResult.file == "" {
		return 1
	}
	return 0
}

func historyGraphWithWorking(commits []gitrepo.Commit, head string, working bool) []historyGraphRow {
	if !working {
		return commitGraph(commits)
	}
	rows := make([]gitrepo.Commit, 1, len(commits)+1)
	rows[0] = gitrepo.Commit{Hash: "working-tree", Parents: []string{head}}
	rows = append(rows, commits...)
	graph := commitGraph(rows)
	graph[0].Working = true
	return graph
}

func historyWorkingFile(files []gitrepo.File, selected string, staged bool) (string, bool) {
	if selected != "" && fileExists(files, selected, staged) {
		return selected, staged
	}
	for _, file := range files {
		if isWorking(file) {
			return file.Path, false
		}
	}
	for _, file := range files {
		if isStaged(file) {
			return file.Path, true
		}
	}
	return "", false
}

func (app *nativeApp) selectHistoryWorking() {
	if app.historyWorkingOffset() == 0 || app.busy {
		return
	}
	file, staged := historyWorkingFile(app.repository.snapshot.Files, app.selectedFile, app.staged)
	app.selectHistoryWorkingFile(file, staged)
}

func (app *nativeApp) selectHistoryWorkingFile(file string, staged bool) {
	if app.repository == nil || app.screen != "history" || app.historyWorkingOffset() == 0 {
		return
	}
	path := app.repository.snapshot.Path
	app.load(locales.ReadingDiff(), nativeReadDiff, func(ctx context.Context) (func(), error) {
		diff, err := app.service.Diff(ctx, path, file, staged)
		return func() {
			selection := &nativeSelection{file: file, staged: staged}
			if err == nil {
				selection.diff = diff
			}
			app.applyHistoryWorkingSelection(selection)
		}, err
	})
}

func (app *nativeApp) applyHistoryWorkingSelection(selection *nativeSelection) {
	app.setSelection(selection)
	app.historyWorking = true
	app.historySelected, app.commitSelected = -1, -1
	app.commitHash, app.commitFile, app.commitFiles = "", "", nil
	app.viewedTag, app.viewedCommit = "", gitrepo.Commit{}
}

func (app *nativeApp) openHistoryWorkingChanges() {
	if app.repository == nil || app.busy || app.readTask != nil {
		return
	}
	file, staged := app.selectedFile, app.staged
	app.switchWorkspace("changes")
	app.refSelected = nativeRefKey{}
	app.fileQuery, app.fileFilter = "", ""
	clear(app.fileCollapsed)
	app.clearDiff()
	if file != "" {
		app.selectFile(file, staged)
	}
}

type historyWorkingFileRow struct {
	file   gitrepo.File
	staged bool
	header bool
}

func (app *nativeApp) historyWorkingFiles(c *ui.Context) {
	var rows []historyWorkingFileRow
	for _, staged := range []bool{true, false} {
		var group []historyWorkingFileRow
		for _, file := range app.repository.snapshot.Files {
			if staged && isStaged(file) || !staged && isWorking(file) {
				group = append(group, historyWorkingFileRow{file: file, staged: staged})
			}
		}
		if len(group) > 0 {
			rows = append(rows, historyWorkingFileRow{staged: staged, header: true})
			rows = append(rows, group...)
		}
	}
	app.workingSelected = -1
	for index, row := range rows {
		if !row.header && row.file.Path == app.selectedFile && row.staged == app.staged {
			app.workingSelected = index
			break
		}
	}
	state := app.workingList
	state.Selected = &app.workingSelected
	state.Header = func(index int) bool { return rows[index].header }
	state.Key = func(index int) any {
		row := rows[index]
		return struct {
			path           string
			staged, header bool
		}{row.file.Path, row.staged, row.header}
	}
	state.Label = func(index int) string {
		row := rows[index]
		if row.header {
			if row.staged {
				return locales.StagedFiles()
			}
			return locales.UnstagedFiles()
		}
		if row.staged {
			return locales.SelectStagedFile(row.file.Path)
		}
		return locales.SelectUnstagedFile(row.file.Path)
	}
	theme := *c.Theme()
	theme.Accent = theme.Accent.Alpha(0.16)
	theme.InactiveAccent, theme.AccentText = theme.Accent, theme.Text
	ui.Themed(c, &theme).Fill().MinWidth(0).Children(func() {
		if ui.List(c, state, len(rows), func(index int) {
			row := rows[index]
			if row.header {
				ui.Row(c).FillWidth().Height(30).Padding(0, 12).Background(c.Theme().Surface).AlignItems(ui.Center).Children(func() {
					ui.Text(c, state.Label(index)).Bold().FontSize(12)
				})
				return
			}
			ui.Row(c).FillWidth().Height(30).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				nativeFileIcon(c, row.file.Path)
				ui.Text(c, row.file.Path).FontSize(12).Grow(1).MinWidth(0).SingleLine().Tooltip(row.file.Path)
				ui.Text(c, nativeFileStatus(row.file, row.staged)).FontSize(10).TextColor(c.Theme().TextMuted)
			})
		}).Fill().Label(locales.WorkingTreeFiles()).Disabled(app.busy || app.readTask != nil && app.readTask.kind != nativeReadDiff).
			Changed() && app.workingSelected >= 0 && app.workingSelected < len(rows) {
			row := rows[app.workingSelected]
			if !row.header {
				app.selectHistoryWorkingFile(row.file.Path, row.staged)
			}
		}
	})
}
