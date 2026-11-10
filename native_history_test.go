// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysicing/XSourceTree/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func TestNativeGraphConnections(t *testing.T) {
	cases := map[string][]gitrepo.Commit{
		"octopus": {
			{Hash: "merge", Parents: []string{"main", "one", "two"}},
			{Hash: "one", Parents: []string{"base"}},
			{Hash: "two", Parents: []string{"base"}},
			{Hash: "main", Parents: []string{"base"}},
			{Hash: "base"},
		},
		"independent": {
			{Hash: "first", Parents: []string{"root"}},
			{Hash: "other", Parents: []string{"other-root"}},
			{Hash: "other-root"},
			{Hash: "root"},
		},
		"filtered": {
			{Hash: "tip", Parents: []string{"hidden"}},
			{Hash: "base"},
		},
		"compression": {
			{Hash: "one", Parents: []string{"first-root"}},
			{Hash: "two", Parents: []string{"second-root"}},
			{Hash: "first-root"},
			{Hash: "second-root"},
		},
	}
	for name, commits := range cases {
		t.Run(name, func(t *testing.T) {
			graph := commitGraph(commits)
			for index, row := range graph {
				if row.Column < 0 || row.Column >= row.Columns {
					t.Fatalf("节点越过图谱列: %+v", row)
				}
				for _, edge := range row.Edges {
					if index+1 == len(graph) || edge.To >= len(graph[index+1].Incoming) ||
						edge.Color != graph[index+1].Incoming[edge.To] {
						t.Fatalf("跨行连线不连续: row=%d edge=%+v graph=%+v", index, edge, graph)
					}
				}
			}
			if name == "filtered" && (len(graph[0].Edges) != 0 || len(graph[1].Incoming) != 0) {
				t.Fatal("搜索结果错误连接了不相邻的提交")
			}
			if name == "compression" && (graph[1].Color != graph[3].Color || graph[1].Column == graph[3].Column) {
				t.Fatal("轨道压缩改变了分支颜色")
			}
		})
	}
}

func nativeHistoryFixture(t *testing.T) (*nativeApp, string, func()) {
	t.Helper()
	app, path, finish := nativeFixture(t)
	commit := func(subject string) {
		t.Helper()
		nativeGit(t, path, "add", ".")
		nativeGit(t, path, "commit", "-m", subject)
	}
	subjects := []string{
		"feat(repo): 添加本地仓库管理", "feat(diff): 支持并排查看文件差异",
		"fix(commit): 保留未提交的草稿", "feat(branch): 支持创建和切换分支",
		"fix(search): 按作者和提交 ID 搜索", "feat(stash): 支持预览储藏内容",
		"chore(build): 统一构建版本信息", "feat(history): 添加提交文件列表",
	}
	for index, subject := range subjects {
		writeNativeFile(t, path, "hello.txt", fmt.Sprintf("package main\n\nfunc main() {\n\tprintln(\"revision %d\")\n}\n", index))
		commit(subject)
	}
	nativeGit(t, path, "switch", "-c", "feature/native-history")
	writeNativeFile(t, path, "history.go", "package main\n\nconst rowHeight = 28\n")
	commit("feat(history): 使用连续曲线绘制分支图谱")
	writeNativeFile(t, path, "history.go", "package main\n\nconst rowHeight = 28\nconst graphWidth = 80\n")
	commit("fix(history): 保持分支轨道颜色稳定")
	nativeGit(t, path, "switch", "main")
	for index, subject := range []string{
		"feat(ui): 支持深色模式", "fix(ui): 优化窄窗口下的布局", "chore(release): 更新发布校验",
	} {
		writeNativeFile(t, path, "hello.txt", fmt.Sprintf("package main\n\nfunc main() {\n\tprintln(\"layout %d\")\n}\n", index))
		commit(subject)
	}
	nativeGit(t, path, "merge", "--no-ff", "feature/native-history", "-m", "feat(history): 合并提交历史布局优化")
	writeNativeFile(t, path, "hello.txt", "package main\n\nfunc main() {\n\tprintln(\"清晰地浏览提交历史\")\n\tprintln(\"选择文件查看差异\")\n}\n")
	writeNativeFile(t, path, "release.txt", "version: 0.0.3-preview\n")
	commit("feat(history): 展示清晰的提交记录和文件差异")
	nativeGit(t, path, "tag", "v0.0.3-preview")
	nativeGit(t, path, "branch", "preview/very-long-history-branch-name")
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.repository.snapshot.Name = "XSourceTree"
	return app, path, finish
}

