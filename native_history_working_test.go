// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysicing/51code/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func TestNativeHistoryWorkingFiles(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "暂存版本\n")
	nativeGit(t, path, "add", "hello.txt")
	writeNativeFile(t, path, "hello.txt", "工作区版本\n")
	writeNativeFile(t, path, "new.txt", "新文件内容\n")
	app.refresh()
	finish()
	app.screen = "history"
	app.searchHistory(false)
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	label := "未提交更改 · 2 个文件"
	if err := tester.Click(label); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if !app.historyWorking || app.commitHash != "" || app.staged || app.selectedFile != "hello.txt" ||
		!strings.Contains(app.diff.Text, "+工作区版本") || !strings.Contains(app.diff.Text, "-暂存版本") {
		t.Fatalf("未展示未暂存差异: working=%v staged=%v diff=%s", app.historyWorking, app.staged, app.diff.Text)
	}
	row, ok := tester.AccessByRole(ui.RoleRow, label)
	if !ok || !row.Selected || row.Bounds.H > 30 {
		t.Fatalf("工作区状态行未紧凑选中: %+v", row)
	}
	if !tester.HasText("已暂存文件") || !tester.HasText("未暂存文件") {
		t.Fatal("缺少文件分组")
	}
	if err := tester.Click("选择 已暂存文件 hello.txt"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if !app.staged || !strings.Contains(app.diff.Text, "+暂存版本") || strings.Contains(app.diff.Text, "工作区版本") {
		t.Fatalf("已暂存差异混入工作区内容: %s", app.diff.Text)
	}
	if err := tester.Click("选择 未暂存文件 new.txt"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.staged || !strings.Contains(app.diff.Text, "新文件内容") {
		t.Fatalf("未跟踪文件未显示: %s", app.diff.Text)
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-history-working-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "native-history-working-dark.png")
	}
	app.fileQuery, app.fileFilter = "hidden", "tracked"
	if err := tester.Click("前往文件变更"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.screen != "changes" || app.historyWorking || app.selectedFile != "new.txt" || app.filesFiltered() ||
		!strings.Contains(app.diff.Text, "新文件内容") {
		t.Fatal("前往文件变更没有保留所选文件")
	}
	if nativeGit(t, path, "diff", "--cached", "--name-only") != "hello.txt" {
		t.Fatal("查看工作区改变了暂存区")
	}
}

func TestNativeHistoryWorkingSelectionAndRefresh(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "修改内容\n")
	app.refresh()
	finish()
	app.screen = "history"
	app.searchHistory(false)
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("初始提交"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.historySelected != 0 || app.commitHash != app.history[0].Hash {
		t.Fatal("状态行影响了真实提交的索引")
	}
	tester.Key(0, ui.KeyUp)
	finish()
	tester.Frame()
	if !app.historyWorking {
		t.Fatal("方向键未选择工作区行")
	}
	if err := tester.RightClick("未提交更改 · 1 个文件"); err != nil {
		t.Fatal(err)
	}
	if len(tester.Menu()) != 0 {
		t.Fatalf("工作区行出现提交操作菜单: %v", tester.Menu())
	}
	writeNativeFile(t, path, "hello.txt", "刷新后的内容\n")
	app.refresh()
	finish()
	tester.Frame()
	if !app.historyWorking || app.selectedFile != "hello.txt" || !strings.Contains(app.diff.Text, "刷新后的内容") {
		t.Fatal("刷新未保留工作区选择或更新差异")
	}
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "完成修改")
	app.refresh()
	finish()
	tester.Frame()
	if app.historyWorking || app.historyWorkingOffset() != 0 || app.commitHash != app.repository.snapshot.Head ||
		!strings.Contains(app.diff.Text, "刷新后的内容") || app.problem != "" {
		t.Fatalf("工作区清空后没有回到 HEAD: hash=%s problem=%s", app.commitHash, app.problem)
	}
	selected := app.commitHash
	writeNativeFile(t, path, "hello.txt", "再次修改\n")
	app.refresh()
	finish()
	tester.Frame()
	if app.historyWorking || app.commitHash != selected || app.historyWorkingOffset() != 1 {
		t.Fatal("新变更抢走了历史选择")
	}
	row, ok := tester.AccessByRole(ui.RoleRow, "完成修改")
	if !ok || !row.Selected {
		t.Fatal("插入状态行后没有保持所选提交")
	}
}

