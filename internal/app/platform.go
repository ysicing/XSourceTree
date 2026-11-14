// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"runtime"
	"strings"

	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo"
)

const desktopReleasesURL = "https://github.com/ysicing/51code/releases/latest"

func shortcutLabelForPlatform(shortcut, platform string) string {
	if platform == "darwin" {
		return shortcut
	}
	return strings.NewReplacer("⌥", "Alt+", "⌘", "Ctrl+", "⇧", "Shift+", "↩", "Enter").Replace(shortcut)
}

func shortcutLabel(shortcut string) string {
	return shortcutLabelForPlatform(shortcut, runtime.GOOS)
}

func platformMenu(items []*mygo.MenuItem, platform string) []*mygo.MenuItem {
	if platform == "darwin" {
		return items
	}
	application := make([]*mygo.MenuItem, 0, len(items[0].Submenu))
	for _, item := range items[0].Submenu {
		switch item.Role {
		case mygo.RoleServices, mygo.RoleHide, mygo.RoleHideOthers, mygo.RoleUnhide:
			continue
		}
		if item.Type == mygo.MenuItemSeparator &&
			(len(application) == 0 || application[len(application)-1].Type == mygo.MenuItemSeparator) {
			continue
		}
		application = append(application, item)
	}
	if len(application) > 0 && application[len(application)-1].Type == mygo.MenuItemSeparator {
		application = application[:len(application)-1]
	}
	items[0].Submenu = application
	return items
}

func revealRepositoryLabel() string {
	if runtime.GOOS == "darwin" {
		return locales.RevealFinder()
	}
	return locales.OpenRepositoryFolder()
}

func credentialKeyHint() string {
	if runtime.GOOS == "darwin" {
		return locales.KeychainKeyHint()
	}
	return locales.CredentialKeyHint()
}
