// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

type nativeReferenceCreation struct {
	path     string
	hash     string
	tag      bool
	created  bool
	name     string
	checking bool
	checked  bool
	problem  string
	revision uint64
	cancel   context.CancelFunc
}

func (app *nativeApp) canCreateReference() bool {
	return app.repository != nil && app.writable && !app.busy && !app.closed && app.dialog == nil
}

func (app *nativeApp) referenceFrom(tag bool, kind, name string) {
	if !app.canCreateReference() {
		return
	}
	path := app.repository.snapshot.Path
	namespace, label := "refs/heads/", locales.ReferenceBranch()
	if kind == "tag" {
		namespace, label = "refs/tags/", locales.ReferenceTag()
	}
	app.run(locales.ReadingCreationTarget(), func(ctx context.Context) (func(), error) {
		hash, err := app.service.ResolveCreationTarget(ctx, path, namespace+name)
		return func() {
			if err == nil && app.repository != nil && app.repository.snapshot.Path == path {
				app.referenceDialog(tag, label+" "+name, hash, false)
			}
		}, err
	})
}

func (app *nativeApp) referenceDialog(tag bool, source, hash string, checkout bool) {
	if !app.canCreateReference() {
		return
	}
	state := &nativeReferenceCreation{path: app.repository.snapshot.Path, hash: hash, tag: tag}
	title, field := locales.NewBranch(), locales.BranchName()
	if tag {
		title, field = locales.NewLocalTag(), locales.TagName()
	}
	dialog := &nativeDialog{
		title: title, action: locales.Create(), width: 580,
		detail: locales.CreationTarget(source + " · " + shortHash(hash)),
		fields: []nativeField{{label: field}}, check: checkout,
	}
	if hash == "" {
		dialog.detail = locales.CreationTarget(source)
		if tag {
			state.problem = locales.NoCreationCommit()
		} else {
			dialog.detail += "\n" + locales.UnbornBranchTarget()
			dialog.check = true
		}
	}
	dialog.cancel = func() {
		if state.cancel != nil {
			state.cancel()
		}
	}
	dialog.onChange = func() { app.checkReferenceName(dialog, state) }
	dialog.canSubmit = func() bool {
		return state.created || state.checked && !state.checking && state.problem == "" &&
			state.name == dialog.fields[0].value && (!tag || hash != "")
	}
	dialog.body = func(c *ui.Context) {
		ui.Row(c).FillWidth().MinHeight(24).Gap(6).AlignItems(ui.Center).Children(func() {
			switch {
			case state.created:
				ui.Text(c, locales.BranchCreated(state.name)).FontSize(12).Selectable()
			case state.checking:
				ui.Spinner(c).Size(12, 12)
				ui.Text(c, locales.CheckingReferenceName()).FontSize(12).TextColor(c.Theme().TextMuted)
			case state.problem != "":
				ui.Text(c, state.problem).FontSize(12).TextColor(c.Theme().Danger).
					Grow(1).MinWidth(0).MaxLines(3).Selectable().Tooltip(state.problem)
				if ui.Button(c, locales.RecheckReferenceName()).Clicked() {
					app.checkReferenceName(dialog, state)
				}
			case state.checked:
				ui.Text(c, locales.ReferenceNameAvailable()).FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
		if !tag && !state.created {
			ui.Checkbox(c, &dialog.check, locales.SwitchAfterCreation()).Disabled(hash == "")
		}
		switch {
		case state.created:
			dialog.action = locales.RetryBranchSwitch()
		case dialog.check && !tag:
			dialog.action = locales.CreateAndSwitch()
		default:
			dialog.action = locales.Create()
		}
	}
	dialog.submit = func(_ *nativeDialog) {
		if app.repository == nil || app.repository.snapshot.Path != state.path {
			return
		}
		name, shouldSwitch, alreadyCreated := dialog.fields[0].value, dialog.check && !tag, state.created
		dialog.preserveHistory = !shouldSwitch
		created := alreadyCreated
		label := locales.CreatingBranch()
		if tag {
			label = locales.CreatingTag()
		} else if alreadyCreated {
			label = locales.SwitchingBranch()
		}
		dialog.onResult = func(err error) {
			if err == nil {
				switch {
				case tag:
					app.status = locales.LocalTagCreated(name)
				case shouldSwitch:
					app.status = locales.BranchCreatedAndSwitched(name)
				default:
					app.status = locales.BranchCreated(name)
				}
			} else if created && !tag {
				state.created, state.name = true, name
				dialog.fields[0].readOnly = true
				dialog.dismiss = locales.Close()
			}
		}
		submit := func() {
			app.dialog = dialog
			app.mutateForm(dialog, label, func(ctx context.Context, path string) error {
				if tag {
					return app.service.CreateTag(ctx, path, name, hash)
				}
				if hash == "" {
					return app.service.CreateBranch(ctx, path, name)
				}
				if !alreadyCreated {
					if err := app.service.CreateBranchAt(ctx, path, name, hash); err != nil {
						return err
					}
					created = true
				}
				if shouldSwitch {
					if err := app.service.SwitchCreatedBranch(ctx, path, name, hash); err != nil {
						return fmt.Errorf("%s: %w", locales.BranchCreatedSwitchFailed(name), err)
					}
				}
				return nil
			})
		}
		if shouldSwitch && app.conflictDirty() {
			app.leaveConflict(submit)
			app.dialog.parent = dialog
		} else {
			submit()
		}
	}
	app.dialog = dialog
}

func (app *nativeApp) checkReferenceName(dialog *nativeDialog, state *nativeReferenceCreation) {
	if state.created || app.closed || app.dialog != dialog {
		return
	}
	if state.cancel != nil {
		state.cancel()
	}
	state.revision++
	state.name, state.problem, state.checked = dialog.fields[0].value, "", false
	dialog.problem = ""
	if state.name == "" || state.tag && state.hash == "" {
		state.checking = false
		if state.tag && state.hash == "" {
			state.problem = locales.NoCreationCommit()
		}
		return
	}
	state.checking = true
	name, path, tag, revision := state.name, state.path, state.tag, state.revision
	ctx, cancel := context.WithTimeout(app.ctx, 5*time.Second)
	state.cancel = cancel
	go func() {
		defer cancel()
		timer := time.NewTimer(180 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		err := app.service.CheckReferenceName(ctx, path, name, tag)
		if ctx.Err() == context.Canceled {
			return
		}
		app.post(func() {
			if app.closed || app.dialog != dialog || state.revision != revision ||
				dialog.fields[0].value != name || app.repository == nil || app.repository.snapshot.Path != path {
				return
			}
			state.checking, state.checked, state.problem = false, true, ""
			if err != nil {
				state.problem = err.Error()
			}
		})
	}()
}
