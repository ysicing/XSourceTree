// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ysicing/51code/internal/gitrepo"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestNativeDiffKeepsWholeFileContext(t *testing.T) {
	app, path, finish := nativeFixture(t)
	before := strings.Repeat("unchanged context\n", 30) + "old value\n" + strings.Repeat("trailing context\n", 30)
	after := strings.Replace(before, "old value", "new value", 1)
	writeNativeFile(t, path, "hello.txt", before)
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "full context")
	writeNativeFile(t, path, "hello.txt", after)
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	if len(app.diffViews) != 1 || app.diffViews[0].Old.String() != before || app.diffViews[0].New.String() != after {
		t.Fatal("差异组件没有收到完整的两个文件版本")
	}
	app.diffViews[0].ExpandAll()
	if app.diffViews[0].Old.LineCount() != 62 {
		t.Fatal("展开上下文缺少补丁之外的内容")
	}
}

func TestNativeFullFileUnavailableStates(t *testing.T) {
	for _, test := range []struct {
		name, message string
		content       gitrepo.FileContent
	}{
		{"missing", "此版本不存在该文件", gitrepo.FileContent{}},
		{"binary", "二进制文件", gitrepo.FileContent{Present: true, Binary: true}},
		{"large", "完整文件超过 1 MiB，请使用外部编辑器查看。", gitrepo.FileContent{Present: true, Truncated: true}},
		{"link", "符号链接、子模块及特殊文件不提供全文预览。", gitrepo.FileContent{Present: true, Unsupported: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, _, _ := nativeFixture(t)
			app.setDiff(gitrepo.Diff{
				Old: &gitrepo.FileContent{Path: "hello.txt", Revision: "INDEX", Present: true, Text: "previous\n"},
				New: &gitrepo.FileContent{Path: "hello.txt", Revision: "WORKTREE", Present: true, Text: "stale\n"},
			})
			test.content.Path, test.content.Revision = "hello.txt", "WORKTREE"
			app.setDiff(gitrepo.Diff{Old: app.diff.Old, New: &test.content})
			tester := ui.NewTester(app.view, 1024, 680)
			if err := tester.Click("全文"); err != nil {
				t.Fatal(err)
			}
			if _, found := tester.Find(test.message); !found {
				t.Fatal("缺少对应的不可预览状态", test.message)
			}
			if _, found := tester.AccessByRole(ui.RoleTextField, "文件全文"); found {
				t.Fatal("不可预览版本仍显示上一次的文件正文")
			}
		})
	}
}

