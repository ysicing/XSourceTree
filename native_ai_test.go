// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ysicing/XSourceTree/internal/commitai"
	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

func nativeAIFixture(t *testing.T, wait bool) (*nativeApp, string, func(), <-chan struct{}, chan struct{}) {
	t.Helper()
	app, _, finish := nativeFixture(t)
	app.aiKeys = &fakeAIKeys{values: make(map[string]string)}
	path := app.repository.snapshot.Path
	writeNativeFile(t, path, "hello.txt", "staged synthetic change\n")
	nativeGit(t, path, "add", ".")
	app.refresh()
	finish()
	entered := make(chan struct{}, 1)
	release := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			_, _ = writer.Write([]byte(`{"data":[{"id":"group/codex"},{"id":"test-model"}]}`))
		case "/v1/responses":
			var body struct {
				Input string `json:"input"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if !strings.Contains(body.Input, "+staged synthetic change") || strings.Contains(body.Input, "unstaged secret") {
				t.Errorf("生成请求内容不正确: %s", body.Input)
			}
			entered <- struct{}{}
			if wait {
				select {
				case <-release:
				case <-request.Context().Done():
					return
				}
			}
			_, _ = writer.Write([]byte(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"feat(ai): 添加提交说明生成"}]}]}`))
		default:
			t.Errorf("未知接口: %s", request.URL.Path)
		}
	}))
	t.Cleanup(func() {
		app.cancel()
		server.Close()
	})
	app.prefs.AI.Endpoint, app.prefs.AIConsent = server.URL+"/v1", true
	return app, path, finish, entered, release
}

func awaitNativeAI(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("AI 未发出生成请求")
	}
}

func TestNativeAIInlineConsentGenerateAndUndo(t *testing.T) {
	app, path, finish, entered, _ := nativeAIFixture(t, false)
	writeNativeFile(t, path, "hello.txt", "unstaged secret\n")
	app.refresh()
	finish()
	app.selectFile("hello.txt", true)
	finish()
	app.prefs.AIConsent = false
	app.prefs.AI.Model = "group/codex"
	tester := ui.NewTester(app.view, 1320, 860)
	if err := tester.Click("AI 生成"); err != nil {
		t.Fatal(err)
	}
	if app.dialog == nil || app.ai != nil {
		t.Fatal("首次使用应先提示数据发送范围")
	}
	tester.Frame()
	if err := tester.Click("开始生成"); err != nil {
		t.Fatal(err)
	}
	if app.busy {
		t.Fatal("AI 生成不应锁定仓库操作")
	}
	finish()
	tester.Frame()
	if app.prefs.Drafts[path] != "feat(ai): 添加提交说明生成" || !app.ai.applied {
		t.Fatalf("空草稿未填入: %+v", app.ai)
	}
	if nativeGit(t, path, "log", "-1", "--format=%s") != "初始提交" {
		t.Fatal("AI 生成不应自动提交")
	}
	if nativeGit(t, path, "show", ":hello.txt") != "staged synthetic change" {
		t.Fatal("AI 生成不应修改暂存内容")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "ai-composer-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "ai-composer-dark.png")
		narrow := ui.NewTester(app.view, 1024, 680)
		saveNativeScreenshot(t, narrow, directory, "ai-composer-narrow.png")
	}
	if err := tester.Click("撤销生成"); err != nil {
		t.Fatal(err)
	}
	if app.prefs.Drafts[path] != "" {
		t.Fatal("撤销生成应恢复原草稿")
	}
	restored := newNativeApp(app.service, app.stateFile)
	defer restored.cancel()
	if !restored.prefs.AIConsent || restored.prefs.Drafts[path] != "" {
		t.Fatal("AI 偏好和撤销结果未持久化")
	}
	awaitNativeAI(t, entered)
	app.requestCommitAI()
	if app.dialog != nil {
		t.Fatal("已同意发送后不应重复确认")
	}
	finish()
}

