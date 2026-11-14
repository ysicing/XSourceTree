// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysicing/51code/internal/gitrepo"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestNativeFocusRefreshKeepsUnchangedView(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "外部修改\n")
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	repository, diff := app.repository, app.diffViews[0]
	app.selectedFiles["false:hello.txt"] = true
	app.prefs.Drafts[path] = "提交草稿"
	app.refreshOnFocus()
	if app.busy || app.readTask != nil {
		t.Error("切回窗口不应进入全局忙碌状态或占用当前读取")
	}
	finish()
	tester.Frame()
	if app.repository != repository || app.diffViews[0] != diff || !app.selectedFiles["false:hello.txt"] ||
		app.prefs.Drafts[path] != "提交草稿" || app.selectedFile != "hello.txt" {
		t.Fatal("仓库未变化时重建了界面或丢失阅读状态")
	}
}

func TestNativeFocusRefreshDetectsEditsWithUnchangedStatus(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "第一次修改\n")
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	app.selectedFiles["false:hello.txt"] = true
	writeNativeFile(t, path, "hello.txt", "第二次修改\n")
	app.refreshOnFocus()
	finish()
	if app.selectedFile != "hello.txt" || !app.selectedFiles["false:hello.txt"] ||
		!strings.Contains(app.diff.Text, "第二次修改") || strings.Contains(app.diff.Text, "第一次修改") {
		t.Fatal("相同 M 状态下的外部编辑没有局部更新，或丢失了勾选")
	}
}

func TestNativeFocusRefreshKeepsLoadedHistory(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "new-commit"}[changed], func(t *testing.T) {
			app, path, finish := nativePagedHistoryFixture(t, 650)
			tester := ui.NewTester(app.view, 1024, 680)
			app.historyList.ScrollTo(80, ui.Start)
			tester.Frame()
			finish()
			app.selectCommit(app.history[250].Hash)
			finish()
			app.historyList.ScrollTo(250, ui.Start)
			tester.Frame()
			first, _ := app.historyList.Visible()
			anchor := app.history[first].Hash
			hash, diff, list, source := app.commitHash, app.diff.Text, app.historyList, &app.history[0]
			if changed {
				nativeGit(t, path, "commit", "--allow-empty", "-m", "外部新增提交")
			}
			app.refreshOnFocus()
			if app.busy {
				t.Error("切回历史时进入全局忙碌状态")
			}
			finish()
			tester.Frame()
			after, _ := app.historyList.Visible()
			if len(app.history) != 600 || app.historyList != list || app.commitHash != hash ||
				app.diff.Text != diff || app.history[after].Hash != anchor {
				t.Fatalf("切回窗口丢失历史页、选择或滚动锚点: count=%d first=%d after=%d", len(app.history), first, after)
			}
			if !changed && &app.history[0] != source {
				t.Fatal("引用未变化时重新查询了历史")
			}
			if changed && app.history[0].Subject != "外部新增提交" {
				t.Fatal("未更新外部新增的提交")
			}
		})
	}
}

func TestNativeFocusRefreshKeepsViewOnFailure(t *testing.T) {
	app, path, finish := nativeFixture(t)
	before := app.repository
	metadata, held := filepath.Join(path, ".git"), filepath.Join(path, ".git-held")
	if err := os.Rename(metadata, held); err != nil {
		t.Fatal(err)
	}
	app.refreshOnFocus()
	finish()
	if app.repository != before || app.problem == "" || app.writable {
		t.Fatal("刷新失败应保留界面并阻止仓库写操作")
	}
	if err := os.Rename(held, metadata); err != nil {
		t.Fatal(err)
	}
	app.refreshOnFocus()
	finish()
	if !app.writable || app.problem != "" {
		t.Fatal("仓库恢复后未恢复可写状态")
	}
}

func TestNativeFocusRefreshYieldsToUserSelection(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "外部修改\n")
	app.refresh()
	finish()
	blocked := make(chan struct{})
	app.readDone = blocked
	app.refreshOnFocus()
	request := app.focusRefresh
	app.refreshOnFocus()
	if request == nil || app.focusRefresh != request {
		t.Fatal("重复聚焦应合并正在进行的检查")
	}
	app.selectFile("hello.txt", false)
	if app.focusRefresh != nil || app.readTask == nil {
		t.Fatal("用户选择应取消后台检查并优先读取")
	}
	close(blocked)
	finish()
	finish()
	if app.problem != "" || app.selectedFile != "hello.txt" || !strings.Contains(app.diff.Text, "外部修改") {
		t.Fatal("过期检查覆盖了用户最新选择")
	}
}

