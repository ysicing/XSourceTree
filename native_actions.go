// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

func (app *nativeApp) visibleFiles(staged bool) []gitrepo.File {
	var files []gitrepo.File
	if app.repository == nil {
		return files
	}
	query := strings.ToLower(app.fileQuery)
	for _, file := range app.repository.snapshot.Files {
		if (staged && isStaged(file) || !staged && isWorking(file)) &&
			strings.Contains(strings.ToLower(file.Path), query) && matchesNativeFileFilter(file, app.fileFilter) {
			files = append(files, file)
		}
	}
	return files
}

func (app *nativeApp) checkedFiles(staged bool) []string {
	var files []string
	if app.repository == nil {
		return files
	}
	for _, file := range app.visibleFiles(staged) {
		if !file.Conflict && app.selectedFiles[fmt.Sprintf("%t:%s", staged, file.Path)] {
			files = append(files, file.Path)
		}
	}
	return files
}

func (app *nativeApp) stageFiles(staged, all bool) {
	var files []string
	if all {
		for _, file := range app.visibleFiles(staged) {
			if !file.Conflict {
				files = append(files, file.Path)
			}
		}
	} else {
		files = app.checkedFiles(staged)
	}
	app.stagePaths(staged, files)
}

func (app *nativeApp) stageSelectedFile() {
	app.stagePaths(app.staged, []string{app.selectedFile})
}

func (app *nativeApp) stagePaths(staged bool, files []string) {
	if len(files) == 0 {
		return
	}
	app.mutate(locales.UpdatingIndex(), func(ctx context.Context, path string) error {
		if staged {
			return app.service.Unstage(ctx, path, files)
		}
		return app.service.Stage(ctx, path, files)
	})
}

func (app *nativeApp) branchDialog() {
	if !app.canCreateReference() {
		return
	}
	app.referenceDialog(false, "HEAD · "+app.repository.snapshot.Branch, app.repository.snapshot.Head, true)
}

func (app *nativeApp) renameBranchDialog(old string) {
	app.dialog = &nativeDialog{
		title: locales.RenameBranch(), action: locales.Rename(), fields: []nativeField{{label: locales.BranchName(), value: old}},
		submit: func(dialog *nativeDialog) {
			name := dialog.fields[0].value
			app.mutateForm(dialog, locales.RenamingBranch(), func(ctx context.Context, path string) error {
				return app.service.RenameBranch(ctx, path, old, name)
			})
		},
	}
}

func (app *nativeApp) deleteBranch(name string, force bool) {
	title := locales.DeleteLocalBranch()
	detail := name + "\n" + locales.DeleteBranchDetail()
	if force {
		title = locales.ForceDeleteLocalBranch()
		detail += "\n" + locales.ForceDeleteBranchDetail()
	}
	app.confirm(title, detail, locales.Delete(), func() {
		app.mutate(locales.DeletingBranch(), func(ctx context.Context, path string) error {
			return app.service.DeleteBranch(ctx, path, name, force)
		})
	})
}

func (app *nativeApp) tagDialog() {
	if app.repository == nil || app.busy || !app.writable || app.readTask != nil {
		return
	}
	hash := app.commitHash
	if hash == "" {
		hash = app.repository.snapshot.Head
	}
	app.referenceDialog(true, locales.ReferenceCommit(), hash, false)
}

func (app *nativeApp) deleteTag(name string) {
	app.confirm(locales.DeleteLocalTag(), name, locales.Delete(), func() {
		app.mutate(locales.DeletingTag(), func(ctx context.Context, path string) error {
			return app.service.DeleteTag(ctx, path, name)
		})
	})
}

func (app *nativeApp) mergeDialog() {
	var names []string
	for _, branch := range app.repository.branches {
		if !branch.Current {
			names = append(names, branch.Name)
		}
	}
	if len(names) == 0 {
		app.problem = locales.NoMergeBranches()
		return
	}
	app.dialog = &nativeDialog{
		title: locales.MergeIntoCurrent(), detail: app.repository.snapshot.Branch, action: locales.Merge(),
		options: names, choice: names[0],
		submit: func(dialog *nativeDialog) {
			name := dialog.choice
			app.leaveConflict(func() {
				app.dialog = dialog
				app.mutateForm(dialog, locales.Merging(), func(ctx context.Context, path string) error {
					return app.service.Merge(ctx, path, name)
				})
			})
		},
	}
}

