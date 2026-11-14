// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"testing"

	"github.com/ysicing/XSourceTree/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func TestNativeLargeFileListBuildsVisibleRows(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.repository.snapshot.Files = nil
	for index := 0; index < 5000; index++ {
		app.repository.snapshot.Files = append(app.repository.snapshot.Files, gitrepo.File{
			Path: fmt.Sprintf("packages/long-module-name/src/file-%05d.go", index), Index: " ", Working: "M",
		})
	}
	tester := ui.NewTester(app.view, 1024, 680)
	count := 0
	for _, node := range tester.AccessAll() {
		if node.Role == ui.RoleCheckBox {
			count++
		}
	}
	if count > 80 {
		t.Fatalf("构建了 %d 个复选框，文件列表没有按可视区域加载", count)
	}
	first := "选择 未暂存文件 packages/long-module-name/src/file-00000.go"
	if err := tester.Click(first); err != nil {
		t.Fatal(err)
	}
	if len(app.checkedFiles(false)) != 1 {
		t.Fatal("虚拟列表复选框未保留独立勾选")
	}
	list, found := tester.Access("未暂存文件列表")
	if !found {
		t.Fatal("缺少文件列表")
	}
	for index := 0; index < 40; index++ {
		tester.Scroll(list.Bounds.X+list.Bounds.W/2, list.Bounds.Y+list.Bounds.H/2, 0, 600)
	}
	if first, found := tester.Access(first); found && !first.Offscreen {
		t.Fatal("测试未将首个文件滚出可视区域")
	}
	if len(app.checkedFiles(false)) != 1 {
		t.Fatal("滚出可视区域丢失勾选")
	}
}
