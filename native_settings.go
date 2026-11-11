// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"slices"
	"strings"
	"time"

	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

const (
	appearanceSystem = "跟随系统"
	appearanceLight  = "浅色"
	appearanceDark   = "深色"

	shortcutLibrary = "shortcut-library"
	shortcutAddRepo = "shortcut-add-repository"
	shortcutScan    = "shortcut-scan"
	shortcutRefresh = "shortcut-refresh"
	shortcutCommit  = "shortcut-commit"
)

type nativeShortcuts struct {
	Library       string `json:"library"`
	AddRepository string `json:"addRepository"`
	Scan          string `json:"scan"`
	Refresh       string `json:"refresh"`
	Commit        string `json:"commit"`
}

var nativeShortcutChoices = []string{
	"⌘⇧L",
	"⌘O",
	"⌘⇧O",
	"⌘R",
	"⌘↩",
	"⌘⇧R",
	"⌘⇧↩",
	"⌥⌘L",
	"⌥⌘R",
}

var nativeUpdateCheckFrequencies = []string{"每小时", "每 6 小时", "每天", "每周"}

func nativeUpdateCheckInterval(frequency string) time.Duration {
	switch frequency {
	case "每小时":
		return time.Hour
	case "每 6 小时":
		return 6 * time.Hour
	case "每周":
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func defaultNativeShortcuts() nativeShortcuts {
	return nativeShortcuts{
		Library: "⌘⇧L", AddRepository: "⌘O", Scan: "⌘⇧O", Refresh: "⌘R", Commit: "⌘↩",
	}
}

func appearanceBackground(appearance string) string {
	switch appearance {
	case appearanceLight:
		return "#ffffff"
	case appearanceDark:
		return "#191b20"
	default:
		return "light-dark(#ffffff, #191b20)"
	}
}

func appearanceThemeSource(appearance string) mygo.ThemeSource {
	switch appearance {
	case appearanceLight:
		return mygo.ThemeLight
	case appearanceDark:
		return mygo.ThemeDark
	default:
		return mygo.ThemeSystem
	}
}

func normalizeNativePreferences(prefs *nativePreferences) {
	if !slices.Contains(nativeUpdateCheckFrequencies, prefs.UpdateCheckFrequency) {
		prefs.UpdateCheckFrequency = "每天"
	}
	if prefs.Appearance != appearanceSystem && prefs.Appearance != appearanceLight && prefs.Appearance != appearanceDark {
		prefs.Appearance = appearanceSystem
	}
	defaults := defaultNativeShortcuts()
	if prefs.Shortcuts.Library == "" {
		prefs.Shortcuts.Library = defaults.Library
	}
	if prefs.Shortcuts.AddRepository == "" {
		prefs.Shortcuts.AddRepository = defaults.AddRepository
	}
	if prefs.Shortcuts.Scan == "" {
		prefs.Shortcuts.Scan = defaults.Scan
	}
	if prefs.Shortcuts.Refresh == "" {
		prefs.Shortcuts.Refresh = defaults.Refresh
	}
	if prefs.Shortcuts.Commit == "" {
		prefs.Shortcuts.Commit = defaults.Commit
	}
	if !shortcutsAvailable(prefs.Shortcuts) || !shortcutsUnique(prefs.Shortcuts) {
		prefs.Shortcuts = defaults
	}
}

func shortcutAccelerator(shortcut string) string {
	return strings.NewReplacer(
		"⌘", "CmdOrCtrl+",
		"⇧", "Shift+",
		"⌥", "Alt+",
		"↩", "Enter",
	).Replace(shortcut)
}

func shortcutsUnique(shortcuts nativeShortcuts) bool {
	values := []string{
		shortcuts.Library, shortcuts.AddRepository, shortcuts.Scan, shortcuts.Refresh, shortcuts.Commit,
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func shortcutsAvailable(shortcuts nativeShortcuts) bool {
	available := make(map[string]struct{}, len(nativeShortcutChoices))
	for _, choice := range nativeShortcutChoices {
		available[choice] = struct{}{}
	}
	for _, value := range []string{
		shortcuts.Library, shortcuts.AddRepository, shortcuts.Scan, shortcuts.Refresh, shortcuts.Commit,
	} {
		if _, exists := available[value]; !exists {
			return false
		}
	}
	return true
}

func applyNativeShortcuts(shortcuts nativeShortcuts) {
	menu := mygo.App.Menu()
	if menu == nil {
		return
	}
	values := map[string]string{
		shortcutLibrary: shortcutAccelerator(shortcuts.Library),
		shortcutAddRepo: shortcutAccelerator(shortcuts.AddRepository),
		shortcutScan:    shortcutAccelerator(shortcuts.Scan),
		shortcutRefresh: shortcutAccelerator(shortcuts.Refresh),
		shortcutCommit:  shortcutAccelerator(shortcuts.Commit),
	}
	for id, accelerator := range values {
		if item := menu.ItemByID(id); item != nil {
			item.SetAccelerator(accelerator)
		}
	}
}

func (app *nativeApp) settingsDialog() {
	app.settingsDialogAt(0)
}

func (app *nativeApp) aiSettingsDialog() {
	app.settingsDialogAt(4)
}

func (app *nativeApp) settingsDialogAt(initialTab int) {
	if app.busy || app.dialog != nil {
		return
	}
	app.settingsAutoChecks = updater.AutomaticChecks()
	app.settingsAutoDownloads = updater.AutomaticDownloads()
	app.shortcutDraft = app.prefs.Shortcuts
	app.settingsError = ""
	dialog := &nativeDialog{title: locales.ApplicationSettings(), titleText: locales.ApplicationSettings, width: 620, height: 520, dismiss: locales.Cancel()}
	tab := initialTab
	identity := &nativeIdentityEditor{}
	ai := &nativeAISettings{manual: app.prefs.AI.Endpoint != ""}
	dialog.onClose = func() {
		ai.stop()
		identity.close(app, dialog)
	}
	dialog.submit = func(dialog *nativeDialog) { identity.save(app, dialog) }
	dialog.canSubmit = func() bool { return tab == 1 && identity.dirty() }
	dialog.body = func(c *ui.Context) {
		categories := []string{locales.General(), "Git", locales.Shortcuts(), locales.Updates(), "AI"}
		ui.Column(c).Fill().MinHeight(0).Gap(16).Children(func() {
			if ui.Tabs(c, &tab, categories...).Label(locales.SettingsCategories()).Changed() {
				dialog.problem = ""
				if tab != 4 {
					ai.stop()
				}
				if tab == 1 && !identity.ready {
					identity.load(app, dialog)
				}
			}
			dialog.action = ""
			if tab == 1 {
				dialog.action = locales.Save()
			}
			ui.Column(c).Grow(1).MinHeight(0).MinWidth(0).Gap(20).Padding(4, 0).Children(func() {
				if tab != 4 {
					ui.Text(c, categories[tab]).FontSize(18).Bold()
				}
				ui.Scroll(c).Key(tab).Grow(1).MinHeight(0).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(16).Padding(2, 4, 6).Children(func() {
						switch tab {
						case 0:
							app.generalPreferences(c)
						case 1:
							ui.Text(c, locales.GlobalGitScope()).FontSize(12).TextColor(c.Theme().TextMuted)
							identity.view(app, dialog, c)
						case 2:
							shortcutPreference(app, c, locales.LocalRepositories(), &app.shortcutDraft.Library)
							shortcutPreference(app, c, locales.AddLocalRepository(), &app.shortcutDraft.AddRepository)
							shortcutPreference(app, c, locales.ScanFolders(), &app.shortcutDraft.Scan)
							shortcutPreference(app, c, locales.Refresh(), &app.shortcutDraft.Refresh)
							shortcutPreference(app, c, locales.Commit(), &app.shortcutDraft.Commit)
							ui.Row(c).FillWidth().Children(func() {
								if ui.Button(c, locales.RestoreShortcuts()).Clicked() {
									app.shortcutDraft = defaultNativeShortcuts()
									app.applyShortcutDraft()
								}
							})
						case 3:
							if ui.Checkbox(c, &app.settingsAutoChecks, locales.AutomaticUpdates()).Changed() {
								updater.SetAutomaticChecks(app.settingsAutoChecks)
							}
							ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
								ui.Text(c, locales.UpdateFrequency()).FontSize(13).Grow(1)
								frequencies := []string{locales.Hourly(), locales.SixHourly(), locales.Daily(), locales.Weekly()}
								selected := frequencies[slices.Index(nativeUpdateCheckFrequencies, app.prefs.UpdateCheckFrequency)]
								choice := ui.Select(c, &selected, frequencies).
									Label(locales.UpdateFrequency()).Width(180).Disabled(!app.settingsAutoChecks)
								if choice.Changed() {
									app.prefs.UpdateCheckFrequency = nativeUpdateCheckFrequencies[slices.Index(frequencies, selected)]
									app.settingsError = ""
									app.saveSettings()
								}
							})
							ui.Text(c, locales.FrequencyNextLaunch()).FontSize(12).TextColor(c.Theme().TextMuted)
							if ui.Checkbox(c, &app.settingsAutoDownloads, locales.AutomaticDownloads()).Changed() {
								updater.SetAutomaticDownloads(app.settingsAutoDownloads)
							}
							lastCheck := updater.LastCheck()
							lastCheckLabel := locales.NeverChecked()
							if !lastCheck.IsZero() {
								lastCheckLabel = lastCheck.Local().Format("2006-01-02 15:04")
							}
							ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
								ui.Text(c, locales.LastCheck()).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
								ui.Text(c, lastCheckLabel).FontSize(12).TextColor(c.Theme().TextMuted)
							})
							if app.availableVersion != "" {
								ui.Text(c, locales.NewVersion(app.availableVersion)).FontSize(13).TextColor(c.Theme().Accent)
							}
							ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
								if ui.Button(c, locales.CheckNow()).Clicked() {
									updater.CheckForUpdates()
								}
							})
						case 4:
							ai.view(app, dialog, c)
						}
					})
				})
			})
			if tab != 1 {
				dialog.problem = app.settingsError
			}
		})
	}
	app.dialog = dialog
}

