// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

type ApplicationService struct{}

func (*ApplicationService) Version() string {
	return mygo.App.Version()
}

func (*ApplicationService) CheckForUpdates() {
	updater.CheckForUpdates()
}
