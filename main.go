// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import "github.com/ysicing/51code/internal/app"

var (
	buildNumber string
	buildCommit string
	buildTime   string
)

func main() {
	app.Run(app.BuildInfo{
		Number: buildNumber,
		Commit: buildCommit,
		Time:   buildTime,
	})
}