func (app *nativeApp) stashDialog() {
	app.dialog = &nativeDialog{
		title: locales.NewStash(), action: locales.Stash(), fields: []nativeField{{label: locales.OptionalDescription(), optional: true}},
		checkLabel: locales.IncludeUntracked(),
		canSubmit: func() bool {
			for _, file := range app.repository.snapshot.Files {
				if !file.Conflict && (file.Index != "?" || app.dialog != nil && app.dialog.check) {
					return true
				}
			}
			return false
		},
		submit: func(dialog *nativeDialog) {
			message, untracked := dialog.fields[0].value, dialog.check
			app.leaveConflict(func() {
				app.dialog = dialog
				app.mutateForm(dialog, locales.SavingStash(), func(ctx context.Context, path string) error {
					return app.service.CreateStash(ctx, path, message, untracked)
				})
			})
		},
	}
}

func (app *nativeApp) applyStash(pop bool) {
	if app.repository == nil || app.busy || !app.writable || app.readTask != nil || app.stashHash == "" {
		return
	}
	hash := app.stashHash
	title, detail, action := locales.ApplyStashTitle(), locales.ApplyStashDetail(app.repository.snapshot.Path), locales.Apply()
	if pop {
		title, detail, action = locales.PopStashTitle(), locales.PopStashDetail(app.repository.snapshot.Path), locales.ApplyAndDrop()
	}
	app.confirm(title, detail, action, func() {
		app.mutate(locales.ApplyingStash(), func(ctx context.Context, path string) error {
			return app.service.ApplyStash(ctx, path, hash, pop)
		})
	})
	app.dialog.danger = pop
}

func (app *nativeApp) dropStash() {
	if app.repository == nil || app.busy || !app.writable || app.readTask != nil || app.stashHash == "" {
		return
	}
	hash := app.stashHash
	app.confirm(locales.DeleteStashTitle(), shortHash(hash)+"\n"+locales.CannotUndo(), locales.Delete(), func() {
		app.mutate(locales.DeletingStash(), func(ctx context.Context, path string) error {
			return app.service.DropStash(ctx, path, hash)
		})
	})
}

func (app *nativeApp) discardFile() {
	path, file := app.repository.snapshot.Path, app.selectedFile
	app.run(locales.CheckingRecovery(), func(ctx context.Context) (func(), error) {
		preview, err := app.service.DiscardPreview(ctx, path, file)
		return func() {
			if err != nil {
				return
			}
			if !preview.Supported {
				app.problem = preview.Reason
				return
			}
			app.confirm(locales.DiscardFileTitle(), file+"\n"+locales.DiscardFileDetail(), locales.DiscardChanges(), func() {
				app.mutate(locales.DiscardingWithBackup(), func(ctx context.Context, path string) error {
					_, err := app.service.Discard(ctx, path, file, preview.Token)
					return err
				})
			})
		}, err
	})
}

func (app *nativeApp) resolveConflict(choice string) {
	conflict := *app.conflict
	content := ""
	if app.conflictEdit.Buffer != nil {
		content = app.conflictEdit.Buffer.String()
	}
	action := func() {
		app.mutate(locales.ResolvingConflict(), func(ctx context.Context, path string) error {
			return app.service.ResolveConflict(ctx, path, conflict.Path, conflict.Token, choice, content)
		})
	}
	if choice == "result" {
		action()
	} else {
		title, detail, label := locales.ResolveOursTitle(), locales.ResolveOursDetail(), locales.UseOurs()
		switch choice {
		case "theirs":
			title, detail, label = locales.ResolveTheirsTitle(), locales.ResolveTheirsDetail(), locales.UseTheirs()
		case "delete":
			title, detail, label = locales.ResolveDeleteTitle(), locales.ResolveDeleteDetail(), locales.DeleteFile()
		}
		app.confirm(title, conflict.Path+"\n"+detail, label, action)
	}
}