func TestNativeAIFirstUseOpensGlobalSettings(t *testing.T) {
	app, _, finish, entered, _ := nativeAIFixture(t, false)
	app.prefs.AIConsent = false
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("AI 生成"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if app.dialog == nil || app.dialog.title != "应用设置" {
		t.Fatal("未配置模型时应先打开全局 AI 设置")
	}
	if _, found := tester.Find("连接"); !found {
		t.Fatal("首次使用应直接进入 AI 分类")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "ai-first-use-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "ai-first-use-dark.png")
		tester.SetSize(780, 480)
		saveNativeScreenshot(t, tester, directory, "ai-first-use-narrow.png")
		tester.SetSize(1024, 680)
		tester.SetDark(false)
	}
	app.closeDialog(app.dialog)
	if app.ai != nil || app.prefs.AIConsent {
		t.Fatal("关闭设置不能授权或自动生成")
	}
	app.requestCommitAI()
	tester.Frame()
	if err := tester.Click("连接"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.prefs.AI.Model != "group/codex" || app.settingsError != "" {
		t.Fatalf("连接测试应选中可用模型: %s", app.settingsError)
	}
	restored := newNativeApp(app.service, app.stateFile)
	defer restored.cancel()
	if restored.prefs.AI.Model != "group/codex" || restored.prefs.AIConsent {
		t.Fatal("模型应全局保存，但配置不代表同意发送")
	}
	app.closeDialog(app.dialog)
	app.requestCommitAI()
	if app.dialog == nil || app.dialog.title != "使用 AI 生成提交说明？" {
		t.Fatal("配置后仍需单独确认数据发送")
	}
	app.closeDialog(app.dialog)
	app.requestCommitAI()
	if app.dialog == nil || app.dialog.title != "使用 AI 生成提交说明？" || app.prefs.AIConsent || app.ai != nil {
		t.Fatal("取消发送后再次点击应继续确认")
	}
	select {
	case <-entered:
		t.Fatal("配置和取消确认期间不能发送差异")
	default:
	}
}

func TestNativeAIKeepsEditsDuringGeneration(t *testing.T) {
	app, path, finish, entered, release := nativeAIFixture(t, true)
	app.generateCommitAI()
	awaitNativeAI(t, entered)
	app.prefs.Drafts[path] = "手动编辑的提交说明"
	app.draftRevision++
	release <- struct{}{}
	finish()
	if app.prefs.Drafts[path] != "手动编辑的提交说明" || app.ai.generated == "" || app.ai.applied {
		t.Fatal("生成期间的编辑不应被覆盖，应展示建议")
	}
	app.adoptCommitAI()
	finish()
	if app.prefs.Drafts[path] != "feat(ai): 添加提交说明生成" {
		t.Fatal("确认采用后应填入建议")
	}
	app.undoCommitAI()
	if app.prefs.Drafts[path] != "手动编辑的提交说明" {
		t.Fatal("撤销应恢复采用前的手动草稿")
	}
}

func TestNativeAIExistingDraftRequiresAdoption(t *testing.T) {
	app, path, finish, _, _ := nativeAIFixture(t, false)
	app.prefs.Drafts[path] = "原有草稿"
	app.generateCommitAI()
	finish()
	if app.ai.applied || app.prefs.Drafts[path] != "原有草稿" {
		t.Fatal("已有草稿应先预览")
	}
	app.adoptCommitAI()
	app.prefs.Drafts[path] = "采用期间继续编辑"
	app.draftRevision++
	finish()
	if app.prefs.Drafts[path] != "采用期间继续编辑" || app.ai.applied {
		t.Fatal("采用前的再次编辑也必须受保护")
	}
}

func TestNativeAIFailureLeavesManualCommitAvailable(t *testing.T) {
	app, path, finish, _, _ := nativeAIFixture(t, false)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	app.prefs.AI.Endpoint = server.URL + "/v1"
	app.prefs.Drafts[path] = "手动提交"
	app.generateCommitAI()
	finish()
	if app.ai.problem == "" || app.busy || app.prefs.Drafts[path] != "手动提交" {
		t.Fatal("AI 失败不应影响手动提交")
	}
	tester := ui.NewTester(app.view, 1024, 680)
	if _, found := tester.AccessByRole(ui.RoleButton, "重试"); !found {
		t.Fatal("连接失败应显示重试，而不是重新生成")
	}
	if info, found := tester.AccessByRole(ui.RoleButton, "前往设置"); !found || info.Bounds.W >= 160 {
		t.Fatal("连接失败应提供紧凑的设置入口")
	}
	if info, found := tester.AccessByRole(ui.RoleButton, "提交 1 个文件"); !found || info.Disabled {
		t.Fatal("AI 失败后应保留可用的提交按钮")
	}
	app.commit()
	finish()
	if app.problem != "" || nativeGit(t, path, "log", "-1", "--format=%s") != "手动提交" {
		t.Fatalf("手动提交失败: %s", app.problem)
	}
}

func TestNativeAILargeContextRecovery(t *testing.T) {
	app, path, finish, entered, _ := nativeAIFixture(t, false)
	writeNativeFile(t, path, "large.txt", strings.Repeat("large content\n", 10000))
	nativeGit(t, path, "add", ".")
	app.refresh()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("AI 生成"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	warning := "暂存改动较多，暂时无法生成提交说明。可手动填写并提交。"
	if _, found := tester.Find(warning); !found {
		t.Fatal("超限应说明仅 AI 生成受限，仍可手动提交")
	}
	if info, found := tester.AccessByRole(ui.RoleButton, "AI 生成"); !found || !info.Disabled {
		t.Fatal("暂存内容未变化时不应允许重复触发超限失败")
	}
	blocked := app.ai
	app.requestCommitAI()
	if app.ai != blocked {
		t.Fatal("生成入口本身也应阻止相同内容的超限重试")
	}
	for _, label := range []string{"重新生成", "AI 设置", "前往设置"} {
		if _, found := tester.AccessByRole(ui.RoleButton, label); found {
			t.Fatalf("超限时不应显示无效操作 %s", label)
		}
	}
	if err := tester.Click("手动填写"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.Focused("提交说明") {
		t.Fatal("手动填写应聚焦提交说明输入框")
	}
	tester.Type("手动提交大改动")
	tester.Frame()
	if app.prefs.Drafts[path] != "手动提交大改动" {
		t.Fatal("超限后应继续保留手动编辑")
	}
	if info, found := tester.AccessByRole(ui.RoleButton, "提交 2 个文件"); !found || info.Disabled {
		t.Fatal("AI 超限不应禁用手动提交")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "ai-limit-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "ai-limit-dark.png")
		tester.SetDark(false)
	}
	app.refresh()
	finish()
	tester.Frame()
	if info, _ := tester.AccessByRole(ui.RoleButton, "AI 生成"); !info.Disabled {
		t.Fatal("刷新相同暂存内容不应解除超限状态")
	}
	select {
	case <-entered:
		t.Fatal("超限差异不应发送到 AI")
	default:
	}
	writeNativeFile(t, path, "large.txt", "smaller change\n")
	nativeGit(t, path, "add", ".")
	app.refresh()
	finish()
	tester.Frame()
	if info, found := tester.AccessByRole(ui.RoleButton, "AI 生成"); !found || info.Disabled {
		t.Fatal("外部暂存内容变化后刷新应恢复生成入口")
	}
	if _, found := tester.Find(warning); found {
		t.Fatal("暂存内容变化后应清除旧超限提示")
	}
	if err := tester.Click("AI 生成"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.prefs.Drafts[path] != "手动提交大改动" || app.ai.generated == "" {
		t.Fatal("恢复生成后应保留手动草稿，先展示建议")
	}
	if _, found := tester.AccessByRole(ui.RoleButton, "重新生成"); !found {
		t.Fatal("成功生成后才应显示重新生成")
	}
	awaitNativeAI(t, entered)
}

func TestNativeAIErrorRecoveryActions(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		status   int
		body     string
		model    string
		settings bool
	}{
		{name: "invalid-message", status: 200, body: `{"status":"completed","output":[]}`},
		{name: "invalid-response", status: 200, body: "invalid json"},
		{name: "rate-limited", status: 429},
		{name: "unavailable", status: 503},
		{name: "authentication", status: 401, settings: true},
		{name: "missing-model", status: 200, model: "missing", settings: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			app, path, finish, _, _ := nativeAIFixture(t, false)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/v1/models" {
					_, _ = writer.Write([]byte(`{"data":[{"id":"test-model"}]}`))
					return
				}
				writer.WriteHeader(scenario.status)
				_, _ = writer.Write([]byte(scenario.body))
			}))
			defer server.Close()
			app.prefs.AI.Endpoint, app.prefs.AI.Model = server.URL+"/v1", scenario.model
			app.prefs.Drafts[path] = "保留草稿"
			app.generateCommitAI()
			finish()
			tester := ui.NewTester(app.view, 1024, 680)
			if app.ai.problem == "" || app.prefs.Drafts[path] != "保留草稿" {
				t.Fatal("失败应保留错误和原有草稿")
			}
			if _, found := tester.AccessByRole(ui.RoleButton, "前往设置"); found != scenario.settings {
				t.Fatalf("错误恢复入口不正确: settings=%v, error=%s", found, app.ai.problem)
			}
			if info, found := tester.AccessByRole(ui.RoleButton, "重试"); !found || info.Disabled {
				t.Fatal("失败后应保留可用的重试入口")
			}
		})
	}
}

