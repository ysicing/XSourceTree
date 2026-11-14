// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ysicing/51code/internal/commitai"
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

type fakeAIKeys struct {
	mu     sync.Mutex
	values map[string]string
	err    error
}

func (store *fakeAIKeys) Load(account string) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.values[account], store.err
}

func (store *fakeAIKeys) Save(account, value string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.err != nil {
		return store.err
	}
	store.values[account] = value
	return nil
}

func (store *fakeAIKeys) Delete(account string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.err != nil {
		return store.err
	}
	delete(store.values, account)
	return nil
}

func nativeCustomAIFixture(t *testing.T) (*nativeApp, func(), *ui.Tester) {
	t.Helper()
	app, _, finish, _, _ := nativeAIFixture(t, false)
	app.prefs.AI.Protocol, app.prefs.AI.Model, app.prefs.AIConsent = commitai.Responses, "group/codex", false
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	app.aiSettingsDialog()
	return app, finish, ui.NewTester(app.view, 1024, 680)
}

func typeNativeAIKey(t *testing.T, tester *ui.Tester, key string) {
	t.Helper()
	field, found := tester.AccessByRole(ui.RoleTextField, "API 密钥")
	if !found || field.Disabled || field.Offscreen {
		t.Fatal("密钥输入框不可编辑")
	}
	tester.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
	tester.Type(key)
}

