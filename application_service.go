package main

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
