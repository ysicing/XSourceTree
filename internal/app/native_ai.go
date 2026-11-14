// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/ysicing/51code/internal/commitai"
	"github.com/ysicing/51code/internal/gitrepo"
	"github.com/ysicing/51code/locales"

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
	tooLarge  bool
	settings  bool
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

func (app *nativeApp) newAIClient(options commitai.Options) (*commitai.Client, error) {
	if options.Protocol != "" {
		account, err := commitai.KeyAccount(options)
		if err != nil {
			return nil, err
		}
		options.APIKey, err = app.aiKeys.Load(account)
		if err != nil {
			return nil, err
		}
	}
	return commitai.NewWithOptions(options)
}

func (app *nativeApp) canGenerateCommit() bool {
	if app.closed || app.busy || !app.writable || app.repository == nil ||
		app.screen != "changes" || app.repository.snapshot.Operation.Kind != "" ||
		app.aiEditor != nil && (app.aiEditor.keySaving || app.aiEditor.key != "") ||
		app.ai != nil && (app.ai.running || app.ai.tooLarge) {
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
		return
	}
	if app.prefs.AIConsent {
		app.generateCommitAI()
		return
	}
	path := app.repository.snapshot.Path
	detail := locales.AiConsentDetail()
	if app.prefs.AI.Protocol != "" {
		detail = locales.CustomAiConsent(app.prefs.AI.Endpoint)
	}
	app.dialog = &nativeDialog{
		title:  locales.AiConsentTitle(),
		detail: detail,
		action: locales.StartGeneration(),
		submit: func(_ *nativeDialog) {
			app.dialog = nil
			if app.repository == nil || app.repository.snapshot.Path != path {
				return
			}
			app.prefs.AIConsent = true
			if err := app.savePreferences(); err != nil {
				app.prefs.AIConsent = false
				app.problem = locales.SaveAiFailed(err.Error())
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
		var settings bool
		err := func() error {
			repo, err := app.service.manager.Get(path)
			if err != nil {
				return err
			}
			staged, err := repo.CommitContext(ctx)
			token = staged.Token
			if err != nil {
				return err
			}
			client, err := app.newAIClient(options)
			if err != nil {
				settings = true
				return err
			}
			defer client.Close()
			message, err = client.Generate(ctx, options, staged.Diff)
			if err != nil {
				var settingsError *commitai.SettingsError
				settings = errors.As(err, &settingsError)
				return err
			}
			current, err := repo.CommitToken(ctx)
			if err != nil {
				return err
			}
			if current != token {
				return errors.New(locales.IndexChanged())
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
				state.token, state.tooLarge, state.settings = token, errors.Is(err, gitrepo.ErrCommitContextTooLarge), settings
				if state.tooLarge {
					state.problem = locales.AiContextTooLarge()
				}
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
		return locales.GenerationCancelled()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return locales.AiTimeout()
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
		state.problem = locales.GeneratedDraftSaveFailed(err.Error())
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
				state.problem = locales.IndexChanged()
			case app.draftRevision != revision || app.prefs.Drafts[state.path] != draft:
				state.problem = locales.DraftEdited()
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
		state.problem = locales.UndoDraftSaveFailed(err.Error())
	}
}

func (app *nativeApp) commitAIHeader(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, locales.CommitMessage()).FontSize(13).Grow(1)
		if app.ai != nil && app.ai.running {
			ui.Text(c, locales.Generating()).FontSize(12).TextColor(c.Theme().TextMuted)
			if ui.Button(c, locales.CancelGeneration()).FontSize(12).Clicked() {
				app.stopCommitAI()
			}
		} else {
			label, tooltip := locales.GenerateAi(), locales.GenerateTooltip()
			if app.ai != nil {
				switch {
				case app.ai.tooLarge:
					tooltip = locales.AiContextLimitDetail()
				case app.ai.generated != "":
					label = locales.Regenerate()
				case app.ai.problem != "":
					label = locales.Retry()
				}
			}
			if ui.Button(c, label).FontSize(12).Disabled(!app.canGenerateCommit()).
				Tooltip(tooltip).Clicked() {
				app.requestCommitAI()
			}
		}
		iconButton(c, locales.AiGenerationOptions(), nativeIcons["more"]).
			Disabled(app.ai != nil && app.ai.running).Menu(func(menu *ui.Menu) {
			if menu.Item(locales.AiSettingsMenu()).Chosen() {
				app.aiSettingsDialog()
			}
		})
	})
}

func (app *nativeApp) commitAIResult(c *ui.Context) {
	state := app.ai
	if state == nil {
		return
	}
	if state.problem != "" {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			problem, color, detail := state.problem, c.Theme().Danger, state.problem
			if state.tooLarge {
				problem, color, detail = locales.AiContextTooLarge(), c.Theme().TextMuted, locales.AiContextLimitDetail()
			}
			ui.Text(c, problem).FontSize(12).TextColor(color).Grow(1).MinWidth(0).MaxLines(3).Tooltip(detail)
			if state.tooLarge {
				ui.Button(c, locales.WriteManually()).FontSize(12).OnClick(func() { app.commitInput.Focus(c) })
			} else if state.settings {
				if ui.Button(c, locales.GoToAiSettings()).FontSize(12).Clicked() {
					app.aiSettingsDialog()
				}
			}
		})
	}
	if state.generated == "" || state.running {
		return
	}
	if state.applied {
		ui.Row(c).FillWidth().Children(func() {
			if ui.Button(c, locales.UndoGeneration()).FontSize(12).
				Disabled(app.draftRevision != state.revision || app.prefs.Drafts[state.path] != state.generated).Clicked() {
				app.undoCommitAI()
			}
		})
		return
	}
	ui.Text(c, state.generated).FontSize(12).MaxLines(3).Selectable()
	ui.Row(c).FillWidth().Children(func() {
		if ui.Button(c, locales.UseSuggestion()).FontSize(12).Disabled(!app.canGenerateCommit()).Clicked() {
			app.adoptCommitAI()
		}
	})
}