func TestNativeAIComposerSettingsMenu(t *testing.T) {
	app, _, _, _, _ := nativeAIFixture(t, false)
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	tester := ui.NewTester(app.view, 1024, 680)
	if _, found := tester.AccessByRole(ui.RoleButton, "AI 设置"); found {
		t.Fatal("提交区不应常驻全宽 AI 设置按钮")
	}
	if err := tester.Click("AI 生成选项"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("AI 设置…"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if app.dialog == nil || app.dialog.title != "应用设置" {
		t.Fatal("生成按钮旁的菜单应打开 AI 设置")
	}
	if _, found := tester.Find("连接"); !found {
		t.Fatal("菜单入口应直接进入 AI 分类")
	}
}

func TestNativeAIComposerLimitLayout(t *testing.T) {
	app, path, _ := nativeFixture(t)
	for _, language := range []string{"zh-Hans", "zh-Hant", "en"} {
		t.Setenv("MYGO_LOCALE", language)
		for _, appearance := range []string{appearanceLight, appearanceDark} {
			app.prefs.Appearance = appearance
			tester := ui.NewTester(app.view, 1024, 680)
			tester.SetLocale(language)
			app.ai = &nativeCommitAI{path: path, tooLarge: true, problem: locales.AiContextTooLarge()}
			tester.Frame()
			for _, label := range []string{locales.CommitMessage(), locales.AiGenerationOptions(), locales.WriteManually(), locales.AiContextTooLarge()} {
				info, found := tester.Access(label)
				if !found || info.Offscreen || info.Bounds.W <= 0 || info.Bounds.H <= 0 ||
					info.Bounds.X+info.Bounds.W > 1024 || info.Bounds.Y+info.Bounds.H > 680 {
					t.Fatalf("%s %s 控件 %s 被裁切: %+v", language, appearance, label, info)
				}
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				saveNativeScreenshot(t, tester, directory, "ai-limit-"+language+"-"+appearance+".png")
			}
		}
	}
}

func TestNativeAIAllowsDiffBrowsingDuringGeneration(t *testing.T) {
	app, _, finish, entered, release := nativeAIFixture(t, true)
	app.generateCommitAI()
	awaitNativeAI(t, entered)
	state := app.ai
	app.selectFile("hello.txt", true)
	finish()
	if app.ai != state || !state.running || app.selectedFile != "hello.txt" {
		t.Fatal("查看差异不应中断 AI 生成")
	}
	release <- struct{}{}
	finish()
	if !app.ai.applied || app.ai.problem != "" {
		t.Fatalf("查看差异后未生成: %+v", app.ai)
	}
}

func TestNativeAIRejectsStaleIndex(t *testing.T) {
	for _, stage := range []string{"during-generation", "before-adoption"} {
		t.Run(stage, func(t *testing.T) {
			app, path, finish, entered, release := nativeAIFixture(t, stage == "during-generation")
			app.prefs.Drafts[path] = "保留草稿"
			app.generateCommitAI()
			awaitNativeAI(t, entered)
			if stage == "before-adoption" {
				finish()
			}
			writeNativeFile(t, path, "hello.txt", "new staged change\n")
			nativeGit(t, path, "add", ".")
			if stage == "during-generation" {
				release <- struct{}{}
			} else {
				app.adoptCommitAI()
			}
			finish()
			if app.ai.problem == "" || app.ai.generated != "" || app.prefs.Drafts[path] != "保留草稿" {
				t.Fatalf("旧结果未作废: %+v", app.ai)
			}
		})
	}
}

func TestNativeAICancelNavigationAndCloseDropResults(t *testing.T) {
	for _, action := range []string{"cancel", "library", "close", "switch"} {
		t.Run(action, func(t *testing.T) {
			app, path, finish, entered, _ := nativeAIFixture(t, true)
			app.generateCommitAI()
			awaitNativeAI(t, entered)
			switch action {
			case "cancel":
				app.stopCommitAI()
			case "library":
				app.showLibrary()
			case "close":
				app.closed = true
				app.cancel()
			case "switch":
				result := *app.repository
				result.snapshot.Path = t.TempDir()
				app.adopt(&result)
			}
			finish()
			if app.prefs.Drafts[path] != "" || app.problem != "" {
				t.Fatal("离开、关闭或取消后不应再采用结果")
			}
		})
	}
}

func TestNativeAISettingsAndRepositoryInheritance(t *testing.T) {
	app, path, finish, _, _ := nativeAIFixture(t, false)
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	app.aiSettingsDialog()
	tester := ui.NewTester(app.view, 1024, 680)
	for _, label := range []string{"Magpie 地址", "AI 模型", "连接", "连接方式", "连接与模型", "生成偏好", "提交说明语言", "提交说明格式"} {
		if _, found := tester.Find(label); !found {
			t.Fatalf("AI 设置缺少 %s", label)
		}
	}
	if err := tester.Click("连接"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.prefs.AI.Model != "group/codex" || app.settingsError != "" {
		t.Fatalf("连接测试或模型选择失败: %+v %s", app.prefs.AI, app.settingsError)
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "ai-settings-light.png")
		tester.SetDark(true)
		saveNativeScreenshot(t, tester, directory, "ai-settings-dark.png")
	}
	tester.SetSize(780, 480)
	tester.SetDark(false)
	navigation, found := tester.Find("应用设置分类")
	if !found {
		t.Fatal("AI 设置缺少分类导航")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "ai-settings-narrow.png")
	}
	tester.Scroll(500, 300, 0, 500)
	tester.Frame()
	if info, found := tester.AccessByRole(ui.RolePopUpButton, "提交说明格式"); !found || info.Bounds.W <= 0 || info.Bounds.H <= 0 ||
		info.Bounds.Y+info.Bounds.H > 480 {
		t.Fatal("紧凑窗口无法滚动到 AI 输出格式设置")
	}
	if bounds, found := tester.Find("应用设置分类"); !found || bounds != navigation {
		t.Fatal("滚动 AI 内容时不应移动分类导航")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "ai-settings-narrow-scrolled.png")
	}
	tester.SetSize(1024, 680)
	app.closeDialog(app.dialog)
	app.prefs.AI.Language, app.prefs.AI.Format = commitai.English, commitai.Plain
	options := app.commitAIOptions(path)
	if options.Language != commitai.English || options.Format != commitai.Plain {
		t.Fatal("仓库默认应继承应用设置")
	}
	app.prefs.RepositoryAI[path] = nativeAIOverride{Language: commitai.Chinese}
	options = app.commitAIOptions(path)
	if options.Language != commitai.Chinese || options.Format != commitai.Plain || options.Endpoint != app.prefs.AI.Endpoint {
		t.Fatal("仓库覆盖不应修改连接或其他全局设置")
	}
	if err := app.savePreferences(); err != nil {
		t.Fatal(err)
	}
	restored := newNativeApp(app.service, app.stateFile)
	defer restored.cancel()
	if restored.commitAIOptions(path).Language != commitai.Chinese {
		t.Fatal("仓库设置未持久化")
	}
	app.repositorySettingsDialog()
	finish()
	tester.Frame()
	if err := tester.Click("AI"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if info, found := tester.AccessByRole(ui.RolePopUpButton, "提交说明语言"); !found || info.Value != commitai.Chinese {
		t.Fatalf("仓库 AI 页未显示语言覆盖: %+v", info)
	}
	if _, found := tester.Find("连接方式"); found {
		t.Fatal("仓库设置不应重复连接配置")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "ai-repository-settings.png")
	}
}

