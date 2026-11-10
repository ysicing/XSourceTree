// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"slices"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type nativeSelection struct {
	file     string
	staged   bool
	diff     gitrepo.Diff
	patch    gitrepo.Patch
	conflict *gitrepo.Conflict
}

type nativeLine struct {
	ID      int
	Hunk    int
	Text    string
	Checked bool
}

func (app *nativeApp) readSelection(ctx context.Context, path, file string, staged bool, files []gitrepo.File) (*nativeSelection, error) {
	result := &nativeSelection{file: file, staged: staged}
	for _, entry := range files {
		if entry.Path == file && entry.Conflict {
			conflict, err := app.service.Conflict(ctx, path, file)
			result.conflict = &conflict
			return result, err
		}
	}
	var err error
	result.diff, err = app.service.Diff(ctx, path, file, staged)
	if err != nil {
		return nil, err
	}
	result.patch, err = app.service.Patch(ctx, path, file, staged)
	return result, err
}

func (app *nativeApp) selectFile(file string, staged bool) {
	if app.repository == nil {
		return
	}
	path := app.repository.snapshot.Path
	files := app.repository.snapshot.Files
	app.leaveConflict(func() {
		app.run("正在读取差异…", func(ctx context.Context) (func(), error) {
			selection, err := app.readSelection(ctx, path, file, staged, files)
			return func() {
				app.clearDiff()
				app.selectedFile, app.staged = file, staged
				if err == nil {
					app.setSelection(selection)
				}
			}, err
		})
	})
}

func (app *nativeApp) clearDiff() {
	app.diff, app.patch = gitrepo.Diff{}, gitrepo.Patch{}
	app.diffViews, app.diffNames, app.lineChoices = nil, nil, nil
	app.hunkOffsets = nil
	app.rawDiff = editor.State{}
	app.conflict = nil
	app.hunk = 0
}

func (app *nativeApp) setSelection(selection *nativeSelection) {
	app.clearDiff()
	app.selectedFile, app.staged = selection.file, selection.staged
	app.patch = selection.patch
	if selection.conflict != nil {
		app.conflict = selection.conflict
		app.conflictEdit = editor.State{
			Buffer: editor.NewBuffer(selection.conflict.Result), SoftWrap: true, Label: "合并结果",
		}
		for index, side := range []gitrepo.ConflictSide{selection.conflict.Base, selection.conflict.Ours, selection.conflict.Theirs} {
			text := side.Text
			if !side.Present {
				text = "此版本不存在"
			} else if side.Binary {
				text = "二进制文件"
			}
			app.conflictBase[index] = editor.State{Buffer: editor.NewBuffer(text), ReadOnly: true, SoftWrap: true}
		}
		return
	}
	app.setDiff(selection.diff)
	if selection.patch.Supported {
		app.lineChoices = patchLines(selection.patch.Text)
	}
}

func patchLines(text string) []nativeLine {
	var result []nativeLine
	hunk := -1
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			hunk++
		} else if hunk >= 0 && (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")) {
			result = append(result, nativeLine{ID: len(result), Hunk: hunk, Text: line})
		}
	}
	return result
}

func (app *nativeApp) setDiff(diff gitrepo.Diff) {
	app.diff = diff
	app.diffViews, app.diffNames = nil, nil
	app.hunkOffsets = nil
	patches, err := editor.ParsePatch(diff.Text)
	if err == nil && !diff.Truncated {
		for _, patch := range patches {
			if patch.Binary || len(patch.Hunks) == 0 {
				// 混合提交保留完整原始差异，避免隐藏二进制、重命名或权限变更。
				app.diffViews, app.diffNames, app.hunkOffsets = nil, nil, nil
				break
			}
			state := &editor.DiffState{Wrap: app.prefs.Wrap, Indicators: editor.DiffClassic}
			state.SetPatch(patch)
			app.diffViews = append(app.diffViews, state)
			name := patch.NewName
			if name == "" {
				name = patch.OldName
			}
			app.diffNames = append(app.diffNames, name)
			if len(patches) == 1 {
				app.hunkOffsets = patchHunkOffsets(patch)
			}
		}
	}
	app.rawDiff = editor.State{
		Buffer: editor.NewBuffer(diff.Text), ReadOnly: true, SoftWrap: app.prefs.Wrap,
		Label: "差异文本", NoScrollPastEnd: true,
	}
}