type nativeAISettings struct {
	models      []string
	endpoint    string
	problem     string
	manual      bool
	connected   bool
	checking    bool
	cancel      context.CancelFunc
	serial      uint64
	key         string
	showKey     bool
	keyProblem  string
	keySaved    bool
	keySaving   bool
	manualModel bool
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
	if editor.key != "" || editor.keySaving {
		return
	}
	editor.reset()
	serial := editor.serial
	ctx, cancel := context.WithCancel(app.ctx)
	editor.cancel, editor.checking = cancel, true
	app.settingsError = ""
	options := app.prefs.AI
	go func() {
		defer cancel()
		var models []string
		var endpoint string
		err := func() error {
			var err error
			endpoint = options.Endpoint
			if options.Protocol != "" {
				endpoint, err = commitai.CustomEndpoint(options)
				if err != nil {
					return err
				}
			} else if strings.TrimSpace(endpoint) == "" {
				endpoint, err = commitai.DiscoverEndpoint()
				if err != nil {
					return err
				}
			}
			client, err := app.newAIClient(options)
			if err != nil {
				return err
			}
			defer client.Close()
			models, err = client.Models(ctx)
			return err
		}()
		app.post(func() {
			if app.closed || app.dialog != dialog || editor.serial != serial {
				return
			}
			editor.checking, editor.cancel = false, nil
			if err != nil && !errors.Is(err, commitai.ErrNoModels) {
				editor.problem = commitAIError(err)
				if options.Protocol != "" {
					editor.problem = locales.ModelListFailed(editor.problem)
				}
				return
			}
			editor.models, editor.endpoint, editor.connected = models, endpoint, true
			if len(models) == 0 {
				if options.Protocol == "" {
					editor.problem = locales.ConfigureMagpieModels()
				} else {
					editor.problem = locales.ModelListFailed(locales.NoModels())
				}
				return
			}
			model, err := commitai.SelectModel(models, app.prefs.AI.Model)
			if err != nil {
				editor.problem = err.Error()
				return
			}
			app.prefs.AI.Model = model
			app.settingsError = ""
			app.saveSettings()
		})
	}()
}

