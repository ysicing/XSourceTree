// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ysicing/51code/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func TestNativeRevisionFileViewsKeepSelection(t *testing.T) {
	app := &nativeApp{prefs: nativePreferences{FileSort: "path"}}
	files := []gitrepo.CommitFile{
		{Path: "b/main.go", Status: "A"},
		{Path: "a/z.go", Status: "M"},
		{Path: "a/main.go", Status: "D"},
		{Path: "a", Status: "D"},
	}
	list, selected, chosen := &ui.ListState{}, 1, ""
	tester := ui.NewTester(func(c *ui.Context) {
		app.revisionFileList(c, files, list, &selected, "提交文件列表", "", func(path string) {
			chosen = path
		})
	}, 280, 480)
	choose := func(button, item string) {
		t.Helper()
		if err := tester.Click(button); err != nil {
			t.Fatal(err)
		}
		if err := tester.ChooseMenuItem(item); err != nil {
			t.Fatal(err)
		}
	}
	for _, order := range []string{"按文件名排序", "按文件状态排序", "按路径排序", "倒序"} {
		choose("文件排序", order)
		if selected < 0 || files[selected].Path != "a/z.go" || chosen != "" {
			t.Fatalf("排序改变了所选文件或触发了读取: %d %q", selected, chosen)
		}
	}
	choose("文件视图", "目录树")
	if item, found := tester.AccessByRole(ui.RoleTreeItem, "a/z.go"); !found || !item.Selected {
		t.Fatal("目录树没有保留文件选择")
	}
	if err := tester.Click("a/"); err != nil {
		t.Fatal(err)
	}
	if _, found := tester.AccessByRole(ui.RoleTreeItem, "a/z.go"); found {
		t.Fatal("折叠目录后仍显示其子文件")
	}
	if selected < 0 || files[selected].Path != "a/z.go" || chosen != "" {
		t.Fatal("折叠目录清除了文件选择或触发读取")
	}
	tester.Key(0, ui.KeyRight)
	if item, found := tester.AccessByRole(ui.RoleTreeItem, "a/z.go"); !found || !item.Selected {
		t.Fatal("方向键展开目录后未恢复文件选择")
	}
	if _, found := tester.AccessByRole(ui.RoleTreeItem, "a"); !found {
		t.Fatal("同名目录吞掉了已删除文件")
	}
	if err := tester.Click("b/main.go"); err != nil {
		t.Fatal(err)
	}
	if files[selected].Path != "b/main.go" || chosen != "b/main.go" {
		t.Fatal("目录树选择了错误路径")
	}
	chosen = ""
	choose("文件视图", "平面列表")
	if row, found := tester.AccessByRole(ui.RoleListItem, "b/main.go"); !found || !row.Selected || chosen != "" {
		t.Fatalf("返回列表丢失选择或重复读取: selected=%d chosen=%q found=%t row=%+v", selected, chosen, found, row)
	}
	if err := tester.Click("a/main.go"); err != nil {
		t.Fatal(err)
	}
	if files[selected].Path != "a/main.go" || chosen != "a/main.go" {
		t.Fatal("排序后列表选择了错误路径")
	}
}

func TestNativeRevisionFilesEmptyLoadingAndLayout(t *testing.T) {
	app := &nativeApp{prefs: nativePreferences{FileSort: "path"}}
	files := []gitrepo.CommitFile{{Path: "src/很长的目录名称/another/long/path/main.go", Status: "M"}}
	list, selected, reads := &ui.ListState{}, 0, 0
	tester := ui.NewTester(func(c *ui.Context) {
		app.revisionFileList(c, files, list, &selected, "提交文件列表", "空提交", func(string) {
			reads++
		})
	}, 280, 440)
	for _, width := range []int{260, 420} {
		tester.SetSize(width, 440)
		for _, dark := range []bool{false, true} {
			tester.SetDark(dark)
			for _, tree := range []bool{false, true} {
				app.prefs.FileTree = tree
				tester.Frame()
				for _, label := range []string{"文件排序", "文件视图"} {
					item, found := tester.AccessByRole(ui.RoleMenuButton, label)
					if !found || item.Offscreen || item.Bounds.X < 0 || item.Bounds.X+item.Bounds.W > float32(width) {
						t.Fatalf("窄窗口控件被裁切: %d %t %t %s %+v", width, dark, tree, label, item)
					}
				}
				if tree {
					item, found := tester.AccessByRole(ui.RoleTreeItem, files[0].Path)
					if !found || item.Bounds.X+item.Bounds.W < float32(width)-2 {
						t.Fatalf("目录树行没有铺满文件栏: %d %+v", width, item)
					}
				}
				if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
					saveNativeScreenshot(t, tester, directory, fmt.Sprintf("revision-files-%d-dark-%t-tree-%t.png", width, dark, tree))
				}
			}
		}
	}
	app.readTask = &nativeRead{kind: nativeReadCommit}
	tester.Frame()
	if tree, found := tester.AccessByRole(ui.RoleTree, "提交文件列表"); !found || !tree.Disabled {
		t.Fatal("提交读取期间没有禁用目录树")
	}
	app.readTask = &nativeRead{kind: nativeReadDiff}
	tester.Frame()
	if tree, found := tester.AccessByRole(ui.RoleTree, "提交文件列表"); !found || tree.Disabled {
		t.Fatal("Diff 读取期间不应禁用目录树")
	}
	app.readTask = nil
	files = nil
	selected = -1
	tester.Frame()
	if _, found := tester.Find("空提交"); !found || selected != -1 || reads != 0 {
		t.Fatal("空提交缺少提示或发起了读取")
	}
}

