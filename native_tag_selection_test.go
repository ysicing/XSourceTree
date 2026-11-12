// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ysicing/XSourceTree/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func TestNativeTagSelectionResolvesAnnotatedTagsAndScrolls(t *testing.T) {
	app, path, finish := nativeFixture(t)
	base := app.repository.snapshot.Head
	nativeGit(t, path, "tag", "-a", "--no-sign", "-m", "release", "v1")
	for index := range 24 {
		nativeGit(t, path, "commit", "--allow-empty", "-m", fmt.Sprintf("后续提交 %02d", index))
	}
	head := nativeGit(t, path, "rev-parse", "HEAD")
	app.refresh()
	finish()
	app.screen = "history"
	app.searchHistory(false)
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("标签"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("v1"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.problem != "" || app.commitHash != base || app.historySelected != 24 || app.viewedTag != "v1" ||
		!strings.Contains(app.diff.Text, "原始内容") || app.viewedCommit.Subject != "初始提交" {
		t.Fatalf("附注标签没有展示提交详情: hash=%s selected=%d problem=%s", app.commitHash, app.historySelected, app.problem)
	}
	row, ok := tester.AccessByRole(ui.RoleRow, "初始提交")
	table, found := tester.Find("提交历史表格")
	if !ok || !found || !row.Selected || row.Bounds.Y < table.Y || row.Bounds.Y+row.Bounds.H > table.Y+table.H {
		t.Fatalf("目标提交未滚动进入可见区域: row=%+v table=%+v", row, table)
	}
	if nativeGit(t, path, "rev-parse", "HEAD") != head || app.repository.snapshot.Branch != "main" {
		t.Fatal("查看标签改变了当前分支")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-tag-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "native-tag-dark.png")
	}
}

