// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

type nativeRefNode struct {
	name, reference string
	remote          bool
	current         bool
	preferred       bool
	children        []*nativeRefNode
}

type nativeRefKey struct {
	repository, kind, reference string
}

func nativeReferenceNodes(names []string, prefix, current, preferred string) []*nativeRefNode {
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
			node.preferred = node.preferred || name == preferred
			children = &node.children
		}
		nodes[name].current = name == current
	}
	var sortNodes func([]*nativeRefNode)
	sortNodes = func(nodes []*nativeRefNode) {
		slices.SortFunc(nodes, func(left, right *nativeRefNode) int {
			if left.preferred != right.preferred {
				if left.preferred {
					return -1
				}
				return 1
			}
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
	key := nativeRefKey{repository: app.repository.snapshot.Path, kind: title}
	collapsed, known := app.refCollapsed[key]
	expanded := !collapsed
	if !known && title == "标签" {
		expanded = false
	}
	label = locales.ReferenceTree(nativeReferenceLabel(title))
	if title == "远端" {
		label = locales.RemoteBranchTree()
	}
	ui.Column(c).Key(title+":"+app.repository.snapshot.Path).FillWidth().Margin(16, 0, 0, 0).Children(func() {
		tree := ui.TreeBase(c)
		tree.Root.FillWidth().Label(label).Children(func() {
			section := tree.Item(&expanded)
			visibleTitle := nativeReferenceTitle(title)
			header := section.Row.Label(nativeReferenceLabel(title)).Height(30).FillWidth().Padding(0, 8).Gap(8).AlignItems(ui.Center).Radius(5)
			header.FocusRing(header.FocusVisible())
			if header.Hovered() || header.Pressed() {
				header.Background(c.Theme().SurfaceHover)
			}
			if header.Clicked() {
				expanded = !expanded
			}
			header.Children(func() {
				ui.Icon(c, icon).FontSize(18).TextColor(c.Theme().TextMuted)
				ui.Text(c, visibleTitle).FontSize(12).FontWeight(500).TextColor(c.Theme().TextMuted).Grow(1)
				nativeDisclosure(c, expanded, c.Theme().TextMuted)
			})
			app.refCollapsed[key] = !expanded
			section.Group(func() {
				app.referenceItems(c, tree, nodes, title, leaf)
			})
		})
	})
}

func nativeReferenceTitle(kind string) string {
	if kind == "本地分支" {
		return locales.Branch()
	}
	return nativeReferenceLabel(kind)
}

func nativeReferenceLabel(kind string) string {
	switch kind {
	case "本地分支":
		return locales.LocalBranches()
	case "标签":
		return locales.Tags()
	case "远端":
		return locales.Remote()
	default:
		return kind
	}
}

func nativeDisclosure(c *ui.Context, expanded bool, color ui.Color) {
	degrees := float32(0)
	if expanded {
		degrees = 90
	}
	ui.Icon(c, nativeIcons["chevron"]).FontSize(10).Rotate(degrees).TextColor(color)
}

func (app *nativeApp) referenceItems(c *ui.Context, tree ui.TreeParts, nodes []*nativeRefNode, kind string, leaf func(string, ui.Element)) {
	for _, node := range nodes {
		group := ui.Column(c).Key(node.reference).FillWidth()
		key := nativeRefKey{repository: app.repository.snapshot.Path, kind: kind, reference: node.reference}
		collapsed, known := app.refCollapsed[key]
		expanded := !collapsed
		if !known && node.remote {
			expanded = false
		}
		group.Children(func() {
			var open *bool
			if len(node.children) > 0 || node.remote {
				open = &expanded
			}
			item := tree.Item(open)
			label := node.reference
			switch {
			case node.remote:
				label = nativeReferenceLabel("远端") + " " + node.reference
			case open != nil:
				label = locales.ReferenceDirectory(nativeReferenceLabel(kind), node.reference)
			}
			row := item.Row.Label(label).Tooltip(node.reference).FillWidth().MinWidth(0).
				Height(26).Gap(4).AlignItems(ui.Center).Padding(0, 8).
				PaddingStart(22 + 16*float32(item.Depth()-1)).Radius(5).FontSize(12)
			if row.Clicked() {
				app.refSelected = key
			}
			selected := app.refSelected == key
			color := c.Theme().TextMuted
			if selected {
				row.Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
				color = c.Theme().AccentText
			} else if row.Pressed() || row.Hovered() {
				row.Background(c.Theme().SurfaceHover)
			}
			row.AccessSelected(selected)
			row.FocusRing(row.FocusVisible())
			row.Children(func() {
				arrow := ui.Box(c).Size(14, 14).Shrink(0)
				if open != nil {
					arrow.Children(func() {
						nativeDisclosure(c, *open, color)
					})
				} else if node.current {
					arrow.AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						dot := c.Theme().Accent
						if selected {
							dot = c.Theme().AccentText
						}
						ui.Box(c).Size(7, 7).Radius(4).Background(dot)
					})
				}
				if open != nil && !node.remote {
					ui.Icon(c, nativeIcons["folder"]).FontSize(15).Shrink(0).TextColor(color)
				}
				text := ui.Text(c, node.name).Grow(1).MinWidth(0).SingleLine()
				if node.current {
					text.FontWeight(600)
				}
			})
			if open != nil {
				if row.Clicked() {
					*open = !*open
				}
				app.refCollapsed[key] = !*open
				item.Group(func() { app.referenceItems(c, tree, node.children, kind, leaf) })
			} else if !node.remote {
				leaf(node.reference, row)
			}
		})
	}
}

