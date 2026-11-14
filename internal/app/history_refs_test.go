// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNativeHistoryFullRefs(t *testing.T) {
	app, path, finish := nativeHistoryFixture(t)
	nativeGit(t, path, "branch", "feature/完整显示所有分支名称")
	nativeGit(t, path, "update-ref", "refs/remotes/origin/main", "HEAD")
	nativeGit(t, path, "update-ref", "refs/remotes/origin/dependabot/go_modules/golang.org/x/sys-0.50.0", "HEAD")
	nativeGit(t, path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	app.searchHistory(false)
	finish()

	refs := strings.Split(app.history[0].Refs, ", ")
	for index := range refs {
		refs[index] = strings.TrimPrefix(refs[index], "tag: ")
	}
	labels := strings.Join(refs, " ")
	if len(refs) < 6 {
		t.Fatalf("缺少多引用测试数据: %v", refs)
	}
	subject := app.history[0].Subject
	tester := ui.NewTester(app.view, 2000, 860)
	references, found := tester.Find(labels)
	if !found || references.W < 500 {
		t.Fatalf("未完整展示所有分支与标签: refs=%v bounds=%+v found=%v", refs, references, found)
	}
	message, found := tester.AccessByRole(ui.RoleText, subject)
	if !found || message.Bounds.X < references.X+references.W {
		t.Fatalf("引用标签遮挡了提交说明: refs=%+v message=%+v", references, message)
	}
	if err := tester.Click(subject); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.commitHash != app.history[0].Hash {
		t.Fatal("显示完整引用后无法选择提交")
	}
	for _, dark := range []bool{false, true} {
		tester.SetDark(dark)
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		tester.SetSize(2000, 860)
		if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
			saveNativeScreenshot(t, tester, directory, "native-history-full-refs-"+appearance+".png")
		}
		tester.SetSize(1024, 680)
		app.historyList.Columns.Widths = map[string]float32{"提交说明": 80}
		tester.Frame()
		row, found := tester.AccessByRole(ui.RoleRow, subject)
		if !found || row.Bounds.H > 30 || !row.Selected {
			t.Fatalf("窄窗口改变了紧凑行高或提交选择: %+v", row)
		}
		for _, label := range []string{"所有分支", "显示远程分支", "搜索"} {
			bounds, found := tester.Find(label)
			if !found || bounds.X < 0 || bounds.X+bounds.W > 1024 {
				t.Fatalf("窄窗口控件不可见: %s %+v", label, bounds)
			}
		}
		if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
			saveNativeScreenshot(t, tester, directory, "native-history-full-refs-narrow-"+appearance+".png")
		}
		table, found := tester.Find("提交历史表格")
		if !found {
			t.Fatal("缺少历史表格")
		}
		tester.Scroll(table.X+table.W/2, row.Bounds.Y+row.Bounds.H/2, 1e6, 0)
		message, found := tester.AccessByRole(ui.RoleText, subject)
		if !found || message.Offscreen || message.Bounds.W < 150 ||
			message.Bounds.X < table.X || message.Bounds.X+message.Bounds.W > table.X+table.W {
			t.Fatalf("窄窗口无法横向滚动查看提交说明: table=%+v message=%+v", table, message)
		}
		if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
			saveNativeScreenshot(t, tester, directory, "native-history-full-refs-scrolled-"+appearance+".png")
		}
		tester.Scroll(table.X+table.W/2, row.Bounds.Y+row.Bounds.H/2, -1e6, 0)
	}
	app.screen = "search"
	tester.Frame()
	if _, found := tester.Find(labels); !found {
		t.Fatal("搜索结果未沿用完整引用显示")
	}
}
