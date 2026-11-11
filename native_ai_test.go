// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ysicing/XSourceTree/internal/commitai"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

func nativeAIFixture(t *testing.T, wait bool) (*nativeApp, string, func(), <-chan struct{}, chan struct{}) {
	t.Helper()
	app, _, finish := nativeFixture(t)
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
	if _, found := tester.Find("测试连接"); !found {
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
	if err := tester.Click("测试连接"); err != nil {
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
	if info, found := tester.AccessByRole(ui.RoleButton, "提交 1 个文件"); !found || info.Disabled {
		t.Fatal("AI 失败后应保留可用的提交按钮")
	}
	app.commit()
	finish()
	if app.problem != "" || nativeGit(t, path, "log", "-1", "--format=%s") != "手动提交" {
		t.Fatalf("手动提交失败: %s", app.problem)
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
	for _, label := range []string{"Magpie 地址", "AI 模型", "测试连接", "自动发现", "提交说明语言", "提交说明格式"} {
		if _, found := tester.Find(label); !found {
			t.Fatalf("AI 设置缺少 %s", label)
		}
	}
	if err := tester.Click("测试连接"); err != nil {
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
	if _, found := tester.Find("测试连接"); found {
		t.Fatal("仓库设置不应重复连接配置")
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, tester, directory, "ai-repository-settings.png")
	}
}