func TestNativeAICustomKeySaveGenerateAndClear(t *testing.T) {
	app, finish, tester := nativeCustomAIFixture(t)
	editor, settings := app.aiEditor, app.dialog
	typeNativeAIKey(t, tester, "synthetic-only-key")
	info, found := tester.AccessByRole(ui.RoleTextField, "API 密钥")
	if !found || !info.Password || strings.Contains(info.Value, "synthetic-only-key") {
		t.Fatal("密钥输入必须隐藏内容及辅助功能中的明文值")
	}
	editor.check(app, settings)
	if editor.checking || app.canGenerateCommit() {
		t.Fatal("未保存密钥时不能连接或生成")
	}
	if err := tester.Click("保存密钥"); err != nil {
		t.Fatal(err)
	}
	if app.closeBlockReason() == "" {
		t.Fatal("更新钥匙串期间应阻止退出")
	}
	app.closeDialog(settings)
	if app.dialog != settings {
		t.Fatal("写入钥匙串期间不能关闭设置")
	}
	finish()
	tester.Frame()
	account, err := commitai.KeyAccount(app.prefs.AI)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := app.aiKeys.Load(account)
	if err != nil || saved != "synthetic-only-key" || editor.key != "" || !editor.keySaved {
		t.Fatal("保存后应清空输入，仅保留钥匙串记录")
	}
	if err := app.savePreferences(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(app.stateFile)
	if err != nil || strings.Contains(string(data), "synthetic-only-key") {
		t.Fatal("普通配置文件不能保存 API 密钥")
	}
	if err := tester.Click("清除密钥"); err != nil {
		t.Fatal(err)
	}
	finish()
	if saved, err := app.aiKeys.Load(account); err != nil || saved != "" || editor.keySaved {
		t.Fatal("清除密钥后不能继续使用旧记录")
	}
	app.closeDialog(settings)
	app.requestCommitAI()
	if app.dialog == nil || !strings.Contains(app.dialog.detail, app.prefs.AI.Endpoint) || app.prefs.AIConsent {
		t.Fatal("首次生成必须明确确认自定义服务的发送目的地")
	}
	app.dialog.submit(app.dialog)
	finish()
	if app.ai == nil || app.ai.generated != "feat(ai): 添加提交说明生成" {
		t.Fatal("自定义服务未完成生成")
	}
}

func TestNativeAICustomKeyFailureAndDraftClose(t *testing.T) {
	for _, method := range []string{"button", "escape", "backdrop"} {
		t.Run(method, func(t *testing.T) {
			app, finish, tester := nativeCustomAIFixture(t)
			editor, settings := app.aiEditor, app.dialog
			store := app.aiKeys.(*fakeAIKeys)
			store.err = errors.New("测试钥匙串拒绝写入")
			typeNativeAIKey(t, tester, "synthetic-unsaved-key")
			if err := tester.Click("保存密钥"); err != nil {
				t.Fatal(err)
			}
			finish()
			tester.Frame()
			if editor.key != "synthetic-unsaved-key" || editor.keyProblem == "" || editor.keySaving {
				t.Fatal("写入失败必须保留输入并允许重试")
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
			if app.dialog == settings || app.dialog == nil || app.dialog.parent != settings {
				t.Fatal("未保存密钥应先确认放弃")
			}
			if err := tester.Click("取消"); err != nil {
				t.Fatal(err)
			}
			if app.dialog != settings || app.aiEditor != editor || editor.key != "synthetic-unsaved-key" {
				t.Fatal("取消关闭必须保留密钥草稿")
			}
			app.closeDialog(settings)
			tester.Frame()
			if err := tester.Click("放弃修改"); err != nil {
				t.Fatal(err)
			}
			if app.dialog != nil || app.aiEditor != nil || editor.key != "" || len(store.values) != 0 {
				t.Fatal("放弃草稿应清空内存输入且不写入钥匙串")
			}
		})
	}
}

func TestNativeAICustomKeyScopeAndReadFailure(t *testing.T) {
	app, _, finish, entered, _ := nativeAIFixture(t, false)
	store := app.aiKeys.(*fakeAIKeys)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		token := ""
		switch request.Header.Get("anthropic-version") {
		case "":
			token = request.Header.Get("Authorization")
		case "2023-06-01":
			token = request.Header.Get("x-api-key")
		}
		expected := ""
		if request.URL.Path == "/old/models" && request.Header.Get("anthropic-version") == "" {
			expected = "Bearer synthetic-scoped-key"
		}
		if token != expected {
			t.Error("密钥必须仅发送至对应协议和地址")
		}
		_, _ = writer.Write([]byte(`{"data":[{"id":"test-model"}]}`))
	}))
	defer server.Close()
	options := commitai.Options{Protocol: commitai.Responses, Endpoint: server.URL + "/old"}
	account, err := commitai.KeyAccount(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(account, "synthetic-scoped-key"); err != nil {
		t.Fatal(err)
	}
	for _, change := range []commitai.Options{
		options,
		{Protocol: commitai.Responses, Endpoint: server.URL + "/v1"},
		{Protocol: commitai.Claude, Endpoint: server.URL + "/old"},
	} {
		client, err := app.newAIClient(change)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Models(app.ctx)
		client.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	store.err = errors.New("测试钥匙串拒绝读取")
	app.prefs.AI.Protocol, app.prefs.AI.Model = commitai.Responses, "group/codex"
	app.generateCommitAI()
	finish()
	if app.ai == nil || app.ai.problem == "" || !app.ai.settings {
		t.Fatal("钥匙串读取失败必须提示设置问题")
	}
	select {
	case <-entered:
		t.Fatal("读取密钥失败后不能降级发送请求")
	default:
	}
}

func TestNativeAICustomManualModelWithoutListing(t *testing.T) {
	app, finish, tester := nativeCustomAIFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			writer.WriteHeader(http.StatusNotFound)
		case "/v1/responses":
			_, _ = writer.Write([]byte(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"feat(ai): 支持手动模型"}]}]}`))
		default:
			t.Error("请求路径错误")
		}
	}))
	defer server.Close()
	app.prefs.AI.Endpoint = server.URL + "/v1"
	if err := tester.Click("连接"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.aiEditor.problem == "" || !strings.Contains(app.aiEditor.problem, "手动填写模型 ID") {
		t.Fatal("不提供模型列表的服务应提示手动输入")
	}
	if field, found := tester.AccessByRole(ui.RoleTextField, "AI 模型"); !found || field.Disabled {
		t.Fatal("模型列表不可用时仍需允许输入模型")
	}
	app.closeDialog(app.dialog)
	app.generateCommitAI()
	finish()
	if app.ai == nil || app.ai.generated != "feat(ai): 支持手动模型" {
		t.Fatal("模型列表不可用不能阻止已填写模型的生成")
	}
}

func TestNativeAICustomSettingsLayout(t *testing.T) {
	app, _, tester := nativeCustomAIFixture(t)
	app.prefs.AI.Endpoint = "https://example.test/" + strings.Repeat("long-path/", 12) + "v1"
	app.prefs.AI.Model = strings.Repeat("long-model-", 18)
	for _, language := range []string{"zh-Hans", "zh-Hant", "en"} {
		t.Setenv("MYGO_LOCALE", language)
		tester.SetLocale(language)
		for _, size := range [][2]int{{1024, 680}, {780, 480}} {
			tester.SetSize(size[0], size[1])
			for _, dark := range []bool{false, true} {
				tester.SetDark(dark)
				tester.Frame()
				panel, found := tester.AccessByRole(ui.RoleDialog, "")
				if !found {
					t.Fatal("缺少设置面板")
				}
				for _, label := range []string{locales.ServiceAddress(), locales.ApiKey()} {
					field, found := tester.AccessByRole(ui.RoleTextField, label)
					if !found || field.Bounds.X < panel.Bounds.X ||
						field.Bounds.X+field.Bounds.W > panel.Bounds.X+panel.Bounds.W {
						t.Fatal("长文本不能将服务或密钥输入框挤出面板")
					}
				}
				if close, found := tester.AccessByRole(ui.RoleButton, locales.CloseSettings()); !found || close.Offscreen {
					t.Fatal("紧凑窗口必须保留关闭入口")
				}
				if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
					saveNativeScreenshot(t, tester, directory, fmt.Sprintf("ai-custom-%s-%d-%t.png", language, size[1], dark))
				}
			}
		}
	}
}

func TestNativeAIKeyGuardAfterIdentityCloseCancelled(t *testing.T) {
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	settingsTestUpdater.Do(func() {
		mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true, Language: "zh-Hans"}))
	})
	app, _, finish := nativeIdentityFixture(t)
	app.aiKeys = &fakeAIKeys{values: make(map[string]string)}
	app.prefs.AI = commitai.Options{Protocol: commitai.Responses, Endpoint: "https://example.test/v1"}
	app.aiSettingsDialog()
	tester := ui.NewTester(app.view, 1024, 680)
	editor, settings := app.aiEditor, app.dialog
	if err := tester.Click("Git"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	field, found := tester.AccessByRole(ui.RoleTextField, "姓名")
	if !found {
		t.Fatal("缺少 Git 身份编辑入口")
	}
	tester.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
	tester.Key(ui.Cmd, ui.KeyA)
	tester.Type("Pending Synthetic User")
	if err := tester.Click("AI"); err != nil {
		t.Fatal(err)
	}
	app.closeDialog(settings)
	tester.Frame()
	if app.dialog == settings || app.dialog == nil {
		t.Fatal("尚未保存的 Git 身份应触发关闭确认")
	}
	if err := tester.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if app.dialog != settings || app.aiEditor != editor {
		t.Fatal("取消关闭后应恢复当前 AI 设置的生命周期保护")
	}
	typeNativeAIKey(t, tester, "synthetic-reopened-key")
	if err := tester.Click("保存密钥"); err != nil {
		t.Fatal(err)
	}
	if app.closeBlockReason() == "" {
		t.Fatal("取消关闭后写入钥匙串仍需阻止退出")
	}
	finish()
}
