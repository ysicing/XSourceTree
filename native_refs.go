// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

type nativeRefNode struct {
	name, reference string
	remote          bool
	children        []*nativeRefNode
}

type nativeRefKey struct {
	repository, kind, reference string
}

func nativeReferenceNodes(names []string, prefix string) []*nativeRefNode {
	var roots []*nativeRefNode
	nodes := make(map[string]*nativeRefNode)
	for _, name := range names {
		children := &roots
		path := ""
		for _, part := range strings.Split(name, "/") {
			if path != "" {
				path += "/"
			}
			path += part
			node := nodes[path]
			if node == nil {
				node = &nativeRefNode{name: part, reference: prefix + path}
				nodes[path] = node
				*children = append(*children, node)
			}
			children = &node.children
		}
	}
	var sortNodes func([]*nativeRefNode)
	sortNodes = func(nodes []*nativeRefNode) {
		slices.SortFunc(nodes, func(left, right *nativeRefNode) int {
			if (len(left.children) > 0) != (len(right.children) > 0) {
				if len(left.children) > 0 {
					return -1
				}
				return 1
			}
			return cmp.Compare(left.name, right.name)
		})
		for _, node := range nodes {
			sortNodes(node.children)
		}
	}
	sortNodes(roots)
	return roots
}

func (app *nativeApp) referenceSidebar(c *ui.Context, title, label string, icon *ui.SVG, nodes []*nativeRefNode, leaf func(string, ui.Element)) {
	c.Key(title)
	ui.SidebarSection(c, title, nil, func() {
		ui.Column(c).Key(app.repository.snapshot.Path).FillWidth().Children(func() {
			tree := ui.TreeBase(c)
			tree.Root.FillWidth().Label(label).Children(func() {
				app.referenceItems(c, tree, nodes, title, icon, leaf)
			})
		})
	})
}

func (app *nativeApp) referenceItems(c *ui.Context, tree ui.TreeParts, nodes []*nativeRefNode, kind string, icon *ui.SVG, leaf func(string, ui.Element)) {
	for _, node := range nodes {
		group := ui.Column(c).Key(node.reference).FillWidth()
		key := nativeRefKey{repository: app.repository.snapshot.Path, kind: kind, reference: node.reference}
		expanded := !app.refCollapsed[key]
		group.Children(func() {
			var open *bool
			if len(node.children) > 0 {
				open = &expanded
			}
			item := tree.Item(open)
			label, symbol := node.reference, icon
			switch {
			case node.remote:
				label, symbol = "远端 "+node.reference, nativeIcons["cloud"]
			case open != nil:
				label, symbol = kind+"目录 "+node.reference, nativeIcons["folder"]
			}
			row := item.Row.Label(label).Tooltip(node.reference).FillWidth().MinWidth(0).
				Height(28).Gap(4).AlignItems(ui.Center).Padding(0, 8).
				PaddingStart(4 + 16*float32(item.Depth())).Radius(c.Theme().Radius)
			if row.Hovered() {
				row.Background(c.Theme().SurfaceHover)
			}
			row.FocusRing(row.FocusVisible())
			row.Children(func() {
				arrow := ui.Box(c).Size(14, 14).Shrink(0)
				if open != nil {
					arrow.Children(func() {
						degrees := float32(0)
						if *open {
							degrees = 90
						}
						ui.Icon(c, nativeIcons["chevron"]).FontSize(12).Rotate(degrees).TextColor(c.Theme().TextMuted)
					})
				}
				ui.Icon(c, symbol).FontSize(15).Shrink(0).TextColor(c.Theme().Accent)
				ui.Text(c, node.name).Grow(1).MinWidth(0).SingleLine()
			})
			if open != nil {
				if row.Clicked() {
					*open = !*open
				}
				app.refCollapsed[key] = !*open
				item.Group(func() { app.referenceItems(c, tree, node.children, kind, icon, leaf) })
			} else if !node.remote {
				leaf(node.reference, row)
			}
		})
	}
}

func (app *nativeApp) localBranchSidebar(c *ui.Context) {
	var names []string
	branches := make(map[string]gitrepo.Branch)
	for _, branch := range app.repository.branches {
		if !branch.Remote {
			names = append(names, branch.Name)
			branches[branch.Name] = branch
		}
	}
	app.referenceSidebar(c, "本地分支", "本地分支树", nativeIcons["branch"], nativeReferenceNodes(names, ""), func(name string, item ui.Element) {
		branch := branches[name]
		if branch.Current {
			item.Background(c.Theme().SurfacePressed)
		}
		item.AccessSelected(branch.Current).ContextMenu(func(menu *ui.Menu) {
			if menu.Item("重命名分支…").Disabled(!app.writable).Chosen() {
				app.renameBranchDialog(name)
			}
			if menu.Item("删除分支…").Disabled(branch.Current || !app.writable).Chosen() {
				app.deleteBranch(name, false)
			}
			if menu.Item("强制删除分支…").Disabled(branch.Current || !app.writable).Chosen() {
				app.deleteBranch(name, true)
			}
		}).Children(func() {
			if branch.Current {
				ui.Icon(c, nativeIcons["check"]).FontSize(13).TextColor(c.Theme().TextMuted)
			}
		})
		if item.Clicked() && app.writable && name != app.repository.snapshot.Branch {
			app.leaveConflict(func() {
				app.mutate("正在切换分支…", func(ctx context.Context, path string) error {
					return app.service.SwitchBranch(ctx, path, name)
				})
			})
		}
	})
}

func (app *nativeApp) tagSidebar(c *ui.Context) {
	if len(app.repository.tags) == 0 {
		return
	}
	var names []string
	tags := make(map[string]gitrepo.Tag)
	for _, tag := range app.repository.tags {
		names = append(names, tag.Name)
		tags[tag.Name] = tag
	}
	app.referenceSidebar(c, "标签", "标签树", nativeIcons["tag"], nativeReferenceNodes(names, ""), func(name string, item ui.Element) {
		tag := tags[name]
		item.ContextMenu(func(menu *ui.Menu) {
			if menu.Item("删除标签…").Disabled(!app.writable).Chosen() {
				app.deleteTag(name)
			}
		})
		if item.Clicked() {
			app.leaveConflict(func() { app.screen = "history"; app.selectCommit(tag.Hash) })
		}
	})
}