func (app *nativeApp) localBranchSidebar(c *ui.Context) {
	var names []string
	var current string
	var preferred string
	branches := make(map[string]gitrepo.Branch)
	for _, branch := range app.repository.branches {
		if !branch.Remote {
			names = append(names, branch.Name)
			branches[branch.Name] = branch
			if branch.Current {
				current = branch.Name
			}
			if branch.Default {
				preferred = branch.Name
			}
		}
	}
	app.referenceSidebar(c, "本地分支", "本地分支树", nativeIcons["branch"], nativeReferenceNodes(names, "", current, preferred), func(name string, item ui.Element) {
		branch := branches[name]
		if branch.Current {
			item.Tooltip(name + " · " + locales.CurrentBranch())
		}
		item.ContextMenu(func(menu *ui.Menu) {
			unavailable := !app.canCreateReference()
			if menu.Item(locales.BranchFromBranch()).Disabled(unavailable).Chosen() {
				app.referenceFrom(false, "branch", name)
			}
			if menu.Item(locales.TagFromBranch()).Disabled(unavailable).Chosen() {
				app.referenceFrom(true, "branch", name)
			}
			menu.Separator()
			if menu.Item(locales.RenameBranchMenu()).Disabled(!app.writable).Chosen() {
				app.renameBranchDialog(name)
			}
			if menu.Item(locales.DeleteBranchMenu()).Disabled(branch.Current || !app.writable).Chosen() {
				app.deleteBranch(name, false)
			}
			if menu.Item(locales.ForceDeleteBranchMenu()).Disabled(branch.Current || !app.writable).Chosen() {
				app.deleteBranch(name, true)
			}
		})
		if item.Clicked() && app.writable && !branch.Current {
			app.leaveConflict(func() {
				app.mutate(locales.SwitchingBranch(), func(ctx context.Context, path string) error {
					return app.service.SwitchBranch(ctx, path, name)
				})
			})
		}
	})
}

func (app *nativeApp) tagSidebar(c *ui.Context) {
	var names []string
	for _, tag := range app.repository.tags {
		names = append(names, tag.Name)
	}
	services := c.Services()
	app.referenceSidebar(c, "标签", "标签树", nativeIcons["tag"], nativeReferenceNodes(names, "", "", ""), func(name string, item ui.Element) {
		item.ContextMenu(func(menu *ui.Menu) {
			app.refSelected = nativeRefKey{repository: app.repository.snapshot.Path, kind: "标签", reference: name}
			if menu.Item(locales.CopyTagName()).Chosen() {
				services.WriteClipboard(name)
			}
			if menu.Item(locales.ViewTagCommit()).Disabled(app.busy).Chosen() {
				app.selectTag(name, false)
			}
			menu.Separator()
			if menu.Item(locales.BranchFromTag()).Disabled(!app.canCreateReference()).Chosen() {
				app.referenceFrom(false, "tag", name)
			}
			menu.Separator()
			if menu.Item(locales.DeleteTagMenu()).Disabled(!app.writable).Chosen() {
				app.deleteTag(name)
			}
		})
		if item.Clicked() {
			app.selectTag(name, false)
		}
	})
}
