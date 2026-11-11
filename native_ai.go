// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ysicing/XSourceTree/internal/commitai"

	"github.com/egoist/mygo/ui"
)

type nativeAIOverride struct {
	Language string `json:"language,omitempty"`
	Format   string `json:"format,omitempty"`
}

type nativeCommitAI struct {
	path      string
	token     string
	previous  string
	generated string
	problem   string
	running   bool
	applied   bool
	revision  uint64
	cancel    context.CancelFunc
}

func (app *nativeApp) commitAIOptions(path string) commitai.Options {
	options := app.prefs.AI
	override := app.prefs.RepositoryAI[path]
	if override.Language != "" {
		options.Language = override.Language
	}
	if override.Format != "" {
		options.Format = override.Format
	}
	commitai.Normalize(&options)
	return options
}

func (app *nativeApp) canGenerateCommit() bool {
	if app.closed || app.busy || !app.writable || app.repository == nil ||
		app.screen != "changes" || app.repository.snapshot.Operation.Kind != "" {
		return false
	}
	staged := false
	for _, file := range app.repository.snapshot.Files {
		if file.Conflict {
			return false
		}
		staged = staged || isStaged(file)
	}
	return staged
}

func (app *nativeApp) stopCommitAI() {
	if app.ai != nil && app.ai.cancel != nil {
		app.ai.cancel()
	}
	app.ai = nil
}

func (app *nativeApp) requestCommitAI() {
	if !app.canGenerateCommit() || app.dialog != nil {
		return
	}
	if !app.prefs.AIConsent && strings.TrimSpace(app.prefs.AI.Model) == "" {
		app.aiSettingsDialog()
		app.dialog.detail = "先测试连接并选择模型，AI 配置对所有仓库生效。关闭设置后，再点击“AI 生成”。"
		return
	}
	if app.prefs.AIConsent {
		app.generateCommitAI()
		return
	}
	path := app.repository.snapshot.Path
	app.dialog = &nativeDialog{
		title:  "使用 AI 生成提交说明？",
		detail: "已暂存文件的路径和差异会通过本机 Magpie 发送给所选模型。Magpie 可能连接云端模型，请确认暂存内容适合发送。确认后将记住此授权，适用于所有仓库。",
		action: "开始生成",
		submit: func(_ *nativeDialog) {
			app.dialog = nil
			if app.repository == nil || app.repository.snapshot.Path != path {
				return
			}
			app.prefs.AIConsent = true
			if err := app.savePreferences(); err != nil {
				app.prefs.AIConsent = false
				app.problem = "无法保存 AI 偏好：" + err.Error()
				return
			}
			app.generateCommitAI()
		},
	}
}

func (app *nativeApp) generateCommitAI() {
	if !app.canGenerateCommit() {
		return
	}
	app.stopCommitAI()
	path := app.repository.snapshot.Path
	ctx, cancel := context.WithCancel(app.ctx)
	state := &nativeCommitAI{
		path: path, previous: app.prefs.Drafts[path], revision: app.draftRevision,
		running: true, cancel: cancel,
	}
	app.ai = state
	options := app.commitAIOptions(path)
	go func() {
		defer cancel()
		var token, message string
		err := func() error {
			repo, err := app.service.manager.Get(path)
			if err != nil {
				return err
			}
			staged, err := repo.CommitContext(ctx)
			if err != nil {
				return err
			}
			client, err := commitai.New(options.Endpoint)
			if err != nil {
				return err
			}
			defer client.Close()
			message, err = client.Generate(ctx, options, staged.Diff)
			if err != nil {
				return err
			}
			token = staged.Token
			current, err := repo.CommitToken(ctx)
			if err != nil {
				return err
			}
			if current != token {
				return errors.New("暂存区已变化，请重新生成")
			}
			return nil
		}()
		app.post(func() {
			if !app.commitAICurrent(state) {
				return
			}
			state.running, state.cancel = false, nil
			if err != nil {
				state.problem = commitAIError(err)
				return
			}
			state.token, state.generated = token, message
			if strings.TrimSpace(state.previous) == "" && app.draftRevision == state.revision && app.prefs.Drafts[path] == state.previous {
				app.applyCommitAI(state)
			}
		})
	}()
}

func commitAIError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "已取消生成"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "AI 请求超时，请重试"
	}
	return err.Error()
}

func (app *nativeApp) commitAICurrent(state *nativeCommitAI) bool {
	return !app.closed && app.ai == state && app.repository != nil && app.repository.snapshot.Path == state.path
}

