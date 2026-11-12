// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/internal/workspace"

	"github.com/egoist/mygo/ui"
)

func TestNativeFilteredBulkStaging(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "visible.txt", "visible\n")
	writeNativeFile(t, path, "hidden.txt", "hidden\n")
	app.refresh()
	finish()
	app.fileQuery = "VISIBLE"
	app.stageFiles(false, true)
	finish()
	if got := nativeGit(t, path, "diff", "--cached", "--name-only"); got != "visible.txt" {
		t.Fatalf("筛选后暂存了不可见文件: %s", got)
	}
	nativeGit(t, path, "add", "hidden.txt")
	app.refresh()
	finish()
	app.stageFiles(true, true)
	finish()
	if got := nativeGit(t, path, "diff", "--cached", "--name-only"); got != "hidden.txt" {
		t.Fatalf("筛选后取消了不可见文件的暂存: %s", got)
	}
	app.selectedFiles["false:hidden.txt"] = true
	if len(app.checkedFiles(false)) != 0 {
		t.Fatal("筛选后仍操作隐藏的勾选文件")
	}
}

func TestNativeSelectionDialogsRender(t *testing.T) {
	app, path, _ := nativeFixture(t)
	app.scanResult = &workspace.ScanResult{Repositories: []workspace.LocalRepository{{Path: path}}}
	app.scanSelected = map[string]bool{}
	app.scanDialog()
	tester := ui.NewTester(app.view, 1320, 860)
	if err := tester.Click(path); err != nil {
		t.Fatal(err)
	}
	if !app.scanSelected[path] {
		t.Fatal("扫描结果无法勾选")
	}
	if err := tester.Click("清空选择"); err != nil {
		t.Fatal(err)
	}
	if info, ok := tester.AccessByRole(ui.RoleButton, "添加所选仓库"); !ok || !info.Disabled {
		t.Fatal("未选择仓库仍可添加")
	}
	app.lineChoices = []nativeLine{{ID: 1, Text: "+第一行"}, {ID: 2, Text: "-第二行"}}
	app.linesDialog()
	tester.Frame()
	if err := tester.Click("+第一行"); err != nil {
		t.Fatal(err)
	}
	if !app.lineChoices[0].Checked || app.lineChoices[1].Checked {
		t.Fatal("逐行选择状态串行")
	}
}

