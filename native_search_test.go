// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

func TestNativeSearchNavigationKeepsIndependentState(t *testing.T) {
	app, path, finish := nativeHistoryFixture(t)
	writeNativeFile(t, path, "hello.txt", "未提交内容\n")
	app.refresh()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if tester.HasText("搜索提交、作者或 ID") || tester.HasText("文件路径（可选）") {
		t.Fatal("历史页仍显示搜索输入")
	}
	hash := app.history[3].Hash
	app.selectCommit(hash)
	finish()
	tester.Frame()
	historyList, commitList, historyDiff := app.historyList, app.commitList, app.diff.Text
	if err := tester.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	if app.screen != "search" || app.readTask != nil || app.historyLoaded || len(app.history) != 0 {
		t.Fatal("首次进入搜索不应自动查询或复用历史结果")
	}
	if err := tester.Click("搜索提交、作者或 ID"); err != nil {
		t.Fatalf("%v mode=%q texts=%v", err, app.historyFilter.Mode, tester.Texts())
	}
	tester.Type("feat(diff)")
	tester.Key(0, ui.KeyEnter)
	finish()
	tester.Frame()
	if app.problem != "" || len(app.history) != 1 || app.historyWorkingOffset() != 0 || tester.HasText("图谱") {
		t.Fatalf("搜索结果包含工作区或历史图谱: %s", app.problem)
	}
	if err := tester.Click(app.history[0].Subject); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	searchHash, searchList, searchDiff := app.commitHash, app.historyList, app.diff.Text
	for _, screen := range []string{"提交历史", "搜索"} {
		if err := tester.Click(screen); err != nil {
			t.Fatal(err)
		}
		if app.readTask != nil {
			t.Fatal("切换已有结果不应重新查询")
		}
		if screen == "提交历史" {
			if app.commitHash != hash || app.historyList != historyList || app.commitList != commitList || app.diff.Text != historyDiff {
				t.Fatal("搜索覆盖了历史选择、滚动状态或差异")
			}
		} else if app.commitHash != searchHash || app.historyList != searchList || app.diff.Text != searchDiff || app.historyQuery != "feat(diff)" {
			t.Fatal("返回搜索丢失条件、结果或选择")
		}
	}
	if err := tester.Click("在提交历史中查看"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.screen != "history" || app.commitHash != searchHash || app.historySelected < 0 || app.historyQuery != "" {
		t.Fatal("搜索结果未定位到纯提交历史")
	}
	if err := tester.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	if app.commitHash != searchHash || app.historyQuery != "feat(diff)" {
		t.Fatal("定位历史破坏了搜索状态")
	}
}

func TestNativeSearchTypesDatesAndCompactLayout(t *testing.T) {
	app, _, finish := nativeHistoryFixture(t)
	app.switchWorkspace("search")
	tester := ui.NewTester(app.view, 1024, 680)
	searchBounds, ok := tester.Find("搜索提交、作者或 ID")
	if !ok {
		t.Fatal("搜索输入框未显示")
	}
	for _, label := range []string{"搜索类型", "文件路径（可选）", "日期范围", "执行搜索"} {
		bounds, found := tester.Find(label)
		if !found || bounds.Y+bounds.H/2 != searchBounds.Y+searchBounds.H/2 || bounds.X+bounds.W > 1024 {
			t.Fatalf("搜索栏未在紧凑窗口中保持单行: %s %+v", label, bounds)
		}
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-search-single-row.png")
	}
	app.historyQuery = "Native Test"
	if err := tester.Click("全部内容"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("用户"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if app.historyFilter.Mode != "author" {
		t.Fatalf("搜索类型没有切换到用户: mode=%q texts=%v", app.historyFilter.Mode, tester.Texts())
	}
	if err := tester.Click("日期范围"); err != nil {
		t.Fatal(err)
	}
	app.searchSince = time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)
	app.searchUntil = time.Date(2099, 12, 31, 0, 0, 0, 0, time.Local)
	tester.Frame()
	if err := tester.Click("执行搜索"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.problem != "" || len(app.history) == 0 || app.historyResult.filter.Mode != "author" ||
		app.historyResult.filter.Since != "2000-01-01" || app.historyResult.filter.Until != "2099-12-31" {
		t.Fatalf("搜索类型和日期未一起生效: %+v %s", app.historyResult, app.problem)
	}
	if err := tester.Click(app.history[0].Subject); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	app.historyQuery = strings.Repeat("长文本/", 30)
	for _, language := range []string{"en", "zh-Hant", "zh-Hans"} {
		t.Setenv("MYGO_LOCALE", language)
		tester.SetLocale(language)
		for _, dark := range []bool{false, true} {
			tester.SetDark(dark)
			for _, label := range []string{locales.SearchType(), locales.RunSearch(), locales.SearchDateRange(),
				locales.SearchStartDate(), locales.SearchEndDate(), locales.SearchResults(), locales.ViewInCommitHistory()} {
				bounds, ok := tester.Find(label)
				if !ok || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > 1024 || bounds.Y+bounds.H > 680 {
					t.Fatalf("紧凑窗口裁切 %s/%s: %+v", language, label, bounds)
				}
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				name := "native-search-" + language + "-light.png"
				if dark {
					name = "native-search-" + language + "-dark.png"
				}
				saveNativeScreenshot(t, tester, directory, name)
			}
		}
	}
	if !tester.HasText("条件已修改，按回车重新搜索") {
		t.Fatal("修改条件后没有区分已显示结果")
	}
	before := app.history
	app.searchSince, app.searchUntil = app.searchUntil, app.searchSince
	tester.Frame()
	if err := tester.Click("执行搜索"); err != nil {
		t.Fatal(err)
	}
	if app.readTask != nil || &app.history[0] != &before[0] || !tester.HasText("开始日期不能晚于结束日期") {
		t.Fatal("无效日期不应执行查询或清空旧结果")
	}
}

func TestNativeSearchFileHistoryAndTagNavigation(t *testing.T) {
	app, path, finish := nativeFixture(t)
	nativeGit(t, path, "tag", "v1")
	writeNativeFile(t, path, "hello.txt", "修改\n")
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("文件历史"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.screen != "search" || app.historyFile != "hello.txt" || len(app.history) != 1 || app.historyWorkingOffset() != 0 {
		t.Fatalf("文件历史没有进入搜索页: screen=%s file=%s problem=%s", app.screen, app.historyFile, app.problem)
	}
	app.historyQuery, app.historyFilter.Mode = "missing", "author"
	app.executeSearch()
	finish()
	tester.Frame()
	if !tester.HasText("没有匹配的提交") {
		t.Fatal("没有展示空搜索结果")
	}
	app.selectTag("v1", false)
	finish()
	tester.Frame()
	if app.screen != "history" || app.commitHash != app.repository.snapshot.Head || app.historyQuery != "" ||
		app.historyFilter.Mode != "" || app.historySelected < 0 {
		t.Fatal("搜索条件污染了标签历史定位")
	}
	app.switchWorkspace("search")
	if app.historyQuery != "missing" || app.historyFile != "hello.txt" || app.historyFilter.Mode != "author" {
		t.Fatal("查看标签丢失搜索草稿")
	}
}

func TestNativeSearchRefreshUsesAppliedRequest(t *testing.T) {
	app, _, finish := nativeHistoryFixture(t)
	app.switchWorkspace("search")
	app.historyQuery = "feat(diff)"
	app.executeSearch()
	finish()
	app.selectCommit(app.history[0].Hash)
	finish()
	hash, diff := app.commitHash, app.diff.Text
	app.historyQuery, app.historyFilter.Mode = "draft query", "author"
	app.refresh()
	finish()
	if app.problem != "" || len(app.history) != 1 || app.commitHash != hash || app.diff.Text != diff ||
		app.historyQuery != "draft query" || app.historyResult.query != "feat(diff)" || app.historyFilter.Mode != "author" {
		t.Fatalf("刷新应用了未执行的草稿或丢失详情: %s %+v", app.problem, app.historyResult)
	}
	app.historyMore = true
	app.searchHistory(true)
	finish()
	if app.problem != "" || len(app.history) != 1 || app.commitHash != hash || app.historyResult.query != "feat(diff)" {
		t.Fatal("加载更多没有沿用已执行条件")
	}
}

func TestNativeSearchReturnToWorkingHistory(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "第一次修改\n")
	app.refresh()
	finish()
	app.switchWorkspace("history")
	app.searchHistory(false)
	finish()
	app.selectHistoryWorking()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	for _, committed := range []bool{false, true} {
		if err := tester.Click("文件变更"); err != nil {
			t.Fatal(err)
		}
		writeNativeFile(t, path, "hello.txt", "更新后的修改\n")
		if committed {
			nativeGit(t, path, "add", ".")
			nativeGit(t, path, "commit", "-m", "提交工作区修改")
		}
		app.refresh()
		finish()
		tester.Frame()
		if err := tester.Click("提交历史"); err != nil {
			t.Fatal(err)
		}
		finish()
		tester.Frame()
		if app.problem != "" || !strings.Contains(app.diff.Text, "更新后的修改") {
			t.Fatalf("返回历史仍显示过期差异: %s %s", app.problem, app.diff.Text)
		}
		if committed && (app.historyWorking || app.commitHash != app.repository.snapshot.Head) {
			t.Fatal("工作区清空后未回到 HEAD")
		}
		if !committed && !app.historyWorking {
			t.Fatal("返回历史丢失工作区选择")
		}
	}
}

func TestNativeSearchReturnRefreshesChangedRepository(t *testing.T) {
	app, path, finish := nativeHistoryFixture(t)
	hash := app.history[0].Hash
	app.selectCommit(hash)
	finish()
	app.switchWorkspace("search")
	app.executeSearch()
	finish()
	app.selectCommit(hash)
	finish()
	app.historyQuery = "未执行的搜索"
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("文件变更"); err != nil {
		t.Fatal(err)
	}
	nativeGit(t, path, "commit", "--allow-empty", "-m", "新的提交")
	app.refresh()
	finish()
	tester.Frame()
	for _, screen := range []string{"提交历史", "搜索"} {
		if err := tester.Click(screen); err != nil {
			t.Fatal(err)
		}
		finish()
		tester.Frame()
		if app.problem != "" || app.history[0].Subject != "新的提交" || app.commitHash != hash {
			t.Fatalf("返回 %s 没有刷新结果或保留选择: %s", screen, app.problem)
		}
		if screen == "搜索" && (app.historyQuery != "未执行的搜索" || app.historyResult.query != "") {
			t.Fatal("返回搜索应用了未执行的草稿")
		}
	}
	for _, screen := range []string{"提交历史", "搜索"} {
		if err := tester.Click(screen); err != nil {
			t.Fatal(err)
		}
		if app.busy || app.readTask != nil {
			t.Fatal("仓库未变化时重复刷新缓存")
		}
	}
}

func TestNativeSearchCancellationKeepsResultsAndDraft(t *testing.T) {
	app, _, finish := nativeFixture(t)
	app.switchWorkspace("search")
	app.executeSearch()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	started, stopped := make(chan struct{}), make(chan struct{})
	app.historyQuery = "draft"
	app.historyFilter.Mode = "changes"
	app.load("正在搜索历史…", nativeReadHistory, func(ctx context.Context) (func(), error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	})
	<-started
	tester.Frame()
	if err := tester.Click("取消搜索"); err != nil {
		t.Fatal(err)
	}
	<-stopped
	finish()
	tester.Frame()
	if len(app.history) != 1 || app.historyQuery != "draft" || app.historyFilter.Mode != "changes" || app.readTask != nil {
		t.Fatal("取消搜索丢失结果或草稿")
	}
	app.load("正在搜索历史…", nativeReadHistory, func(ctx context.Context) (func(), error) {
		<-ctx.Done()
		return func() { app.history = nil }, ctx.Err()
	})
	app.switchWorkspace("history")
	finish()
	if app.screen != "history" || app.historyLoaded {
		t.Fatal("过期搜索覆盖了历史页")
	}
	app.switchWorkspace("search")
	if len(app.history) != 1 {
		t.Fatal("过期搜索清空了原搜索结果")
	}
}
