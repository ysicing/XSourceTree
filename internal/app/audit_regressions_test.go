// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ysicing/51code/internal/workspace"
)

func TestNativeCommitShortcutRequiresChangesScreen(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "暂存内容\n")
	nativeGit(t, path, "add", ".")
	app.refresh()
	finish()
	app.prefs.Drafts[app.repository.snapshot.Path] = "不应提交"
	head := nativeGit(t, path, "rev-parse", "HEAD")
	for _, leave := range []func(){app.showLibrary, func() { app.switchWorkspace("history") }} {
		leave()
		app.commit()
		if app.busy || nativeGit(t, path, "rev-parse", "HEAD") != head {
			t.Fatalf("在 %s 页面通过快捷键提交了不可见的暂存内容", app.screen)
		}
		app.cancelRead()
	}
}

func TestNativeWorktreeReviewKeepsOpenedDialog(t *testing.T) {
	app, path, finish := nativeFixture(t)
	linked := filepath.Join(t.TempDir(), "linked")
	nativeGit(t, path, "worktree", "add", "-b", "topic", linked)
	app.openWorktrees()
	finish()
	app.reviewWorktree(linked, true)
	opened := &nativeDialog{title: "设置"}
	app.dialog = opened
	finish()
	if app.dialog != opened {
		t.Fatal("工作树审查结果替换了期间打开的弹窗")
	}
}

func TestNativeCorruptPreferencesAreBackedUpBeforeSaving(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "native-state.json")
	original := []byte(`{"drafts": {"/repo": "未完成的提交说明"`)
	if err := os.WriteFile(stateFile, original, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := workspace.New(filepath.Join(dir, "recent.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := newNativeApp(&RepositoryService{manager: manager}, stateFile)
	t.Cleanup(app.cancel)
	if app.problem == "" {
		t.Fatal("损坏的偏好文件没有提示")
	}
	if err := app.savePreferences(); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(stateFile + ".corrupt-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("未备份损坏的偏好文件: %v %v", backups, err)
	}
	if data, err := os.ReadFile(backups[0]); err != nil || string(data) != string(original) {
		t.Fatalf("备份内容不完整: %q %v", data, err)
	}
}

func TestNativeRefreshKeepsLoadedHistoryPages(t *testing.T) {
	app, _, finish := nativePagedHistoryFixture(t, 250)
	app.searchHistory(true)
	finish()
	loaded := len(app.history)
	if loaded <= 150 {
		t.Fatalf("续载未生效: %d", loaded)
	}
	app.selectCommit(app.history[150].Hash)
	finish()
	hash, list := app.commitHash, app.historyList
	app.refresh()
	finish()
	if len(app.history) != loaded || app.commitHash != hash || app.historySelected != 150 || app.historyList != list {
		t.Fatalf("刷新截断了已加载的历史: count=%d want=%d selected=%d", len(app.history), loaded, app.historySelected)
	}
}