func TestNativeTagSelectionPreservesFiltersAndOffersExplicitReveal(t *testing.T) {
	app, path, finish := nativeFixture(t)
	base := app.repository.snapshot.Head
	tag := strings.Repeat("release-", 8) + "v1"
	nativeGit(t, path, "tag", tag)
	nativeGit(t, path, "commit", "--allow-empty", "-m", "latest")
	app.refresh()
	finish()
	app.screen = "history"
	app.historyQuery, app.historyFile = "latest", "absent.txt"
	app.historyFilter = gitrepo.HistoryFilter{CurrentBranch: true, HideRemotes: true}
	app.searchHistory(false)
	finish()
	request := app.historyRequest()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("标签"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click(tag); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.problem != "" || app.commitHash != base || app.historyRequest() != request ||
		len(app.history) != 0 || app.historySelected != -1 || app.viewedCommit.Subject != "初始提交" {
		t.Fatalf("查看筛选外标签丢失详情或修改筛选: request=%+v hash=%s problem=%s", app.historyRequest(), app.commitHash, app.problem)
	}
	for _, label := range []string{"在全部历史中查看", "此提交不在当前筛选结果中"} {
		bounds, found := tester.Find(label)
		if !found || bounds.X < 0 || bounds.X+bounds.W > 1024 || bounds.Y+bounds.H > 680 {
			t.Fatalf("紧凑窗口下查看入口不可见: %s %+v", label, bounds)
		}
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-tag-filtered-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "native-tag-filtered-dark.png")
	}
	diff, list, commitList := app.diff.Text, app.historyList, app.commitList
	if err := tester.RightClick(tag); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("从此标签新建分支…"); err != nil {
		t.Fatal(err)
	}
	finish()
	app.dialog.fields[0].value = "hotfix-filtered"
	app.dialog.onChange()
	finish()
	tester.Frame()
	if err := tester.Click("创建"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.problem != "" || app.commitHash != base || app.viewedTag != tag || app.diff.Text != diff ||
		app.historyRequest() != request || app.historyList != list || app.commitList != commitList ||
		nativeGit(t, path, "rev-parse", "hotfix-filtered") != base {
		t.Fatalf("筛选外标签创建分支丢失当前查看内容: %s", app.problem)
	}
	if err := tester.Click("在全部历史中查看"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.problem != "" || app.historyRequest() != (nativeHistoryRequest{}) || app.commitHash != base ||
		app.historySelected < 0 || len(app.history) != 2 {
		t.Fatalf("显式查看全部历史未定位提交: selected=%d problem=%s", app.historySelected, app.problem)
	}
}

func TestNativeTagContextMenuUsesClickedTag(t *testing.T) {
	app, path, finish := nativeFixture(t)
	nativeGit(t, path, "tag", "v1")
	nativeGit(t, path, "commit", "--allow-empty", "-m", "later")
	nativeGit(t, path, "tag", "v2")
	head := nativeGit(t, path, "rev-parse", "HEAD")
	app.refresh()
	finish()
	app.selectTag("v1", false)
	finish()
	selected := app.commitHash
	var clipboard ui.Services
	tester := ui.NewTester(func(c *ui.Context) {
		clipboard = c.Services()
		app.view(c)
	}, 1024, 680)
	if err := tester.Click("标签"); err != nil {
		t.Fatal(err)
	}
	if err := tester.RightClick("v2"); err != nil {
		t.Fatal(err)
	}
	if app.commitHash != selected || app.refSelected.reference != "v2" {
		t.Fatal("右键改变了提交详情或没有标明操作对象")
	}
	for _, label := range []string{"复制标签名", "查看标签提交", "从此标签新建分支…", "删除标签…"} {
		if !slices.Contains(tester.Menu(), label) {
			t.Fatalf("标签菜单缺少 %s: %v", label, tester.Menu())
		}
	}
	if err := tester.ChooseMenuItem("复制标签名"); err != nil {
		t.Fatal(err)
	}
	if clipboard.ReadClipboard() != "v2" || app.commitHash != selected {
		t.Fatal("复制标签操作对象错误或改变了详情")
	}
	if err := tester.RightClick("v2"); err != nil {
		t.Fatal(err)
	}
	app.writable = false
	if err := tester.ChooseMenuItem("查看标签提交"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.problem != "" || app.commitHash != head || app.viewedTag != "v2" {
		t.Fatalf("只读状态无法查看右键标签: hash=%s problem=%s", app.commitHash, app.problem)
	}
}

func TestNativeTagSelectionBeyondFirstPageAndLoadMore(t *testing.T) {
	app, path, finish := nativeFixture(t)
	for index := range 205 {
		nativeGit(t, path, "commit", "--allow-empty", "-m", fmt.Sprintf("历史提交 %03d", index))
		if index == 100 {
			nativeGit(t, path, "tag", "v-old")
		}
	}
	target := nativeGit(t, path, "rev-parse", "v-old")
	app.refresh()
	finish()
	app.selectTag("v-old", false)
	finish()
	if app.problem != "" || app.commitHash != target || app.historyOffset != 100 ||
		app.historySelected != 4 || len(app.history) != 100 || !app.historyMore {
		t.Fatalf("旧标签未加载目标页: offset=%d selected=%d rows=%d problem=%s",
			app.historyOffset, app.historySelected, len(app.history), app.problem)
	}
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("加载更多"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if len(app.history) != 106 || app.historyMore || app.historySelected != 4 || app.commitHash != target {
		t.Fatalf("从定位页加载更多丢失或重复提交: %d, more=%v", len(app.history), app.historyMore)
	}
	seen := make(map[string]bool)
	for _, commit := range app.history {
		if seen[commit.Hash] {
			t.Fatal("定位后的分页有重复提交")
		}
		seen[commit.Hash] = true
	}
	if err := tester.Click("返回最新提交"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.historyOffset != 0 || len(app.history) != 100 || app.viewedTag != "" || app.commitHash != "" {
		t.Fatal("返回最新提交未恢复首屏")
	}
}

func TestNativeTagSelectionRejectsObsoleteAndCancelledReads(t *testing.T) {
	app, path, finish := nativeFixture(t)
	nativeGit(t, path, "tag", "v1")
	nativeGit(t, path, "commit", "--allow-empty", "-m", "later")
	nativeGit(t, path, "tag", "v2")
	head := nativeGit(t, path, "rev-parse", "HEAD")
	app.selectTag("v1", false)
	app.selectTag("v2", false)
	finish()
	finish()
	if app.problem != "" || app.viewedTag != "v2" || app.commitHash != head {
		t.Fatalf("快速切换标签被旧结果覆盖: tag=%s hash=%s problem=%s", app.viewedTag, app.commitHash, app.problem)
	}
	history, list, commitList := app.history, app.historyList, app.commitList
	app.selectTag("v1", false)
	app.readTask.cancel()
	finish()
	if app.commitHash != head || app.viewedTag != "v2" || app.historyList != list ||
		app.commitList != commitList || !slices.EqualFunc(app.history, history, func(a, b gitrepo.Commit) bool { return a.Hash == b.Hash }) {
		t.Fatal("取消标签定位破坏了已有内容")
	}
	app.selectTag("v1", false)
	app.openRepository(path)
	finish()
	finish()
	if app.screen != "changes" || app.commitHash != "" || app.viewedTag != "" {
		t.Fatal("切换仓库后回填了旧标签结果")
	}
}
