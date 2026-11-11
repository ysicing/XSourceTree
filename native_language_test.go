// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"testing"

	"github.com/ysicing/XSourceTree/internal/commitai"
	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

func TestNativeGeneralLanguageSelection(t *testing.T) {
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	t.Setenv("MYGO_LOCALE", "zh-Hans")
	app, _, _ := nativeFixture(t)
	app.prefs.AI.Language = commitai.English
	app.prefs.Appearance = appearanceDark
	app.prefs.UpdateCheckFrequency = "每周"
	var choices []string
	app.chooseLanguage = func(tag string) {
		choices = append(choices, tag)
		if tag == "" {
			tag = "zh-Hans"
		}
		if err := os.Setenv("MYGO_LOCALE", tag); err != nil {
			t.Fatal(err)
		}
	}
	app.settingsDialog()
	tester := ui.NewTester(app.view, 1024, 680)
	for _, choice := range []struct{ previous, option, tag, heading string }{
		{"自动检测", "English", "en", "General"},
		{"English", "繁體中文", "zh-Hant", "一般"},
		{"繁體中文", "简体中文", "zh-Hans", "通用"},
		{"简体中文", "自动检测", "", "通用"},
	} {
		if err := tester.Click(choice.previous); err != nil {
			t.Fatal(err)
		}
		if err := tester.Click(choice.option); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
		if app.chosenLanguage != choice.tag || choices[len(choices)-1] != choice.tag {
			t.Fatalf("语言选择没有映射到稳定的语言标签: %q, %v", app.chosenLanguage, choices)
		}
		if _, found := tester.Find(choice.heading); !found {
			t.Fatalf("语言切换后设置页没有即时更新: %s", choice.heading)
		}
		if app.prefs.Appearance != appearanceDark || app.prefs.UpdateCheckFrequency != "每周" || app.prefs.AI.Language != commitai.English {
			t.Fatal("界面语言切换修改了外观、更新频率或 AI 输出语言")
		}
	}
	if len(choices) != 4 {
		t.Fatalf("界面重绘重复触发了语言设置: %v", choices)
	}
	if err := tester.Click("关闭设置"); err != nil {
		t.Fatal(err)
	}
	app.settingsDialog()
	tester.Frame()
	if info, found := tester.AccessByRole(ui.RolePopUpButton, "语言"); !found || info.Value != "自动检测" {
		t.Fatalf("重新打开设置后未保留自动检测: %+v", info)
	}
}

func TestNativeLocalizedSettingsLayout(t *testing.T) {
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	for _, language := range []string{"zh-Hans", "zh-Hant", "en"} {
		t.Run(language, func(t *testing.T) {
			t.Setenv("MYGO_LOCALE", language)
			app, _, _ := nativeFixture(t)
			app.chosenLanguage = language
			app.settingsDialog()
			tester := ui.NewTester(app.view, 1024, 680)
			for _, size := range [][2]int{{1024, 680}, {780, 480}} {
				tester.SetSize(size[0], size[1])
				for _, dark := range []bool{false, true} {
					tester.SetDark(dark)
					tester.SetLocale(language)
					for _, label := range []string{locales.General(), locales.Appearance(), locales.Language(), locales.CloseSettings()} {
						bounds, found := tester.Find(label)
						if !found || bounds.W <= 0 || bounds.H <= 0 || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(size[0]) || bounds.Y+bounds.H > float32(size[1]) {
							t.Fatalf("语言 %s 的设置内容超出窗口: %s %+v", language, label, bounds)
						}
					}
					if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
						suffix := "light"
						if dark {
							suffix = "dark"
						}
						saveNativeScreenshot(t, tester, directory, "settings-general-"+language+"-"+suffix+".png")
					}
				}
			}
		})
	}
}

func TestNativeLanguageMatching(t *testing.T) {
	for _, test := range []struct{ preferred, want string }{
		{"zh-CN", "zh-Hans"}, {"zh-SG", "zh-Hans"},
		{"zh-TW", "zh-Hant"}, {"zh-HK", "zh-Hant"}, {"zh-MO", "zh-Hant"},
		{"en-US", "en"}, {"en-GB", "en"},
	} {
		if got := i18n.Match([]string{test.preferred}, locales.Catalog.Languages()); got != test.want {
			t.Errorf("系统语言 %s 匹配为 %s，预期 %s", test.preferred, got, test.want)
		}
	}
	if got := locales.In("de").General(); got != "通用" {
		t.Fatalf("不支持的语言未回退到简体中文: %s", got)
	}
	if locales.In("en").CommitFiles(1) != "Commit 1 File" || locales.In("en").CommitFiles(2) != "Commit 2 Files" {
		t.Fatal("英文文件数没有正确处理单复数")
	}
}

func TestNativeLocalizedPreferencesKeepStoredValues(t *testing.T) {
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	t.Setenv("MYGO_LOCALE", "en")
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	automaticChecks := updater.AutomaticChecks()
	t.Cleanup(func() {
		updater.SetAutomaticChecks(automaticChecks)
		mygo.App.SetPath(mygo.PathUserData, "")
	})
	app, _, _ := nativeFixture(t)
	app.chosenLanguage = "zh-TW"
	app.settingsDialog()
	tester := ui.NewTester(app.view, 1024, 680)
	if info, found := tester.AccessByRole(ui.RolePopUpButton, "Language"); !found || info.Value != "繁體中文" {
		t.Fatalf("地区语言标签没有匹配对应选项: %+v", info)
	}
	for _, label := range []string{"Dark", "Updates"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if !app.settingsAutoChecks {
		if err := tester.Click("Automatically check for updates"); err != nil {
			t.Fatal(err)
		}
	}
	for _, label := range []string{locales.Daily(), locales.Weekly()} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	restored := newNativeApp(app.service, app.stateFile)
	defer restored.cancel()
	if restored.prefs.Appearance != appearanceDark || restored.prefs.UpdateCheckFrequency != "每周" {
		t.Fatalf("翻译标签被写入已有配置: %+v", restored.prefs)
	}
	language, format := commitai.Chinese, commitai.Conventional
	aiTester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { aiOutputPreferences(c, &language, &format, false) })
	}, 500, 300)
	for _, label := range []string{locales.SimplifiedChinese(), locales.English(), commitai.Conventional, locales.PlainText()} {
		if err := aiTester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if language != commitai.English || format != commitai.Plain {
		t.Fatalf("AI 输出偏好被写入翻译标签: %q, %q", language, format)
	}
	app.dialog = nil
	app.busy, app.scanning, app.status = true, true, locales.ScanningFolders()
	tester.Frame()
	if _, found := tester.AccessByRole(ui.RoleButton, "Cancel"); !found {
		t.Fatal("英文扫描状态缺少取消入口")
	}
	app.scanning = false
	tester.Frame()
	if _, found := tester.AccessByRole(ui.RoleButton, "Cancel"); found {
		t.Fatal("非扫描任务错误显示取消入口")
	}
}
