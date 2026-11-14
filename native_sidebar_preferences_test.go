// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

func sidebarSettingsTester(t *testing.T, app *nativeApp) *ui.Tester {
	t.Helper()
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	app.settingsDialog()
	return ui.NewTester(app.view, 1024, 680)
}

func TestNativeSidebarVisibilityIndependentAndPersistent(t *testing.T) {
	app, path, finish := nativeFixture(t)
	writeNativeFile(t, path, "hello.txt", "保留菜单入口\n")
	app.refresh()
	finish()
	shortcuts := app.prefs.Shortcuts
	tester := sidebarSettingsTester(t, app)
	labels := []string{"搜索", "版本比较", "贮藏", "工作树"}
	for index, label := range labels {
		if checkbox, found := tester.AccessByRole(ui.RoleCheckBox, label); !found || !checkbox.Checked {
			t.Fatalf("缺少默认开启的独立开关 %s: %+v", label, checkbox)
		}
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
		for otherIndex, other := range labels {
			checkbox, found := tester.AccessByRole(ui.RoleCheckBox, other)
			if !found || checkbox.Checked != (otherIndex > index) {
				t.Fatalf("切换 %s 影响了 %s 的独立状态: %+v", label, other, checkbox)
			}
		}
	}
	if app.settingsError != "" || app.prefs.Shortcuts != shortcuts {
		t.Fatal("侧栏设置保存失败或改变了快捷键", app.settingsError)
	}
	if err := tester.Click("关闭设置"); err != nil {
		t.Fatal(err)
	}
	for _, label := range labels {
		if _, found := tester.Find(label); found {
			t.Fatalf("侧栏仍显示已隐藏的 %s", label)
		}
	}
	for _, label := range []string{"文件变更", "提交历史"} {
		if _, found := tester.Find(label); !found {
			t.Fatalf("侧栏缺少常驻入口 %s", label)
		}
	}
	if err := tester.Click("更多"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tester.Menu(), "贮藏更改…") {
		t.Fatal("隐藏贮藏侧栏入口后菜单消失")
	}
	if err := tester.ChooseMenuItem("贮藏更改…"); err != nil {
		t.Fatal(err)
	}
	if app.dialog == nil || app.dialog.action != "贮藏" {
		t.Fatal("隐藏贮藏后无法从菜单打开表单")
	}
	if err := tester.Click("取消"); err != nil {
		t.Fatal(err)
	}
	app.selectedFile = "hello.txt"
	app.fileHistoryAction()
	finish()
	tester.Frame()
	if app.screen != "search" || app.historyFile != "hello.txt" {
		t.Fatal("隐藏搜索后其他入口无法继续查看文件历史")
	}

	restored := newNativeApp(app.service, app.stateFile)
	t.Cleanup(restored.cancel)
	restored.repository, restored.writable = app.repository, true
	restored.screen = "changes"
	tester = sidebarSettingsTester(t, restored)
	for _, label := range labels {
		if checkbox, found := tester.AccessByRole(ui.RoleCheckBox, label); !found || checkbox.Checked {
			t.Fatalf("重启后未保留 %s 的隐藏状态", label)
		}
	}
	if err := tester.Click("贮藏"); err != nil {
		t.Fatal(err)
	}
	tester.Key(0, ui.KeySpace)
	tester.Frame()
	if checkbox, found := tester.AccessByRole(ui.RoleCheckBox, "贮藏"); !found || checkbox.Checked {
		t.Fatal("空格键未切换贮藏开关")
	}
	tester.Key(0, ui.KeySpace)
	tester.Frame()
	if err := tester.Click("关闭设置"); err != nil {
		t.Fatal(err)
	}
	if _, found := tester.Find("贮藏"); !found {
		t.Fatal("重新开启贮藏后入口未恢复")
	}
	for _, label := range []string{"搜索", "版本比较", "工作树"} {
		if _, found := tester.Find(label); found {
			t.Fatalf("开启贮藏错误地重新开启了 %s", label)
		}
	}
}

func TestNativeSidebarVisibilityLeavesHiddenPage(t *testing.T) {
	for _, item := range []struct{ screen, label string }{
		{"search", "搜索"}, {"compare", "版本比较"}, {"stash", "贮藏"}, {"worktrees", "工作树"},
	} {
		t.Run(item.screen, func(t *testing.T) {
			app, _, finish := nativeFixture(t)
			app.switchWorkspace(item.screen)
			app.load("正在读取测试…", nativeReadCommit, func(ctx context.Context) (func(), error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
			tester := sidebarSettingsTester(t, app)
			if err := tester.Click(item.label); err != nil {
				t.Fatal(err)
			}
			finish()
			tester.Frame()
			if app.screen != "changes" || app.readTask != nil || app.settingsError != "" {
				t.Fatal("关闭当前入口没有返回文件变更或取消旧读取")
			}
			if err := tester.Click("关闭设置"); err != nil {
				t.Fatal(err)
			}
			if _, found := tester.Find(item.label); found {
				t.Fatal("已关闭的入口仍出现在侧栏")
			}
		})
	}
}

func TestNativeSidebarVisibilitySaveFailure(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.switchWorkspace("search")
	stateFile := app.stateFile
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	app.stateFile = filepath.Join(blocker, "state.json")
	tester := sidebarSettingsTester(t, app)
	if err := tester.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	if checkbox, found := tester.AccessByRole(ui.RoleCheckBox, "搜索"); !found || !checkbox.Checked ||
		app.settingsError == "" || app.screen != "search" {
		t.Fatal("保存失败未恢复开关或错误地切走当前页面")
	}
	app.stateFile = stateFile
	if err := tester.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	if app.settingsError != "" || app.screen != "changes" {
		t.Fatal("保存失败后无法重试")
	}
}

func TestNativeSidebarVisibilityLegacyPreferences(t *testing.T) {
	app, _, _ := nativeFixture(t)
	for _, test := range []struct {
		name, data string
		search     bool
	}{
		{"旧配置", `{}`, true},
		{"部分配置", `{"sidebarItems":{"search":false}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, []byte(test.data), 0600); err != nil {
				t.Fatal(err)
			}
			restored := newNativeApp(app.service, path)
			t.Cleanup(restored.cancel)
			tester := sidebarSettingsTester(t, restored)
			for _, label := range []string{"搜索", "版本比较", "贮藏", "工作树"} {
				want := label != "搜索" || test.search
				if checkbox, found := tester.AccessByRole(ui.RoleCheckBox, label); !found || checkbox.Checked != want {
					t.Fatalf("配置缺失字段没有保持默认显示: %s %+v", label, checkbox)
				}
			}
		})
	}
}
