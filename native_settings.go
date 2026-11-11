// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
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
	dialog := &nativeDialog{title: "应用设置", width: 740, dismiss: "取消"}
	tab := initialTab
	identity := &nativeIdentityEditor{}
	ai := &nativeAISettings{}
	dialog.onClose = func() {
		ai.stop()
		identity.close(app, dialog)
	}
	dialog.submit = func(dialog *nativeDialog) { identity.save(app, dialog) }
	dialog.canSubmit = func() bool { return tab == 1 && identity.dirty() }
	dialog.body = func(c *ui.Context) {
		categories := []string{"外观", "Git", "快捷键", "更新", "AI"}
		ui.Row(c).FillWidth().Gap(24).AlignItems(ui.Stretch).Children(func() {
			tabs := ui.TabsBase(c, &tab, len(categories))
			tabs.List.Column().Label("应用设置分类").Width(140).Shrink(0).
				Gap(4).Padding(8).Radius(10).Background(c.Theme().Surface).Children(func() {
				for index, category := range categories {
					item := tabs.Tab(index).FillWidth().Padding(10, 12).Radius(7)
					if index == tab {
						item.Background(c.Theme().Accent.Alpha(0.12)).TextColor(c.Theme().Accent).FontWeight(600)
					} else if item.Hovered() {
						item.Background(c.Theme().SurfaceHover)
					}
					item.Children(func() { ui.Text(c, category).SingleLine() })
				}
			})
			if tabs.List.Changed() {
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
				dialog.action = "保存"
			}
			_, height := c.Size()
			contentHeight := min(400, max(100, height-240))
			ui.Column(c).Grow(1).MinWidth(0).Gap(20).Padding(4, 0).Children(func() {
				ui.Text(c, categories[tab]).FontSize(18).Bold()
				ui.Scroll(c).Key(tab).MaxHeight(contentHeight).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(16).Padding(2, 4, 6).Children(func() {
						switch tab {
						case 0:
							app.appearancePreferences(c)
						case 1:
							ui.Text(c, "保存到全局 Git 配置，也会影响其他 Git 工具。").FontSize(12).TextColor(c.Theme().TextMuted)
							identity.view(app, dialog, c)
						case 2:
							shortcutPreference(app, c, "本地仓库", &app.shortcutDraft.Library)
							shortcutPreference(app, c, "添加本地仓库", &app.shortcutDraft.AddRepository)
							shortcutPreference(app, c, "扫描文件夹", &app.shortcutDraft.Scan)
							shortcutPreference(app, c, "刷新", &app.shortcutDraft.Refresh)
							shortcutPreference(app, c, "提交", &app.shortcutDraft.Commit)
							ui.Row(c).FillWidth().Children(func() {
								if ui.Button(c, "恢复默认快捷键").Clicked() {
									app.shortcutDraft = defaultNativeShortcuts()
									app.applyShortcutDraft()
								}
							})
						case 3:
							if ui.Checkbox(c, &app.settingsAutoChecks, "自动检查更新").Changed() {
								updater.SetAutomaticChecks(app.settingsAutoChecks)
							}
							ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
								ui.Text(c, "检查频率").FontSize(13).Grow(1)
								choice := ui.Select(c, &app.prefs.UpdateCheckFrequency, nativeUpdateCheckFrequencies).
									Label("检查频率").Width(180).Disabled(!app.settingsAutoChecks)
								if choice.Changed() {
									app.settingsError = ""
									app.saveSettings()
								}
							})
							ui.Text(c, "检查频率更改将在下次启动后生效。").FontSize(12).TextColor(c.Theme().TextMuted)
							if ui.Checkbox(c, &app.settingsAutoDownloads, "自动下载更新（下次启动时安装）").Changed() {
								updater.SetAutomaticDownloads(app.settingsAutoDownloads)
							}
							lastCheck := updater.LastCheck()
							lastCheckLabel := "尚未检查"
							if !lastCheck.IsZero() {
								lastCheckLabel = lastCheck.Local().Format("2006-01-02 15:04")
							}
							ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
								ui.Text(c, "上次检查").FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
								ui.Text(c, lastCheckLabel).FontSize(12).TextColor(c.Theme().TextMuted)
							})
							if app.availableVersion != "" {
								ui.Text(c, "新版本 v"+app.availableVersion).FontSize(13).TextColor(c.Theme().Accent)
							}
							ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
								if ui.Button(c, "立即检查更新").Clicked() {
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
	ui.RadioGroup(c, func() {
		for _, appearance := range []string{appearanceSystem, appearanceLight, appearanceDark} {
			selected := app.prefs.Appearance == appearance
			option := ui.RadioBase(c, &app.prefs.Appearance, appearance).Label(appearance).
				Column().Grow(1).Shrink(1).MinWidth(0).Padding(8).Gap(10).Radius(10).
				Border(2, ui.Transparent)
			switch {
			case selected:
				option.BorderColor(c.Theme().Accent).Background(c.Theme().Accent.Alpha(0.06))
			case option.Hovered():
				option.Background(c.Theme().SurfaceHover)
			}
			option.Children(func() {
				themes := []*ui.Theme{ui.LightTheme(), ui.DarkTheme()}
				if appearance == appearanceLight {
					themes = themes[:1]
				} else if appearance == appearanceDark {
					themes = themes[1:]
				}
				ui.Row(c).FillWidth().Height(80).Radius(6).Clip().Border(1, c.Theme().Border).AccessHidden(true).Children(func() {
					for _, theme := range themes {
						ui.Themed(c, theme).Grow(1).FillHeight().Center().Children(func() {
							ui.Text(c, "Aa").FontSize(26).Bold()
						})
					}
				})
				ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Text(c, appearance).FontSize(13).Grow(1).SingleLine()
					mark := ui.Icon(c, nativeIcons["check"]).FontSize(14).AccessHidden(true)
					if !selected {
						mark.Opacity(0)
					}
				})
			})
			if option.Changed() {
				app.settingsError = ""
				app.applyAppearance()
				app.saveSettings()
			}
		}
	}).Row().FillWidth().Gap(8).Label("外观模式")
}

func (app *nativeApp) applyAppearance() {
	if app.win == nil {
		return
	}
	if err := app.win.SetBackgroundColor(appearanceBackground(app.prefs.Appearance)); err != nil {
		app.settingsError = fmt.Sprintf("无法应用外观：%v", err)
		return
	}
	app.win.SetWindowButtonsTheme(appearanceThemeSource(app.prefs.Appearance))
}

func (app *nativeApp) shortcutDraftChanged() {
	if !shortcutsUnique(app.shortcutDraft) {
		app.settingsError = "快捷键不能重复，请为每项选择不同组合。"
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
		app.settingsError = fmt.Sprintf("无法保存设置：%v", err)
	}
}

func shortcutPreference(app *nativeApp, c *ui.Context, label string, value *string) {
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(13).Grow(1)
		choice := ui.Select(c, value, nativeShortcutChoices).Label(label + "快捷键").Width(180)
		if choice.Changed() {
			app.shortcutDraftChanged()
		}
	})
}
