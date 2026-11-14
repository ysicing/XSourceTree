// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import "github.com/egoist/mygo"

func protectWindowClose(win *mygo.Window, app *nativeApp) {
	checking := false
	win.OnClose(func(event *mygo.CloseEvent) {
		if checking {
			event.PreventDefault()
			return
		}
		checking = true
		defer func() { checking = false }()
		if !app.prepareClose() {
			event.PreventDefault()
		}
	})
}