func TestNativeAIConnectionStates(t *testing.T) {
	for _, scenario := range []struct {
		name, response, model string
		connected, retry      bool
	}{
		{name: "success", response: `{"data":[{"id":"group/codex"}]}`, connected: true},
		{name: "empty", response: `{"data":[]}`, model: "saved-model", connected: true},
		{name: "missing-model", response: `{"data":[{"id":"group/codex"}]}`, model: "removed-model", connected: true},
		{name: "retry", response: `{"data":[{"id":"group/codex"}]}`, model: "group/codex", retry: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			app, _, finish := nativeFixture(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/v1/models" {
					t.Errorf("配置时不应访问 %s", request.URL.Path)
				}
				if requests.Add(1) == 1 && scenario.retry {
					writer.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = writer.Write([]byte(scenario.response))
			}))
			defer server.Close()
			app.prefs.AI.Endpoint, app.prefs.AI.Model = server.URL+"/v1", scenario.model
			editor := &nativeAISettings{manual: true}
			label := "连接"
			if scenario.retry {
				editor.connected, editor.models = true, []string{scenario.model}
				label = "重新检测"
			}
			dialog := &nativeDialog{title: "应用设置", width: 620, height: 520}
			dialog.body = func(c *ui.Context) { editor.view(app, dialog, c) }
			app.dialog = dialog
			tester := ui.NewTester(app.view, 1024, 680)
			if err := tester.Click(label); err != nil {
				t.Fatal(err)
			}
			if len(editor.models) != 0 {
				t.Fatal("重新检测时不应保留旧模型列表")
			}
			if info, found := tester.AccessByRole(ui.RolePopUpButton, "AI 模型"); !found || !info.Disabled {
				t.Fatal("加载模型期间应禁用选择")
			}
			finish()
			tester.Frame()
			if editor.connected != scenario.connected || editor.checking {
				t.Fatalf("连接状态错误: %+v", editor)
			}
			switch scenario.name {
			case "success":
				if editor.problem != "" || app.prefs.AI.Model != "group/codex" {
					t.Fatal("成功后应选择可用模型")
				}
			case "empty":
				if editor.problem == "" || len(editor.models) != 0 || app.prefs.AI.Model != scenario.model {
					t.Fatal("没有模型时应保留配置并说明如何恢复")
				}
				if _, found := tester.Access("已连接 · 0 个模型"); !found {
					t.Fatal("辅助功能应能读取实际连接状态")
				}
			case "missing-model":
				if editor.problem == "" || app.prefs.AI.Model != scenario.model {
					t.Fatal("不能静默替换已失效的模型")
				}
				if err := tester.Click("AI 模型"); err != nil {
					t.Fatal(err)
				}
				if err := tester.Click("group/codex"); err != nil {
					t.Fatal(err)
				}
				if editor.problem != "" || app.prefs.AI.Model != "group/codex" {
					t.Fatal("选择有效模型后应清除提示并保存")
				}
			case "retry":
				if editor.problem == "" || len(editor.models) != 0 || app.prefs.AI.Model != scenario.model {
					t.Fatal("检测失败应禁用旧列表并保留已保存的模型")
				}
				if err := tester.Click("重试"); err != nil {
					t.Fatal(err)
				}
				finish()
				tester.Frame()
				if !editor.connected || editor.problem != "" {
					t.Fatal("重试成功后应恢复模型选择")
				}
			}
			model, found := tester.AccessByRole(ui.RolePopUpButton, "AI 模型")
			if !found || model.Disabled != (len(editor.models) == 0) {
				t.Fatalf("模型控件状态错误: %+v", model)
			}
			if app.prefs.AIConsent || app.ai != nil {
				t.Fatal("配置不能授予发送授权或开始生成")
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				saveNativeScreenshot(t, tester, directory, "ai-connection-"+scenario.name+".png")
			}
		})
	}
}

