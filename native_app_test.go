// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/internal/workspace"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func nativeGit(t *testing.T, path string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", path}, args...)...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func nativeFixture(t *testing.T) (*nativeApp, string, func()) {
	t.Helper()
	path := t.TempDir()
	nativeGit(t, path, "init", "-b", "main")
	nativeGit(t, path, "config", "user.name", "Native Test")
	nativeGit(t, path, "config", "user.email", "native@example.test")
	nativeGit(t, path, "config", "commit.gpgsign", "false")
	writeNativeFile(t, path, "hello.txt", "原始内容\n")
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "初始提交")
	manager, err := workspace.New(filepath.Join(t.TempDir(), "recent.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := newNativeApp(&RepositoryService{manager: manager}, filepath.Join(t.TempDir(), "native-state.json"))
	updates := make(chan func(), 1)
	app.post = func(update func()) { updates <- update }
	finish := func() {
		t.Helper()
		select {
		case update := <-updates:
			update()
		case <-time.After(15 * time.Second):
			t.Fatal("后台任务未返回")
		}
	}
	t.Cleanup(app.cancel)
	app.openRepository(path)
	finish()
	if app.problem != "" {
		t.Fatal(app.problem)
	}
	return app, path, finish
}

func writeNativeFile(t *testing.T, path, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(path, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeStageCommitAndDraftRecovery(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "修改后\n")
	app.refresh()
	finish()
	app.stagePaths(false, []string{"hello.txt"})
	finish()
	app.prefs.Drafts[app.repository.snapshot.Path] = "原生界面提交"
	if err := app.savePreferences(); err != nil {
		t.Fatal(err)
	}
	restored := newNativeApp(app.service, app.stateFile)
	defer restored.cancel()
	if restored.prefs.Drafts[app.repository.snapshot.Path] != "原生界面提交" {
		t.Fatal("未恢复提交草稿")
	}
	app.commit()
	finish()
	if app.problem != "" || len(app.repository.snapshot.Files) != 0 {
		t.Fatalf("提交后状态异常: %s", app.problem)
	}
	if got := nativeGit(t, path, "log", "-1", "--format=%s"); got != "原生界面提交" {
		t.Fatal(got)
	}
	if len(app.prefs.Drafts) != 0 {
		t.Fatal("提交成功后仍保留草稿")
	}
}

func TestNativeFailedHookRefreshesAndKeepsDraft(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "修改\n")
	app.stagePaths(false, []string{"hello.txt"})
	finish()
	hook := filepath.Join(path, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf 'hook 修改\\n' > hook.txt\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	app.prefs.Drafts[app.repository.snapshot.Path] = "保留草稿"
	app.commit()
	finish()
	if app.problem == "" || app.prefs.Drafts[app.repository.snapshot.Path] != "保留草稿" {
		t.Fatal("失败提交必须显示错误并保留草稿")
	}
	if !slices.ContainsFunc(app.repository.snapshot.Files, func(file gitrepo.File) bool { return file.Path == "hook.txt" }) {
		t.Fatal("hook 失败后没有刷新文件状态")
	}
}

func TestNativeFailedRefreshDisablesWrites(t *testing.T) {
	app, path, finish := nativeFixture(t)
	if err := os.Rename(filepath.Join(path, ".git"), filepath.Join(path, ".git-hidden")); err != nil {
		t.Fatal(err)
	}
	app.refresh()
	finish()
	if app.writable || app.problem == "" {
		t.Fatal("状态读取失败后必须禁止写入")
	}
	app.stagePaths(false, []string{"hello.txt"})
	if app.busy {
		t.Fatal("过期状态发起了写操作")
	}
}

func TestNativePatchLineIDsAndPartialStaging(t *testing.T) {
	app, path, finish := nativeFixture(t)
	lines := make([]string, 20)
	for index := range lines {
		lines[index] = "保持行"
	}
	before := strings.Join(lines, "\n") + "\n"
	writeNativeFile(t, path, "hello.txt", before)
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "准备区块")
	lines[1], lines[18] = "第一处变更", "第二处变更"
	after := strings.Join(lines, "\n") + "\n"
	writeNativeFile(t, path, "hello.txt", after)
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	if !app.patch.Supported || len(app.patch.Hunks) != 2 || len(app.lineChoices) != 4 {
		t.Fatalf("区块解析异常: %#v, %#v", app.patch, app.lineChoices)
	}
	for index := range app.lineChoices {
		app.lineChoices[index].Checked = app.lineChoices[index].Hunk == 1
	}
	app.applyLines()
	finish()
	indexText := nativeGit(t, path, "show", ":hello.txt")
	if app.problem != "" || strings.Contains(indexText, "第一处变更") || !strings.Contains(indexText, "第二处变更") {
		t.Fatalf("逐行暂存异常: %s\n%s", app.problem, indexText)
	}
	content, err := os.ReadFile(filepath.Join(path, "hello.txt"))
	if err != nil || string(content) != after {
		t.Fatal("逐行暂存修改了工作区")
	}
}

func TestNativeConflictResolutionAndFailureKeepsEditor(t *testing.T) {
	app, path, finish := nativeFixture(t)
	nativeGit(t, path, "checkout", "-b", "incoming")
	writeNativeFile(t, path, "hello.txt", "传入\n")
	nativeGit(t, path, "commit", "-am", "传入变更")
	nativeGit(t, path, "checkout", "main")
	writeNativeFile(t, path, "hello.txt", "当前\n")
	nativeGit(t, path, "commit", "-am", "当前变更")
	command := exec.Command("git", "-C", path, "-c", "commit.gpgsign=false", "merge", "incoming")
	if err := command.Run(); err == nil {
		t.Fatal("预期合并冲突")
	}
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	if app.conflict == nil || !app.conflict.Editable {
		t.Fatal("未打开冲突编辑器")
	}
	app.conflictEdit.Buffer = editor.NewBuffer("合并结果\n")
	token := app.conflict.Token
	app.mutate("创建标签", func(ctx context.Context, path string) error {
		return app.service.CreateTag(ctx, path, "conflict-draft", app.repository.snapshot.Head)
	})
	finish()
	if app.conflict == nil || app.conflict.Token != token || app.conflictEdit.Buffer.String() != "合并结果\n" {
		t.Fatal("元数据操作丢失了冲突草稿")
	}
	app.prefs.Drafts[app.repository.snapshot.Path] = "不应提交未解决冲突"
	app.commit()
	if app.busy || app.problem == "" || app.conflictEdit.Buffer.String() != "合并结果\n" {
		t.Fatal("快捷键提交应保护未解决冲突")
	}
	writeNativeFile(t, path, "hello.txt", "外部编辑\n")
	app.resolveConflict("result")
	finish()
	if app.problem == "" || app.conflictEdit.Buffer.String() != "合并结果\n" {
		t.Fatal("过期冲突保存应保留编辑草稿")
	}
	app.conflictEdit.Buffer = editor.NewBuffer(app.conflict.Result)
	app.refresh()
	finish()
	app.selectFile("hello.txt", false)
	finish()
	app.conflictEdit.Buffer = editor.NewBuffer("最终结果\n")
	app.resolveConflict("result")
	finish()
	if app.problem != "" || app.conflict != nil {
		t.Fatalf("冲突解决后未刷新: %s", app.problem)
	}
	if got := nativeGit(t, path, "show", ":hello.txt"); got != "最终结果" {
		t.Fatal(got)
	}
}

func TestNativeBusyPreventsOverlappingTasks(t *testing.T) {
	app, _, finish := nativeFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	app.run("阻塞操作", func(_ context.Context) (func(), error) {
		close(started)
		<-release
		return nil, errors.New("预期错误")
	})
	<-started
	app.run("不应运行", func(_ context.Context) (func(), error) {
		t.Error("并发任务被执行")
		return nil, nil
	})
	close(release)
	finish()
	if app.busy || app.problem != "预期错误" {
		t.Fatal("任务结束没有释放忙状态")
	}
}

func TestNativeUIChineseDraftAndDiffLayouts(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "新的中文内容\n")
	app.refresh()
	finish()
	tester := ui.NewTester(app.view, 1320, 860)
	if err := tester.Click("提交说明"); err != nil {
		t.Fatal(err)
	}
	tester.Type("修复中文输入")
	if app.prefs.Drafts[app.repository.snapshot.Path] != "修复中文输入" {
		t.Fatal("中文提交说明没有写回状态")
	}
	app.selectFile("hello.txt", false)
	finish()
	tester.Frame()
	if err := tester.Click("并排"); err != nil {
		t.Fatal(err)
	}
	if !app.prefs.Split || app.diffViews[0].Layout != editor.DiffSplit {
		t.Fatal("未切换为并排 Diff")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-diff-light.png")
	}
	tester.SetDark(true)
	tester.Frame()
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-diff.png")
	}
	if err := tester.Click("统一"); err != nil {
		t.Fatal(err)
	}
	if app.prefs.Split || app.diffViews[0].Layout != editor.DiffUnified {
		t.Fatal("未切换为统一 Diff")
	}
	tester.SetSize(1024, 680)
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-diff-compact.png")
	}
	tester.SetSize(1320, 860)
	app.showLibrary()
	tester.Frame()
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-library.png")
	}
}

