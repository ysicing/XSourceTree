// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (app *nativeApp) versionButton(c *ui.Context) bool {
	label := "检查更新"
	text := buildVersionLabel()
	color := c.Theme().TextMuted
	tooltip := label + "\n当前版本：" + text
	if app.availableVersion != "" {
		text = "新版本 v" + app.availableVersion
		label = text + "，检查更新"
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
