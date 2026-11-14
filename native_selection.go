// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/locales"

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

type nativeReadKind uint8

const (
	nativeReadDiff nativeReadKind = iota
	nativeReadHistory
	nativeReadCommit
	nativeReadHistoryPage
)

type nativeRead struct {
	label  string
	kind   nativeReadKind
	cancel context.CancelFunc
}

func (app *nativeApp) cancelRead() {
	if app.focusRefresh != nil {
		app.focusRefresh.cancel()
		app.focusRefresh = nil
	}
	if app.readTask != nil {
		app.readTask.cancel()
		app.readTask = nil
		if app.screen == "compare" && app.comparison != nil {
			app.comparison.restoreSelection()
		}
	}
}

func (app *nativeApp) load(label string, kind nativeReadKind, task func(context.Context) (func(), error)) {
	if app.repository == nil || app.busy || app.closed {
		return
	}
	app.cancelRead()
	ctx, cancel := context.WithCancel(app.ctx)
	request := &nativeRead{label: label, kind: kind, cancel: cancel}
	previous, done := app.readDone, make(chan struct{})
	repository, screen := app.repository, app.screen
	app.readTask, app.readDone, app.problem = request, done, ""
	go func() {
		// 等待已取消的读取退出，避免历史搜索注册与 Git 写操作重叠。
		if previous != nil {
			<-previous
		}
		var apply func()
		err := ctx.Err()
		if err == nil {
			apply, err = task(ctx)
		}
		cancel()
		close(done)
		app.post(func() {
			if app.closed || app.readTask != request {
				return
			}
			app.readTask = nil
			if app.repository != repository || app.screen != screen {
				return
			}
			if apply != nil {
				apply()
			}
			if err != nil {
				if kind == nativeReadHistory && screen != "search" {
					app.historyFilter = app.historyResult.filter
				}
				app.problem = err.Error()
			}
		})
	}()
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
		app.load(locales.ReadingDiff(), nativeReadDiff, func(ctx context.Context) (func(), error) {
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
			Buffer: editor.NewBuffer(selection.conflict.Result), SoftWrap: true, Label: locales.MergeResult(),
		}
		for index, side := range []gitrepo.ConflictSide{selection.conflict.Base, selection.conflict.Ours, selection.conflict.Theirs} {
			text := side.Text
			if !side.Present {
				text = locales.VersionAbsent()
			} else if side.Binary {
				text = locales.BinaryFile()
			}
			app.conflictBase[index] = editor.State{Buffer: editor.NewBuffer(text), ReadOnly: true, SoftWrap: true}
		}
		return
	}
	diff := selection.diff
	if selection.patch.Supported {
		// 展示与操作令牌使用同一份稳定补丁；完整版本不匹配时只允许查看和整文件操作。
		diff.Text = selection.patch.Text
	}
	app.setDiff(diff)
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
	app.fileViewer.setSources(diff)
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
			if len(patches) == 1 && diff.Old != nil && diff.New != nil &&
				!diff.Old.Binary && !diff.New.Binary && !diff.Old.Truncated && !diff.New.Truncated &&
				!diff.Old.Unsupported && !diff.New.Unsupported {
				// 保留 Git 生成的增删段，不能因补齐上下文而改变逐行暂存的坐标。
				if err := state.SetPatchFiles(patch, diff.Old.Text, diff.New.Text); err != nil {
					app.fileViewer.contextChanged = true
				}
			}
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
		Label: locales.DiffText(), NoScrollPastEnd: true,
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

type nativeHistoryRequest struct {
	query, file string
	filter      gitrepo.HistoryFilter
}

func (app *nativeApp) historyRequest() nativeHistoryRequest {
	request := nativeHistoryRequest{query: app.historyQuery, file: app.historyFile, filter: app.historyFilter}
	if app.screen == "search" {
		request.filter.Since, request.filter.Until = "", ""
		if app.searchDateEnabled {
			request.filter.Since, request.filter.Until = app.searchSince.Format(time.DateOnly), app.searchUntil.Format(time.DateOnly)
		}
	}
	return request
}

func (app *nativeApp) searchHistory(more bool) {
	if app.repository == nil || app.busy {
		return
	}
	path, request := app.repository.snapshot.Path, app.historyRequest()
	if more && app.screen == "search" {
		request = app.historyResult
	}
	more = more && request == app.historyResult
	skip := 0
	limit, kind, label := gitrepo.DefaultHistoryPageSize, nativeReadHistory, locales.SearchingHistory()
	if more {
		skip = app.historyOffset + len(app.history)
		limit, kind, label = gitrepo.MaxHistoryPageSize, nativeReadHistoryPage, locales.LoadingHistory()
		app.historyPagePaused = false
	}
	app.load(label, kind, func(ctx context.Context) (func(), error) {
		commits, err := app.service.SearchHistory(ctx, path, request.query, request.file, skip, request.filter, limit)
		return func() {
			// 搜索取消或超时时保留已读取结果。
			if err != nil {
				if more {
					app.historyPagePaused = true
				}
				return
			}
			if more {
				app.historyList.HoldPlace()
				app.history = append(app.history, commits...)
			} else {
				app.history = commits
				app.historyWorking = false
				app.historyOffset, app.viewedTag, app.viewedCommit = 0, "", gitrepo.Commit{}
				app.historySelected, app.commitSelected = -1, -1
				// 旧界面的辅助功能节点仍引用列表状态，替换指针而非原地清零。
				app.historyList, app.commitList = &ui.ListState{}, &ui.ListState{}
				app.commitHash, app.commitFiles, app.commitFile = "", nil, ""
				app.clearDiff()
			}
			app.historyResult = request
			app.historyLoaded = true
			app.historyMore = len(commits) == limit
			app.historyPagePaused = false
		}, err
	})
}

func (app *nativeApp) selectCommit(hash string) {
	if app.repository == nil {
		return
	}
	path := app.repository.snapshot.Path
	app.load(locales.ReadingCommit(), nativeReadCommit, func(ctx context.Context) (func(), error) {
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
			app.viewedTag, app.viewedCommit = "", gitrepo.Commit{}
			app.applyCommitSelection(hash, files, file)
			if diffErr == nil {
				app.setDiff(diff)
			}
		}, diffErr
	})
}

