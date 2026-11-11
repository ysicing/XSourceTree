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

func TestNativeTopToolbar(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.repository.snapshot.Name = strings.Repeat("长仓库名称", 12)
	app.repository.snapshot.Branch = "feature/" + strings.Repeat("long-branch/", 12) + "toolbar"
	for _, size := range [][2]int{{1024, 680}, {1320, 860}} {
		for _, appearance := range []string{appearanceLight, appearanceDark} {
			app.prefs.Appearance = appearance
			for _, screen := range []string{"changes", "history", "stash", "library"} {
				app.screen = screen
				tester := ui.NewTester(app.view, size[0], size[1])
				labels := []string{"获取", "拉取", "推送", "分支", "合并", "仓库设置", "更多", "刷新", "设置"}
				if screen == "library" {
					labels = []string{"添加仓库", "输入路径", "扫描文件夹", "刷新", "设置"}
				}
				for _, label := range labels {
					role := ui.RoleButton
					if label == "更多" {
						role = ui.RoleMenuButton
					}
					info, found := tester.AccessByRole(role, label)
					if !found || info.Offscreen || info.Bounds.W <= 0 || info.Bounds.H <= 0 ||
						info.Bounds.Y < 0 || info.Bounds.Y+info.Bounds.H > 52 ||
						info.Bounds.X+info.Bounds.W > float32(size[0]) {
						t.Errorf("%v %s %s 顶部命令 %s 位置错误: %+v", size, appearance, screen, label, info)
					}
				}
				if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" && size[0] == 1024 &&
					(screen == "changes" || screen == "library") {
					saveNativeScreenshot(t, tester, directory, "native-top-toolbar-"+screen+"-"+appearance+".png")
				}
			}
		}
	}
}