func patchHunkOffsets(patch editor.FilePatch) []int {
	var offsets []int
	runs := 0
	for _, hunk := range patch.Hunks {
		offsets = append(offsets, runs)
		changing := false
		for _, line := range hunk.Lines {
			if line == "" || line[0] == '\\' {
				continue
			}
			edit := line[0] == '+' || line[0] == '-'
			if edit && !changing {
				runs++
			}
			changing = edit
		}
	}
	return offsets
}

func (app *nativeApp) goToPatchHunk() {
	if len(app.diffViews) == 1 && app.hunk < len(app.hunkOffsets) {
		// Native Diff 按连续增删段导航，Git 区块可能包含多段增删。
		app.diffViews[0].GoToHunk(app.hunkOffsets[app.hunk])
	}
}

func (app *nativeApp) searchHistory(more bool) {
	if app.repository == nil || app.busy {
		return
	}
	path, query, file := app.repository.snapshot.Path, app.historyQuery, app.historyFile
	skip := 0
	if more {
		skip = len(app.history)
	}
	app.run("正在搜索历史…", func(ctx context.Context) (func(), error) {
		commits, err := app.service.SearchHistory(ctx, path, query, file, skip)
		return func() {
			// 搜索取消或超时时保留已读取结果。
			if more {
				app.history = append(app.history, commits...)
			} else if err == nil || len(commits) > 0 {
				app.history = commits
				app.historySelected, app.commitSelected = -1, -1
				app.historyList = ui.ListState{}
				app.commitHash, app.commitFiles, app.commitFile = "", nil, ""
				app.clearDiff()
			}
			app.historyMore = len(commits) == 100
		}, err
	})
}

func (app *nativeApp) selectCommit(hash string) {
	if app.repository == nil {
		return
	}
	path := app.repository.snapshot.Path
	app.run("正在读取提交…", func(ctx context.Context) (func(), error) {
		files, err := app.service.CommitFiles(ctx, path, hash)
		if err != nil {
			return func() {
				app.historySelected = slices.IndexFunc(app.history, func(commit gitrepo.Commit) bool { return commit.Hash == app.commitHash })
			}, err
		}
		var diff gitrepo.Diff
		var diffErr error
		file := ""
		if len(files) > 0 {
			file = files[0].Path
			diff, diffErr = app.service.CommitFileDiff(ctx, path, hash, file)
		}
		return func() {
			app.clearDiff()
			app.commitHash, app.commitFiles, app.commitFile = hash, files, file
			app.historySelected = -1
			for index, commit := range app.history {
				if commit.Hash == hash {
					app.historySelected = index
					break
				}
			}
			app.commitSelected = -1
			if len(files) > 0 {
				app.commitSelected = 0
			}
			app.commitList = ui.ListState{}
			if diffErr == nil {
				app.setDiff(diff)
			}
		}, diffErr
	})
}

func (app *nativeApp) selectCommitFile(file string) {
	path, hash := app.repository.snapshot.Path, app.commitHash
	app.run("正在读取文件差异…", func(ctx context.Context) (func(), error) {
		diff, err := app.service.CommitFileDiff(ctx, path, hash, file)
		return func() {
			app.clearDiff()
			app.commitFile = file
			for index, entry := range app.commitFiles {
				if entry.Path == file {
					app.commitSelected = index
					break
				}
			}
			if err == nil {
				app.setDiff(diff)
			}
		}, err
	})
}

func (app *nativeApp) selectStash(hash string) {
	path := app.repository.snapshot.Path
	app.run("正在读取储藏…", func(ctx context.Context) (func(), error) {
		diff, err := app.service.StashDiff(ctx, path, hash)
		return func() {
			app.clearDiff()
			app.stashHash = hash
			if err == nil {
				app.setDiff(diff)
			}
		}, err
	})
}

func (app *nativeApp) applyHunk() {
	if len(app.patch.Hunks) == 0 || !app.patch.Supported {
		return
	}
	file, staged, token, id := app.selectedFile, app.staged, app.patch.Token, app.hunk
	app.mutate("正在应用区块…", func(ctx context.Context, path string) error {
		return app.service.ApplyHunk(ctx, path, file, staged, token, id)
	})
}

func (app *nativeApp) applyLines() {
	var ids []int
	for _, line := range app.lineChoices {
		if line.Checked {
			ids = append(ids, line.ID)
		}
	}
	if len(ids) == 0 {
		app.problem = "请至少选择一行变更"
		return
	}
	file, staged, token := app.selectedFile, app.staged, app.patch.Token
	app.dialog = nil
	app.mutate("正在应用所选行…", func(ctx context.Context, path string) error {
		return app.service.ApplyLines(ctx, path, file, staged, token, ids)
	})
}

func shortHash(hash string) string { return hash[:min(8, len(hash))] }
