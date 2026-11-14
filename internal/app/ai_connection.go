// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"slices"
	"strings"

	"github.com/ysicing/51code/internal/commitai"
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo/ui"
)

func (editor *nativeAISettings) connectionFields(app *nativeApp, dialog *nativeDialog, c *ui.Context) {
	aiSettingsRow(c, locales.ApiProtocol(), func() {
		values := []string{"", commitai.Responses, commitai.Chat, commitai.Claude}
		labels := []string{locales.LocalMagpie(), "OpenAI Responses", "OpenAI Chat Completions", "Claude Messages"}
		selected := labels[max(0, slices.Index(values, app.prefs.AI.Protocol))]
		if ui.Select(c, &selected, labels).Label(locales.ApiProtocol()).FillWidth().Disabled(editor.checking || editor.key != "").Changed() {
			app.prefs.AI.Protocol = values[slices.Index(labels, selected)]
			app.prefs.AIConsent = false
			app.stopCommitAI()
			editor.key, editor.keyProblem, editor.keySaved = "", "", false
			editor.reset()
			app.saveSettings()
		}
	})
	aiSettingsRow(c, locales.ServiceAddress(), func() {
		label, placeholder := locales.MagpieAddress(), "http://127.0.0.1:3425/v1"
		if app.prefs.AI.Protocol != "" {
			label, placeholder = locales.ServiceAddress(), "https://api.openai.com/v1"
			if app.prefs.AI.Protocol == commitai.Claude {
				placeholder = "https://api.anthropic.com/v1"
			}
		}
		if ui.TextInput(c, &app.prefs.AI.Endpoint).Label(label).Placeholder(placeholder).FillWidth().Disabled(editor.checking || editor.key != "").Changed() {
			editor.reset()
			editor.key, editor.keyProblem, editor.keySaved = "", "", false
			app.prefs.AIConsent = false
			app.stopCommitAI()
			app.settingsError = ""
			app.saveSettings()
		}
	})
	if app.prefs.AI.Protocol == "" {
		return
	}
	_, endpointErr := commitai.KeyAccount(app.prefs.AI)
	aiSettingsRow(c, locales.ApiKey(), func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				input := ui.TextInput(c, &editor.key).Label(locales.ApiKey()).Placeholder(locales.KeychainKeyHint()).Grow(1).MinWidth(0).Disabled(editor.checking || endpointErr != nil)
				if !editor.showKey {
					input.Password()
				}
				if input.Changed() {
					editor.keySaved, editor.keyProblem = false, ""
				}
				ui.Checkbox(c, &editor.showKey, locales.ShowApiKey()).Disabled(editor.checking)
			})
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, locales.SaveApiKey()).Disabled(editor.checking || strings.TrimSpace(editor.key) == "").Clicked() {
					editor.saveKey(app, dialog, false)
				}
				if editor.key != "" {
					if ui.Button(c, locales.Cancel()).Disabled(editor.checking).Clicked() {
						editor.key, editor.keyProblem = "", ""
					}
				} else if ui.Button(c, locales.ClearApiKey()).Disabled(editor.checking || endpointErr != nil).Clicked() {
					editor.saveKey(app, dialog, true)
				}
				if editor.keySaved {
					ui.Text(c, locales.ApiKeySaved()).FontSize(12).TextColor(c.Theme().TextMuted)
				}
			})
			if editor.keyProblem != "" {
				ui.Text(c, editor.keyProblem).FontSize(12).TextColor(c.Theme().Danger)
			}
		})
	})
}
