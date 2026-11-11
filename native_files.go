// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

func matchesNativeFileFilter(file gitrepo.File, filter string) bool {
	untracked := file.Index == "?" && file.Working == "?"
	switch filter {
	case "conflicts":
		return file.Conflict
	case "untracked":
		return untracked
	case "tracked":
		return !untracked
	default:
		return true
	}
}

func nativeFileStatus(file gitrepo.File, staged bool) string {
	if file.Conflict {
		return "!"
	}
	if staged {
		return file.Index
	}
	return file.Working
}

func compareNativeFiles(first, second gitrepo.File, order string, descending, staged bool) int {
	var result int
	switch order {
	case "name":
		result = cmp.Compare(filepath.Base(first.Path), filepath.Base(second.Path))
	case "status":
		result = cmp.Compare(nativeFileStatus(first, staged), nativeFileStatus(second, staged))
	}
	if result == 0 {
		result = cmp.Compare(first.Path, second.Path)
	}
	if descending {
		return -result
	}
	return result
}

func (app *nativeApp) filesFiltered() bool {
	return app.fileQuery != "" || app.fileFilter != ""
}

func (app *nativeApp) fileControls(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(8, 12, 4, 12).Gap(6).AlignItems(ui.Center).Children(func() {
		if ui.TextInput(c, &app.fileQuery).Label(locales.FileFilter()).Search().Placeholder(locales.FileFilter()).Grow(1).MinWidth(0).Changed() {
			clear(app.fileCollapsed)
		}
		if app.filesFiltered() {
			if iconButton(c, locales.ClearFilter(), nativeIcons["close"]).Clicked() {
				app.fileQuery, app.fileFilter = "", ""
				clear(app.fileCollapsed)
			}
		}
	})
	ui.Toolbar(c, func() {
		filters := []struct{ value, label string }{
			{"", locales.AllChanges()}, {"conflicts", locales.Conflicts()}, {"untracked", locales.Untracked()}, {"tracked", locales.TrackedChanges()},
		}
		filterLabel := locales.AllChanges()
		for _, filter := range filters {
			if app.fileFilter == filter.value {
				filterLabel = filter.label
			}
		}
		if app.fileFilter == "tracked" {
			filterLabel = locales.Tracked()
		}
		ui.MenuButton(c, filterLabel, func(menu *ui.Menu) {
			for _, filter := range filters {
				if menu.Item(filter.label).Checked(app.fileFilter == filter.value).Chosen() {
					app.fileFilter = filter.value
					clear(app.fileCollapsed)
				}
			}
		}).Label(locales.FileFiltering()).FontSize(12)
		orderLabel := locales.Path()
		switch app.prefs.FileSort {
		case "name":
			orderLabel = locales.FileName()
		case "status":
			orderLabel = locales.Status()
		}
		if app.prefs.FileDescending {
			orderLabel += " ↓"
		}
		ui.MenuButton(c, orderLabel, func(menu *ui.Menu) {
			for _, order := range []struct{ value, label string }{
				{"path", locales.SortByPath()}, {"name", locales.SortByName()}, {"status", locales.SortByStatus()},
			} {
				if menu.Item(order.label).Checked(app.prefs.FileSort == order.value).Chosen() {
					app.prefs.FileSort = order.value
				}
			}
			menu.Separator()
			if menu.Item(locales.Descending()).Checked(app.prefs.FileDescending).Chosen() {
				app.prefs.FileDescending = !app.prefs.FileDescending
			}
		}).Label(locales.FileSorting()).FontSize(12)
		ui.Spacer(c)
		viewLabel := locales.List()
		if app.prefs.FileTree {
			viewLabel = locales.Tree()
		}
		ui.MenuButton(c, viewLabel, func(menu *ui.Menu) {
			if menu.Item(locales.FlatList()).Checked(!app.prefs.FileTree).Chosen() {
				app.prefs.FileTree = false
			}
			if menu.Item(locales.Tree()).Checked(app.prefs.FileTree).Chosen() {
				app.prefs.FileTree = true
			}
		}).Label(locales.FileView()).FontSize(12)
	}).Label(locales.FileDisplayOptions()).FillWidth().Padding(0, 8, 4, 8)
}

type nativeFileNode struct {
	path     string
	file     *gitrepo.File
	children []*nativeFileNode
}

func nativeFileNodes(files []gitrepo.File, order string, descending, staged bool) []*nativeFileNode {
	var roots []*nativeFileNode
	directories := make(map[string]*nativeFileNode)
	for _, file := range files {
		children := &roots
		parts := strings.Split(file.Path, "/")
		path := ""
		for _, part := range parts[:len(parts)-1] {
			path += part + "/"
			node := directories[path]
			if node == nil {
				node = &nativeFileNode{path: strings.TrimSuffix(path, "/")}
				directories[path] = node
				*children = append(*children, node)
			}
			children = &node.children
		}
		*children = append(*children, &nativeFileNode{path: file.Path, file: &file})
	}
	var sortNodes func([]*nativeFileNode)
	sortNodes = func(nodes []*nativeFileNode) {
		slices.SortFunc(nodes, func(first, second *nativeFileNode) int {
			if (first.file == nil) != (second.file == nil) {
				if first.file == nil {
					return -1
				}
				return 1
			}
			if first.file != nil {
				return compareNativeFiles(*first.file, *second.file, order, descending, staged)
			}
			result := cmp.Compare(first.path, second.path)
			if descending {
				return -result
			}
			return result
		})
		for _, node := range nodes {
			sortNodes(node.children)
		}
	}
	sortNodes(roots)
	return roots
}

