// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ysicing/XSourceTree/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func TestNativeChangesSectionsKeepStagedVisible(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.repository.snapshot.Files = []gitrepo.File{
		{Path: "README.md", Index: "M", Working: " "},
		{Path: "Taskfile.yml", Index: "M", Working: " "},
	}
	for _, path := range []string{
		"backend/config-example.yaml",
		"backend/hscontrol/stargate/component_desktop.go",
		"backend/hscontrol/stargate/component_mobile.go",
		"backend/hscontrol/stargate/manager.go",
		"backend/hscontrol/types/manager.go",
	} {
		app.repository.snapshot.Files = append(app.repository.snapshot.Files, gitrepo.File{Path: path, Index: " ", Working: "M"})
	}
	for index := range 40 {
		path := fmt.Sprintf("backend/hscontrol/types/mobile_session_%02d.go", index)
		app.repository.snapshot.Files = append(app.repository.snapshot.Files, gitrepo.File{Path: path, Index: " ", Working: "M"})
	}
	for _, viewport := range []struct {
		name          string
		width, height int
		files         float32
	}{
		{"wide", 1320, 860, 520},
		{"compact", 1024, 680, 360},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			app.fileQuery = ""
			app.prefs.Files = viewport.files
			tester := ui.NewTester(app.view, viewport.width, viewport.height)
			staged, found := tester.Access("已暂存文件操作")
			if !found || staged.Offscreen || staged.Bounds.H == 0 {
				t.Fatal("已暂存分区标题不可见")
			}
			working, found := tester.Access("未暂存文件操作")
			if !found || working.Offscreen || working.Bounds.H == 0 || staged.Bounds.Y >= working.Bounds.Y {
				t.Fatal("未按已暂存、未暂存的顺序展示两个可见分区")
			}
			if working.Bounds.Y-staged.Bounds.Y > 140 {
				t.Fatal("少量已暂存文件占据了过多空间")
			}
			for _, path := range []string{"backend/hscontrol/stargate/manager.go", "backend/hscontrol/types/manager.go"} {
				if _, found := tester.Access(path); !found {
					t.Fatalf("同名文件未以连续相对路径显示: %s", path)
				}
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				saveNativeScreenshot(t, tester, directory, "changes-"+viewport.name+"-light.png")
				tester.SetDark(true)
				saveNativeScreenshot(t, tester, directory, "changes-"+viewport.name+"-dark.png")
			}
			list, found := tester.Access("未暂存文件列表")
			if !found {
				t.Fatal("缺少可独立滚动的未暂存列表")
			}
			x, y := list.Bounds.X+list.Bounds.W/2, list.Bounds.Y+list.Bounds.H/2
			tester.Move(x, y)
			if viewport.name == "compact" {
				path := "backend/hscontrol/stargate/component_desktop.go"
				before, found := tester.Find(path)
				if !found {
					t.Fatal("长路径不在视图中")
				}
				tester.Scroll(x, y, 160, 0)
				if after, found := tester.Find(path); !found || before == after {
					t.Fatal("长路径无法横向滚动查看")
				}
				tester.Scroll(x, y, -160, 0)
			}
			before, found := tester.Find("README.md")
			if !found {
				t.Fatal("已暂存文件不在视图中")
			}
			first, found := tester.Find("backend/config-example.yaml")
			if !found {
				t.Fatal("未暂存文件不在视图中")
			}
			tester.Wheel(x, y, 0, 500)
			tester.Advance(time.Second)
			scrolled, found := tester.Find("backend/config-example.yaml")
			if found && scrolled.H > 0 && scrolled.Y >= first.Y {
				t.Fatalf("未暂存文件列表未向下滚动: list=%+v before=%+v after=%+v", list.Bounds, first, scrolled)
			}
			after, found := tester.Find("README.md")
			if !found || before != after {
				t.Fatal("滚动未暂存列表影响了已暂存文件的位置")
			}
			header, found := tester.Access("未暂存文件操作")
			if !found || header.Bounds != working.Bounds {
				t.Fatal("滚动文件时分区操作被带出视野")
			}
			app.fileQuery = "manager.go"
			tester.Frame()
			if files := app.visibleFiles(false); len(files) != 2 {
				t.Fatalf("按路径筛选得到 %d 个文件，预期 2 个", len(files))
			}
			staged, _ = tester.Access("已暂存文件操作")
			working, _ = tester.Access("未暂存文件操作")
			if working.Bounds.Y-staged.Bounds.Y > 90 {
				t.Fatal("空暂存分区未收紧")
			}
		})
	}
}
