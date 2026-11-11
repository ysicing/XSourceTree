// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

type nativeIdentityEditor struct {
	path     string
	settings gitrepo.IdentitySettings
	name     string
	email    string
	override bool
	ready    bool
	saved    bool
}

func identityDraft(value gitrepo.IdentityValue, direct []string) string {
	if len(direct) > 0 {
		return direct[len(direct)-1]
	}
	return value.Value
}

func (editor *nativeIdentityEditor) adopt(settings gitrepo.IdentitySettings) {
	editor.settings = settings
	editor.name = identityDraft(settings.Name, settings.DirectName)
	editor.email = identityDraft(settings.Email, settings.DirectEmail)
	editor.override = settings.Overrides()
	editor.ready = true
}

func (editor *nativeIdentityEditor) dirty() bool {
	if !editor.ready {
		return false
	}
	if editor.path != "" && !editor.override {
		return editor.settings.Overrides()
	}
	return editor.path != "" && !editor.settings.Overrides() ||
		editor.name != identityDraft(editor.settings.Name, editor.settings.DirectName) ||
		editor.email != identityDraft(editor.settings.Email, editor.settings.DirectEmail)
}

func (editor *nativeIdentityEditor) load(app *nativeApp, dialog *nativeDialog) {
	dialog.problem = ""
	app.run(locales.ReadingIdentity(), func(ctx context.Context) (func(), error) {
		settings, err := app.service.Identity(ctx, editor.path)
		return func() {
			if err == nil && app.dialog == dialog {
				editor.adopt(settings)
			}
		}, err
	})
}

func (editor *nativeIdentityEditor) save(app *nativeApp, dialog *nativeDialog) {
	if !editor.dirty() || editor.path != "" && !app.writable {
		return
	}
	request := gitrepo.IdentityRequest{
		Name: editor.name, Email: editor.email, Token: editor.settings.Token,
		Inherit: editor.path != "" && !editor.override,
	}
	app.run(locales.SavingIdentity(), func(ctx context.Context) (func(), error) {
		if err := app.service.SaveIdentity(ctx, editor.path, request); err != nil {
			return nil, err
		}
		settings, err := app.service.Identity(ctx, editor.path)
		if err != nil {
			return func() { editor.ready = false }, fmt.Errorf("%s%w", locales.ReloadIdentityFailed(""), err)
		}
		return func() {
			if app.dialog == dialog {
				editor.adopt(settings)
				editor.saved = true
			}
		}, nil
	})
}

func (editor *nativeIdentityEditor) view(app *nativeApp, dialog *nativeDialog, c *ui.Context) {
	if !editor.ready {
		if app.busy {
			ui.Text(c, locales.ReadingGitConfig()).TextColor(c.Theme().TextMuted)
		} else if ui.Button(c, locales.Reload()).Clicked() {
			editor.load(app, dialog)
		}
		return
	}
	if editor.path != "" {
		if ui.Checkbox(c, &editor.override, locales.OverrideIdentity()).Disabled(!app.writable).Changed() {
			editor.saved = false
		}
		if !editor.override {
			ui.Text(c, locales.InheritedGitConfig()).FontSize(12).TextColor(c.Theme().TextMuted)
		}
		if editor.settings.Name.Scope == "worktree" || editor.settings.Email.Scope == "worktree" {
			ui.Text(c, locales.WorktreeIdentityNotice()).FontSize(12).TextColor(c.Theme().TextMuted)
		}
	}
	if editor.path == "" || editor.override {
		ui.Column(c).FillWidth().Gap(12).Disabled(editor.path != "" && !app.writable).Children(func() {
			for _, field := range []struct {
				label string
				value *string
			}{{locales.Name(), &editor.name}, {locales.Email(), &editor.email}} {
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					ui.Text(c, field.label).FontSize(12)
					if ui.TextInput(c, field.value).Label(field.label).FillWidth().Changed() {
						editor.saved = false
					}
				})
			}
		})
	}
	ui.Divider(c)
	ui.Text(c, locales.EffectiveIdentity()).FontSize(12).Bold()
	for _, field := range []struct {
		label string
		value gitrepo.IdentityValue
	}{{locales.Name(), editor.settings.Name}, {locales.Email(), editor.settings.Email}} {
		value := field.value.Value
		if value == "" {
			value = locales.Unconfigured()
		}
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			ui.Text(c, field.label).FontSize(12).TextColor(c.Theme().TextMuted).Width(50)
			ui.Column(c).FillWidth().MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, value).FontSize(13).MaxLines(1).Selectable().Tooltip(value)
				if field.value.Origin != "" {
					source := identitySource(field.value)
					ui.Text(c, source).FontSize(11).TextColor(c.Theme().TextMuted).MaxLines(1).Tooltip(field.value.Origin)
				}
			})
		})
	}
	if editor.saved {
		ui.Text(c, locales.Saved()).FontSize(12).TextColor(c.Theme().TextMuted)
	} else if editor.dirty() {
		ui.Text(c, locales.SaveToApply()).FontSize(12).TextColor(c.Theme().TextMuted)
	}
}

func identitySource(value gitrepo.IdentityValue) string {
	scope := map[string]string{
		"system": locales.SystemScope(), "global": locales.GlobalScope(), "local": locales.Repository(), "worktree": locales.WorktreeScope(), "command": locales.CommandScope(),
	}[value.Scope]
	if scope == "" {
		scope = value.Scope
	}
	origin := strings.TrimPrefix(value.Origin, "file:")
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(origin, home+"/") {
		origin = "~/" + strings.TrimPrefix(origin, home+"/")
	}
	return scope + " · " + origin
}

func (editor *nativeIdentityEditor) close(app *nativeApp, dialog *nativeDialog) {
	if !editor.dirty() {
		app.dialog = dialog.parent
		return
	}
	app.confirm(locales.DiscardUnsavedTitle(), locales.DiscardIdentityDetail(), locales.DiscardEdits(), func() {
		app.dialog = dialog.parent
	})
	app.dialog.parent = dialog
}

func (app *nativeApp) repositorySettingsDialog() {
	if app.busy || app.dialog != nil {
		return
	}
	if app.repository == nil || app.screen == "library" {
		app.problem = locales.OpenRepositoryFirst()
		return
	}
	identity := &nativeIdentityEditor{path: app.repository.snapshot.Path}
	app.settingsError = ""
	tab := 0
	dialog := &nativeDialog{
		title:  locales.RepositorySettingsTitle(app.repository.snapshot.Name),
		detail: app.repository.snapshot.Path, dismiss: locales.Cancel(),
	}
	dialog.onClose = func() { identity.close(app, dialog) }
	dialog.submit = func(dialog *nativeDialog) { identity.save(app, dialog) }
	dialog.canSubmit = func() bool { return tab == 0 && app.writable && identity.dirty() }
	dialog.body = func(c *ui.Context) {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			ui.Segmented(c, &tab, locales.CommitIdentity(), locales.Remotes(), "AI").Label(locales.RepositorySettingsCategories())
			dialog.action = ""
			if tab == 0 {
				dialog.action = locales.Save()
			}
			_, height := c.Size()
			contentHeight := min(390, max(120, height-280))
			ui.Scroll(c).Key(tab).MinHeight(min(240, contentHeight)).MaxHeight(contentHeight).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					if tab == 0 {
						identity.view(app, dialog, c)
					} else if tab == 1 {
						app.remoteSettingsView(c)
					} else {
						app.repositoryAISettings(c, identity.path)
					}
				})
			})
		})
	}
	app.dialog = dialog
	identity.load(app, dialog)
}
