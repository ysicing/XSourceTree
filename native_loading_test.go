// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ysicing/51code/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func TestNativeFileListVerticalScrollKeepsHorizontalPosition(t *testing.T) {
	for _, tree := range []bool{false, true} {
		for _, staged := range []bool{false, true} {
			t.Run(fmt.Sprintf("tree=%t/staged=%t", tree, staged), func(t *testing.T) {
				app, _, _ := nativeFixture(t)
				app.prefs.FileTree = tree
				app.repository.snapshot.Files = nil
				first := ""
				for index := range 100 {
					path := fmt.Sprintf("%s-file-%03d.go", strings.Repeat("long-path-", 8), index)
					if index == 0 {
						first = path
					}
					file := gitrepo.File{Path: path, Index: " ", Working: "M"}
					if staged {
						file.Index, file.Working = "M", " "
					}
					app.repository.snapshot.Files = append(app.repository.snapshot.Files, file)
				}
				tester := ui.NewTester(app.view, 1024, 680)
				label := "未暂存文件列表"
				if staged {
					label = "已暂存文件列表"
				}
				list, found := tester.Access(label)
				if !found {
					t.Fatal("缺少文件列表")
				}
				before, found := tester.Access(first)
				if !found {
					t.Fatal("缺少首个文件")
				}
				x, y := list.Bounds.X+list.Bounds.W/2, list.Bounds.Y+list.Bounds.H/2
				tester.Scroll(x, y, 0, 400)
				tester.Scroll(x, y, 0, -1000)
				for range 4 {
					tester.Scroll(x, y, 6, -50)
				}
				after, found := tester.Access(first)
				if !found || after.Bounds.X != before.Bounds.X || after.Bounds.Y != before.Bounds.Y {
					t.Fatalf("到达顶部后的纵向滑动改变了横向位置: before=%+v after=%+v", before.Bounds, after.Bounds)
				}
				tester.Scroll(x, y, 80, 0)
				sideways, _ := tester.Access(first)
				if sideways.Bounds.X >= after.Bounds.X {
					t.Fatal("主动横向滑动失效")
				}
				tester.Scroll(x, y, 0, 1e6)
				bottom := fmt.Sprintf("%s-file-099.go", strings.Repeat("long-path-", 8))
				last, _ := tester.Access(bottom)
				tester.Scroll(x, y, -6, 50)
				afterLast, _ := tester.Access(bottom)
				if afterLast.Bounds.X != last.Bounds.X {
					t.Fatal("底部纵向滑动改变了横向位置")
				}
			})
		}
	}
}

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
