// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNativeWorktreesDiscoverAndOpen(t *testing.T) {
	app, path, finish := nativeFixture(t)
	linked := filepath.Join(t.TempDir(), "中文工作区")
	nativeGit(t, path, "worktree", "add", "-b", "topic", linked)
	linked, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("Worktree"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if len(app.worktrees.items) != 2 {
		t.Fatal("没有发现同仓库的其他工作区")
	}
	if err := tester.Click(linked); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("打开工作区"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.repository.snapshot.Path != linked || app.repository.snapshot.Branch != "topic" || app.screen != "changes" {
		t.Fatal("没有作为独立工作目录打开链接工作区")
	}
}

func TestNativeWorktreeCreateAndFailurePreservesDraft(t *testing.T) {
	app, path, finish := nativeFixture(t)
	app.openWorktrees()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("新建 Worktree…"); err != nil {
		t.Fatal(err)
	}
	dialog := app.dialog
	linked := filepath.Join(t.TempDir(), "linked")
	dialog.fields[0].value, dialog.fields[1].value, dialog.fields[2].value = linked, "topic", "HEAD"
	tester.Frame()
	if err := tester.Click("创建"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.dialog != nil || len(app.worktrees.items) != 2 ||
		nativeGit(t, linked, "branch", "--show-current") != "topic" {
		t.Fatal("创建工作区失败或未刷新列表")
	}
	app.createWorktreeDialog()
	dialog = app.dialog
	dialog.fields[0].value, dialog.fields[1].value, dialog.fields[2].value = linked, "another", "HEAD"
	dialog.submit(dialog)
	finish()
	if app.dialog != dialog || dialog.problem == "" || dialog.fields[1].value != "another" {
		t.Fatal("创建失败未保留草稿和错误")
	}
	if nativeGit(t, path, "branch", "--list", "another") != "" {
		t.Fatal("失败创建留下无关分支")
	}
}

func TestNativeWorktreeMergeAndRemove(t *testing.T) {
	app, path, finish := nativeFixture(t)
	linked := filepath.Join(t.TempDir(), "linked")
	nativeGit(t, path, "worktree", "add", "-b", "topic", linked)
	writeNativeFile(t, linked, "feature.txt", "feature\n")
	nativeGit(t, linked, "add", ".")
	nativeGit(t, linked, "commit", "-m", "feature")
	app.openWorktrees()
	finish()
	canonical, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click(canonical); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("工作区操作"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("合并到当前工作区…"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	dialog := app.dialog
	if dialog == nil || !strings.Contains(dialog.detail, "topic") {
		t.Fatal("合并确认缺少源分支信息")
	}
	if err := tester.Click("合并"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.problem != "" || app.dialog != nil || nativeGit(t, path, "rev-parse", "HEAD") != nativeGit(t, linked, "rev-parse", "HEAD") {
		t.Fatal("合并未更新当前工作区", app.problem)
	}
	if err := tester.Click("工作区操作"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("移除工作区…"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	dialog = app.dialog
	if dialog == nil || !dialog.danger || !strings.Contains(dialog.detail, canonical) {
		t.Fatal("移除未明确确认目录")
	}
	if err := tester.Click("移除"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.problem != "" || len(app.worktrees.items) != 1 {
		t.Fatal("移除后未刷新工作区", app.problem)
	}
	if _, err := os.Stat(linked); !os.IsNotExist(err) {
		t.Fatalf("链接工作目录未移除: %v", err)
	}
	if nativeGit(t, path, "branch", "--list", "topic") == "" {
		t.Fatal("移除工作区误删本地分支")
	}
}

func TestNativeWorktreeSafetyAndCanceledReads(t *testing.T) {
	app, path, finish := nativeFixture(t)
	linked := filepath.Join(t.TempDir(), "linked")
	nativeGit(t, path, "worktree", "add", "-b", "topic", linked)
	writeNativeFile(t, linked, "untracked.txt", "keep me\n")
	app.openWorktrees()
	finish()
	app.reviewWorktree(linked, true)
	finish()
	if app.dialog == nil || app.dialog.valid() && app.dialog.action != "" {
		t.Fatal("未提交内容没有阻止移除")
	}
	if _, err := os.Stat(filepath.Join(linked, "untracked.txt")); err != nil {
		t.Fatal("预检破坏工作区数据")
	}
	app.dialog = nil
	app.loadWorktrees()
	app.switchWorkspace("changes")
	finish()
	if app.screen != "changes" || app.readTask != nil {
		t.Fatal("离开工作区管理后应用了过期读取")
	}
}

func TestNativeWorktreesAppearanceAndReadOnly(t *testing.T) {
	app, path, finish := nativeFixture(t)
	linked := filepath.Join(t.TempDir(), strings.Repeat("long-worktree-", 8))
	nativeGit(t, path, "worktree", "add", "-b", "feature/"+strings.Repeat("long-name-", 8), linked)
	linked, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	app.openWorktrees()
	finish()
	app.writable = false
	for _, size := range [][2]int{{1024, 680}, {1320, 860}} {
		for _, appearance := range []string{appearanceLight, appearanceDark} {
			app.prefs.Appearance = appearance
			tester := ui.NewTester(app.view, size[0], size[1])
			if create, found := tester.AccessByRole(ui.RoleButton, "新建 Worktree…"); !found || !create.Disabled {
				t.Fatal("仓库不可写时仍允许创建工作区")
			}
			if err := tester.Click(linked); err != nil {
				t.Fatal(err)
			}
			if open, found := tester.AccessByRole(ui.RoleButton, "打开工作区"); !found || open.Disabled || open.Offscreen {
				t.Fatal("不可写仓库仍应允许打开工作区")
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				saveNativeScreenshot(t, tester, directory, fmt.Sprintf("worktrees-%s-%d.png", appearance, size[0]))
			}
		}
	}
}