func (editor *nativeAISettings) saveKey(app *nativeApp, dialog *nativeDialog, clear bool) {
	if editor.checking {
		return
	}
	options, key := app.prefs.AI, strings.TrimSpace(editor.key)
	account, err := commitai.KeyAccount(options)
	if err != nil {
		editor.keyProblem = err.Error()
		return
	}
	if !clear && key == "" {
		return
	}
	app.stopCommitAI()
	editor.reset()
	serial := editor.serial
	editor.checking, editor.keySaving, editor.keyProblem, editor.keySaved = true, true, "", false
	go func() {
		var err error
		if clear {
			err = app.aiKeys.Delete(account)
		} else {
			err = app.aiKeys.Save(account, key)
		}
		app.post(func() {
			if app.closed || app.dialog != dialog || editor.serial != serial {
				return
			}
			editor.checking, editor.keySaving = false, false
			if err != nil {
				editor.keyProblem = err.Error()
				return
			}
			editor.key, editor.keySaved = "", !clear
		})
	}()
}

func (editor *nativeAISettings) reset() {
	editor.stop()
	editor.models, editor.endpoint, editor.problem, editor.connected = nil, "", "", false
}

func aiSettingsRow(c *ui.Context, label string, content func()) {
	ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(13).Width(112).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Children(content)
	})
}

