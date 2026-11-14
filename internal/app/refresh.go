// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"reflect"
	"slices"
	"time"

	"github.com/ysicing/51code/internal/gitrepo"
)

func (app *nativeApp) refreshOnFocus() {
	if app.closed || app.busy || app.readTask != nil || app.focusRefresh != nil ||
		app.dialog != nil || app.conflictDirty() || app.repository == nil || app.screen == "library" {
		return
	}
	repository, screen := app.repository, app.screen
	path, file, staged := repository.snapshot.Path, app.selectedFile, app.staged
	history := app.nativeHistoryState
	blockedAI, blockedToken := app.ai, ""
	if blockedAI != nil && blockedAI.tooLarge {
		blockedToken = blockedAI.token
	}
	ctx, cancel := context.WithTimeout(app.ctx, 30*time.Second)
	request := &nativeRead{cancel: cancel}
	previous, done := app.readDone, make(chan struct{})
	app.focusRefresh, app.readDone = request, done
	go func() {
		// 与前台读取共用完成链；用户操作取消检查后，写任务仍须等 Git 读取退出。
		if previous != nil {
			<-previous
		}
		result, err := app.readRepository(ctx, path, false)
		var commits []gitrepo.Commit
		reloadHistory := err == nil && history.historyLoaded && (screen == "history" || screen == "search") &&
			historyReferencesChanged(repository, result)
		if reloadHistory {
			commits, err = app.readFocusHistory(ctx, path, history)
		}
		var selection *nativeSelection
		if err == nil && (screen == "changes" || history.historyWorking) {
			if history.historyWorking {
				file, staged = historyWorkingFile(result.snapshot.Files, file, staged)
			}
			if file != "" && fileExists(result.snapshot.Files, file, staged) {
				if history.historyWorking {
					selection, err = app.readHistoryWorkingSelection(ctx, path, file, staged, result.snapshot.Files)
				} else {
					selection, err = app.readSelection(ctx, path, file, staged, result.snapshot.Files)
				}
			}
		}
		var worktrees []gitrepo.Worktree
		if err == nil && screen == "worktrees" {
			worktrees, err = app.service.Worktrees(ctx, path)
		}
		refreshedToken := ""
		if err == nil && blockedToken != "" {
			if repo, getErr := app.service.manager.Get(path); getErr == nil {
				refreshedToken, _ = repo.CommitToken(ctx)
			}
		}
		cancel()
		close(done)
		app.post(func() {
			if app.closed || app.focusRefresh != request {
				return
			}
			app.focusRefresh = nil
			if app.repository != repository || app.screen != screen || app.dialog != nil || app.conflictDirty() {
				return
			}
			if err != nil {
				if app.problem == "" || app.problem == app.focusError {
					app.problem = err.Error()
				}
				app.focusError, app.writable = err.Error(), false
				return
			}
			if app.problem == app.focusError {
				app.problem = ""
			}
			app.focusError, app.writable = "", true
			if app.ai == blockedAI && refreshedToken != "" && refreshedToken != blockedToken {
				app.stopCommitAI()
			}
			if !reflect.DeepEqual(repository, result) {
				app.historyList.HoldPlace()
				app.repository = result
				app.library = app.service.Library()
				if len(app.selectedFiles) > 0 {
					valid := make(map[string]bool, len(result.snapshot.Files)*2)
					for _, entry := range result.snapshot.Files {
						if isStaged(entry) {
							valid["true:"+entry.Path] = true
						}
						if isWorking(entry) {
							valid["false:"+entry.Path] = true
						}
					}
					for key := range app.selectedFiles {
						if !valid[key] {
							delete(app.selectedFiles, key)
						}
					}
				}
			}
			if reloadHistory {
				if app.historySelected >= 0 && app.historySelected < len(app.history) {
					app.viewedCommit = app.history[app.historySelected]
				}
				app.historyList.HoldPlace()
				app.history, app.historyMore = commits, len(commits) == max(len(history.history), gitrepo.DefaultHistoryPageSize)
				app.historyPagePaused = false
				app.historySelected = slices.IndexFunc(commits, func(commit gitrepo.Commit) bool { return commit.Hash == app.commitHash })
			}
			if selection != nil {
				app.refreshFileSelection(selection)
			} else if screen == "changes" && app.selectedFile != "" || history.historyWorking {
				app.selectedFile = ""
				app.clearDiff()
				if history.historyWorking {
					app.historyWorking = false
					if result.snapshot.Head != "" {
						app.selectCommit(result.snapshot.Head)
					}
				}
			}
			if screen == "stash" && app.stashHash != "" &&
				!slices.ContainsFunc(result.stashes, func(stash gitrepo.Stash) bool { return stash.Hash == app.stashHash }) {
				app.stashHash = ""
				app.clearDiff()
			}
			if screen == "worktrees" && app.worktrees != nil && !reflect.DeepEqual(app.worktrees.items, worktrees) {
				app.worktrees.setItems(worktrees)
			}
		})
	}()
}

func historyReferencesChanged(previous, current *repositoryView) bool {
	return previous.snapshot.Head != current.snapshot.Head || !slices.Equal(previous.branches, current.branches) ||
		!slices.Equal(previous.tags, current.tags) || !slices.Equal(previous.stashes, current.stashes)
}

func (app *nativeApp) readFocusHistory(ctx context.Context, path string, state nativeHistoryState) ([]gitrepo.Commit, error) {
	return app.readHistoryRange(ctx, path, state.historyResult, state.historyOffset,
		max(len(state.history), gitrepo.DefaultHistoryPageSize))
}

// readHistoryRange 重读已加载的全部历史，刷新后保留分页进度与选中位置。
func (app *nativeApp) readHistoryRange(ctx context.Context, path string, request nativeHistoryRequest, offset, count int) ([]gitrepo.Commit, error) {
	commits := make([]gitrepo.Commit, 0, count)
	for len(commits) < count {
		limit := min(count-len(commits), gitrepo.MaxHistoryPageSize)
		page, err := app.service.SearchHistory(ctx, path, request.query, request.file,
			offset+len(commits), request.filter, limit)
		if err != nil {
			return nil, err
		}
		commits = append(commits, page...)
		if len(page) < limit {
			break
		}
	}
	return commits, nil
}

func (app *nativeApp) refreshFileSelection(selection *nativeSelection) {
	if selection.patch.Supported {
		selection.diff.Text = selection.patch.Text
	}
	if app.selectedFile == selection.file && app.staged == selection.staged &&
		reflect.DeepEqual(app.diff, selection.diff) && reflect.DeepEqual(app.conflict, selection.conflict) &&
		app.patch.Supported == selection.patch.Supported {
		app.patch = selection.patch
		return
	}
	app.setSelection(selection)
}
