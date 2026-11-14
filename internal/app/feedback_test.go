// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestNativeFeedbackEntry(t *testing.T) {
	app, _, _ := nativeFixture(t)
	repository := app.repository
	for _, state := range []string{"repository", "library", "busy", "error", "update"} {
		app.repository = repository
		app.screen, app.busy, app.problem, app.availableVersion = "changes", false, "", ""
		switch state {
		case "library":
			app.showLibrary()
			app.repository = nil
		case "busy":
			app.busy, app.status = true, strings.Repeat("正在执行 Git 操作", 20)
		case "error":
			app.problem = strings.Repeat("Git 操作失败，请查看详情", 20)
		case "update":
			app.availableVersion = "0.0.12"
		}
		for _, appearance := range []string{appearanceLight, appearanceDark} {
			app.prefs.Appearance = appearance
			tester := ui.NewTester(app.view, 1024, 680)
			feedback, found := tester.AccessByRole(ui.RoleButton, "反馈问题或建议")
			if !found || feedback.Disabled || feedback.Offscreen || feedback.Bounds.W < 24 || feedback.Bounds.H < 24 ||
				feedback.Bounds.X < 0 || feedback.Bounds.Y < 646 || feedback.Bounds.Y+feedback.Bounds.H > 680 {
				t.Fatalf("%s %s 反馈入口不可用: %+v", state, appearance, feedback)
			}
			versionLabel := "检查更新"
			if app.availableVersion != "" {
				versionLabel = "新版本 v0.0.12，检查更新"
			}
			version, found := tester.AccessByRole(ui.RoleButton, versionLabel)
			if !found || version.Offscreen || feedback.Bounds.X+feedback.Bounds.W > version.Bounds.X ||
				version.Bounds.X+version.Bounds.W > 1024 {
				t.Fatalf("反馈入口没有位于版本左侧: feedback=%+v version=%+v", feedback, version)
			}
			tester.Move(feedback.Bounds.X+feedback.Bounds.W/2, feedback.Bounds.Y+feedback.Bounds.H/2)
			tester.Advance(time.Second)
			if !tester.HasText("反馈问题或建议") {
				t.Fatal("悬停未显示反馈提示")
			}
			if err := tester.Click("反馈问题或建议"); err != nil {
				t.Fatal(err)
			}
			urls := tester.OpenedURLs()
			if len(urls) != 1 || urls[0] != "https://github.com/ysicing/51code/issues/new" {
				t.Fatalf("反馈入口打开了错误地址: %v", urls)
			}
			tester.Frame()
			if len(tester.OpenedURLs()) != 1 {
				t.Fatal("重绘时重复打开了反馈页面")
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" && state == "update" {
				saveNativeScreenshot(t, tester, directory, "native-feedback-"+appearance+".png")
			}
		}
	}
}

func TestNativeFeedbackOpenFailure(t *testing.T) {
	app, _, _ := nativeFixture(t)
	tester := ui.NewTester(app.view, 1024, 680)
	tester.FailOpenURL(errors.New("no browser"))
	if err := tester.Click("反馈问题或建议"); err != nil {
		t.Fatal(err)
	}
	if app.problem != "无法打开反馈页面：no browser" {
		t.Fatal("打开失败未反馈具体原因", app.problem)
	}
	tester.FailOpenURL(nil)
	if err := tester.Click("反馈问题或建议"); err != nil {
		t.Fatal(err)
	}
	if len(tester.OpenedURLs()) != 2 {
		t.Fatal("反馈页面打开失败后无法重试")
	}
}
