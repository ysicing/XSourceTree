// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

func (app *nativeApp) branchPushDialog(publish bool) {
	if len(app.repository.remotes) == 0 {
		app.remoteDialog()
		return
	}
	snapshot := app.repository.snapshot
	remotes := append([]gitrepo.RemoteConfig{}, app.repository.remotes...)
	var upstreamRemote, upstreamBranch string
	for _, branch := range app.repository.branches {
		if branch.Current && !branch.Remote {
			upstreamRemote, upstreamBranch = branch.UpstreamRemote, branch.UpstreamBranch
			break
		}
	}
	var names []string
	choice := remotes[0].Name
	for _, remote := range remotes {
		names = append(names, remote.Name)
		if remote.Name == upstreamRemote || remote.Name == "origin" && choice != upstreamRemote {
			choice = remote.Name
		}
	}
	target := func(remote string) string {
		if remote == upstreamRemote && upstreamBranch != "" {
			return upstreamBranch
		}
		return snapshot.Branch
	}
	title, action, loading := locales.Push(), locales.Push(), locales.Pushing()
	if publish {
		title, action, loading = locales.PublishBranch(), locales.Publish(), locales.PublishingBranch()
	}
	dialog := &nativeDialog{
		title: title, action: action, options: names, choice: choice,
		fields: []nativeField{{label: locales.RemoteBranchName(), value: target(choice)}},
	}
	dialog.choiceChanged = func() { dialog.fields[0].value = target(dialog.choice) }
	dialog.body = func(c *ui.Context) {
		for _, remote := range remotes {
			if remote.Name == dialog.choice {
				ui.Text(c, strings.Join(remote.PushURLs, "\n")).FontSize(12).Selectable()
			}
		}
	}
	dialog.submit = func(dialog *nativeDialog) {
		for _, remote := range remotes {
			if remote.Name != dialog.choice {
				continue
			}
			request := gitrepo.PublishBranchRequest{
				Remote: remote.Name, Token: remote.Token, Name: dialog.fields[0].value,
				Branch: snapshot.Branch, Head: snapshot.Head,
			}
			app.mutateForm(dialog, loading, func(ctx context.Context, path string) error {
				if publish {
					return app.service.PublishBranch(ctx, path, request)
				}
				return app.service.PushBranch(ctx, path, gitrepo.PushBranchRequest(request))
			})
			return
		}
	}
	app.dialog = dialog
}