func TestNativeRepositoryActionsMenu(t *testing.T) {
	app, path, finish := nativeFixture(t)
	tester := ui.NewTester(app.view, 1320, 860)
	if err := tester.Click("更多"); err != nil {
		t.Fatal(err)
	}
	want := []string{"新建标签…", "-", "在 Finder 中显示", "打开恢复副本文件夹"}
	if got := tester.Menu(); !slices.Equal(got, want) || app.dialog != nil {
		t.Fatalf("应直接展开仓库菜单，菜单=%v，弹窗=%v", got, app.dialog)
	}
	tester.CloseMenu()
	if !tester.Focused("更多") || app.dialog != nil {
		t.Fatal("关闭菜单后应保留按钮焦点且不打开弹窗")
	}
	for _, key := range []ui.Key{ui.KeyEnter, ui.KeySpace, ui.KeyDown} {
		tester.Key(0, key)
		if got := tester.Menu(); !slices.Equal(got, want) {
			t.Fatalf("按键 %v 未展开菜单: %v", key, got)
		}
		tester.CloseMenu()
	}
	tester.Key(0, ui.KeyEnter)
	if err := tester.ChooseMenuItem("新建标签…"); err != nil {
		t.Fatal(err)
	}
	if app.dialog == nil || app.dialog.title != "新建本地标签" {
		t.Fatal("菜单未直接打开标签表单")
	}
	if err := tester.Click("标签名称"); err != nil {
		t.Fatal(err)
	}
	tester.Type("menu-tag")
	if err := tester.Click("创建"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.problem != "" || app.dialog != nil {
		t.Fatalf("创建标签失败: %s", app.problem)
	}
	if got := nativeGit(t, path, "rev-parse", "menu-tag"); got != nativeGit(t, path, "rev-parse", "HEAD") {
		t.Fatal("菜单创建的标签未指向当前提交")
	}
}

func TestNativeRepositoryActionsUnavailable(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.writable = false
	tester := ui.NewTester(app.view, 1320, 860)
	if err := tester.Click("更多"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("新建标签…"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("仓库不可写时应禁用新建标签: %v", err)
	}
	if app.dialog != nil {
		t.Fatal("禁用菜单项不应打开表单")
	}
	tester.CloseMenu()
	app.busy = true
	tester.Frame()
	if err := tester.Click("更多"); err != nil {
		t.Fatal(err)
	}
	if tester.Menu() != nil || app.dialog != nil {
		t.Fatal("后台操作期间不应打开仓库菜单")
	}
}

func TestNativeOfficialSidebarAndFileMenu(t *testing.T) {
	app, _, finish := nativeFixture(t)
	app.library.Groups = []workspace.Group{{ID: "daily", Name: "日常开发"}}
	tester := ui.NewTester(app.view, 1320, 860)
	if err := tester.Click("储藏"); err != nil {
		t.Fatal(err)
	}
	if app.screen != "stash" {
		t.Fatal("官方侧栏没有切换页面")
	}
	if err := tester.Click("提交历史"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.screen != "history" || len(app.history) == 0 {
		t.Fatal("侧栏切换没有加载提交历史")
	}
	if err := tester.Click("文件变更"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("文件选项"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("按文件名排序"); err != nil {
		t.Fatal(err)
	}
	if !app.prefs.ByName {
		t.Fatal("文件选项没有保存排序偏好")
	}
	if err := tester.Click("文件选项"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("按路径排序"); err != nil {
		t.Fatal(err)
	}
	if app.prefs.ByName {
		t.Fatal("文件选项没有切换回路径排序")
	}
	if err := tester.Click("返回仓库"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("日常开发"); err != nil {
		t.Fatal(err)
	}
	if app.screen != "library" || app.group != "daily" {
		t.Fatal("侧栏分组没有筛选仓库")
	}
	tester.Key(0, ui.KeyHome)
	if app.group != "" {
		t.Fatal("侧栏键盘导航没有返回全部仓库")
	}
}

func TestNativeSidebarScopesLibraryNavigation(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.library.Groups = []workspace.Group{{ID: "daily", Name: "日常开发"}}
	tester := ui.NewTester(app.view, 1320, 860)
	for _, screen := range []string{"changes", "history", "stash"} {
		app.screen = screen
		tester.Frame()
		for _, label := range []string{"本地仓库", "仓库组", "日常开发", "管理仓库组 日常开发", "新建仓库组"} {
			if _, found := tester.Find(label); found {
				t.Errorf("%s 页面不应显示全局导航 %q", screen, label)
			}
		}
		for _, label := range []string{"返回仓库", "文件变更", "提交历史", "储藏", "本地分支", "main"} {
			if _, found := tester.Find(label); !found {
				t.Errorf("%s 页面缺少仓库导航 %q", screen, label)
			}
		}
	}
	if err := tester.Click("返回仓库"); err != nil {
		t.Fatal(err)
	}
	if app.screen != "library" {
		t.Fatal("返回入口未切换到仓库首页")
	}
	tester.Frame()
	for _, label := range []string{"本地仓库", "仓库组", "日常开发", "新建仓库组"} {
		if _, found := tester.Find(label); !found {
			t.Errorf("仓库首页缺少全局导航 %q", label)
		}
	}
	if _, found := tester.Find("返回仓库"); found {
		t.Fatal("仓库首页不应显示返回入口")
	}
	if err := tester.Click("日常开发"); err != nil {
		t.Fatal(err)
	}
	if app.group != "daily" {
		t.Fatal("返回后未能按仓库组筛选")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		app.screen = "changes"
		tester.Frame()
		saveNativeScreenshot(t, tester, directory, "native-repository-sidebar.png")
		app.showLibrary()
		tester.Frame()
		saveNativeScreenshot(t, tester, directory, "native-library-sidebar.png")
	}
}

func TestNativeFileSelectionStagesOnlyCheckedFiles(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "已选择的修改\n")
	writeNativeFile(t, path, "other.txt", "未选择的文件\n")
	app.refresh()
	finish()
	tester := ui.NewTester(app.view, 1320, 860)
	for _, label := range []string{"选择 未暂存文件 hello.txt", "暂存所选文件"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	finish()
	tester.Frame()
	if got := nativeGit(t, path, "diff", "--cached", "--name-only"); got != "hello.txt" {
		t.Fatalf("批量暂存包含未选择的文件: %s", got)
	}
	for _, label := range []string{"选择 已暂存文件 hello.txt", "取消暂存所选文件"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	finish()
	if got := nativeGit(t, path, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("取消暂存后索引仍有变更: %s", got)
	}
	content, err := os.ReadFile(filepath.Join(path, "hello.txt"))
	if err != nil || string(content) != "已选择的修改\n" {
		t.Fatal("取消暂存改变了工作区内容")
	}
}

func saveNativeScreenshot(t *testing.T, tester *ui.Tester, directory, name string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, tester.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeGraphKeepsMergeTracks(t *testing.T) {
	graph := commitGraph([]gitrepo.Commit{
		{Hash: "merge", Parents: []string{"main", "topic"}},
		{Hash: "topic", Parents: []string{"base"}},
		{Hash: "main", Parents: []string{"base"}},
		{Hash: "base"},
	})
	if len(graph[0].Edges) != 2 || graph[1].Column != 1 || graph[2].Column != 0 || graph[3].Column != 0 {
		t.Fatalf("合并与分叉轨道错误: %+v", graph)
	}
	if graph[1].Color != graph[2].Incoming[1] || graph[0].Color != graph[3].Color {
		t.Fatalf("轨道压缩后颜色不稳定: %+v", graph)
	}
}

func TestNativePatchNavigationAndMixedMetadata(t *testing.T) {
	patch := editor.FilePatch{Hunks: []editor.PatchHunk{
		{Lines: []string{" context", "-old", "+new", " gap", "-old2", "+new2"}},
		{Lines: []string{"-other", "+changed"}},
	}}
	if offsets := patchHunkOffsets(patch); !slices.Equal(offsets, []int{0, 2}) {
		t.Fatal(offsets)
	}
	app := &nativeApp{}
	text := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n" +
		"diff --git a/icon.png b/icon.png\nBinary files a/icon.png and b/icon.png differ\n"
	app.setDiff(gitrepo.Diff{Text: text})
	if len(app.diffViews) != 0 || app.rawDiff.Buffer.String() != text {
		t.Fatal("混合提交隐藏了二进制变更")
	}
}