func (app *nativeApp) applyCommitSelection(hash string, files []gitrepo.CommitFile, file string) {
	app.clearDiff()
	app.historyWorking = false
	app.commitHash, app.commitFiles, app.commitFile = hash, files, file
	app.historySelected = slices.IndexFunc(app.history, func(commit gitrepo.Commit) bool { return commit.Hash == hash })
	if app.historySelected >= 0 {
		app.historyList.ScrollIntoView(app.historySelected + app.historyWorkingOffset())
	}
	app.commitSelected = -1
	if len(files) > 0 {
		app.commitSelected = 0
	}
	app.commitList = &ui.ListState{}
}

func (app *nativeApp) selectTag(name string, all bool) {
	if app.repository == nil || app.busy {
		return
	}
	app.leaveConflict(func() {
		app.switchWorkspace("history")
		path, request := app.repository.snapshot.Path, app.historyResult
		if len(app.history) == 0 {
			request = app.historyRequest()
		}
		if all {
			request = nativeHistoryRequest{}
		}
		app.load(locales.ReadingCommit(), nativeReadHistory, func(ctx context.Context) (func(), error) {
			hash, err := app.service.ResolveCreationTarget(ctx, path, "refs/tags/"+name)
			if err != nil {
				return nil, err
			}
			location, err := app.service.LocateHistory(ctx, path, hash, request.query, request.file, request.filter)
			if err != nil {
				return nil, err
			}
			files, err := app.service.CommitFiles(ctx, path, hash)
			if err != nil {
				return nil, err
			}
			file := ""
			var diff gitrepo.Diff
			if len(files) > 0 {
				file = files[0].Path
				diff, err = app.service.CommitFileDiff(ctx, path, hash, file)
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return func() {
				if len(location.Commits) > 0 {
					// 已加载目标时保留当前页与滚动状态，否则切换到目标所在页。
					index := slices.IndexFunc(app.history, func(commit gitrepo.Commit) bool { return commit.Hash == hash })
					if all || request != app.historyResult || index < 0 {
						app.history, app.historyOffset = location.Commits, location.Offset
						app.historyMore = len(location.Commits) == gitrepo.DefaultHistoryPageSize
						app.historyPagePaused = false
						app.historyList = &ui.ListState{}
					}
					app.historyResult = request
					if all {
						app.historyQuery, app.historyFile, app.historyFilter = "", "", request.filter
					}
				}
				app.viewedTag, app.viewedCommit = name, location.Commit
				app.historyLoaded = true
				app.refSelected = nativeRefKey{repository: path, kind: "标签", reference: name}
				app.applyCommitSelection(hash, files, file)
				if err == nil {
					app.setDiff(diff)
				}
			}, err
		})
	})
}

func (app *nativeApp) selectCommitFile(file string) {
	if app.repository == nil || app.readTask != nil && app.readTask.kind != nativeReadDiff {
		return
	}
	path, hash := app.repository.snapshot.Path, app.commitHash
	app.load(locales.ReadingFileDiff(), nativeReadDiff, func(ctx context.Context) (func(), error) {
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
	if app.repository == nil {
		return
	}
	path := app.repository.snapshot.Path
	app.load(locales.ReadingStash(), nativeReadDiff, func(ctx context.Context) (func(), error) {
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
	if len(app.patch.Hunks) == 0 || !app.canApplyPatch() {
		return
	}
	file, staged, token, id := app.selectedFile, app.staged, app.patch.Token, app.hunk
	app.mutate(locales.ApplyingHunk(), func(ctx context.Context, path string) error {
		return app.service.ApplyHunk(ctx, path, file, staged, token, id)
	})
}

func (app *nativeApp) applyLines() {
	if !app.canApplyPatch() {
		return
	}
	var ids []int
	for _, line := range app.lineChoices {
		if line.Checked {
			ids = append(ids, line.ID)
		}
	}
	if len(ids) == 0 {
		app.problem = locales.SelectOneLine()
		return
	}
	file, staged, token := app.selectedFile, app.staged, app.patch.Token
	app.mutateForm(app.dialog, locales.ApplyingLines(), func(ctx context.Context, path string) error {
		return app.service.ApplyLines(ctx, path, file, staged, token, ids)
	})
}

func shortHash(hash string) string { return hash[:min(8, len(hash))] }