func TestNativeFullFileFindAndVersionState(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "value one\nvalue two\n")
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	tester := ui.NewTester(app.view, 1320, 860)
	if err := tester.Click("全文"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("查找"); err != nil {
		t.Fatal(err)
	}
	tester.Type("value")
	if count, _ := app.fileViewer.states[1].FindMatches(); count != 2 {
		t.Fatalf("全文查找没有使用当前版本: %d", count)
	}
	buffer := app.fileViewer.states[1].Buffer
	for _, label := range []string{"差异", "全文"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if app.fileViewer.states[1].Buffer != buffer {
		t.Fatal("往返全文重建了编辑器状态")
	}
	if count, _ := app.fileViewer.states[1].FindMatches(); count != 2 {
		t.Fatal("往返差异丢失了文件内查找")
	}
}

func TestNativeDiffContextMismatchKeepsGitPatch(t *testing.T) {
	app, _, _ := nativeFixture(t)
	patch := "diff --git a/hello.txt b/hello.txt\n--- a/hello.txt\n+++ b/hello.txt\n@@ -1 +1 @@\n-old\n+new\n"
	app.setDiff(gitrepo.Diff{
		Text: patch,
		Old:  &gitrepo.FileContent{Path: "hello.txt", Revision: "INDEX", Present: true, Text: "outside edit\n"},
		New:  &gitrepo.FileContent{Path: "hello.txt", Revision: "WORKTREE", Present: true, Text: "new\n"},
	})
	tester := ui.NewTester(app.view, 1024, 680)
	if _, found := tester.Find("文件内容与差异不一致，已保留原始区块。请刷新后重试。"); !found {
		t.Fatal("未提示外部变更导致的上下文不匹配")
	}
	if app.diff.Text != patch || app.diffViews[0].Old.String() != "old" {
		t.Fatal("上下文不一致时覆盖了 Git 的原始补丁")
	}
}

func TestNativePatchSnapshotNeverStagesUnseenChanges(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "displayed change\n")
	app.refresh()
	finish()
	display, err := app.service.Diff(context.Background(), app.repository.snapshot.Path, "hello.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	writeNativeFile(t, path, "hello.txt", "external change\n")
	action, err := app.service.Patch(context.Background(), app.repository.snapshot.Path, "hello.txt", false)
	if err != nil || !action.Supported {
		t.Fatal("无法准备外部编辑后的区块", err)
	}
	app.setSelection(&nativeSelection{file: "hello.txt", diff: display, patch: action})
	tester := ui.NewTester(app.view, 1440, 900)
	if !app.fileViewer.contextChanged {
		t.Fatal("未识别展示内容和可操作区块来自不同版本")
	}
	for _, label := range []string{"暂存此区块", "选择行…"} {
		if field, found := tester.AccessByRole(ui.RoleButton, label); found && !field.Disabled {
			t.Fatal("不一致的区块操作仍可用", label)
		}
	}
	app.applyHunk()
	if app.busy {
		finish()
	}
	for index := range app.lineChoices {
		app.lineChoices[index].Checked = true
	}
	app.applyLines()
	if app.busy {
		finish()
	}
	if got := nativeGit(t, path, "diff", "--cached", "--name-only"); got != "" {
		t.Fatal("暂存了差异中未展示的外部内容", got)
	}
}

func TestNativeRenamedFullFileShowsSelectedVersionPath(t *testing.T) {
	app, path, finish := nativeFixture(t)
	if err := os.Rename(filepath.Join(path, "hello.txt"), filepath.Join(path, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	nativeGit(t, path, "add", "-A")
	app.refresh()
	finish()
	app.selectFile("renamed.txt", true)
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("全文"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("文件版本"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, found := tester.Find("hello.txt"); !found {
		t.Fatal("重命名前版本仍显示重命名后的路径")
	}
}

func TestNativeDeletedFullFileUsesPreviousVersion(t *testing.T) {
	app, path, finish := nativeFixture(t)
	if err := os.Remove(filepath.Join(path, "hello.txt")); err != nil {
		t.Fatal(err)
	}
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("全文"); err != nil {
		t.Fatal(err)
	}
	field, found := tester.AccessByRole(ui.RoleTextField, "文件全文")
	if !found || field.Value != "原始内容\n" {
		t.Fatal("删除文件未默认展示删除前的版本")
	}
}

func TestNativeDiffLayoutMigratesLegacyPreference(t *testing.T) {
	for _, test := range []struct {
		json, layout string
	}{
		{`{"split":true}`, "split"},
		{`{"split":false}`, "unified"},
		{`{"split":true,"diffLayout":"auto"}`, "auto"},
		{`{"diffLayout":"unknown"}`, "auto"},
	} {
		app, _, _ := nativeFixture(t)
		stateFile := filepath.Join(t.TempDir(), "preferences.json")
		if err := os.WriteFile(stateFile, []byte(test.json), 0600); err != nil {
			t.Fatal(err)
		}
		restored := newNativeApp(app.service, stateFile)
		t.Cleanup(restored.cancel)
		if restored.prefs.DiffLayout != test.layout {
			t.Fatalf("布局偏好迁移错误: %s -> %s", test.json, restored.prefs.DiffLayout)
		}
		if err := restored.savePreferences(); err != nil {
			t.Fatal(err)
		}
		reopened := newNativeApp(app.service, stateFile)
		t.Cleanup(reopened.cancel)
		if reopened.prefs.DiffLayout != test.layout {
			t.Fatal("重新打开时丢失布局偏好")
		}
	}
}

func TestNativeDiffReaderScreenshots(t *testing.T) {
	directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS")
	if directory == "" {
		t.Skip("未请求界面截图")
	}
	app, path, finish := nativeFixture(t)
	before := "package preview\n\nimport (\n\t\"io\"\n\t\"os\"\n)\n\n"
	before += strings.Repeat("// Unchanged context outside the patch.\n", 12)
	before += "func ReadPreview(path string) (string, error) {\n\tfile, err := os.Open(path)\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n\tdefer file.Close()\n\n\tconst limit = 256 << 10\n\tdata, err := io.ReadAll(io.LimitReader(file, limit))\n\tif err != nil {\n\t\treturn \"\", err\n\t}\n\treturn string(data), nil\n}\n"
	before += strings.Repeat("// Trailing context.\n", 15)
	after := strings.ReplaceAll(before, "limit", "previewLimit")
	writeNativeFile(t, path, "preview.go", before)
	nativeGit(t, path, "add", "preview.go")
	nativeGit(t, path, "commit", "-m", "preview fixture")
	writeNativeFile(t, path, "preview.go", after)
	app.refresh()
	finish()
	app.repository.snapshot.Name = "XSourceTree"
	app.selectFile("preview.go", false)
	finish()
	tester := ui.NewTester(app.view, 1440, 900)
	for _, width := range []int{1440, 1024} {
		tester.SetSize(width, 800)
		for _, dark := range []bool{false, true} {
			tester.SetDark(dark)
			appearance := "light"
			if dark {
				appearance = "dark"
			}
			saveNativeScreenshot(t, tester, directory, fmt.Sprintf("reader-diff-%s-%d.png", appearance, width))
			if err := tester.Click("全文"); err != nil {
				t.Fatal(err)
			}
			saveNativeScreenshot(t, tester, directory, fmt.Sprintf("reader-full-%s-%d.png", appearance, width))
			if err := tester.Click("差异"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestNativeFullFileVersionsAreReadOnly(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "index content\n")
	nativeGit(t, path, "add", "hello.txt")
	writeNativeFile(t, path, "hello.txt", "working content\n")
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	tester := ui.NewTester(app.view, 1320, 860)
	diffState := app.diffViews[0]
	if err := tester.Click("全文"); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		field, found := tester.AccessByRole(ui.RoleTextField, "文件全文")
		if !found || !field.ReadOnly || field.Value != want {
			t.Fatalf("全文版本错误或可编辑: %+v", field)
		}
	}
	check("working content\n")
	if err := tester.Click("文件全文"); err != nil {
		t.Fatal(err)
	}
	tester.Type("must not change")
	check("working content\n")
	if err := tester.Click("文件版本"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("暂存区"); err != nil {
		t.Fatal(err)
	}
	check("index content\n")
	if _, visible := tester.Find("暂存此区块"); visible {
		t.Fatal("全文模式仍展示区块暂存操作")
	}
	if err := tester.Click("差异"); err != nil {
		t.Fatal(err)
	}
	if app.diffViews[0] != diffState {
		t.Fatal("切换全文后重建了差异，丢失滚动和区块状态")
	}
	if content, err := os.ReadFile(filepath.Join(path, "hello.txt")); err != nil || string(content) != "working content\n" {
		t.Fatal("只读预览修改了工作区", err)
	}
}

func TestNativeDiffAutomaticLayoutAndManualOverride(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "modified\n")
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	tester := ui.NewTester(app.view, 1800, 900)
	if app.diffViews[0].Layout != editor.DiffSplit {
		t.Fatal("宽面板未自动使用并排")
	}
	tester.SetSize(1024, 680)
	if app.diffViews[0].Layout != editor.DiffUnified {
		t.Fatal("窄面板未自动使用统一")
	}
	if err := tester.Click("差异布局"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("并排"); err != nil {
		t.Fatal(err)
	}
	tester.SetSize(1800, 900)
	tester.SetSize(1024, 680)
	if app.diffViews[0].Layout != editor.DiffSplit {
		t.Fatal("自动布局覆盖了手动选择")
	}
	if err := app.savePreferences(); err != nil {
		t.Fatal(err)
	}
	restored := newNativeApp(app.service, app.stateFile)
	t.Cleanup(restored.cancel)
	restored.setDiff(app.diff)
	ui.NewTester(restored.diffPanel, 400, 400)
	if restored.diffViews[0].Layout != editor.DiffSplit {
		t.Fatal("手动布局没有持久化")
	}
}