func (app *nativeApp) appearancePreferences(c *ui.Context) {
	appearances := []string{appearanceSystem, appearanceLight, appearanceDark}
	labels := []string{locales.SystemAppearance(), locales.LightAppearance(), locales.DarkAppearance()}
	selected := slices.Index(appearances, app.prefs.Appearance)
	if ui.Segmented(c, &selected, labels...).Label(locales.AppearanceMode()).AlignSelf(ui.Start).Changed() {
		app.prefs.Appearance = appearances[selected]
		app.settingsError = ""
		app.applyAppearance()
		app.saveSettings()
	}
}

var nativeLanguageTags = []string{"", "zh-Hans", "zh-Hant", "en"}

func (app *nativeApp) generalPreferences(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, locales.Appearance()).FontSize(13)
		app.appearancePreferences(c)
	})
	ui.Divider(c)
	ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.Language()).FontSize(13).Grow(1)
		labels := []string{locales.AutomaticLanguage(), "简体中文", "繁體中文", "English"}
		tag := app.chosenLanguage
		if tag != "" {
			tag = i18n.Match([]string{tag}, nativeLanguageTags[1:])
		}
		index := max(0, slices.Index(nativeLanguageTags, tag))
		selected := labels[index]
		c.Key("application-language")
		if ui.Select(c, &selected, labels).Label(locales.Language()).Width(200).Changed() {
			app.chosenLanguage = nativeLanguageTags[slices.Index(labels, selected)]
			app.chooseLanguage(app.chosenLanguage)
		}
	})
}