func TestNativeHistoryWorkspace(t *testing.T) {
	app, _, finish := nativeHistoryFixture(t)
	tester := ui.NewTester(app.view, 1487, 1058)
	subject := app.history[0].Subject
	if err := tester.Click(subject); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.commitHash != app.history[0].Hash || app.commitFile != "hello.txt" || len(app.commitFiles) != 2 ||
		!strings.Contains(app.diff.Text, "清晰地浏览提交历史") || strings.Contains(app.diff.Text, "version:") {
		t.Fatalf("选择提交没有预览首个文件: hash=%s file=%s files=%v diff=%s problem=%s",
			app.commitHash, app.commitFile, app.commitFiles, app.diff.Text, app.problem)
	}
	row, ok := tester.AccessByRole(ui.RoleRow, subject)
	if !ok || !row.Selected || row.Bounds.H > 30 || row.Description != "" {
		t.Fatalf("提交行未紧凑选中，或仍有遮挡浮层: %+v, found=%v", row, ok)
	}
	table, ok := tester.Find("提交历史表格")
	if !ok || table.W < 1200 {
		t.Fatalf("提交历史没有占据主区域全宽: %+v", table)
	}
	files, ok := tester.Find("提交文件列表")
	layout, layoutOK := tester.Find("差异布局")
	if !ok || !layoutOK || files.Y < table.Y+table.H || layout.X < files.X+files.W {
		t.Fatalf("文件列表和 Diff 没有位于历史下方: history=%+v files=%+v diff=%+v", table, files, layout)
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-history-light.png")
	}
	tester.Key(0, ui.KeyDown)
	finish()
	tester.Frame()
	if app.commitHash != app.history[1].Hash {
		t.Fatal("方向键没有切换到下一个提交")
	}
	if err := tester.Click(subject); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if err := tester.Click("release.txt"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.commitFile != "release.txt" || !strings.Contains(app.diff.Text, "version:") || strings.Contains(app.diff.Text, "println") {
		t.Fatal("文件选择没有更新到对应差异")
	}
	tester.Key(0, ui.KeyUp)
	finish()
	tester.Frame()
	if app.commitFile != "hello.txt" {
		t.Fatal("文件列表不支持方向键选择")
	}
	for _, label := range []string{"并排", "换行"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if !app.prefs.Split || app.prefs.Wrap {
		t.Fatal("差异布局和换行操作失效")
	}
	files, _ = tester.Find("提交文件列表")
	table, _ = tester.Find("提交历史表格")
	dividerX, dividerY := files.X+files.W/2, table.Y+table.H+0.5
	before := app.prefs.History
	tester.Press(dividerX, dividerY)
	tester.Move(dividerX, dividerY-40)
	tester.Release(dividerX, dividerY-40)
	if app.prefs.History >= before {
		t.Fatal("拖动没有改变历史区域比例")
	}
	app.prefs.History = 56
	tester.Frame()
	tester.SetDark(true)
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-history-dark.png")
	}
	tester.SetSize(1024, 680)
	for _, label := range []string{"搜索", "新建标签", "差异布局", "提交文件列表"} {
		bounds, found := tester.Find(label)
		if !found || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > 1024 || bounds.Y+bounds.H > 680 {
			t.Fatalf("窄窗口操作不可见: %s %+v", label, bounds)
		}
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-history-narrow.png")
	}
	app.historyQuery = "没有这个提交"
	app.searchHistory(false)
	finish()
	tester.Frame()
	if len(app.history) != 0 || app.commitHash != "" || app.commitFile != "" || app.diff.Text != "" {
		t.Fatal("空搜索仍保留上次提交的文件或差异")
	}
	if _, ok := tester.Find("没有匹配的提交"); !ok {
		t.Fatal("缺少空结果反馈")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-history-empty.png")
	}
}

func TestNativeHistorySelectionFailure(t *testing.T) {
	app, _, finish := nativeFixture(t)
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.selectCommit(app.history[0].Hash)
	finish()
	selectedHash, selectedFile, diff := app.commitHash, app.commitFile, app.diff.Text
	app.historySelected = -1
	app.selectCommit(strings.Repeat("0", 40))
	finish()
	if app.problem == "" || app.historySelected != 0 || app.commitHash != selectedHash || app.commitFile != selectedFile || app.diff.Text != diff {
		t.Fatal("提交读取失败后，选中行与详情不一致")
	}
}

func TestNativeHistoryPaginationKeepsSelection(t *testing.T) {
	app, path, finish := nativeFixture(t)
	for index := range 101 {
		nativeGit(t, path, "commit", "--allow-empty", "-m", fmt.Sprintf("历史提交 %03d", index))
	}
	app.screen = "history"
	app.searchHistory(false)
	finish()
	if len(app.history) != 100 || !app.historyMore {
		t.Fatalf("历史首屏分页异常: %d, more=%v", len(app.history), app.historyMore)
	}
	app.selectCommit(app.history[0].Hash)
	finish()
	hash := app.commitHash
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("加载更多"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if len(app.history) != 102 || app.historyMore || app.commitHash != hash || app.historySelected != 0 {
		t.Fatal("加载更多丢失了选中的提交或分页状态")
	}
	if row, ok := tester.AccessByRole(ui.RoleRow, app.history[0].Subject); !ok || !row.Selected {
		t.Fatal("加载更多后没有保持选中行")
	}
	app.historyFile = "hello.txt"
	if err := tester.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if len(app.history) != 1 || app.history[0].Subject != "初始提交" || app.commitHash != "" {
		t.Fatal("文件路径筛选没有替换历史结果或清空旧选择")
	}
}

func TestNativeHistoryPreferencesAndEmptyCommit(t *testing.T) {
	app, path, finish := nativeFixture(t)
	oldState := filepath.Join(t.TempDir(), "old-state.json")
	if err := os.WriteFile(oldState, []byte(`{"sidebar":240,"files":320,"wrap":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	restored := newNativeApp(app.service, oldState)
	defer restored.cancel()
	if restored.prefs.History != 56 || restored.prefs.HistoryFiles != 280 || restored.prefs.Sidebar != 240 || restored.prefs.Wrap {
		t.Fatalf("旧版偏好迁移错误: %+v", restored.prefs)
	}
	restored.prefs.History, restored.prefs.HistoryFiles = 40, 220
	if err := restored.savePreferences(); err != nil {
		t.Fatal(err)
	}
	reopened := newNativeApp(app.service, oldState)
	defer reopened.cancel()
	if reopened.prefs.History != 40 || reopened.prefs.HistoryFiles != 220 {
		t.Fatal("没有恢复历史区域的分隔比例和文件列表宽度")
	}
	nativeGit(t, path, "commit", "--allow-empty", "-m", "空提交")
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.selectCommit(app.history[0].Hash)
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if app.commitFile != "" || len(app.commitFiles) != 0 || app.diff.Text != "" {
		t.Fatal("空提交显示了其他提交的文件差异")
	}
	if _, ok := tester.Find("此提交没有文件变更"); !ok {
		t.Fatal("缺少空提交反馈")
	}
}
