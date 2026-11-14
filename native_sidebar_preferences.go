// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo/ui"
)

type nativeSidebarItems struct {
	Search    bool `json:"search"`
	Compare   bool `json:"compare"`
	Stash     bool `json:"stash"`
	Worktrees bool `json:"worktrees"`
}

func (app *nativeApp) sidebarPreference(c *ui.Context, screen, label string, visible *bool) {
	c.Key("sidebar-preference:" + screen)
	previous := *visible
	ui.Checkbox(c, visible, label).OnClick(func() {
		app.settingsError = ""
		if err := app.savePreferences(); err != nil {
			*visible = previous
			app.settingsError = locales.SaveSettingsError(err.Error())
			return
		}
		if !*visible && app.screen == screen {
			app.switchWorkspace("changes")
		}
	})
}