func (editor *nativeAISettings) view(app *nativeApp, dialog *nativeDialog, c *ui.Context) {
	ui.Column(c).FillWidth().Gap(20).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.ConnectionAndModel()).FontSize(13).Bold()
			ui.Spacer(c)
			ui.Text(c, locales.AllRepositories()).FontSize(12).TextColor(c.Theme().TextMuted)
		})
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			aiSettingsRow(c, locales.ConnectionMethod(), func() {
				mode := locales.DiscoverMagpie()
				if editor.manual {
					mode = locales.ManualAddress()
				}
				if ui.Select(c, &mode, []string{locales.DiscoverMagpie(), locales.ManualAddress()}).
					Label(locales.ConnectionMethod()).FillWidth().Disabled(editor.checking || editor.key != "").Changed() {
					editor.manual = mode == locales.ManualAddress()
					editor.reset()
					editor.key, editor.keyProblem, editor.keySaved = "", "", false
					app.stopCommitAI()
					app.prefs.AIConsent = false
					app.settingsError = ""
					if !editor.manual {
						if app.prefs.AI.Protocol != "" {
							app.prefs.AI.Model = ""
						}
						app.prefs.AI.Endpoint = ""
						app.prefs.AI.Protocol = ""
						app.saveSettings()
					} else if app.prefs.AI.Endpoint == "" {
						app.prefs.AI.Protocol = commitai.Responses
						app.saveSettings()
					}
				}
			})
			if editor.manual {
				editor.connectionFields(app, dialog, c)
			}
			aiSettingsRow(c, locales.ConnectionStatus(), func() {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					status, label := locales.NotConnected(), locales.Connect()
					color := c.Theme().TextMuted
					switch {
					case editor.checking:
						status, label = locales.Connecting(), locales.ConnectingProgress()
						ui.Spinner(c).Size(14, 14)
					case editor.connected:
						status, label = locales.ConnectedModels(len(editor.models)), locales.Recheck()
					case editor.problem != "":
						status, label, color = locales.ConnectionFailed(), locales.Retry(), c.Theme().Danger
					}
					ui.Text(c, status).FontSize(13).TextColor(color).
						Grow(1).MinWidth(0).Tooltip(editor.endpoint)
					if editor.connected {
						if ui.Button(c, label).Disabled(editor.checking || editor.key != "").Clicked() {
							editor.check(app, dialog)
						}
					} else if ui.PrimaryButton(c, label).
						Disabled(editor.checking || editor.key != "" || (editor.manual && strings.TrimSpace(app.prefs.AI.Endpoint) == "")).Clicked() {
						editor.check(app, dialog)
					}
				})
			})
			if editor.problem != "" {
				aiSettingsRow(c, "", func() {
					ui.Text(c, editor.problem).FontSize(12).TextColor(c.Theme().Danger).Selectable()
				})
			}
			aiSettingsRow(c, locales.Model(), func() {
				if app.prefs.AI.Protocol != "" && len(editor.models) > 0 {
					if ui.Checkbox(c, &editor.manualModel, locales.ManualModel()).Disabled(editor.checking).Changed() {
						editor.problem = ""
					}
				}
				if app.prefs.AI.Protocol != "" && (editor.manualModel || len(editor.models) == 0) {
					if ui.TextInput(c, &app.prefs.AI.Model).Label(locales.AiModel()).Placeholder(locales.ModelIdHint()).FillWidth().Disabled(editor.checking).Changed() {
						editor.problem = ""
						app.saveSettings()
					}
					return
				}
				if len(editor.models) > 0 {
					if ui.Select(c, &app.prefs.AI.Model, editor.models).Label(locales.AiModel()).
						FillWidth().Tooltip(app.prefs.AI.Model).Disabled(editor.checking).Changed() {
						editor.problem, app.settingsError = "", ""
						app.saveSettings()
					}
				} else {
					model := app.prefs.AI.Model
					if model == "" {
						model = locales.ChooseModelAfterConnecting()
					}
					if editor.connected {
						model = locales.NoModels()
					}
					ui.Select(c, &model, []string{model}).Label(locales.AiModel()).FillWidth().Disabled(true)
				}
			})
		})
		ui.Divider(c)
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			ui.Text(c, locales.GenerationPreferences()).FontSize(13).Bold()
			if aiOutputPreferences(c, &app.prefs.AI.Language, &app.prefs.AI.Format, false) {
				app.settingsError = ""
				app.saveSettings()
			}
		})
		ui.Text(c, locales.StagedDataNotice()).FontSize(12).TextColor(c.Theme().TextMuted)
		if !app.prefs.AIConsent && editor.connected && len(editor.models) > 0 && editor.problem == "" && app.settingsError == "" {
			ui.Text(c, locales.AiReady()).FontSize(12).TextColor(c.Theme().TextMuted)
		}
	})
}

func aiOutputPreferences(c *ui.Context, language, format *string, inherit bool) bool {
	languages := []string{commitai.Chinese, commitai.English}
	formats := []string{commitai.Conventional, commitai.Plain}
	languageLabels := []string{locales.SimplifiedChinese(), locales.English()}
	formatLabels := []string{commitai.Conventional, locales.PlainText()}
	if inherit {
		languages = append([]string{"跟随应用设置"}, languages...)
		formats = append([]string{"跟随应用设置"}, formats...)
		languageLabels = append([]string{locales.InheritApplication()}, languageLabels...)
		formatLabels = append([]string{locales.InheritApplication()}, formatLabels...)
	}
	changed := false
	aiSettingsRow(c, locales.Language(), func() {
		selected := languageLabels[max(0, slices.Index(languages, *language))]
		if ui.Select(c, &selected, languageLabels).Label(locales.CommitLanguage()).FillWidth().Changed() {
			*language = languages[slices.Index(languageLabels, selected)]
			changed = true
		}
	})
	aiSettingsRow(c, locales.Format(), func() {
		selected := formatLabels[max(0, slices.Index(formats, *format))]
		if ui.Select(c, &selected, formatLabels).Label(locales.CommitFormat()).FillWidth().Changed() {
			*format = formats[slices.Index(formatLabels, selected)]
			changed = true
		}
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