func TestNativeRevisionFileSortingPreservesPathIdentity(t *testing.T) {
	app := &nativeApp{prefs: nativePreferences{FileSort: "path"}}
	files := []gitrepo.CommitFile{
		{Path: "b/main.go", Status: "A"},
		{Path: "a/z.go", Status: "M"},
		{Path: "a/main.go", Status: "D"},
	}
	list, selected := &ui.ListState{}, 0
	tester := ui.NewTester(func(c *ui.Context) {
		app.revisionFileList(c, files, list, &selected, "比较文件列表", "", func(string) {})
	}, 280, 400)
	for _, scenario := range []struct {
		order string
		want  []string
	}{
		{"path", []string{"a/main.go", "a/z.go", "b/main.go"}},
		{"name", []string{"a/main.go", "b/main.go", "a/z.go"}},
		{"status", []string{"b/main.go", "a/main.go", "a/z.go"}},
	} {
		for _, descending := range []bool{false, true} {
			app.prefs.FileSort, app.prefs.FileDescending = scenario.order, descending
			tester.Frame()
			want := slices.Clone(scenario.want)
			if descending {
				slices.Reverse(want)
			}
			for index, path := range want {
				if files[index].Path != path {
					t.Fatalf("排序 %s descending=%t: %+v", scenario.order, descending, files)
				}
			}
			if selected < 0 || files[selected].Path != "b/main.go" {
				t.Fatal("排序按旧索引选择了其他文件")
			}
		}
	}
}

func TestNativeRevisionFileViewsLoadMatchingDiff(t *testing.T) {
	app, path, finish := nativeFixture(t)
	base := app.repository.snapshot.Head
	for _, name := range []string{"a/main.go", "a/z.go", "b/main.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(path, name)), 0700); err != nil {
			t.Fatal(err)
		}
		writeNativeFile(t, path, name, name+"\n")
	}
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "文件视图测试")
	head := nativeGit(t, path, "rev-parse", "HEAD")
	app.refresh()
	finish()
	app.writable = false
	for _, screen := range []string{"history", "search", "compare"} {
		app.switchWorkspace(screen)
		app.prefs.FileTree, app.prefs.FileSort, app.prefs.FileDescending = false, "path", false
		if screen == "compare" {
			app.comparison.base, app.comparison.target = base, head
			app.startComparison()
		} else {
			app.searchHistory(false)
			finish()
			app.selectCommit(head)
		}
		finish()
		tester := ui.NewTester(app.view, 1320, 860)
		choose := func(button, item string) {
			t.Helper()
			if err := tester.Click(button); err != nil {
				t.Fatal(err)
			}
			if err := tester.ChooseMenuItem(item); err != nil {
				t.Fatal(err)
			}
		}
		before := app.diff.Text
		choose("文件排序", "按文件名排序")
		choose("文件排序", "倒序")
		choose("文件视图", "目录树")
		if app.diff.Text != before || app.readTask != nil {
			t.Fatalf("%s: 切换展示触发读取或更改了 Diff", screen)
		}
		if err := tester.Click("b/main.go"); err != nil {
			t.Fatal(err)
		}
		finish()
		tester.Frame()
		if !strings.Contains(app.diff.Text, "+b/main.go") {
			t.Fatalf("%s: 目录树加载了错误文件: %s", screen, app.diff.Text)
		}
		choose("文件视图", "平面列表")
		if err := tester.Click("a/main.go"); err != nil {
			t.Fatal(err)
		}
		finish()
		tester.Frame()
		if !strings.Contains(app.diff.Text, "+a/main.go") {
			t.Fatalf("%s: 排序后的列表加载了错误文件: %s", screen, app.diff.Text)
		}
		choose("文件视图", "目录树")
		for _, dark := range []bool{false, true} {
			tester.SetDark(dark)
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				saveNativeScreenshot(t, tester, directory, fmt.Sprintf("revision-%s-dark-%t.png", screen, dark))
			}
		}
	}
}
