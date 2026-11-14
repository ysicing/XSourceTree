// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

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

func TestNativeHistoryWorkingStaging(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "修改内容\n")
	writeNativeFile(t, path, "other.txt", "另一个文件\n")
	app.refresh()
	finish()
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.selectHistoryWorking()
	finish()
	app.prefs.Drafts[app.repository.snapshot.Path] = "保留提交草稿"
	app.fileQuery, app.fileFilter = "隐藏所有文件", "tracked"
	app.historyQuery = "尚未执行的搜索"
	tester := ui.NewTester(app.view, 1440, 900)
	for _, action := range []string{"暂存文件 hello.txt", "取消暂存文件 hello.txt", "全部暂存", "全部取消暂存"} {
		if err := tester.Click(action); err != nil {
			t.Fatal(err)
		}
		finish()
		tester.Frame()
		staged := action == "暂存文件 hello.txt" || action == "全部暂存"
		if app.problem != "" || app.screen != "history" || !app.historyWorking || app.selectedFile != "hello.txt" ||
			app.staged != staged || !app.canApplyPatch() {
			t.Fatalf("%s 后未保留工作区与补丁: file=%s staged=%v problem=%s", action, app.selectedFile, app.staged, app.problem)
		}
		expected := ""
		if action == "暂存文件 hello.txt" {
			expected = "hello.txt"
		} else if action == "全部暂存" {
			expected = "hello.txt\nother.txt"
		}
		if got := nativeGit(t, path, "diff", "--cached", "--name-only"); got != expected {
			t.Fatalf("%s 暂存范围错误: %q", action, got)
		}
		if app.historyResult.query != "" || app.historyQuery != "尚未执行的搜索" ||
			app.prefs.Drafts[app.repository.snapshot.Path] != "保留提交草稿" {
			t.Fatal("暂存改变了历史筛选或提交草稿")
		}
	}
	app.selectCommit(app.history[0].Hash)
	finish()
	tester.Frame()
	for _, label := range []string{"暂存文件", "暂存此区块", "选择行…"} {
		if _, found := tester.AccessByRole(ui.RoleButton, label); found {
			t.Fatal("已提交记录出现写操作", label)
		}
	}
	app.stageSelectedFile()
	app.applyHunk()
	if app.busy {
		t.Fatal("已提交记录执行了暂存")
	}
}

