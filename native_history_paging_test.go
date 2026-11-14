// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func nativePagedHistoryFixture(t *testing.T, count int) (*nativeApp, string, func()) {
	t.Helper()
	app, path, finish := nativeFixture(t)
	root := nativeGit(t, path, "rev-parse", "HEAD")
	var stream strings.Builder
	for index := range count {
		subject := fmt.Sprintf("分页提交 %05d", index)
		fmt.Fprintf(&stream, "commit refs/heads/main\nmark :%d\ncommitter Native Test <native@example.test> %d +0000\ndata %d\n%s\n",
			index+1, 1800000000+index, len(subject), subject)
		if index == 0 {
			fmt.Fprintf(&stream, "from %s\n\n", root)
		} else {
			fmt.Fprintf(&stream, "from :%d\n\n", index)
		}
	}
	command := exec.Command("git", "-C", path, "fast-import", "--quiet")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	command.Stdin = strings.NewReader(stream.String())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("import history: %v\n%s", err, output)
	}
	app.refresh()
	finish()
	app.switchWorkspace("history")
	app.searchHistory(false)
	finish()
	return app, path, finish
}

func TestNativeHistoryScrollLoadsBatchesWithoutMovingReadingPosition(t *testing.T) {
	app, _, finish := nativePagedHistoryFixture(t, 1250)
	app.selectCommit(app.history[2].Hash)
	finish()
	hash, diff, list := app.commitHash, app.diff.Text, app.historyList
	tester := ui.NewTester(app.view, 1024, 680)
	if tester.HasText("加载更多") || len(app.history) != 100 || app.readTask != nil {
		t.Fatal("首屏应只读取 100 条且不显示加载更多")
	}
	for _, page := range []struct{ row, count int }{{80, 600}, {580, 1100}, {1080, 1251}} {
		app.historyList.ScrollTo(page.row, ui.Start)
		tester.Frame()
		first, _ := app.historyList.Visible()
		if app.readTask == nil || app.busy {
			t.Fatal("接近列表底部未启动局部续载")
		}
		table, found := tester.Access("提交历史表格")
		if !found || table.Disabled {
			t.Fatal("续载期间不能禁用历史表格")
		}
		finish()
		tester.Frame()
		after, _ := app.historyList.Visible()
		if len(app.history) != page.count || app.commitHash != hash || app.diff.Text != diff ||
			app.historySelected != 2 || app.historyList != list || after != first {
			t.Fatalf("续载改变了阅读上下文: count=%d want=%d first=%d after=%d", len(app.history), page.count, first, after)
		}
	}
	app.historyList.ScrollToEnd()
	tester.Frame()
	if app.historyMore || app.readTask != nil || tester.HasText("加载更多") {
		t.Fatal("到达末尾后仍继续请求")
	}
	seen := make(map[string]bool)
	for _, commit := range app.history {
		if seen[commit.Hash] {
			t.Fatal("续载出现重复提交")
		}
		seen[commit.Hash] = true
	}
}

func TestNativeHistoryScrollFailureWaitsForRetry(t *testing.T) {
	app, path, finish := nativePagedHistoryFixture(t, 150)
	tester := ui.NewTester(app.view, 1024, 680)
	metadata, held := filepath.Join(path, ".git"), filepath.Join(path, ".git-held")
	if err := os.Rename(metadata, held); err != nil {
		t.Fatal(err)
	}
	app.historyList.ScrollTo(80, ui.Start)
	tester.Frame()
	if app.readTask == nil {
		t.Fatal("未启动续载")
	}
	finish()
	tester.Frame()
	if len(app.history) != 100 || app.problem == "" || app.readTask != nil || !tester.HasText("重试") {
		t.Fatal("失败后应保留结果并等待手动重试")
	}
	for range 3 {
		tester.Frame()
	}
	if app.readTask != nil {
		t.Fatal("续载失败后陷入自动重试")
	}
	if err := os.Rename(held, metadata); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("重试"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.problem != "" || len(app.history) != 151 || app.historyMore || tester.HasText("重试") {
		t.Fatal("重试后没有恢复正常分页")
	}
}

func TestNativeHistoryScrollCancelKeepsLoadedRows(t *testing.T) {
	app, _, finish := nativePagedHistoryFixture(t, 650)
	tester := ui.NewTester(app.view, 1024, 680)
	app.historyList.ScrollTo(80, ui.Start)
	tester.Frame()
	if app.readTask == nil || !tester.HasText("取消") {
		t.Fatal("续载没有局部取消入口")
	}
	if err := tester.Click("取消"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.readTask != nil || len(app.history) != 100 || !tester.HasText("重试") {
		t.Fatal("取消后没有保留已读数据并暂停续载")
	}
	if err := tester.Click("重试"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if len(app.history) != 600 || !app.historyMore {
		t.Fatal("取消后无法重试续载")
	}
}

func TestNativeHistoryScrollRejectsObsoletePage(t *testing.T) {
	app, _, finish := nativePagedHistoryFixture(t, 650)
	tester := ui.NewTester(app.view, 1024, 680)
	app.historyList.ScrollTo(80, ui.Start)
	tester.Frame()
	if app.readTask == nil {
		t.Fatal("未启动续载")
	}
	app.historyQuery = "分页提交 00000"
	app.historyFilter.CurrentBranch = true
	app.searchHistory(false)
	finish()
	finish()
	tester.Frame()
	if len(app.history) != 1 || app.history[0].Subject != "分页提交 00000" || app.historyMore ||
		app.historyResult.query != app.historyQuery || !app.historyResult.filter.CurrentBranch {
		t.Fatal("过期续载结果混入新条件")
	}
}
