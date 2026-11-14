// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestPlatformShortcutLabels(t *testing.T) {
	for _, platform := range []string{"darwin", "windows", "linux"} {
		want := "Ctrl+Shift+Enter"
		if platform == "darwin" {
			want = "⌘⇧↩"
		}
		if got := shortcutLabelForPlatform("⌘⇧↩", platform); got != want {
			t.Fatalf("%s shortcut = %q, want %q", platform, got, want)
		}
	}
}

func TestNativeManualUpdateDownload(t *testing.T) {
	app := &nativeApp{}
	tester := ui.NewTester(app.manualUpdatePreferences, 480, 120)
	tester.FailOpenURL(errors.New("no browser"))
	if err := tester.Click(locales.DownloadRelease()); err != nil {
		t.Fatal(err)
	}
	if app.settingsError != "no browser" {
		t.Fatal("打开失败应保留设置并显示具体错误")
	}
	tester.FailOpenURL(nil)
	if err := tester.Click(locales.DownloadRelease()); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	urls := tester.OpenedURLs()
	if len(urls) != 2 || urls[1] != "https://github.com/ysicing/51code/releases/latest" || app.settingsError != "" {
		t.Fatalf("手动下载无法重试或在重绘时重复打开: %v, %s", urls, app.settingsError)
	}
}

func TestPlatformMenuPreservesCommands(t *testing.T) {
	for _, platform := range []string{"darwin", "windows", "linux"} {
		items := []*mygo.MenuItem{
			{Label: "51Code", Submenu: []*mygo.MenuItem{
				{ID: "about"}, {ID: "updates"}, {ID: "settings"},
				mygo.Separator(), {Role: mygo.RoleServices},
				mygo.Separator(), {Role: mygo.RoleHide},
				{Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide},
				mygo.Separator(), {Role: mygo.RoleQuit},
			}},
			{Role: mygo.RoleEditMenu},
		}
		menu := platformMenu(items, platform)
		application := menu[0].Submenu
		ids := []string{}
		for index, item := range application {
			ids = append(ids, item.ID)
			if platform != "darwin" {
				switch item.Role {
				case mygo.RoleServices, mygo.RoleHide, mygo.RoleHideOthers, mygo.RoleUnhide:
					t.Fatalf("%s menu contains macOS role %s", platform, item.Role)
				}
				if item.Type == mygo.MenuItemSeparator &&
					(index == 0 || index == len(application)-1 || application[index-1].Type == mygo.MenuItemSeparator) {
					t.Fatalf("%s menu contains empty section", platform)
				}
			}
		}
		for _, id := range []string{"about", "settings"} {
			if !slices.Contains(ids, id) {
				t.Fatalf("%s lost command %s", platform, id)
			}
		}
		if application[len(application)-1].Role != mygo.RoleQuit || menu[1].Role != mygo.RoleEditMenu {
			t.Fatalf("%s lost quit or editing commands", platform)
		}
		if platform == "darwin" && len(application) != 11 {
			t.Fatal("macOS menu changed")
		}
	}
}
