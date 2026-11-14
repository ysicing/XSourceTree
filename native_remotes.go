// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"slices"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

func remoteBranchParts(reference string, remotes []gitrepo.RemoteConfig) (string, string) {
	remote, name, found := strings.Cut(reference, "/")
	if !found {
		name = reference
	}
	for _, config := range remotes {
		if len(config.Name) > len(remote) && strings.HasPrefix(reference, config.Name+"/") {
			remote, name = config.Name, strings.TrimPrefix(reference, config.Name+"/")
		}
	}
	return remote, name
}

func (app *nativeApp) remoteSidebar(c *ui.Context) {
	groups := map[string][]string{}
	defaults := map[string]string{}
	for _, remote := range app.repository.remotes {
		groups[remote.Name] = nil
	}
	for _, branch := range app.repository.branches {
		if branch.Remote {
			remote, name := remoteBranchParts(branch.Name, app.repository.remotes)
			groups[remote] = append(groups[remote], name)
			if branch.Default {
				defaults[remote] = name
			}
		}
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	slices.Sort(names)
	var nodes []*nativeRefNode
	for _, remote := range names {
		nodes = append(nodes, &nativeRefNode{
			name: remote, reference: remote, remote: true,
			children: nativeReferenceNodes(groups[remote], remote+"/", "", defaults[remote]),
		})
	}
	app.referenceSidebar(c, "远端", "远端分支树", nativeIcons["cloud"], nodes, func(reference string, item ui.Element) {
		_, name := remoteBranchParts(reference, app.repository.remotes)
		if name == "HEAD" {
			item.Tooltip(reference + " · " + locales.RemoteHead())
		} else if item.Clicked() && app.writable {
			app.checkoutDialog(reference)
		}
	})
}
