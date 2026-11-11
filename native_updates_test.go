// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNativeVersionUpdateEntry(t *testing.T) {
	app := &nativeApp{}
	clicks := 0
	tester := ui.NewTester(func(c *ui.Context) {
		if app.versionButton(c) {
			clicks++
		}
	}, 280, 34)
	if err := tester.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	if clicks != 1 {
		t.Fatal("点击当前版本未触发更新入口")
	}
	app.availableVersion = "0.0.6"
	tester.Frame()
	if _, found := tester.Find("新版本 v0.0.6"); !found {
		t.Fatal("发现更新后未显示可用版本")
	}
	if err := tester.Click("新版本 v0.0.6，检查更新"); err != nil {
		t.Fatal(err)
	}
	if clicks != 2 {
		t.Fatal("新版本提示无法再次打开更新入口")
	}
	app.availableVersion = ""
	tester.Frame()
	if _, found := tester.Find("新版本 v0.0.6"); found {
		t.Fatal("清除可用版本后仍显示过期提示")
	}
	if info, found := tester.AccessByRole(ui.RoleButton, "检查更新"); !found || info.Disabled {
		t.Fatal("清除更新提示后应仍可手动检查")
	}
}

func TestNativeUpdateNoticeLayout(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.availableVersion = "0.0.6"
	app.problem = "Git 操作失败，请查看详情"
	for _, dark := range []bool{false, true} {
		tester := ui.NewTester(app.view, 1024, 680)
		tester.SetDark(dark)
		tester.Frame()
		info, found := tester.AccessByRole(ui.RoleButton, "新版本 v0.0.6，检查更新")
		if !found || info.Disabled {
			t.Fatal("错误提示不应遮挡或禁用更新入口")
		}
		if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
			name := "update-notice-light.png"
			if dark {
				name = "update-notice-dark.png"
			}
			saveNativeScreenshot(t, tester, directory, name)
		}
	}
}