func TestNativeHistoryWorkingFiltersAndUnborn(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "尚未提交\n")
	app.refresh()
	finish()
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.selectHistoryWorking()
	finish()
	app.historyQuery = "初始"
	app.searchHistory(false)
	finish()
	if app.historyWorking || app.historyWorkingOffset() != 0 || app.diff.Text != "" {
		t.Fatal("提交搜索仍显示工作区详情")
	}
	app.historyQuery, app.historyFile = "", "hello.txt"
	app.searchHistory(false)
	finish()
	if app.historyWorkingOffset() != 0 {
		t.Fatal("文件历史中仍显示工作区行")
	}
	app.historyFile = ""
	app.searchHistory(false)
	finish()
	app.historyOffset = 100
	if app.historyWorkingOffset() != 0 {
		t.Fatal("较早历史页中仍显示工作区行")
	}
	app.historyOffset = 0
	nativeGit(t, path, "checkout", "--orphan", "empty")
	nativeGit(t, path, "rm", "--cached", "-f", "hello.txt")
	app.historyFilter.CurrentBranch = true
	app.refresh()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if len(app.history) != 0 || app.repository.snapshot.Head != "" {
		t.Fatal("测试仓库应没有当前分支历史")
	}
	if err := tester.Click("未提交更改 · 1 个文件"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if !app.historyWorking || !strings.Contains(app.diff.Text, "尚未提交") {
		t.Fatal("尚无首次提交时无法查看工作区")
	}
}

func TestNativeHistoryWorkingGraphTargetsHEAD(t *testing.T) {
	commits := []gitrepo.Commit{
		{Hash: "other", Parents: []string{"base"}},
		{Hash: "head", Parents: []string{"base"}},
		{Hash: "base"},
	}
	graph := historyGraphWithWorking(commits, "head", true)
	if len(graph) != 4 || !graph[0].Working || len(graph[0].Edges) != 1 {
		t.Fatal("未创建工作区图谱节点")
	}
	lane := graph[0].Edges[0].To
	if graph[1].Column == lane {
		t.Fatal("工作区错误连接到其他分支的最新提交")
	}
	for _, edge := range graph[1].Edges {
		if edge.From == lane {
			lane = edge.To
			break
		}
	}
	if graph[2].Column != lane {
		t.Fatal("工作区没有连接到当前 HEAD")
	}
	for _, head := range []string{"missing", ""} {
		graph = historyGraphWithWorking(commits, head, true)
		if len(graph[0].Edges) != 0 {
			t.Fatal("HEAD 不可见时画出了错误连线")
		}
	}
}

func TestNativeHistoryWorkingReadFailureAndStagedOnly(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "已暂存内容\n")
	nativeGit(t, path, "add", "hello.txt")
	app.refresh()
	finish()
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.selectHistoryWorking()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if !app.staged || !strings.Contains(app.diff.Text, "已暂存内容") ||
		!tester.HasText("已暂存文件") || tester.HasText("未暂存文件") {
		t.Fatal("只有已暂存文件时选错分组")
	}
	nativeGit(t, path, "commit", "-m", "暂存完成")
	writeNativeFile(t, path, "temporary.txt", "临时内容\n")
	app.refresh()
	finish()
	if app.selectedFile != "temporary.txt" || app.staged {
		t.Fatal("旧文件离开工作区后没有选择剩余文件")
	}
	if err := os.Remove(filepath.Join(path, "temporary.txt")); err != nil {
		t.Fatal(err)
	}
	app.selectHistoryWorkingFile("temporary.txt", false)
	finish()
	if app.problem == "" || app.diff.Text != "" || !app.historyWorking {
		t.Fatal("读取失败仍显示旧差异或未提供错误反馈")
	}
	app.refresh()
	finish()
	if app.problem != "" || app.historyWorking || app.commitHash != app.repository.snapshot.Head {
		t.Fatal("刷新没有恢复已消失的工作区文件")
	}
}