func (app *nativeApp) applyCommitAI(state *nativeCommitAI) {
	state.previous = app.prefs.Drafts[state.path]
	app.prefs.Drafts[state.path] = state.generated
	app.draftRevision++
	state.applied = true
	state.revision = app.draftRevision
	if err := app.savePreferences(); err != nil {
		state.problem = "已填入提交说明，但保存草稿失败：" + err.Error()
	}
}

func (app *nativeApp) adoptCommitAI() {
	state := app.ai
	if state == nil || state.running || state.generated == "" || !app.canGenerateCommit() {
		return
	}
	revision, draft := app.draftRevision, app.prefs.Drafts[state.path]
	ctx, cancel := context.WithCancel(app.ctx)
	state.running, state.cancel, state.problem = true, cancel, ""
	go func() {
		defer cancel()
		repo, err := app.service.manager.Get(state.path)
		var current string
		if err == nil {
			current, err = repo.CommitToken(ctx)
		}
		app.post(func() {
			if !app.commitAICurrent(state) {
				return
			}
			state.running, state.cancel = false, nil
			switch {
			case err != nil:
				state.problem = commitAIError(err)
			case current != state.token:
				state.generated = ""
				state.problem = "暂存区已变化，请重新生成"
			case app.draftRevision != revision || app.prefs.Drafts[state.path] != draft:
				state.problem = "草稿已编辑，请确认后再次采用建议"
			default:
				app.applyCommitAI(state)
			}
		})
	}()
}

func (app *nativeApp) undoCommitAI() {
	state := app.ai
	if state == nil || !state.applied || app.draftRevision != state.revision || app.prefs.Drafts[state.path] != state.generated {
		return
	}
	app.prefs.Drafts[state.path] = state.previous
	app.draftRevision++
	state.applied = false
	if err := app.savePreferences(); err != nil {
		state.problem = "已撤销生成，但保存草稿失败：" + err.Error()
	}
}

func (app *nativeApp) commitAIHeader(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, "提交说明").FontSize(13).Grow(1)
		if app.ai != nil && app.ai.running {
			ui.Text(c, "正在生成…").FontSize(12).TextColor(c.Theme().TextMuted)
			if ui.Button(c, "取消生成").FontSize(12).Clicked() {
				app.stopCommitAI()
			}
			return
		}
		label := "AI 生成"
		if app.ai != nil {
			label = "重新生成"
		}
		if ui.Button(c, label).FontSize(12).Disabled(!app.canGenerateCommit()).
			Tooltip("根据全部已暂存变更生成提交说明").Clicked() {
			app.requestCommitAI()
		}
	})
}

func (app *nativeApp) commitAIResult(c *ui.Context) {
	state := app.ai
	if state == nil {
		return
	}
	if state.problem != "" {
		ui.Text(c, state.problem).FontSize(12).TextColor(c.Theme().Danger).MaxLines(3).Tooltip(state.problem)
		if ui.Button(c, "AI 设置").FontSize(12).Clicked() {
			app.aiSettingsDialog()
		}
	}
	if state.generated == "" || state.running {
		return
	}
	if state.applied {
		ui.Row(c).FillWidth().Children(func() {
			if ui.Button(c, "撤销生成").FontSize(12).
				Disabled(app.draftRevision != state.revision || app.prefs.Drafts[state.path] != state.generated).Clicked() {
				app.undoCommitAI()
			}
		})
		return
	}
	ui.Text(c, state.generated).FontSize(12).MaxLines(3).Selectable()
	ui.Row(c).FillWidth().Children(func() {
		if ui.Button(c, "采用建议").FontSize(12).Disabled(!app.canGenerateCommit()).Clicked() {
			app.adoptCommitAI()
		}
	})
}

type nativeAISettings struct {
	models   []string
	message  string
	checking bool
	cancel   context.CancelFunc
	serial   uint64
}

func (editor *nativeAISettings) stop() {
	editor.serial++
	editor.checking = false
	if editor.cancel != nil {
		editor.cancel()
		editor.cancel = nil
	}
}

