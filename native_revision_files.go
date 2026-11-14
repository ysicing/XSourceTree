// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

func (app *nativeApp) revisionFileTree(c *ui.Context, tree ui.TreeParts, nodes []*nativeFileNode, collapsed map[string]bool, indices map[string]int, selected *int, choose func(string), depth int) {
	for _, node := range nodes {
		key := "directory:" + node.path
		if node.file != nil {
			key = "file:" + node.path
		}
		ui.Column(c).Key(key).FillWidth().Children(func() {
			expanded := !collapsed[node.path]
			var open *bool
			if node.file == nil {
				open = &expanded
			}
			item := tree.Item(open)
			label, name := node.path, filepath.Base(node.path)
			active := node.file != nil && *selected == indices[node.path]
			if node.file == nil {
				label += "/"
			}
			row := item.Row.Label(label).Tooltip(node.path).AccessSelected(active).
				Height(30).FillWidth().Padding(0, 12).PaddingStart(12 + float32(depth)*16).
				Gap(8).AlignItems(ui.Center)
			row.FocusRing(row.FocusVisible())
			if active {
				row.Background(c.Theme().Accent)
			} else if row.Hovered() || row.Pressed() {
				row.Background(c.Theme().SurfaceHover)
			}
			row.Children(func() {
				if node.file == nil {
					nativeDisclosure(c, expanded, c.Theme().TextMuted)
					ui.Icon(c, nativeIcons["folder"]).FontSize(15).TextColor(c.Theme().TextMuted)
				} else {
					nativeFileIcon(c, node.path)
				}
				ui.Text(c, name).FontSize(12).Grow(1).MinWidth(0).SingleLine()
				if node.file != nil {
					ui.Text(c, node.file.Index).FontSize(10).TextColor(c.Theme().TextMuted)
				}
			})
			if row.Clicked() {
				if node.file == nil {
					expanded = !expanded
				} else if !active || app.readTask != nil || app.problem != "" {
					*selected = indices[node.path]
					choose(node.path)
				}
			}
			if node.file == nil {
				collapsed[node.path] = !expanded
				item.Group(func() {
					app.revisionFileTree(c, tree, node.children, collapsed, indices, selected, choose, depth+1)
				}).FillWidth()
			}
		})
	}
}