func TestNativeAISettingsLongModelLayout(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.prefs.AI.Model = strings.Repeat("provider/very-long-model-", 10)
	editor := &nativeAISettings{connected: true, models: []string{app.prefs.AI.Model}}
	dialog := &nativeDialog{title: "应用设置", width: 620, height: 520}
	dialog.onClose = func() { app.dialog = nil }
	dialog.body = func(c *ui.Context) { editor.view(app, dialog, c) }
	app.dialog = dialog
	for _, size := range [][2]int{{1024, 680}, {780, 480}} {
		for _, appearance := range []string{appearanceLight, appearanceDark} {
			app.prefs.Appearance = appearance
			tester := ui.NewTester(app.view, size[0], size[1])
			panel, found := tester.AccessByRole(ui.RoleDialog, "")
			if !found {
				t.Fatal("缺少设置面板")
			}
			model, found := tester.AccessByRole(ui.RolePopUpButton, "AI 模型")
			if !found || model.Offscreen || model.Bounds.X < panel.Bounds.X ||
				model.Bounds.X+model.Bounds.W > panel.Bounds.X+panel.Bounds.W {
				t.Fatalf("长模型名称不应挤出面板: %+v %+v", panel.Bounds, model.Bounds)
			}
			close, found := tester.AccessByRole(ui.RoleButton, "关闭设置")
			if !found || close.Offscreen {
				t.Fatal("长模型名称不应挤走关闭入口")
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				saveNativeScreenshot(t, tester, directory, fmt.Sprintf("ai-long-model-%s-%d.png", appearance, size[1]))
			}
		}
	}
}