func (editor *nativeAISettings) check(app *nativeApp, dialog *nativeDialog) {
	editor.stop()
	serial := editor.serial
	ctx, cancel := context.WithCancel(app.ctx)
	editor.cancel, editor.checking, editor.message = cancel, true, ""
	options := app.prefs.AI
	go func() {
		defer cancel()
		var models []string
		var endpoint string
		err := func() error {
			client, err := commitai.New(options.Endpoint)
			if err != nil {
				return err
			}
			defer client.Close()
			endpoint = options.Endpoint
			if endpoint == "" {
				endpoint, _ = commitai.DiscoverEndpoint()
			}
			models, err = client.Models(ctx)
			return err
		}()
		app.post(func() {
			if app.closed || app.dialog != dialog || editor.serial != serial {
				return
			}
			editor.checking, editor.cancel = false, nil
			if err != nil {
				app.settingsError = commitAIError(err)
				return
			}
			editor.models = models
			editor.message = fmt.Sprintf("已连接 %s · %d 个模型", endpoint, len(models))
			model, err := commitai.SelectModel(models, app.prefs.AI.Model)
			if err != nil {
				app.settingsError = err.Error()
				return
			}
			app.prefs.AI.Model = model
			app.settingsError = ""
			app.saveSettings()
		})
	}()
}

func (editor *nativeAISettings) view(app *nativeApp, dialog *nativeDialog, c *ui.Context) {
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		ui.Text(c, "Magpie").FontSize(14).Bold()
		ui.Text(c, "Magpie 地址").FontSize(12)
		if ui.TextInput(c, &app.prefs.AI.Endpoint).Label("Magpie 地址").
			Placeholder("自动发现本机 Magpie").FillWidth().Disabled(editor.checking).Changed() {
			editor.models, editor.message = nil, ""
			app.settingsError = ""
			app.saveSettings()
		}
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "自动发现").Disabled(editor.checking).Clicked() {
				app.prefs.AI.Endpoint = ""
				app.saveSettings()
				editor.check(app, dialog)
			}
			label := "测试连接"
			if editor.checking {
				label = "正在连接…"
			}
			if ui.Button(c, label).Disabled(editor.checking).Clicked() {
				editor.check(app, dialog)
			}
		})
		if editor.message != "" {
			ui.Text(c, editor.message).FontSize(12).TextColor(c.Theme().TextMuted).MaxLines(2)
		}
		ui.Text(c, "模型").FontSize(12)
		if len(editor.models) > 0 {
			if ui.Select(c, &app.prefs.AI.Model, editor.models).Label("AI 模型").FillWidth().Changed() {
				app.settingsError = ""
				app.saveSettings()
			}
		} else if ui.TextInput(c, &app.prefs.AI.Model).Label("AI 模型").Placeholder("自动选择可用模型").
			FillWidth().Disabled(editor.checking).Changed() {
			app.saveSettings()
		}
		ui.Divider(c)
		changed := aiOutputPreferences(c, &app.prefs.AI.Language, &app.prefs.AI.Format, false)
		if changed {
			app.settingsError = ""
			app.saveSettings()
		}
		ui.Text(c, "仅发送已暂存的路径与差异；模型可能在云端运行。").FontSize(12).TextColor(c.Theme().TextMuted)
	})
}

func aiOutputPreferences(c *ui.Context, language, format *string, inherit bool) bool {
	languages := []string{commitai.Chinese, commitai.English}
	formats := []string{commitai.Conventional, commitai.Plain}
	if inherit {
		languages = append([]string{"跟随应用设置"}, languages...)
		formats = append([]string{"跟随应用设置"}, formats...)
	}
	changed := false
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "语言").FontSize(13).Grow(1)
		changed = ui.Select(c, language, languages).Label("提交说明语言").Width(200).Changed() || changed
	})
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "格式").FontSize(13).Grow(1)
		changed = ui.Select(c, format, formats).Label("提交说明格式").Width(200).Changed() || changed
	})
	return changed
}

func (app *nativeApp) repositoryAISettings(c *ui.Context, path string) {
	override := app.prefs.RepositoryAI[path]
	language, format := override.Language, override.Format
	if language == "" {
		language = "跟随应用设置"
	}
	if format == "" {
		format = "跟随应用设置"
	}
	if aiOutputPreferences(c, &language, &format, true) {
		if language == "跟随应用设置" {
			language = ""
		}
		if format == "跟随应用设置" {
			format = ""
		}
		app.prefs.RepositoryAI[path] = nativeAIOverride{Language: language, Format: format}
		app.settingsError = ""
		app.saveSettings()
	}
	if app.settingsError != "" {
		ui.Text(c, app.settingsError).FontSize(12).TextColor(c.Theme().Danger)
	}
}
