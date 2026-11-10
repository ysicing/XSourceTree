// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"testing"

	"github.com/ysicing/XSourceTree/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func TestNativeRemoteBranchTree(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.repository.remotes = []gitrepo.RemoteConfig{{Name: "origin"}, {Name: "upstream"}, {Name: "team/mirror"}, {Name: "empty"}}
	app.repository.branches = append(app.repository.branches,
		gitrepo.Branch{Name: "origin/HEAD", Remote: true},
		gitrepo.Branch{Name: "origin/main", Remote: true},
		gitrepo.Branch{Name: "origin/feature/work", Remote: true},
		gitrepo.Branch{Name: "upstream/main", Remote: true},
		gitrepo.Branch{Name: "team/mirror/release/next", Remote: true},
	)
	tester := ui.NewTester(app.view, 1320, 860)
	for _, label := range []string{"远端 origin", "远端 upstream", "远端 team/mirror", "远端 empty"} {
		if _, found := tester.Find(label); !found {
			t.Fatalf("缺少远端分组 %s", label)
		}
	}
	parent, _ := tester.Find("远端 origin")
	child, found := tester.Find("origin/main")
	if !found || child.Y <= parent.Y {
		t.Fatal("远程分支未显示在所属远端下")
	}
	if err := tester.Click("远端 origin"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if _, found := tester.Find("origin/main"); found {
		t.Fatal("折叠后仍显示远端的分支")
	}
	if _, found := tester.Find("upstream/main"); !found {
		t.Fatal("折叠一个远端影响其他远端")
	}
	refreshed := *app.repository
	app.repository = &refreshed
	tester.Frame()
	if _, found := tester.Find("origin/main"); found {
		t.Fatal("刷新仓库后丢失折叠状态")
	}
	tester.Key(0, ui.KeyRight)
	tester.Frame()
	if _, found := tester.Find("origin/main"); !found {
		t.Fatal("右方向键未展开远端")
	}
	if err := tester.Click("origin/HEAD"); err != nil {
		t.Fatal(err)
	}
	tester.Key(0, ui.KeyDown)
	tester.Key(0, ui.KeyEnter)
	if app.dialog == nil || app.dialog.detail != "origin/main" {
		t.Fatal("键盘导航未选中所属远端的分支")
	}
	app.dialog = nil
	tester.Frame()
	if err := tester.Click("origin/HEAD"); err != nil {
		t.Fatal(err)
	}
	if app.dialog != nil {
		t.Fatal("HEAD 默认引用不应打开检出弹窗")
	}
	for _, branch := range []struct{ reference, name string }{
		{"origin/feature/work", "feature/work"},
		{"upstream/main", "main"},
		{"team/mirror/release/next", "release/next"},
	} {
		if err := tester.Click(branch.reference); err != nil {
			t.Fatal(err)
		}
		if app.dialog == nil || app.dialog.detail != branch.reference || app.dialog.fields[0].value != branch.name {
			t.Fatalf("检出的远程引用或本地分支名错误: %+v", branch)
		}
		app.dialog = nil
		tester.Frame()
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "native-remote-tree-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "native-remote-tree-dark.png")
	}
}
