// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ysicing/51code/internal/gitrepo"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

var settingsTestUpdater sync.Once

func TestNativeSettingsKeepsBoundsAcrossCategories(t *testing.T) {
	app, _, finish := nativeIdentityFixture(t)
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	app.settingsDialog()
	tester := ui.NewTester(app.view, 1024, 680)
	for _, size := range [][2]int{{1024, 680}, {780, 480}} {
		tester.SetSize(size[0], size[1])
		if err := tester.Click("通用"); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
		panel, found := tester.AccessByRole(ui.RoleDialog, "")
		if !found {
			t.Fatal("未找到设置弹窗")
		}
		for _, category := range []string{"Git", "快捷键", "更新", "AI", "通用"} {
			if err := tester.Click(category); err != nil {
				t.Fatal(err)
			}
			if category == "Git" && size[1] == 680 {
				finish()
			}
			tester.Frame()
			current, found := tester.AccessByRole(ui.RoleDialog, "")
			if !found || current.Bounds != panel.Bounds {
				t.Fatalf("切换到 %s 时弹窗位置或尺寸改变: 原来 %+v，当前 %+v", category, panel.Bounds, current.Bounds)
			}
			if current.Bounds.Y < 0 || current.Bounds.Y+current.Bounds.H > float32(size[1]) {
				t.Fatalf("设置弹窗超出窗口: %+v", current.Bounds)
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				saveNativeScreenshot(t, tester, directory, fmt.Sprintf("settings-fixed-%s-%d.png", category, size[1]))
			}
		}
	}
}

func nativeIdentityFixture(t *testing.T) (*nativeApp, string, func()) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	config, err := gitrepo.GlobalIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := gitrepo.SaveGlobalIdentity(context.Background(), gitrepo.IdentityRequest{
		Name: "Default User", Email: "default@example.test", Token: config.Token,
	}); err != nil {
		t.Fatal(err)
	}
	app, _, finish := nativeFixture(t)
	return app, app.repository.snapshot.Path, finish
}

