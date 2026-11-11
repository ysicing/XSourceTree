// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"

	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

func (app *nativeApp) versionButton(c *ui.Context) bool {
	label := locales.CheckUpdates()
	text := buildVersionLabel()
	color := c.Theme().TextMuted
	tooltip := label + "\n" + locales.CurrentVersion(text)
	if app.availableVersion != "" {
		text = locales.NewVersion(app.availableVersion)
		label = locales.NewVersionCheck(text)
		tooltip += "\n" + text
		color = c.Theme().Accent
	}
	tooltip = strings.TrimSpace(tooltip + "\n" + buildDetails())
	button := ui.ButtonBase(c).Label(label).Tooltip(tooltip).
		Padding(4, 6).Gap(4).Radius(c.Theme().Radius).Shrink(0).TextColor(color)
	if button.Pressed() {
		button.Background(c.Theme().SurfacePressed)
	} else if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	return button.Children(func() {
		if app.availableVersion != "" {
			ui.Icon(c, nativeIcons["pull"]).FontSize(12)
		}
		ui.Text(c, text).FontSize(11).SingleLine()
	}).Clicked()
}