func (app *nativeApp) operationAction(action string) {
	operation := app.repository.snapshot.Operation
	fn := func() {
		app.leaveConflict(func() {
			app.mutate(locales.ProcessingOperation(operationName(operation.Kind)), func(ctx context.Context, path string) error {
				return app.service.OperationAction(ctx, path, operation.Kind, operation.Token, action)
			})
		})
	}
	if action == "continue" {
		fn()
	} else {
		title, detail, label := locales.AbortOperationTitle(operationName(operation.Kind)), locales.AbortOperationDetail(), locales.AbortOperation(operationName(operation.Kind))
		if action == "skip" {
			title, detail, label = locales.SkipCommitTitle(), locales.SkipCommitDetail(), locales.SkipCommit()
		}
		app.confirm(title, app.repository.snapshot.Path+"\n"+detail, label, fn)
	}
}

func (app *nativeApp) remoteAction(action string) {
	if len(app.repository.remotes) == 0 {
		app.editRemoteDialog(nil)
		app.dialog.detail = locales.NoRemoteAddress()
		return
	}
	if action == "pull" && app.repository.snapshot.Upstream == "" {
		app.dialog = &nativeDialog{
			title: locales.NoUpstreamTitle(), detail: locales.NoUpstreamDetail(),
			action: locales.PublishBranchMenu(), submit: func(_ *nativeDialog) { app.publishDialog() },
		}
		return
	}
	if action == "push" && app.repository.snapshot.Upstream == "" {
		app.publishDialog()
		return
	}
	app.leaveConflict(func() {
		app.mutate(map[string]string{"fetch": locales.Fetching(), "pull": locales.Pulling(), "push": locales.Pushing()}[action], func(ctx context.Context, path string) error {
			return app.service.Remote(ctx, path, action)
		})
	})
}

func (app *nativeApp) remoteDialog() {
	app.dialog = &nativeDialog{
		title: locales.RemoteRepositories(),
		body:  app.remoteSettingsView,
	}
}

func (app *nativeApp) remoteSettingsView(c *ui.Context) {
	parent := app.dialog
	if len(app.repository.remotes) == 0 {
		ui.Text(c, locales.NoRemotes()).TextColor(c.Theme().TextMuted)
	}
	for _, remote := range app.repository.remotes {
		remote := remote
		ui.Column(c).Key(remote.Name).Gap(6).FillWidth().Children(func() {
			ui.Text(c, remote.Name).Bold()
			ui.Text(c, strings.Join(remote.URLs, "\n")).FontSize(12).MaxLines(2).Selectable()
			ui.Row(c).Gap(8).Disabled(!app.writable).Children(func() {
				if ui.Button(c, locales.Edit()).Label(locales.EditRemote(remote.Name)).Clicked() {
					app.editRemoteDialog(&remote)
					app.dialog.parent = parent
				}
				if ui.Button(c, locales.RemoveMenu()).Label(locales.RemoveRemote(remote.Name)).Clicked() {
					app.dialog = &nativeDialog{
						parent: parent, title: locales.RemoveRemoteTitle(),
						detail: remote.Name + "\n" + locales.RemoveRemoteDetail(),
						action: locales.Remove(), danger: true,
						submit: func(dialog *nativeDialog) {
							app.mutateForm(dialog, locales.RemovingRemote(), func(ctx context.Context, path string) error {
								return app.service.RemoveRemote(ctx, path, remote.Name, remote.Token)
							})
						},
					}
				}
			})
		})
		ui.Divider(c)
	}
	if ui.Button(c, locales.AddRemote()).Disabled(!app.writable).FillWidth().Clicked() {
		app.editRemoteDialog(nil)
		app.dialog.parent = parent
	}
}