func TestNativeScanRepositorySelection(t *testing.T) {
	app, _, finish := nativeFixture(t)
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativeGit(t, path, "init", "-b", "main")
	library, err := app.service.CreateGroup("日常开发")
	if err != nil {
		t.Fatal(err)
	}
	app.library, app.group = library, library.Groups[0].ID
	otherPath := "/Users/developer/Projects/archive/XSourceTree"
	app.scanResult = &workspace.ScanResult{Repositories: []workspace.LocalRepository{
		{Name: "XSourceTree", Path: path},
		{Name: "XSourceTree", Path: otherPath},
	}}
	app.scanSelected = map[string]bool{}
	app.scanDialog()
	tester := ui.NewTester(app.view, 1024, 680)
	if _, found := tester.Find("已选 0 / 2 个仓库"); !found {
		t.Fatal("扫描结果缺少已选数量")
	}
	row, found := tester.Find(path)
	if !found || row.H < 48 {
		t.Fatalf("扫描结果缺少可点击的完整仓库行: %+v", row)
	}
	tester.Press(row.X+row.W-20, row.Y+row.H/2)
	tester.Release(row.X+row.W-20, row.Y+row.H/2)
	if !app.scanSelected[path] || app.scanSelected[otherPath] {
		t.Fatal("点击整行没有只选中对应路径")
	}
	tester.Key(0, ui.KeySpace)
	if app.scanSelected[path] {
		t.Fatal("仓库行不支持空格取消选择")
	}
	if err := tester.Click("全选"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if _, found := tester.Find("已选 2 / 2 个仓库"); !found {
		t.Fatal("全选后数量未更新")
	}
	if err := tester.Click(otherPath); err != nil {
		t.Fatal(err)
	}
	if !app.scanSelected[path] || app.scanSelected[otherPath] {
		t.Fatal("同名仓库的选择状态相互影响")
	}
	if err := tester.Click("添加所选仓库"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.problem != "" {
		t.Fatal(app.problem)
	}
	if !slices.ContainsFunc(app.library.Repositories, func(entry workspace.LocalRepository) bool {
		return entry.Path == path && entry.GroupID == library.Groups[0].ID
	}) {
		t.Fatalf("所选仓库未添加到目标分组: %+v", app.library.Repositories)
	}
}

func TestNativeScanDialogLayout(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.showLibrary()
	app.scanResult = &workspace.ScanResult{}
	for _, name := range []string{"DeepAudit", "codereview", "drone-runner-kube", "gaea-v5", "gaea-v5-old", "gitness", "harness", "syncimage", "zeus", "zeus/sig/errors", "zeus/sig/exgin", "zeus/sig/glog", "zeus/sig/jwt", "zeus/sig/ldap", "zeus/sig/util", "zeus/sig/workwx", "zeus/sig/zlog"} {
		app.scanResult.Repositories = append(app.scanResult.Repositories, workspace.LocalRepository{
			Name: filepath.Base(name), Path: filepath.Join("/Users/developer/Projects/yunop-com", name),
		})
	}
	app.scanSelected = map[string]bool{
		app.scanResult.Repositories[3].Path: true,
		app.scanResult.Repositories[5].Path: true,
	}
	app.scanDialog()
	tester := ui.NewTester(app.view, 1024, 680)
	for _, label := range []string{"仓库组", "已选 2 / 17 个仓库", "全选", "清空选择", "取消", "添加所选仓库"} {
		bounds, found := tester.Find(label)
		if !found || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > 1024 || bounds.Y+bounds.H > 680 {
			t.Fatalf("扫描弹窗操作不可见: %s %+v", label, bounds)
		}
	}
	for _, width := range []int{1024, 480} {
		layout := ui.NewTester(app.view, width, 680)
		title, titleFound := layout.Find("选择要添加的仓库")
		action, actionFound := layout.Find("添加所选仓库")
		viewport := float32(width)
		if !titleFound || !actionFound || title.X < 0 || action.X+action.W > viewport ||
			title.X-(viewport-title.X-title.W) > 2 || (viewport-title.X-title.W)-title.X > 2 {
			t.Fatalf("扫描弹窗布局未居中: width=%v title=%+v action=%+v", width, title, action)
		}
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-scan-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "native-scan-dark.png")
	}
	app.scanResult = &workspace.ScanResult{}
	app.scanDialog()
	tester.Frame()
	if _, found := tester.Find("未找到可添加的仓库"); !found {
		t.Fatal("空扫描结果缺少提示")
	}
	if info, found := tester.AccessByRole(ui.RoleButton, "添加所选仓库"); !found || !info.Disabled {
		t.Fatal("空扫描结果仍可添加")
	}
}

func TestNativeHistoryAccessibilityDuringReplacement(t *testing.T) {
	app, path, finish := nativeHistoryFixture(t)
	tester := ui.NewTester(app.view, 1320, 860)
	app.selectCommit(app.history[0].Hash)
	finish()
	tester.Frame()
	app.historyQuery = "no-such-commit"
	app.searchHistory(false)
	finish()
	tester.AccessByRole(ui.RoleRow, "")
	tester.Frame()
	tester.AccessByRole(ui.RoleRow, "")
	app.historyQuery = ""
	app.searchHistory(false)
	finish()
	tester.Frame()
	if _, ok := tester.AccessByRole(ui.RoleRow, app.history[0].Subject); !ok {
		t.Fatal("恢复搜索后无障碍提交行丢失")
	}
	app.selectCommit(app.history[0].Hash)
	finish()
	tester.Frame()
	nativeGit(t, path, "commit", "--allow-empty", "-m", "empty")
	app.selectCommit(nativeGit(t, path, "rev-parse", "HEAD"))
	finish()
	tester.AccessByRole(ui.RoleListItem, "")
	tester.Frame()
	if _, ok := tester.AccessByRole(ui.RoleText, "此提交没有文件变更"); !ok {
		t.Fatal("切到空提交后辅助功能节点未更新")
	}
}

func TestNativeFormKeepsFailedInput(t *testing.T) {
	app, path, finish := nativeFixture(t)
	app.branchDialog()
	dialog := app.dialog
	tester := ui.NewTester(app.view, 1320, 860)
	if info, ok := tester.AccessByRole(ui.RoleButton, "创建并切换"); !ok || !info.Disabled {
		t.Fatal("空分支名称仍可提交")
	}
	dialog.fields[0].value = "taken-after-check"
	dialog.onChange()
	finish()
	nativeGit(t, path, "branch", "taken-after-check")
	tester.Frame()
	if err := tester.Click("创建并切换"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.dialog != dialog || dialog.fields[0].value != "taken-after-check" {
		t.Fatal("失败后丢失表单输入")
	}
	tester.Frame()
	if _, ok := tester.Find(app.problem); !ok || app.problem == "" {
		t.Fatal("表单未显示失败原因")
	}
	dialog.fields[0].value = "valid-branch"
	dialog.onChange()
	finish()
	tester.Frame()
	if err := tester.Click("创建并切换"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.dialog != nil || app.repository.snapshot.Branch != "valid-branch" {
		t.Fatal("修正后无法重试创建分支")
	}
}

func TestNativeRemoteAndConfirmationCopy(t *testing.T) {
	app, path, finish := nativeFixture(t)
	for _, action := range []string{"fetch", "pull", "push"} {
		app.remoteAction(action)
		if app.busy || app.dialog == nil || len(app.dialog.fields) != 2 {
			t.Fatalf("%s 未引导配置远程", action)
		}
	}
	app.dialog = nil
	nativeGit(t, path, "remote", "add", "origin", path)
	app.refresh()
	finish()
	app.remoteAction("pull")
	if app.busy || app.dialog == nil || app.dialog.title != "当前分支未设置上游" {
		t.Fatal("缺少上游时未解释下一步")
	}
	app.editRemoteDialog(&app.repository.remotes[0])
	tester := ui.NewTester(app.view, 1320, 860)
	if field, ok := tester.AccessByRole(ui.RoleTextField, "名称"); !ok || !field.ReadOnly {
		t.Fatal("已有远程名称未设为只读")
	}
	app.stashHash = "test-stash"
	app.applyStash(false)
	if app.dialog.danger || app.dialog.action != "应用" {
		t.Fatal("普通应用储藏被标记为危险操作")
	}
	app.applyStash(true)
	if !app.dialog.danger || app.dialog.action != "应用并删除" || !strings.Contains(app.dialog.detail, "成功") {
		t.Fatal("应用并删除未说明删除时机")
	}
}

func TestNativeLibraryFormRetryAndReturn(t *testing.T) {
	app, _, finish := nativeFixture(t)
	app.groupDialog("")
	tester := ui.NewTester(app.view, 1320, 860)
	tester.Type("日常")
	tester.Key(0, ui.KeyEnter)
	finish()
	if app.dialog != nil || len(app.library.Groups) != 1 || app.library.Groups[0].Name != "日常" {
		t.Fatal("回车未保存新建分组")
	}
	app.groupDialog("")
	dialog := app.dialog
	dialog.fields[0].value = "日常"
	tester.Frame()
	if err := tester.Click("保存"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.dialog != dialog || dialog.problem == "" || len(app.library.Groups) != 1 {
		t.Fatal("重复分组未在表单内显示错误并保留输入")
	}
	dialog.fields[0].value = "新分组"
	tester.Frame()
	if err := tester.Click("保存"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.dialog != nil || len(app.library.Groups) != 2 {
		t.Fatal("分组保存失败后无法重试")
	}
}

func TestNativeSuccessfulFormDoesNotRetryAfterRefreshFailure(t *testing.T) {
	app, path, finish := nativeFixture(t)
	app.tagDialog()
	dialog := app.dialog
	app.mutateForm(dialog, "正在创建标签…", func(ctx context.Context, root string) error {
		if err := app.service.CreateTag(ctx, root, "created", app.repository.snapshot.Head); err != nil {
			return err
		}
		return os.Rename(filepath.Join(path, ".git"), filepath.Join(path, ".git-hidden"))
	})
	finish()
	if app.dialog != nil || app.writable || app.problem == "" {
		t.Fatal("操作成功但刷新失败时仍显示可重复提交的表单")
	}
}

func TestNativeFailedFormBlocksWritesAfterRefreshFailure(t *testing.T) {
	app, path, finish := nativeFixture(t)
	app.tagDialog()
	dialog := app.dialog
	dialog.fields[0].value = "retry-tag"
	app.mutateForm(dialog, "正在创建标签…", func(context.Context, string) error {
		if err := os.Rename(filepath.Join(path, ".git"), filepath.Join(path, ".git-hidden")); err != nil {
			return err
		}
		return errors.New("标签创建失败")
	})
	finish()
	tester := ui.NewTester(app.view, 1320, 860)
	if app.dialog != dialog || dialog.problem == "" {
		t.Fatal("失败表单未保留输入与错误")
	}
	if button, ok := tester.AccessByRole(ui.RoleButton, "创建"); !ok || !button.Disabled {
		t.Fatal("仓库状态无法刷新时仍允许再次写入")
	}
}

func TestNativeAccessibleGroupManagementAndEmptyStates(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.screen = "library"
	app.library.Groups = []workspace.Group{{ID: "daily", Name: "日常"}}
	app.group = "daily"
	tester := ui.NewTester(app.view, 1320, 860)
	if _, ok := tester.AccessByRole(ui.RoleButton, "管理仓库组 日常"); !ok {
		t.Fatal("辅助功能无法发现分组管理按钮")
	}
	if _, ok := tester.Find("此分组暂无仓库"); !ok {
		t.Fatal("空分组误报为搜索失败")
	}
	app.query = "无匹配结果"
	tester.Frame()
	if err := tester.Click("清除搜索"); err != nil {
		t.Fatal(err)
	}
	if app.query != "" {
		t.Fatal("搜索无结果时无法清除条件")
	}
	app.screen = "stash"
	tester.Frame()
	if button, ok := tester.AccessByRole(ui.RoleButton, "新建储藏"); !ok || !button.Disabled {
		t.Fatal("干净工作区仍可发起储藏")
	}
	if _, ok := tester.Find("暂无储藏"); !ok {
		t.Fatal("缺少储藏空状态")
	}
	app.repository.snapshot.Operation.Kind = "merge"
	app.repository.snapshot.Files = []gitrepo.File{{Path: "conflict.txt", Conflict: true}}
	tester.Frame()
	if button, ok := tester.AccessByRole(ui.RoleButton, "继续"); !ok || !button.Disabled {
		t.Fatal("仍有未解决冲突时允许继续")
	}
}
