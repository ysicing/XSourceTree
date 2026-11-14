// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import "github.com/egoist/mygo"

func preserveLegacyApplicationIdentity(app *mygo.Application) error {
	legacyName := "XSourceTree"
	if app.Name() == "51Code Dev" {
		legacyName += " Dev"
	}
	app.SetName(legacyName)
	directory, err := app.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	app.SetPath(mygo.PathUserData, directory)
	return nil
}