func (app *nativeApp) applyAppearance() {
	if app.win == nil {
		return
	}
	if err := app.win.SetBackgroundColor(appearanceBackground(app.prefs.Appearance)); err != nil {
		app.settingsError = locales.AppearanceError(err.Error())
		return
	}
	app.win.SetWindowButtonsTheme(appearanceThemeSource(app.prefs.Appearance))
}

func (app *nativeApp) shortcutDraftChanged() {
	if !shortcutsUnique(app.shortcutDraft) {
		app.settingsError = locales.DuplicateShortcuts()
		return
	}
	app.prefs.Shortcuts = app.shortcutDraft
	app.settingsError = ""
	applyNativeShortcuts(app.prefs.Shortcuts)
	app.saveSettings()
}

func (app *nativeApp) applyShortcutDraft() {
	app.shortcutDraftChanged()
}

func (app *nativeApp) saveSettings() {
	if err := app.savePreferences(); err != nil {
		app.settingsError = locales.SaveSettingsError(err.Error())
	}
}

func shortcutPreference(app *nativeApp, c *ui.Context, label string, value *string) {
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(13).Grow(1)
		choice := ui.Select(c, value, nativeShortcutChoices).Label(locales.ShortcutLabel(label)).Width(180)
		if choice.Changed() {
			app.shortcutDraftChanged()
		}
	})
}