func TestNativeFocusRefreshFinishesBeforeMutation(t *testing.T) {
	app, _, finish := nativeFixture(t)
	blocked, started := make(chan struct{}), make(chan struct{}, 1)
	app.readDone = blocked
	app.refreshOnFocus()
	done := app.readDone
	app.run("测试写入", func(context.Context) (func(), error) {
		select {
		case <-done:
		default:
			return func() { t.Error("写入在已取消的 Git 读取退出前开始") }, nil
		}
		started <- struct{}{}
		return nil, nil
	})
	select {
	case <-started:
		t.Fatal("写操作没有等待后台检查")
	default:
	}
	close(blocked)
	finish()
	finish()
	select {
	case <-started:
	default:
		t.Fatal("取消检查后写操作未继续")
	}
}

func TestNativeFocusRefreshCancelsOnNavigationAndClose(t *testing.T) {
	for _, closeWindow := range []bool{false, true} {
		t.Run(map[bool]string{false: "library", true: "close"}[closeWindow], func(t *testing.T) {
			app, _, finish := nativeFixture(t)
			repository := app.repository
			blocked := make(chan struct{})
			app.readDone = blocked
			app.refreshOnFocus()
			if closeWindow {
				app.closed = true
				app.cancelRead()
				app.cancel()
			} else {
				app.showLibrary()
			}
			if app.focusRefresh != nil {
				t.Fatal("导航或关闭窗口没有取消后台检查")
			}
			close(blocked)
			finish()
			if app.repository != repository || app.problem != "" {
				t.Fatal("取消后的检查仍回填了仓库状态")
			}
		})
	}
}

func TestNativeFocusRefreshSkipsProtectedStates(t *testing.T) {
	for _, state := range []string{"busy", "read", "dialog", "conflict", "closed", "library"} {
		t.Run(state, func(t *testing.T) {
			app, _, _ := nativeFixture(t)
			switch state {
			case "busy":
				app.busy = true
			case "read":
				app.readTask = &nativeRead{}
			case "dialog":
				app.dialog = &nativeDialog{}
			case "conflict":
				app.conflict = &gitrepo.Conflict{Result: "原始结果"}
				app.conflictEdit.Buffer = editor.NewBuffer("未保存的冲突草稿")
			case "closed":
				app.closed = true
			case "library":
				app.screen = "library"
			}
			before := app.readDone
			app.refreshOnFocus()
			if app.focusRefresh != nil || app.readDone != before {
				t.Fatal("受保护状态或仓库列表不应启动自动检查")
			}
		})
	}
}

func TestNativeFocusRefreshDropsObsoleteChecks(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "待暂存\n")
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	app.selectedFiles["false:hello.txt"] = true
	nativeGit(t, path, "add", "hello.txt")
	app.refreshOnFocus()
	finish()
	if len(app.selectedFiles) != 0 || app.selectedFile != "" || app.diff.Text != "" ||
		len(app.repository.snapshot.Files) != 1 || app.repository.snapshot.Files[0].Index != "M" {
		t.Fatal("外部暂存后保留了过期的未暂存勾选或预览")
	}
}

func TestNativeFocusRefreshFollowsCompletedWorkingChanges(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "从外部提交\n")
	app.refresh()
	finish()
	app.switchWorkspace("history")
	app.searchHistory(false)
	finish()
	app.selectHistoryWorking()
	finish()
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "外部完成提交")
	head := nativeGit(t, path, "rev-parse", "HEAD")
	app.refreshOnFocus()
	finish()
	finish()
	if app.problem != "" || app.historyWorking || app.commitHash != head ||
		len(app.repository.snapshot.Files) != 0 || !strings.Contains(app.diff.Text, "从外部提交") {
		t.Fatal("工作区从外部完成提交后未切到当前 HEAD 的详情")
	}
}

func TestNativeFocusRefreshUsesExecutedSearch(t *testing.T) {
	app, path, finish := nativeFixture(t)
	app.switchWorkspace("search")
	app.historyQuery = "初始"
	app.executeSearch()
	finish()
	app.historyQuery = "未执行的草稿"
	nativeGit(t, path, "commit", "--allow-empty", "-m", "初始结果的新提交")
	app.refreshOnFocus()
	finish()
	if app.problem != "" || len(app.history) != 2 || app.historyQuery != "未执行的草稿" ||
		app.historyResult.query != "初始" {
		t.Fatal("自动检查使用了未执行的搜索草稿")
	}
}