func TestNativeApplicationSettingsCategories(t *testing.T) {
	app, _, finish := nativeIdentityFixture(t)
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	app.settingsDialog()
	tester := ui.NewTester(app.view, 1024, 680)
	if _, found := tester.AccessByRole(ui.RoleButton, "关闭设置"); !found {
		t.Fatal("设置缺少可访问的标题栏关闭按钮")
	}
	if _, found := tester.Find("完成"); found {
		t.Fatal("自动保存的设置不应显示完成按钮")
	}
	for _, label := range []string{"通用", "Git", "快捷键", "更新", "AI"} {
		if _, found := tester.Find(label); !found {
			t.Fatalf("缺少设置分类: %s", label)
		}
	}
	if _, found := tester.Find("自动检查更新"); found {
		t.Fatal("外观页混入了更新设置")
	}
	for _, appearance := range []struct{ label, screenshot string }{
		{appearanceLight, "settings-appearance-light.png"},
		{appearanceDark, "settings-appearance-dark.png"},
		{appearanceSystem, "settings-appearance-system.png"},
	} {
		if _, found := tester.AccessByRole(ui.RoleRadio, appearance.label); !found {
			t.Fatalf("缺少可访问的外观选项: %s", appearance.label)
		}
		if err := tester.Click(appearance.label); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
		if info, found := tester.AccessByRole(ui.RoleRadio, appearance.label); !found || !info.Checked {
			t.Fatalf("外观选项没有暴露选中状态: %+v", info)
		}
		restored := newNativeApp(app.service, app.stateFile)
		restored.cancel()
		if app.prefs.Appearance != appearance.label || restored.prefs.Appearance != appearance.label || app.settingsError != "" {
			t.Fatalf("外观未立即应用并保存: %s, %s", app.prefs.Appearance, app.settingsError)
		}
		if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
			saveNativeScreenshot(t, tester, directory, appearance.screenshot)
		}
	}
	tester.Key(0, ui.KeyLeft)
	tester.Frame()
	if app.prefs.Appearance != appearanceDark {
		t.Fatal("外观选择不支持方向键")
	}
	tester.Key(0, ui.KeyTab)
	if info, found := tester.AccessByRole(ui.RolePopUpButton, "语言"); !found || !info.Focused {
		t.Fatalf("外观选择应只有一个 Tab 停留点，下一项应为语言: %+v", info)
	}
	tester.Key(ui.Shift, ui.KeyTab)
	if info, found := tester.AccessByRole(ui.RoleRadio, appearanceDark); !found || !info.Focused {
		t.Fatalf("返回外观选择时未聚焦当前选项: %+v", info)
	}
	for _, choice := range []struct {
		key        ui.Key
		appearance string
	}{
		{ui.KeyHome, appearanceSystem},
		{ui.KeyEnd, appearanceDark},
		{ui.KeyLeft, appearanceLight},
	} {
		tester.Key(0, choice.key)
		tester.Frame()
		restored := newNativeApp(app.service, app.stateFile)
		restored.cancel()
		if app.prefs.Appearance != choice.appearance || restored.prefs.Appearance != choice.appearance {
			t.Fatalf("键盘选择没有即时保存外观: %s, %s", app.prefs.Appearance, restored.prefs.Appearance)
		}
	}
	if err := tester.Click(appearanceSystem); err != nil {
		t.Fatal(err)
	}
	tester.SetSize(780, 480)
	for _, label := range []string{appearanceSystem, appearanceLight, appearanceDark, "关闭设置"} {
		if bounds, found := tester.Find(label); !found || bounds.W <= 0 || bounds.H <= 0 || bounds.X+bounds.W > 780 || bounds.Y+bounds.H > 480 {
			t.Fatalf("紧凑窗口中的外观选项不可见: %s", label)
		}
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "settings-appearance-small.png")
	}
	tester.SetSize(1024, 680)
	if err := tester.Click("通用"); err != nil {
		t.Fatal(err)
	}
	tester.Key(0, ui.KeyRight)
	finish()
	tester.Frame()
	if _, found := tester.Find("Default User"); !found {
		t.Fatal("全局页未显示全局身份")
	}
	if info, found := tester.AccessByRole(ui.RoleButton, "保存"); !found || !info.Disabled {
		t.Fatal("未编辑时不应启用保存")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "settings-global-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "settings-global-dark.png")
	}
	if err := tester.Click("更新"); err != nil {
		t.Fatal(err)
	}
	app.availableVersion = "0.0.6"
	tester.Frame()
	if _, found := tester.Find("新版本 v0.0.6"); !found {
		t.Fatal("更新页未展示可用版本")
	}
	app.availableVersion = ""
	tester.Frame()
	if _, found := tester.Find("新版本 v0.0.6"); found {
		t.Fatal("更新页保留了过期的新版本提示")
	}
	if _, found := tester.Find("自动检查更新"); !found {
		t.Fatal("更新页缺少已有设置")
	}
	if info, found := tester.AccessByRole(ui.RolePopUpButton, "检查频率"); !found || !info.Disabled || info.Value != "每天" {
		t.Fatalf("关闭自动检查时应禁用频率，并默认每天: %+v", info)
	}
	if info, found := tester.AccessByRole(ui.RoleButton, "立即检查更新"); !found || info.Disabled {
		t.Fatal("关闭自动检查不应禁用手动检查")
	}
	if err := tester.Click("自动检查更新"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if info, found := tester.AccessByRole(ui.RolePopUpButton, "检查频率"); !found || info.Disabled {
		t.Fatal("开启自动检查后应启用频率选择")
	}
	if err := tester.Click("每天"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("每周"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if app.prefs.UpdateCheckFrequency != "每周" || app.settingsError != "" {
		t.Fatalf("未保存选中的频率: %q, %s", app.prefs.UpdateCheckFrequency, app.settingsError)
	}
	restored := newNativeApp(app.service, app.stateFile)
	t.Cleanup(restored.cancel)
	if restored.prefs.UpdateCheckFrequency != "每周" {
		t.Fatal("重启后未恢复已保存的检查频率")
	}
	if _, found := tester.Find("检查频率更改将在下次启动后生效。"); !found {
		t.Fatal("缺少频率生效时间提示")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		tester.SetDark(false)
		saveNativeScreenshot(t, tester, directory, "settings-updates-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "settings-updates-dark.png")
	}
	if err := tester.Click("自动检查更新"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if info, found := tester.AccessByRole(ui.RolePopUpButton, "检查频率"); !found || !info.Disabled || info.Value != "每周" {
		t.Fatal("关闭自动检查后应保留频率并禁用选择")
	}
	if updater.AutomaticChecks() {
		t.Fatal("自动检查开关未同步到更新插件")
	}
	tester.SetSize(780, 480)
	tester.SetDark(false)
	for _, label := range []string{"关闭设置", "检查频率", "立即检查更新"} {
		if bounds, found := tester.Find(label); !found || bounds.W <= 0 || bounds.H <= 0 || bounds.Y+bounds.H > 480 {
			t.Fatalf("小窗口中的设置控件不可见: %s", label)
		}
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "settings-updates-small.png")
	}
	if err := tester.Click("关闭设置"); err != nil {
		t.Fatal(err)
	}
	if app.dialog != nil {
		t.Fatal("关闭设置未退出面板")
	}
}

func TestNativeSettingsCloseProtectsIdentityDraft(t *testing.T) {
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	for _, scope := range []string{"global", "repository"} {
		for _, method := range []string{"button", "escape", "backdrop"} {
			t.Run(scope+"/"+method, func(t *testing.T) {
				app, path, finish := nativeIdentityFixture(t)
				if scope == "global" {
					app.settingsDialog()
				} else {
					app.repositorySettingsDialog()
					finish()
				}
				tester := ui.NewTester(app.view, 1024, 680)
				name := "Native Test"
				if scope == "global" {
					if err := tester.Click("Git"); err != nil {
						t.Fatal(err)
					}
					finish()
					tester.Frame()
					name = "Default User"
				}
				settings := app.dialog
				field, found := tester.AccessByRole(ui.RoleTextField, "姓名")
				if !found || field.Disabled {
					t.Fatal("姓名输入框不可编辑")
				}
				tester.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
				tester.Key(ui.Cmd, ui.KeyA)
				tester.Type("Pending Name")
				tester.Frame()
				if button, found := tester.AccessByRole(ui.RoleButton, "保存"); !found || button.Disabled {
					t.Fatal("编辑姓名后未产生可保存的草稿")
				}
				if method == "backdrop" {
					tab := "更新"
					if scope == "repository" {
						tab = "远程"
					}
					if err := tester.Click(tab); err != nil {
						t.Fatal(err)
					}
				}
				switch method {
				case "button":
					if err := tester.Click("关闭设置"); err != nil {
						t.Fatal(err)
					}
				case "escape":
					tester.Key(0, ui.KeyEscape)
				case "backdrop":
					tester.ClickAt(2, 2)
				}
				tester.Frame()
				if app.dialog == nil || app.dialog == settings || app.dialog.parent != settings {
					t.Fatal("关闭设置未确认尚未保存的修改")
				}
				if err := tester.Click("取消"); err != nil {
					t.Fatal(err)
				}
				tester.Frame()
				if app.dialog != settings {
					t.Fatal("取消放弃修改未返回设置")
				}
				if method == "backdrop" {
					tab := "Git"
					if scope == "repository" {
						tab = "提交身份"
					}
					if err := tester.Click(tab); err != nil {
						t.Fatal(err)
					}
				}
				if field, found := tester.AccessByRole(ui.RoleTextField, "姓名"); !found || field.Value != "Pending Name" {
					t.Fatalf("取消关闭丢失了身份草稿: %+v", field)
				}
				if err := tester.Click("关闭设置"); err != nil {
					t.Fatal(err)
				}
				tester.Frame()
				if err := tester.Click("放弃修改"); err != nil {
					t.Fatal(err)
				}
				if app.dialog != nil {
					t.Fatal("确认放弃修改后未关闭设置")
				}
				configPath := path
				if scope == "global" {
					configPath = ""
				}
				identity, err := app.service.Identity(context.Background(), configPath)
				if err != nil || identity.Name.Value != name {
					t.Fatalf("关闭设置写入了未保存的草稿: %+v %v", identity, err)
				}
			})
		}
	}
}

func TestNativeUpdateCheckFrequencyPreferences(t *testing.T) {
	app, _, _ := nativeFixture(t)
	for _, test := range []struct {
		name      string
		data      string
		frequency string
		interval  time.Duration
	}{
		{"旧配置", `{}`, "每天", 24 * time.Hour},
		{"未知值", `{"updateCheckFrequency":"unexpected"}`, "每天", 24 * time.Hour},
		{"每小时", `{"updateCheckFrequency":"每小时"}`, "每小时", time.Hour},
		{"每六小时", `{"updateCheckFrequency":"每 6 小时"}`, "每 6 小时", 6 * time.Hour},
		{"每天", `{"updateCheckFrequency":"每天"}`, "每天", 24 * time.Hour},
		{"每周", `{"updateCheckFrequency":"每周"}`, "每周", 7 * 24 * time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateFile := filepath.Join(t.TempDir(), "native-state.json")
			if err := os.WriteFile(stateFile, []byte(test.data), 0600); err != nil {
				t.Fatal(err)
			}
			restored := newNativeApp(app.service, stateFile)
			t.Cleanup(restored.cancel)
			if restored.prefs.UpdateCheckFrequency != test.frequency {
				t.Fatalf("检查频率 = %q, 预期 %q", restored.prefs.UpdateCheckFrequency, test.frequency)
			}
			if interval := nativeUpdateCheckInterval(restored.prefs.UpdateCheckFrequency); interval != test.interval {
				t.Fatalf("检查间隔 = %v, 预期 %v", interval, test.interval)
			}
		})
	}
}

func TestNativeRepositoryIdentitySaveAndCancel(t *testing.T) {
	app, path, finish := nativeIdentityFixture(t)
	app.repositorySettingsDialog()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("为此仓库单独设置"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if app.dialog != nil || nativeGit(t, path, "config", "--local", "user.name") != "Native Test" {
		t.Fatal("取消操作改变了 Git 配置")
	}
	app.repositorySettingsDialog()
	finish()
	tester.Frame()
	dialog := app.dialog
	if err := tester.Click("为此仓库单独设置"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("保存"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.dialog != dialog || app.problem != "" {
		t.Fatalf("保存未保留设置上下文: %s", app.problem)
	}
	settings, err := app.service.Identity(context.Background(), path)
	if err != nil || settings.Overrides() || settings.Email.Value != "default@example.test" {
		t.Fatalf("恢复继承失败: %+v %v", settings, err)
	}
	if _, found := tester.Find("Default User"); !found {
		t.Fatal("保存后未刷新当前生效身份")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "settings-repository-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "settings-repository-dark.png")
	}
}

func TestNativeRepositoryRemoteReturnsToSettings(t *testing.T) {
	app, path, finish := nativeIdentityFixture(t)
	app.repositorySettingsDialog()
	finish()
	parent := app.dialog
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("远程"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("添加远程"); err != nil {
		t.Fatal(err)
	}
	if app.dialog.parent != parent {
		t.Fatal("远程编辑丢失了设置上下文")
	}
	tester.Frame()
	if err := tester.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if app.dialog != parent {
		t.Fatal("取消编辑没有返回仓库设置")
	}
	tester.Frame()
	if err := tester.Click("添加远程"); err != nil {
		t.Fatal(err)
	}
	app.dialog.fields[0].value = "origin"
	app.dialog.fields[1].value = "https://example.test/repo.git"
	app.dialog.submit(app.dialog)
	finish()
	tester.Frame()
	if app.dialog != parent || len(app.repository.remotes) != 1 {
		t.Fatalf("保存远程没有返回仓库设置: %s", app.problem)
	}
	if err := tester.Click("移除远程 origin"); err != nil {
		t.Fatal(err)
	}
	app.dialog.submit(app.dialog)
	finish()
	tester.Frame()
	if app.dialog != parent || len(app.repository.remotes) != 0 || nativeGit(t, path, "remote") != "" {
		t.Fatalf("移除远程没有返回仓库设置: %s", app.problem)
	}
}

func TestNativeIdentityErrorKeepsDraft(t *testing.T) {
	app, path, finish := nativeIdentityFixture(t)
	settings, err := app.service.Identity(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	editor := &nativeIdentityEditor{path: path}
	editor.adopt(settings)
	editor.name = "Pending Name"
	dialog := &nativeDialog{}
	app.dialog = dialog
	nativeGit(t, path, "config", "user.name", "External Name")
	editor.save(app, dialog)
	finish()
	if dialog.problem == "" || editor.name != "Pending Name" || !editor.dirty() || app.dialog != dialog {
		t.Fatal("保存失败没有保留草稿和错误提示")
	}
	if nativeGit(t, path, "config", "user.name") != "External Name" {
		t.Fatal("过期表单覆盖了外部变更")
	}
}

func TestNativeSettingsParentShowsRefreshFailure(t *testing.T) {
	app, _, finish := nativeIdentityFixture(t)
	parent := &nativeDialog{}
	dialog := &nativeDialog{parent: parent}
	app.dialog = dialog
	app.mutateForm(dialog, "保存配置", func(ctx context.Context, path string) error {
		return exec.CommandContext(ctx, "git", "-C", path, "config", "core.repositoryformatversion", "999").Run()
	})
	finish()
	if app.dialog != parent || parent.problem == "" || app.writable {
		t.Fatal("返回设置页时隐藏了刷新失败，或未禁止后续写入")
	}
}