func TestNativeHistoryWorkingPatchActions(t *testing.T) {
	app, path, finish := nativeFixture(t)
	before := "old first\n" + strings.Repeat("context\n", 12) + "old last\n"
	after := strings.ReplaceAll(before, "old", "new")
	writeNativeFile(t, path, "hello.txt", before)
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "两个独立区块")
	writeNativeFile(t, path, "hello.txt", after)
	app.refresh()
	finish()
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.selectHistoryWorking()
	finish()
	tester := ui.NewTester(app.view, 1800, 900)
	if len(app.patch.Hunks) != 2 {
		t.Fatalf("历史工作区未加载完整区块: %+v", app.patch)
	}
	if err := tester.Click("暂存此区块"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if got := nativeGit(t, path, "show", ":hello.txt"); got != strings.TrimSpace(strings.Replace(before, "old first", "new first", 1)) {
		t.Fatal("区块暂存影响了另一个区块", got)
	}
	if app.staged || !app.canApplyPatch() || len(app.patch.Hunks) != 1 {
		t.Fatal("部分暂存后未保持未暂存区块选择")
	}
	if err := tester.Click("选择 已暂存文件 hello.txt"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if err := tester.Click("取消此区块暂存"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.staged || !app.historyWorking || nativeGit(t, path, "diff", "--cached") != "" {
		t.Fatal("区块取消暂存后未跟随到未暂存分组")
	}
	for _, staged := range []bool{false, true} {
		if staged {
			app.selectHistoryWorkingFile("hello.txt", true)
			finish()
			tester.Frame()
		}
		if err := tester.Click("选择行…"); err != nil {
			t.Fatal(err)
		}
		if err := tester.Click("+new first"); err != nil {
			t.Fatal(err)
		}
		if err := tester.Click("应用所选行"); err != nil {
			t.Fatal(err)
		}
		finish()
		tester.Frame()
		expected := strings.TrimSpace(before)
		if !staged {
			expected = strings.TrimSpace(strings.Replace(before, "old first\n", "old first\nnew first\n", 1))
		}
		if got := nativeGit(t, path, "show", ":hello.txt"); got != expected {
			t.Fatalf("逐行操作范围错误: staged=%v content=%q", staged, got)
		}
		if app.dialog != nil || !app.historyWorking || app.problem != "" || !app.canApplyPatch() {
			t.Fatalf("逐行操作后状态异常: %s", app.problem)
		}
	}
	if content, err := os.ReadFile(filepath.Join(path, "hello.txt")); err != nil || string(content) != after {
		t.Fatal("部分暂存操作改变了工作区", err)
	}
	app.refreshOnFocus()
	finish()
	if !app.canApplyPatch() || len(app.patch.Hunks) != 2 {
		t.Fatal("焦点刷新丢失可操作补丁")
	}
	writeNativeFile(t, path, "hello.txt", "外部修改\n")
	app.applyHunk()
	finish()
	if app.problem == "" || nativeGit(t, path, "diff", "--cached") != "" {
		t.Fatal("未拒绝过期区块或错误修改索引")
	}
}

func TestNativeHistoryWorkingWriteGuards(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "修改内容\n")
	app.refresh()
	finish()
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.selectHistoryWorking()
	finish()
	for _, state := range []string{"readonly", "reading", "busy"} {
		app.writable = state != "readonly"
		app.busy = state == "busy"
		if state == "reading" {
			app.readTask = &nativeRead{kind: nativeReadDiff, cancel: func() {}}
		}
		tester := ui.NewTester(app.view, 1800, 900)
		for _, label := range []string{"暂存文件 hello.txt", "全部暂存", "暂存文件", "暂存此区块", "选择行…"} {
			button, found := tester.AccessByRole(ui.RoleButton, label)
			if !found || !button.Disabled {
				t.Fatalf("%s 未禁用 %s: %+v", state, label, button)
			}
		}
		if app.canApplyPatch() {
			t.Fatal("不可写状态仍允许区块操作", state)
		}
		app.readTask, app.busy, app.writable = nil, false, true
	}
	app.repository.snapshot.Files[0].Conflict = true
	app.selectHistoryWorkingFile("hello.txt", false)
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if app.conflict != nil || app.workingFileActions() || app.canApplyPatch() {
		t.Fatal("历史页意外开放冲突编辑")
	}
	for _, label := range []string{"暂存文件 hello.txt", "全部暂存"} {
		button, found := tester.AccessByRole(ui.RoleButton, label)
		if !found || !button.Disabled {
			t.Fatal("冲突文件仍可直接暂存", label)
		}
	}
}

func TestNativeHistoryWorkingCompactActions(t *testing.T) {
	app, path, finish := nativeFixture(t)
	name := strings.Repeat("long-file-name-", 8) + ".txt"
	writeNativeFile(t, path, name, "original\n")
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "长文件名")
	writeNativeFile(t, path, name, "staged\n")
	nativeGit(t, path, "add", ".")
	writeNativeFile(t, path, name, "working\n")
	app.refresh()
	finish()
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.selectHistoryWorking()
	finish()
	for _, appearance := range []string{appearanceLight, appearanceDark} {
		app.prefs.Appearance = appearance
		tester := ui.NewTester(app.view, 1024, 680)
		for _, label := range []string{"全部暂存", "全部取消暂存", "暂存文件 " + name, "取消暂存文件 " + name} {
			button, found := tester.AccessByRole(ui.RoleButton, label)
			if !found || button.Offscreen || button.Bounds.W < 20 || button.Bounds.X+button.Bounds.W > 1024 ||
				button.Bounds.Y+button.Bounds.H > 680 {
				t.Fatalf("窄窗口操作不可见: %s %+v", label, button)
			}
		}
		if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
			saveNativeScreenshot(t, tester, directory, "native-history-working-compact-"+appearance+".png")
		}
		toolbar, found := tester.AccessByRole(ui.RoleToolbar, "差异操作")
		if !found {
			t.Fatal("缺少差异操作工具栏")
		}
		opened := false
		for _, node := range tester.AccessAll() {
			if node.Name == "更多" && !node.Offscreen &&
				node.Bounds.X >= toolbar.Bounds.X && node.Bounds.Y >= toolbar.Bounds.Y &&
				node.Bounds.X+node.Bounds.W <= toolbar.Bounds.X+toolbar.Bounds.W &&
				node.Bounds.Y+node.Bounds.H <= toolbar.Bounds.Y+toolbar.Bounds.H {
				tester.Press(node.Bounds.X+node.Bounds.W/2, node.Bounds.Y+node.Bounds.H/2)
				tester.Release(node.Bounds.X+node.Bounds.W/2, node.Bounds.Y+node.Bounds.H/2)
				opened = true
				break
			}
		}
		if !opened {
			t.Fatal("窄窗口缺少差异操作溢出菜单")
		}
		if err := tester.ChooseMenuItem("选择行…"); err != nil {
			t.Fatal(err)
		}
		if app.dialog == nil || app.dialog.title != "选择变更行" {
			t.Fatal("无法通过溢出菜单打开逐行操作")
		}
		tester.Key(0, ui.KeyEscape)
		if app.dialog != nil {
			t.Fatal("逐行弹窗无法通过 Esc 关闭")
		}
	}
}