func TestNativeTopToolbarProtectsActions(t *testing.T) {
	app, _, finish := nativeFixture(t)
	tester := ui.NewTester(app.view, 1024, 680)
	app.writable = false
	tester.Frame()
	for _, label := range []string{"获取", "拉取", "推送", "分支", "合并"} {
		info, found := tester.AccessByRole(ui.RoleButton, label)
		if !found || !info.Disabled {
			t.Errorf("不可写仓库应禁用 %s", label)
		}
	}
	for _, label := range []string{"仓库设置", "刷新", "设置"} {
		info, found := tester.AccessByRole(ui.RoleButton, label)
		if !found || info.Disabled {
			t.Errorf("不可写仓库仍应允许查看 %s", label)
		}
	}
	app.writable = true
	app.busy = true
	tester.Frame()
	for _, label := range []string{"获取", "拉取", "推送", "分支", "合并", "仓库设置", "更多", "刷新", "设置"} {
		role := ui.RoleButton
		if label == "更多" {
			role = ui.RoleMenuButton
		}
		info, found := tester.AccessByRole(role, label)
		if !found || !info.Disabled {
			t.Errorf("后台写操作期间应禁用 %s", label)
		}
	}
	app.busy = false
	tester.Frame()
	if err := tester.Click("仓库设置"); err != nil {
		t.Fatal(err)
	}
	if app.dialog == nil {
		t.Fatal("顶部仓库设置入口未打开设置")
	}
	finish()
	toolbar := ui.NewTester(app.toolbar, 800, 52)
	for _, label := range []string{"获取", "仓库设置", "刷新"} {
		info, found := toolbar.AccessByRole(ui.RoleButton, label)
		if !found || !info.Disabled {
			t.Errorf("弹窗打开期间应禁用 %s", label)
		}
	}
	tester.Frame()
	if err := tester.Click("关闭设置"); err != nil {
		t.Fatal(err)
	}
	app.showLibrary()
	tester.Frame()
	if err := tester.Click("输入路径"); err != nil {
		t.Fatal(err)
	}
	if app.dialog == nil || app.dialog.title != "添加本地仓库" {
		t.Fatal("仓库首页顶部入口未打开添加表单")
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

func TestNativeReadDoesNotBlockNavigation(t *testing.T) {
	for _, kind := range []string{"file", "history", "commit", "commit-file", "stash"} {
		t.Run(kind, func(t *testing.T) {
			app, path, finish := nativeFixture(t)
			writeNativeFile(t, path, "hello.txt", "局部刷新\n")
			nativeGit(t, path, "stash", "push", "-m", "测试储藏")
			writeNativeFile(t, path, "hello.txt", "工作区修改\n")
			app.refresh()
			finish()
			if kind == "commit-file" {
				app.screen = "history"
				app.selectCommit(app.repository.snapshot.Head)
				finish()
			}
			tester := ui.NewTester(app.view, 1024, 680)
			switch kind {
			case "file":
				app.selectFile("hello.txt", false)
			case "history":
				app.screen = "history"
				app.searchHistory(false)
			case "commit":
				app.screen = "history"
				app.selectCommit(app.repository.snapshot.Head)
			case "commit-file":
				app.selectCommitFile("hello.txt")
			case "stash":
				app.screen = "stash"
				app.selectStash(app.repository.stashes[0].Hash)
			}
			defer finish()
			if app.busy {
				t.Fatal("只读加载进入了全局忙碌状态")
			}
			tester.Frame()
			if err := tester.Click("返回仓库"); err != nil {
				t.Fatal(err)
			}
			if app.screen != "library" {
				t.Fatal("只读加载阻止了页面导航")
			}
		})
	}
}

func takeNativeUpdate(t *testing.T, updates <-chan func()) func() {
	t.Helper()
	select {
	case update := <-updates:
		return update
	case <-time.After(15 * time.Second):
		t.Fatal("后台读取未返回")
		return nil
	}
}

func TestNativeSelectionLatestResultWins(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "第一份差异\n")
	writeNativeFile(t, path, "second.txt", "第二份差异\n")
	app.refresh()
	finish()
	repository := app.repository
	app.selectedFiles["false:hello.txt"] = true
	app.prefs.Drafts[repository.snapshot.Path] = "保留提交草稿"
	updates := make(chan func(), 2)
	app.post = func(update func()) { updates <- update }
	app.selectFile("hello.txt", false)
	first := takeNativeUpdate(t, updates)
	app.selectFile("second.txt", false)
	second := takeNativeUpdate(t, updates)
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		tester := ui.NewTester(app.view, 1024, 680)
		saveNativeScreenshot(t, tester, directory, "native-local-loading-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "native-local-loading-dark.png")
	}
	second()
	first()
	if app.selectedFile != "second.txt" || !strings.Contains(app.diff.Text, "第二份差异") || app.problem != "" {
		t.Fatalf("较早的请求覆盖了最后一次选择: %s %s", app.selectedFile, app.problem)
	}
	if app.repository != repository || !app.selectedFiles["false:hello.txt"] || app.prefs.Drafts[repository.snapshot.Path] != "保留提交草稿" {
		t.Fatal("读取差异重置了仓库、勾选状态或提交草稿")
	}
	view := app.diffViews[0]
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("second.txt"); err != nil {
		t.Fatal(err)
	}
	if app.readTask != nil || app.diffViews[0] != view {
		t.Fatal("再次点击已选文件重建了差异编辑器")
	}
}

func TestNativeReadDropsResultsAfterLeaving(t *testing.T) {
	for _, action := range []string{"navigate", "refresh", "close"} {
		t.Run(action, func(t *testing.T) {
			app, path, finish := nativeFixture(t)
			writeNativeFile(t, path, "hello.txt", "过期差异\n")
			app.refresh()
			finish()
			updates := make(chan func(), 2)
			app.post = func(update func()) { updates <- update }
			app.selectFile("hello.txt", false)
			stale := takeNativeUpdate(t, updates)
			switch action {
			case "navigate":
				app.showLibrary()
			case "refresh":
				app.refresh()
				takeNativeUpdate(t, updates)()
			case "close":
				app.closed = true
				app.cancelRead()
				app.cancel()
			}
			stale()
			if app.selectedFile != "" || app.diff.Text != "" || app.problem != "" || app.readTask != nil {
				t.Fatal("离开页面或刷新后，过期读取仍回填了界面")
			}
		})
	}
}

func TestNativeMutationWaitsForCanceledRead(t *testing.T) {
	app, _, finish := nativeFixture(t)
	started, canceled, release, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	app.load("读取中", nativeReadDiff, func(ctx context.Context) (func(), error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release
		close(stopped)
		return func() { t.Error("已取消的读取更新了界面") }, ctx.Err()
	})
	<-started
	applied := false
	app.run("写操作", func(_ context.Context) (func(), error) {
		select {
		case <-stopped:
		default:
			t.Error("写操作没有等待已取消的读取退出")
		}
		return func() { applied = true }, nil
	})
	<-canceled
	close(release)
	finish()
	finish()
	if !applied || app.busy || app.readTask != nil || app.problem != "" {
		t.Fatal("取消读取后，写操作未正常完成")
	}
}

func TestNativeLoadingProtectsSelectedActions(t *testing.T) {
	for _, screen := range []string{"history", "stash"} {
		t.Run(screen, func(t *testing.T) {
			app, path, finish := nativeFixture(t)
			app.screen = screen
			if screen == "stash" {
				writeNativeFile(t, path, "hello.txt", "储藏内容\n")
				nativeGit(t, path, "stash", "push", "-m", "保护储藏")
				app.refresh()
				finish()
				app.selectStash(app.repository.stashes[0].Hash)
				finish()
				app.selectStash(app.stashHash)
			} else {
				app.selectCommit(app.repository.snapshot.Head)
				finish()
				app.selectCommit(app.commitHash)
			}
			defer finish()
			tester := ui.NewTester(app.view, 1024, 680)
			labels := []string{"新建标签"}
			if screen == "stash" {
				labels = []string{"应用", "应用并删除", "删除"}
			}
			for _, label := range labels {
				control, found := tester.AccessByRole(ui.RoleButton, label)
				if !found || !control.Disabled {
					t.Errorf("加载期间应禁用 %s", label)
				}
			}
			if err := tester.Click("更多"); err != nil {
				t.Fatal(err)
			}
			if err := tester.ChooseMenuItem("新建标签…"); err == nil || !strings.Contains(err.Error(), "disabled") {
				t.Errorf("加载期间标签菜单应禁用: %v", err)
			}
			tester.CloseMenu()
			app.tagDialog()
			if screen == "stash" {
				app.applyStash(false)
				app.applyStash(true)
				app.dropStash()
			}
			if app.dialog != nil || app.busy {
				t.Fatal("加载期间直接调用仍允许操作旧选择")
			}
		})
	}
}

func TestNativeCommitLoadingProtectsFileSelection(t *testing.T) {
	app, _, finish := nativeFixture(t)
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.selectCommit(app.repository.snapshot.Head)
	finish()
	app.selectCommit(app.commitHash)
	request := app.readTask
	tester := ui.NewTester(app.view, 1024, 680)
	files, found := tester.AccessByRole(ui.RoleList, "提交文件列表")
	if !found || !files.Disabled {
		t.Error("提交读取期间旧文件列表应禁用")
	}
	history, found := tester.AccessByRole(ui.RoleTable, "提交历史表格")
	if !found || history.Disabled {
		t.Error("提交读取期间历史表格应仍可选择")
	}
	app.selectCommitFile("hello.txt")
	if app.readTask != request {
		t.Fatal("旧文件选择取消了新提交读取")
	}
	finish()
	app.selectCommitFile("hello.txt")
	tester.Frame()
	if files, found := tester.AccessByRole(ui.RoleList, "提交文件列表"); !found || files.Disabled {
		t.Error("文件差异读取期间应允许继续选择其他文件")
	}
	finish()
	app.searchHistory(false)
	defer finish()
	tester.Frame()
	if files, found := tester.AccessByRole(ui.RoleList, "提交文件列表"); !found || !files.Disabled {
		t.Error("历史搜索期间旧文件列表应禁用")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-history-loading-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "native-history-loading-dark.png")
	}
}

func TestNativeUIChineseDraftAndDiffLayouts(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "新的中文内容\n")
	app.refresh()
	finish()
	tester := ui.NewTester(app.view, 1320, 860)
	input, found := tester.AccessByRole(ui.RoleTextField, "提交说明")
	if !found {
		t.Fatal("缺少提交说明输入框")
	}
	tester.ClickAt(input.Bounds.X+input.Bounds.W/2, input.Bounds.Y+input.Bounds.H/2)
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
	want := []string{"新建标签…", "储藏修改…", "-", "在 Finder 中显示", "打开恢复副本文件夹"}
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
	if err := tester.Click("文件排序"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("按文件名排序"); err != nil {
		t.Fatal(err)
	}
	if app.prefs.FileSort != "name" {
		t.Fatal("文件选项没有保存排序偏好")
	}
	if err := tester.Click("文件排序"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("按路径排序"); err != nil {
		t.Fatal(err)
	}
	if app.prefs.FileSort != "path" {
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
