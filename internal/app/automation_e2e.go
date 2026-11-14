// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

//go:build e2e

package app

import (
	"log"
	"os"

	"github.com/egoist/mygo"

	_ "github.com/egoist/mygo/plugins/automation"
)

func init() {
	if os.Getenv("MYGO_AUTOMATION") == "" {
		return
	}
	dir, err := os.MkdirTemp("", "xsourcetree-e2e-data-")
	if err != nil {
		log.Fatal(err)
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	mygo.App.OnQuit(func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("清理测试应用数据：%v", err)
		}
	})
}
