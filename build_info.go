// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo"
)

var (
	buildNumber string
	buildCommit string
	buildTime   string
)

func buildVersionLabel() string {
	version := mygo.App.Version()
	if version == "" {
		return "关于"
	}
	label := "v" + version
	if buildNumber != "" {
		label += " (" + buildNumber + ")"
	}
	return label
}

func showNativeAbout() {
	mygo.App.ShowAboutPanel(mygo.AboutPanelOptions{
		ApplicationVersion: mygo.App.Version(),
		Version:            buildNumber,
		Credits:            buildDetails(),
	})
}

func buildDetails() string {
	var details []string
	if buildNumber != "" {
		details = append(details, "构建号："+buildNumber)
	}
	if buildCommit != "" {
		details = append(details, "提交："+buildCommit)
	}
	if buildTime != "" {
		formatted := buildTime
		if timestamp, err := time.Parse(time.RFC3339, buildTime); err == nil {
			formatted = timestamp.Local().Format("2006-01-02 15:04:05 -07:00")
		}
		details = append(details, "构建时间："+formatted)
	}
	return strings.Join(details, "\n")
}
