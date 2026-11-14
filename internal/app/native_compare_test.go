// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNativeComparisonReadOnlyAndSwap(t *testing.T) {
	app, path, finish := nativeFixture(t)
	base := app.repository.snapshot.Head
	writeNativeFile(t, path, "hello.txt", "比较目标\n")
	nativeGit(t, path, "commit", "-am", "target")
	target := nativeGit(t, path, "rev-parse", "HEAD")
	app.refresh()
	finish()
	app.writable = false
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("版本比较"); err != nil {
		t.Fatal(err)
	}
	app.comparison.base, app.comparison.target = base, target
	tester.Frame()
	if err := tester.Click("比较"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.comparison.result == nil || app.comparison.file != "hello.txt" ||
		app.diff.Old.Text != "原始内容\n" || app.diff.New.Text != "比较目标\n" {
		t.Fatal("比较没有载入完整的两个版本")
	}
	for _, label := range []string{"暂存文件", "暂存区块", "选择行…", "丢弃修改…"} {
		if _, found := tester.Find(label); found {
			t.Fatalf("只读比较显示了写操作 %s", label)
		}
	}
	if err := tester.Click("全文"); err != nil {
		t.Fatal(err)
	}
	if !app.fileViewer.states[1].ReadOnly {
		t.Fatal("比较全文可以编辑")
	}
	if err := tester.Click("交换比较方向"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.comparison.result.From != target || app.comparison.result.Target != base ||
		app.diff.Old.Text != "比较目标\n" || app.diff.New.Text != "原始内容\n" {
		t.Fatal("交换没有反转比较内容")
	}
	for _, size := range [][2]int{{1024, 680}, {1320, 860}} {
		for _, appearance := range []string{appearanceLight, appearanceDark} {
			app.prefs.Appearance = appearance
			tester = ui.NewTester(app.view, size[0], size[1])
			for _, label := range []string{"基准版本", "目标版本", "交换比较方向", "比较"} {
				info, found := tester.Find(label)
				if !found || info.X < 0 || info.X+info.W > float32(size[0]) ||
					info.Y < 0 || info.Y+info.H > float32(size[1]) {
					t.Fatalf("比较控件裁切 %v %s %s: %+v", size, appearance, label, info)
				}
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				saveNativeScreenshot(t, tester, directory, fmt.Sprintf("comparison-%s-%d.png", appearance, size[0]))
			}
		}
	}
}

func TestNativeComparisonModesAndFailure(t *testing.T) {
	app, path, finish := nativeFixture(t)
	common := app.repository.snapshot.Head
	nativeGit(t, path, "checkout", "-b", "feature")
	writeNativeFile(t, path, "feature.txt", "feature\n")
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "feature")
	nativeGit(t, path, "checkout", "main")
	writeNativeFile(t, path, "hello.txt", "base changed\n")
	nativeGit(t, path, "commit", "-am", "base")
	app.refresh()
	finish()
	app.switchWorkspace("compare")
	app.comparison.base, app.comparison.target = "main", "feature"
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("共同祖先"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("比较"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	result := app.comparison.result
	if result == nil || result.From != common || len(result.Files) != 1 || result.Files[0].Path != "feature.txt" {
		t.Fatal("界面比较方式未正确传入 Git")
	}
	previous := app.diff
	app.comparison.target = "missing-ref"
	app.startComparison()
	finish()
	tester.Frame()
	if app.problem == "" || app.comparison.result != result || app.diff.Text != previous.Text {
		t.Fatal("读取失败丢失已有比较或缺少错误提示")
	}
	app.switchWorkspace("changes")
	app.switchWorkspace("compare")
	if app.diff.Text != previous.Text {
		t.Fatal("返回比较没有恢复已加载结果")
	}
	app.refresh()
	finish()
	if app.diff.Text != previous.Text {
		t.Fatal("刷新仓库清空固定提交的比较")
	}
}

func TestNativeComparisonIgnoresObsoleteResults(t *testing.T) {
	app, _, finish := nativeFixture(t)
	app.switchWorkspace("compare")
	state := app.comparison
	state.base, state.target = "HEAD", "HEAD"
	app.startComparison()
	app.switchWorkspace("changes")
	finish()
	if state.result != nil || app.screen != "changes" {
		t.Fatal("离开页面后应用了过期比较")
	}
	app.switchWorkspace("compare")
	app.startComparison()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if _, found := tester.Find("两个版本之间没有文件变更"); !found {
		t.Fatal("缺少相同版本的空状态")
	}
	app.openRepository(t.TempDir())
	finish()
	if app.problem == "" {
		t.Fatal("测试需要打开失败")
	}
	if app.comparison != state {
		t.Fatal("打开失败清空原仓库比较")
	}
	other := t.TempDir()
	nativeGit(t, other, "init", "-b", "main")
	nativeGit(t, other, "config", "user.name", "Other Test")
	nativeGit(t, other, "config", "user.email", "other@example.test")
	nativeGit(t, other, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "other")
	app.openRepository(other)
	finish()
	app.switchWorkspace("compare")
	if app.comparison.result != nil || app.diff.Text != "" {
		t.Fatal("切换仓库残留原仓库比较")
	}
}

func TestNativeComparisonSelectsFilesAndPinsReferences(t *testing.T) {
	app, path, finish := nativeFixture(t)
	base := app.repository.snapshot.Head
	writeNativeFile(t, path, "another.txt", "another\n")
	writeNativeFile(t, path, "hello.txt", "target\n")
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "target")
	target := nativeGit(t, path, "rev-parse", "HEAD")
	app.switchWorkspace("compare")
	app.comparison.base, app.comparison.target = base, "HEAD"
	app.startComparison()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("another.txt"); err != nil {
		t.Fatal(err)
	}
	tester.Key(0, ui.KeyDown)
	finish()
	if app.comparison.file != "hello.txt" || app.diff.New.Text != "target\n" {
		t.Fatal("文件列表方向键没有加载比较文件")
	}
	writeNativeFile(t, path, "hello.txt", "moved ref\n")
	nativeGit(t, path, "commit", "-am", "moved")
	app.selectComparisonFile("another.txt")
	finish()
	app.selectComparisonFile("hello.txt")
	finish()
	if app.diff.New.Revision != target || app.diff.New.Text != "target\n" {
		t.Fatal("切文件时重新解析 HEAD，混用了不同提交")
	}
}

func TestNativeComparisonLongInputsAndKeyboard(t *testing.T) {
	app, _, finish := nativeFixture(t)
	app.switchWorkspace("compare")
	app.comparison.base, app.comparison.target = "HEAD", "HEAD"
	tester := ui.NewTester(app.view, 1024, 680)
	input, found := tester.AccessByRole(ui.RoleComboBox, "基准版本")
	if !found {
		t.Fatal("缺少基准版本输入框")
	}
	tester.ClickAt(input.Bounds.X+input.Bounds.W/2, input.Bounds.Y+input.Bounds.H/2)
	tester.Key(ui.Cmd, ui.KeyA)
	tester.Type("HEAD")
	tester.Key(0, ui.KeyEnter)
	finish()
	tester.Frame()
	if app.comparison.result == nil || app.problem != "" {
		t.Fatalf("回车未执行比较: %s", app.problem)
	}
	app.comparison.base = "refs/heads/" + strings.Repeat("very-long-branch/", 15)
	tester.Frame()
	if _, found := tester.Find("交换比较方向"); !found {
		t.Fatal("长版本名称挤走交换入口")
	}
}

func TestNativeComparisonCancelsFileSelection(t *testing.T) {
	app, path, finish := nativeFixture(t)
	base := app.repository.snapshot.Head
	writeNativeFile(t, path, "another.txt", "another\n")
	writeNativeFile(t, path, "hello.txt", "target\n")
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "target")
	app.switchWorkspace("compare")
	app.comparison.base, app.comparison.target = base, "HEAD"
	app.startComparison()
	finish()
	state := app.comparison
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("hello.txt"); err != nil {
		t.Fatal(err)
	}
	if state.selected != 1 || app.readTask == nil {
		t.Fatal("测试未触发待完成的文件切换")
	}
	app.cancelRead()
	finish()
	if state.selected != 0 || state.file != "another.txt" || app.diff.New.Path != "another.txt" {
		t.Fatal("取消读取后文件高亮与正文不一致")
	}
	tester.Frame()
	if err := tester.Click("hello.txt"); err != nil {
		t.Fatal(err)
	}
	app.refresh()
	finish()
	finish()
	if state.selected != 0 || state.file != "another.txt" || app.diff.New.Path != "another.txt" {
		t.Fatal("刷新取消读取后文件高亮与正文不一致")
	}
}

func TestNativeComparisonKeepsResultWhenFirstDiffFails(t *testing.T) {
	app, path, finish := nativeFixture(t)
	base := app.repository.snapshot.Head
	writeNativeFile(t, path, "hello.txt", "target\n")
	nativeGit(t, path, "commit", "-am", "target")
	app.switchWorkspace("compare")
	app.comparison.base, app.comparison.target = base, "HEAD"
	app.startComparison()
	finish()
	result, text := app.comparison.result, app.diff.Text
	writeNativeFile(t, path, "aaa-large.txt", strings.Repeat("large new line\n", 320000))
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "large")
	app.startComparison()
	finish()
	if app.problem == "" || app.comparison.result != result || app.diff.Text != text {
		t.Fatalf("首文件读取失败覆盖已有比较: %s", app.problem)
	}
}
