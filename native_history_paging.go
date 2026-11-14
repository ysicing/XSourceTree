// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo/ui"
)

func (app *nativeApp) historyReadControls(c *ui.Context) {
	read := app.readTask
	if read == nil || read.kind != nativeReadHistory && read.kind != nativeReadHistoryPage {
		return
	}
	ui.Spinner(c).Size(14, 14).Label(read.label)
	label := locales.CancelSearch()
	if read.kind == nativeReadHistoryPage {
		label = locales.Cancel()
	}
	ui.Button(c, label).Height(24).FontSize(11).OnClick(func() {
		if app.readTask != read {
			return
		}
		if read.kind == nativeReadHistoryPage {
			app.historyPagePaused = true
			app.cancelRead()
		} else {
			read.cancel()
		}
	})
}

func (app *nativeApp) loadHistoryNearEnd(c *ui.Context) {
	_, last := app.historyList.Visible()
	if app.busy || app.dialog != nil || app.readTask != nil || !app.historyLoaded ||
		!app.historyMore || app.historyPagePaused || last < 0 {
		return
	}
	if last < len(app.history)+app.historyWorkingOffset()-20 {
		return
	}
	app.searchHistory(true)
	c.Invalidate()
}
