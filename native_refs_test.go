// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"testing"

	"github.com/ysicing/XSourceTree/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func TestNativeReferenceDirectoryStateScopes(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.repository.branches = append(app.repository.branches,
		gitrepo.Branch{Name: "a/b/c"},
		gitrepo.Branch{Name: "origin/a/b/c", Remote: true},
		gitrepo.Branch{Name: "upstream/a/b/c", Remote: true},
	)
	app.repository.tags = []gitrepo.Tag{{Name: "a/b/c", Hash: app.repository.snapshot.Head}}
	tester := ui.NewTester(app.view, 1320, 1000)
	for _, label := range []string{"本地分支目录 a/b", "远端目录 origin/a/b", "标签目录 a/b"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
		if app.busy || app.dialog != nil {
			t.Fatalf("点击目录触发了引用操作: %s", label)
		}
	}
	for _, label := range []string{"本地分支目录 a/b", "远端目录 origin/a/b", "标签目录 a/b"} {
		item, found := tester.Access(label)
		if !found || item.Expanded {
			t.Fatalf("未保留目录的独立折叠状态: %s", label)
		}
	}
	other, found := tester.Access("远端目录 upstream/a/b")
	if !found || !other.Expanded {
		t.Fatal("折叠 origin 的目录影响了 upstream")
	}
	for range 2 {
		if err := tester.Click("本地分支目录 a"); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
	}
	child, found := tester.Access("本地分支目录 a/b")
	if !found || child.Expanded {
		t.Fatal("折叠再展开父目录后丢失子目录的折叠状态")
	}
	previous := app.repository
	next := *previous
	next.snapshot.Path += "-other"
	app.repository = &next
	tester.Frame()
	for _, label := range []string{"本地分支目录 a/b", "远端目录 origin/a/b", "标签目录 a/b"} {
		item, found := tester.Access(label)
		if !found || !item.Expanded {
			t.Fatalf("其他仓库继承了旧仓库的折叠状态: %s", label)
		}
	}
	app.repository = previous
	app.writable = false
	tester.Frame()
	restored, found := tester.Access("远端目录 origin/a/b")
	if !found || restored.Expanded {
		t.Fatal("返回仓库后丢失目录折叠状态")
	}
	if err := tester.Click("远端目录 origin/a/b"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("origin/a/b/c"); err != nil {
		t.Fatal(err)
	}
	if app.dialog != nil {
		t.Fatal("只读状态仍能检出远程分支")
	}
}

func TestNativeReferenceDirectories(t *testing.T) {
	app, path, finish := nativeFixture(t)
	for _, branch := range []string{"a/b/c", "a/b/d", "a/other", "codex/work"} {
		nativeGit(t, path, "branch", branch)
	}
	for _, tag := range []string{"a/b/c", "a/b/d", "v1"} {
		nativeGit(t, path, "tag", tag)
	}
	nativeGit(t, path, "remote", "add", "origin", path)
	nativeGit(t, path, "update-ref", "refs/remotes/origin/a/b/c", "HEAD")
	app.refresh()
	finish()
	tester := ui.NewTester(app.view, 1320, 1000)
	for _, label := range []string{
		"本地分支目录 a", "本地分支目录 a/b", "a/b/c", "a/b/d",
		"远端 origin", "远端目录 origin/a", "远端目录 origin/a/b", "origin/a/b/c",
		"标签目录 a", "标签目录 a/b",
	} {
		if _, found := tester.Find(label); !found {
			t.Fatalf("缺少目录或引用 %q", label)
		}
	}
	if err := tester.Click("本地分支目录 a/b"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if _, found := tester.AccessByRole(ui.RoleTreeItem, "a/b/c"); !found {
		t.Fatal("折叠本地分支目录影响同名标签")
	}
	if _, found := tester.Find("origin/a/b/c"); !found {
		t.Fatal("折叠本地分支目录影响远程分支")
	}
	localTree, found := tester.Access("本地分支目录 a/b")
	if !found || localTree.Expanded {
		t.Fatal("本地目录未折叠")
	}
	app.refresh()
	finish()
	tester.Frame()
	localTree, _ = tester.Access("本地分支目录 a/b")
	if localTree.Expanded {
		t.Fatal("刷新后丢失目录折叠状态")
	}
	tester.Key(0, ui.KeyRight)
	tester.Frame()
	localTree, _ = tester.Access("本地分支目录 a/b")
	if !localTree.Expanded {
		t.Fatal("右方向键未展开目录")
	}
	tester.Key(0, ui.KeyDown)
	tester.Key(0, ui.KeyEnter)
	finish()
	tester.Frame()
	if app.problem != "" || nativeGit(t, path, "branch", "--show-current") != "a/b/c" {
		t.Fatalf("目录中的分支未使用完整引用切换: %s", app.problem)
	}
	current, found := tester.AccessByRole(ui.RoleTreeItem, "a/b/c")
	if !found || !current.Selected {
		t.Fatal("当前分支未标记为选中")
	}
	if err := tester.Click("origin/a/b/c"); err != nil {
		t.Fatal(err)
	}
	if app.dialog == nil || app.dialog.detail != "origin/a/b/c" || app.dialog.fields[0].value != "a/b/c" {
		t.Fatal("嵌套远程分支的检出引用不完整")
	}
	app.dialog = nil
	tester.Frame()
	if err := tester.RightClick("a/b/c"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("重命名分支…"); err != nil {
		t.Fatal(err)
	}
	if app.dialog == nil || app.dialog.fields[0].value != "a/b/c" {
		t.Fatal("嵌套本地分支的重命名引用不完整")
	}
	app.dialog = nil
	tester.Frame()
	if err := tester.Click("本地分支目录 a"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("a/b/c"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.screen != "history" || app.commitHash != nativeGit(t, path, "rev-parse", "refs/tags/a/b/c") {
		t.Fatal("嵌套标签未打开对应提交")
	}
	if err := tester.RightClick("a/b/c"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("删除标签…"); err != nil {
		t.Fatal(err)
	}
	if app.dialog == nil || app.dialog.detail != "a/b/c" {
		t.Fatal("嵌套标签的删除引用不完整")
	}
	app.dialog = nil
	tester.Frame()
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		if err := tester.Click("本地分支目录 a"); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
		saveNativeScreenshot(t, tester, directory, "native-reference-tree-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "native-reference-tree-dark.png")
	}
}