func (app *nativeApp) fileTreeSize(c *ui.Context, nodes []*nativeFileNode, staged bool, depth int) (int, float32) {
	var count int
	var width float32
	for _, node := range nodes {
		count++
		textWidth, _ := c.MeasureText(0, ui.Span{Text: filepath.Base(node.path), Font: c.Theme().Font, Size: 12})
		width = max(width, textWidth+float32(depth)*16+88)
		if node.file == nil && !app.fileCollapsed[fmt.Sprintf("%t:%s", staged, node.path)] {
			childCount, childWidth := app.fileTreeSize(c, node.children, staged, depth+1)
			count += childCount
			width = max(width, childWidth)
		}
	}
	return count, width
}

func (app *nativeApp) fileTree(c *ui.Context, tree ui.TreeParts, nodes []*nativeFileNode, staged bool, depth int) {
	for _, node := range nodes {
		if node.file != nil {
			app.changeFileRow(c, *node.file, staged, &tree, depth)
			continue
		}
		key := fmt.Sprintf("%t:%s", staged, node.path)
		ui.Column(c).Key("directory:" + key).FillWidth().Children(func() {
			expanded := !app.fileCollapsed[key]
			item := tree.Item(&expanded)
			label := locales.UnstagedDirectory(node.path)
			if staged {
				label = locales.StagedDirectory(node.path)
			}
			row := item.Row.Label(label).Tooltip(node.path).Height(28).FillWidth().
				Padding(0, 6).PaddingStart(6 + float32(depth)*16).Gap(6).AlignItems(ui.Center).Radius(5)
			row.FocusRing(row.FocusVisible())
			if row.Hovered() || row.Pressed() {
				row.Background(c.Theme().SurfaceHover)
			}
			if row.Clicked() {
				expanded = !expanded
			}
			row.Children(func() {
				nativeDisclosure(c, expanded, c.Theme().TextMuted)
				ui.Icon(c, nativeIcons["folder"]).FontSize(15).TextColor(c.Theme().TextMuted)
				ui.Text(c, filepath.Base(node.path)).FontSize(12).SingleLine()
			})
			app.fileCollapsed[key] = !expanded
			item.Group(func() { app.fileTree(c, tree, node.children, staged, depth+1) }).FillWidth()
		})
	}
}

func (app *nativeApp) changeFileRow(c *ui.Context, file gitrepo.File, staged bool, tree *ui.TreeParts, depth int) {
	label := locales.SelectUnstagedFile(file.Path)
	if staged {
		label = locales.SelectStagedFile(file.Path)
	}
	key := fmt.Sprintf("%t:%s", staged, file.Path)
	ui.Row(c).Key("file:" + key).FillWidth().PaddingStart(float32(depth) * 16).Gap(2).AlignItems(ui.Center).Children(func() {
		checked := app.selectedFiles[key]
		if ui.Checkbox(c, &checked, "").Label(label).Disabled(file.Conflict || !app.writable).Changed() {
			app.selectedFiles[key] = checked
		}
		selected := app.selectedFile == file.Path && app.staged == staged
		var row ui.Element
		name := file.Path
		if tree == nil {
			row = listRow(c, file.Path, selected)
		} else {
			name = filepath.Base(file.Path)
			row = tree.Item(nil).Row.Label(file.Path).AccessSelected(selected).Radius(5)
			row.FocusRing(row.FocusVisible())
			if selected {
				row.Background(c.Theme().Accent.Alpha(0.12))
			} else if row.Hovered() || row.Pressed() {
				row.Background(c.Theme().SurfaceHover)
			}
		}
		status := nativeFileStatus(file, staged)
		color := c.Theme().Warning
		switch status {
		case "A", "?":
			color = c.Theme().Success
		case "D", "!":
			color = c.Theme().Danger
		}
		if row.Grow(1).Height(28).Padding(4, 6).Gap(6).AlignItems(ui.Center).Tooltip(file.Path).Children(func() {
			ui.Text(c, status).Width(12).Font("monospace").FontSize(11).TextColor(color)
			nativeFileIcon(c, file.Path)
			ui.Text(c, name).FontSize(12).Grow(1).MinWidth(0).SingleLine()
		}).Clicked() && (!selected || app.readTask != nil || app.problem != "") {
			app.selectFile(file.Path, staged)
		}
	})
}