func TestNativeAIConnectionModeAndCancellation(t *testing.T) {
	app, _, finish, _, _ := nativeAIFixture(t, false)
	editor := &nativeAISettings{manual: true}
	dialog := &nativeDialog{title: "应用设置", width: 620, height: 520}
	dialog.body = func(c *ui.Context) { editor.view(app, dialog, c) }
	app.dialog = dialog
	tester := ui.NewTester(app.view, 1024, 680)
	editor.check(app, dialog)
	editor.stop()
	finish()
	if editor.connected || len(editor.models) != 0 || app.prefs.AI.Model != "" {
		t.Fatal("取消后返回的连接结果不应更新配置")
	}
	tester.Frame()
	endpoint := app.prefs.AI.Endpoint
	t.Setenv("MAGPIE_ADDR", endpoint)
	mode, found := tester.AccessByRole(ui.RolePopUpButton, "连接方式")
	if !found {
		t.Fatal("缺少连接方式下拉框")
	}
	tester.ClickAt(mode.Bounds.X+mode.Bounds.W/2, mode.Bounds.Y+mode.Bounds.H/2)
	if err := tester.Click("自动发现本机 Magpie"); err != nil {
		t.Fatal(err)
	}
	if editor.manual || app.prefs.AI.Endpoint != "" {
		t.Fatal("自动模式应清除手动地址")
	}
	if _, found := tester.Find("Magpie 地址"); found {
		t.Fatal("自动模式不应展示手动地址")
	}
	if err := tester.Click("连接"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if !editor.connected || app.prefs.AI.Model != "group/codex" || app.prefs.AI.Endpoint != "" {
		t.Fatal("自动发现应加载模型，不能固化自动发现地址")
	}
	mode, found = tester.AccessByRole(ui.RolePopUpButton, "连接方式")
	if !found {
		t.Fatal("缺少连接方式下拉框")
	}
	tester.ClickAt(mode.Bounds.X+mode.Bounds.W/2, mode.Bounds.Y+mode.Bounds.H/2)
	if err := tester.Click("自定义服务"); err != nil {
		t.Fatal(err)
	}
	if info, found := tester.AccessByRole(ui.RoleButton, "连接"); !found || !info.Disabled {
		t.Fatal("空手动地址不能连接")
	}
	address, found := tester.AccessByRole(ui.RoleTextField, "服务地址")
	if !found {
		t.Fatal("缺少服务地址输入框")
	}
	tester.ClickAt(address.Bounds.X+address.Bounds.W/2, address.Bounds.Y+address.Bounds.H/2)
	tester.Type(endpoint)
	if app.prefs.AI.Endpoint != endpoint || editor.connected || len(editor.models) != 0 {
		t.Fatal("更换地址应保存输入并清除旧连接状态")
	}
	restored := newNativeApp(app.service, app.stateFile)
	defer restored.cancel()
	if restored.prefs.AI.Endpoint != app.prefs.AI.Endpoint || restored.prefs.AI.Model != "group/codex" {
		t.Fatal("切换连接方式不能丢失已保存的模型")
	}
	editor.check(app, dialog)
	app.dialog = nil
	finish()
	if editor.connected || editor.problem != "" {
		t.Fatal("关闭后到达的请求不能更新旧设置页面")
	}
}