func (app *nativeApp) editRemoteDialog(remote *gitrepo.RemoteConfig) {
	name, address, token := "", "", ""
	if remote != nil {
		name, token = remote.Name, remote.Token
		if len(remote.URLs) > 0 {
			address = remote.URLs[0]
		}
	}
	app.dialog = &nativeDialog{
		title: locales.RemoteAddress(), action: locales.Save(),
		fields: []nativeField{{label: locales.RemoteName(), value: name, readOnly: remote != nil}, {label: locales.FetchAddress(), value: address}},
		submit: func(dialog *nativeDialog) {
			nextName, nextAddress := dialog.fields[0].value, dialog.fields[1].value
			if remote != nil && nextName != name {
				dialog.problem = locales.CannotRenameRemote()
				return
			}
			app.mutateForm(dialog, locales.SavingRemote(), func(ctx context.Context, path string) error {
				return app.service.SaveRemote(ctx, path, nextName, nextAddress, token)
			})
		},
	}
}

func (app *nativeApp) publishDialog() {
	if len(app.repository.remotes) == 0 {
		app.remoteDialog()
		return
	}
	snapshot := app.repository.snapshot
	remotes := append([]gitrepo.RemoteConfig{}, app.repository.remotes...)
	var names []string
	for _, remote := range remotes {
		names = append(names, remote.Name)
	}
	app.dialog = &nativeDialog{
		title: locales.PublishBranch(), action: locales.Publish(),
		options: names, choice: names[0],
		fields: []nativeField{{label: locales.RemoteBranchName(), value: snapshot.Branch}},
		body: func(c *ui.Context) {
			for _, remote := range remotes {
				if app.dialog != nil && remote.Name == app.dialog.choice {
					ui.Text(c, strings.Join(remote.PushURLs, "\n")).FontSize(12).Selectable()
				}
			}
		},
		submit: func(dialog *nativeDialog) {
			for _, remote := range remotes {
				if remote.Name != dialog.choice {
					continue
				}
				request := gitrepo.PublishBranchRequest{
					Remote: remote.Name, Token: remote.Token, Name: dialog.fields[0].value,
					Branch: snapshot.Branch, Head: snapshot.Head,
				}
				app.mutateForm(dialog, locales.PublishingBranch(), func(ctx context.Context, path string) error {
					return app.service.PublishBranch(ctx, path, request)
				})
				return
			}
		},
	}
}

func (app *nativeApp) checkoutDialog(remote string) {
	_, name := remoteBranchParts(remote, app.repository.remotes)
	app.dialog = &nativeDialog{
		title: locales.CheckoutRemoteBranch(), detail: remote, action: locales.Checkout(),
		fields: []nativeField{{label: locales.LocalBranchName(), value: name}},
		submit: func(dialog *nativeDialog) {
			name := dialog.fields[0].value
			app.leaveConflict(func() {
				app.dialog = dialog
				app.mutateForm(dialog, locales.CheckingOutBranch(), func(ctx context.Context, path string) error {
					return app.service.CheckoutRemote(ctx, path, remote, name)
				})
			})
		},
	}
}

func operationName(kind string) string {
	switch kind {
	case "merge":
		return locales.Merge()
	case "rebase":
		return locales.Rebase()
	case "cherry-pick":
		return locales.CherryPick()
	default:
		return kind
	}
}

func (app *nativeApp) repositoryActions(menu *ui.Menu) {
	unavailable := app.busy || app.repository == nil
	if menu.Item(locales.NewTagMenu()).Disabled(unavailable || !app.writable || app.readTask != nil).Chosen() {
		app.tagDialog()
	}
	if menu.Item(locales.StashChangesMenu()).Disabled(unavailable || !app.writable ||
		len(app.repository.snapshot.Files) == 0 || app.repository.snapshot.Operation.Kind != "").Chosen() {
		app.stashDialog()
	}
	menu.Separator()
	if menu.Item(locales.RevealFinder()).Disabled(unavailable).Chosen() {
		if err := app.service.RevealRepository(app.repository.snapshot.Path); err != nil {
			app.problem = err.Error()
		}
	}
	if menu.Item(locales.OpenBackupFolder()).Disabled(unavailable).Chosen() {
		if err := app.service.RevealDiscardBackups(); err != nil {
			app.problem = err.Error()
		}
	}
}
