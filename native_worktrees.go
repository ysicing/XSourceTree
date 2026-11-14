// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

type nativeWorktrees struct {
	items     []gitrepo.Worktree
	selected  int
	list      *ui.ListState
	loaded    bool
	canceled  bool
	listWidth float32
}

func (state *nativeWorktrees) setItems(items []gitrepo.Worktree) {
	path := ""
	if state.selected >= 0 && state.selected < len(state.items) {
		path = state.items[state.selected].Path
	}
	state.items, state.selected, state.loaded, state.canceled = items, -1, true, false
	for index, item := range items {
		if item.Current && state.selected == -1 {
			state.selected = index
		}
		if path != "" && item.Path == path {
			state.selected = index
			break
		}
	}
	if state.selected == -1 && len(items) > 0 {
		state.selected = 0
	}
	if state.list == nil {
		state.list = &ui.ListState{}
	}
	state.list.Selected = &state.selected
	state.list.Choose(state.selected)
}

func (app *nativeApp) openWorktrees() {
	if app.repository == nil || app.busy || app.dialog != nil {
		return
	}
	app.leaveConflict(func() {
		app.switchWorkspace("worktrees")
		app.loadWorktrees()
	})
}

func (app *nativeApp) loadWorktrees() {
	if app.repository == nil || app.screen != "worktrees" || app.worktrees == nil || app.busy || app.dialog != nil {
		return
	}
	state, path := app.worktrees, app.repository.snapshot.Path
	state.canceled = false
	app.load(locales.ReadingWorktrees(), nativeReadCommit, func(ctx context.Context) (func(), error) {
		items, err := app.service.Worktrees(ctx, path)
		if err != nil {
			return nil, err
		}
		return func() { state.setItems(items) }, nil
	})
}

func (app *nativeApp) createWorktreeDialog() {
	if app.repository == nil || app.screen != "worktrees" || !app.writable || app.busy ||
		app.readTask != nil || app.dialog != nil {
		return
	}
	repository := app.repository.snapshot.Path
	app.dialog = &nativeDialog{
		title: locales.NewWorktreeMenu(), action: locales.Create(),
		fields: []nativeField{
			{
				label: locales.WorktreePath(),
				value: filepath.Join(filepath.Dir(repository), filepath.Base(repository)+"-worktree"),
			},
			{label: locales.BranchName()},
			{label: locales.WorktreeStart(), value: "HEAD", optional: true},
		},
		checkLabel: locales.ExistingWorktreeBranch(),
		submit: func(dialog *nativeDialog) {
			path := dialog.fields[0].value
			branch, start := strings.TrimSpace(dialog.fields[1].value), strings.TrimSpace(dialog.fields[2].value)
			existing := dialog.check
			app.mutateForm(dialog, locales.CreatingWorktree(), func(ctx context.Context, repository string) error {
				_, err := app.service.CreateWorktree(ctx, repository, path, branch, start, existing)
				return err
			})
		},
	}
}

func (app *nativeApp) reviewWorktree(path string, remove bool) {
	if app.repository == nil || app.screen != "worktrees" || !app.writable || app.busy || app.dialog != nil {
		return
	}
	repository := app.repository.snapshot.Path
	app.load(locales.ReviewingWorktree(), nativeReadCommit, func(ctx context.Context) (func(), error) {
		review, err := app.service.ReviewWorktree(ctx, repository, path)
		if err != nil {
			return nil, err
		}
		return func() {
			item := review.Worktree
			dialog := &nativeDialog{width: 660}
			if remove {
				dialog.title = locales.RemoveWorktreeMenu()
				dialog.detail = item.Path + "\n" + locales.WorktreeRemoveDetail()
				if item.Branch != "" {
					dialog.detail += "\n" + locales.BranchName() + ": " + worktreeBranch(item)
				}
				dialog.danger = true
				if review.CanRemove {
					dialog.action = locales.Remove()
					dialog.submit = func(dialog *nativeDialog) {
						app.mutateForm(dialog, locales.RemovingWorktree(), func(ctx context.Context, repository string) error {
							return app.service.RemoveWorktree(ctx, repository, item.Path, review.Token)
						})
					}
				} else {
					dialog.blocked, dialog.problem = true, review.RemoveProblem
				}
			} else {
				dialog.title = locales.MergeWorktreeMenu()
				dialog.detail = worktreeBranch(item) + " (" + shortHash(item.Head) + ") → " +
					strings.TrimPrefix(review.TargetBranch, "refs/heads/") + " (" + shortHash(review.TargetHead) + ")"
				dialog.body = func(c *ui.Context) {
					ui.Scroll(c).FillWidth().MaxHeight(200).Children(func() {
						ui.Column(c).FillWidth().MinWidth(0).Gap(8).Children(func() {
							ui.Text(c, item.Path).FillWidth().Selectable().Tooltip(item.Path)
							ui.Text(c, item.Head+" → "+review.TargetHead).FontSize(11).
								FillWidth().Selectable()
						})
					})
				}
				if review.CanMerge {
					dialog.action = locales.Merge()
					dialog.submit = func(dialog *nativeDialog) {
						app.mutateForm(dialog, locales.MergingWorktree(), func(ctx context.Context, repository string) error {
							return app.service.MergeWorktree(ctx, repository, item.Path, review.Token)
						})
					}
				} else {
					dialog.blocked, dialog.problem = true, review.MergeProblem
				}
			}
			if dialog.blocked {
				dialog.dismiss = locales.Close()
			}
			app.dialog = dialog
		}, nil
	})
}

func worktreeBranch(item gitrepo.Worktree) string {
	if item.Detached {
		return locales.DetachedWorktree()
	}
	if item.Bare {
		return locales.BareWorktree()
	}
	return strings.TrimPrefix(item.Branch, "refs/heads/")
}

func (app *nativeApp) worktreeMenu(menu *ui.Menu, item gitrepo.Worktree) {
	if menu.Item(locales.OpenWorktree()).Disabled(app.busy || item.Bare || item.Prunable).Chosen() {
		app.openRepository(item.Path)
	}
	menu.Separator()
	unavailable := app.repository == nil || app.busy || !app.writable || app.readTask != nil ||
		app.dialog != nil || item.Current || item.Bare || item.Locked ||
		item.Prunable || item.Problem != "" || item.Operation != "" || item.Dirty || item.Head == ""
	if menu.Item(locales.MergeWorktreeMenu()).Disabled(unavailable).Chosen() {
		app.reviewWorktree(item.Path, false)
	}
	if menu.Item(locales.RemoveWorktreeMenu()).Danger().Disabled(unavailable || item.Main || item.Ignored).Chosen() {
		app.reviewWorktree(item.Path, true)
	}
}

func (app *nativeApp) worktreePanel(c *ui.Context) {
	if app.repository == nil || app.worktrees == nil {
		return
	}
	state := app.worktrees
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		app.operationBar(c)
		ui.Row(c).FillWidth().Padding(10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.Worktrees()).Bold().Grow(1).MinWidth(0).SingleLine()
			if app.readTask != nil {
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, app.readTask.label).FontSize(11).MinWidth(0).Shrink(1).
					SingleLine().TextColor(c.Theme().TextMuted)
				ui.Button(c, locales.Cancel()).OnClick(func() {
					app.cancelRead()
					state.canceled = true
				})
			} else {
				iconButton(c, locales.Refresh(), nativeIcons["refresh"]).Disabled(app.busy).
					OnClick(app.loadWorktrees)
			}
			ui.PrimaryButton(c, locales.NewWorktreeMenu()).Label(locales.NewWorktreeMenu()).
				Disabled(!app.writable || app.busy || app.readTask != nil || app.dialog != nil).
				OnClick(app.createWorktreeDialog)
		})
		ui.Divider(c)
		if state.canceled {
			ui.Text(c, locales.WorktreeReadCanceled()).FontSize(12).TextColor(c.Theme().TextMuted).
				FillWidth().Padding(8, 12)
		}
		if len(state.items) == 0 {
			ui.Column(c).Fill().Center().Padding(20).Gap(8).Children(func() {
				message := locales.WorktreesNotLoaded()
				switch {
				case app.readTask != nil:
					message = app.readTask.label
				case app.problem != "":
					ui.Text(c, app.problem).FillWidth().MaxLines(5).Selectable().
						Tooltip(app.problem).TextColor(c.Theme().Danger)
					return
				case state.canceled:
					return
				case state.loaded:
					message = locales.EmptyWorktrees()
				}
				ui.Text(c, message).TextColor(c.Theme().TextMuted)
			})
			return
		}
		if state.listWidth == 0 {
			state.listWidth = 280
		}
		ui.Split(c, &state.listWidth, func() {
			app.worktreeList(c, state)
		}, func() {
			app.worktreeDetail(c, state)
		}).Fill().SplitReset(280)
	})
}

func (app *nativeApp) worktreeList(c *ui.Context, state *nativeWorktrees) {
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		if state.list == nil {
			state.list = &ui.ListState{}
		}
		state.list.Selected = &state.selected
		state.list.Key = func(index int) any { return state.items[index].Path }
		state.list.Label = func(index int) string { return state.items[index].Path }
		ui.List(c, state.list, len(state.items), func(index int) {
			item := state.items[index]
			ui.Row(c).FillWidth().MinWidth(0).Height(48).Padding(0, 12).Gap(8).
				AlignItems(ui.Center).Tooltip(item.Path).ContextMenu(func(menu *ui.Menu) {
				app.worktreeMenu(menu, item)
			}).Children(func() {
				ui.Icon(c, nativeIcons["folder"]).FontSize(16).Shrink(0)
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, filepath.Base(item.Path)).FontSize(12).FillWidth().SingleLine().
						Tooltip(item.Path)
					ui.Text(c, worktreeBranch(item)).FontSize(11).FillWidth().SingleLine().
						Tooltip(item.Branch)
				})
				if item.Current {
					ui.Text(c, locales.CurrentWorktree()).FontSize(10).Shrink(0)
				}
			})
		}).Label(locales.WorktreeList()).FillWidth().Grow(1).MinHeight(0)
	})
}

func (app *nativeApp) worktreeDetail(c *ui.Context, state *nativeWorktrees) {
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		if state.selected < 0 || state.selected >= len(state.items) {
			ui.Column(c).Fill().Center().Padding(20).Children(func() {
				ui.Text(c, locales.SelectWorktree()).TextColor(c.Theme().TextMuted)
			})
			return
		}
		item := state.items[state.selected]
		ui.Row(c).FillWidth().Padding(10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Button(c, locales.OpenWorktree()).Disabled(app.busy || item.Bare || item.Prunable).
				OnClick(func() { app.openRepository(item.Path) })
			ui.Spacer(c)
			ui.MenuButton(c, locales.More(), func(menu *ui.Menu) {
				app.worktreeMenu(menu, item)
			}).Label(locales.WorktreeActions()).Disabled(app.busy)
		})
		ui.Divider(c)
		ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().MinWidth(0).Padding(12).Gap(12).Children(func() {
				ui.Text(c, filepath.Base(item.Path)).Bold().FontSize(15).FillWidth().SingleLine().Tooltip(item.Path)
				ui.Column(c).FillWidth().MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, locales.WorktreePath()).FontSize(11).TextColor(c.Theme().TextMuted)
					ui.Text(c, item.Path).FillWidth().MaxLines(4).Selectable().Tooltip(item.Path)
				})
				ui.Column(c).FillWidth().MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, locales.BranchName()).FontSize(11).TextColor(c.Theme().TextMuted)
					ui.Text(c, worktreeBranch(item)).FillWidth().MaxLines(3).Selectable().Tooltip(item.Branch)
					if item.Head != "" {
						ui.Text(c, item.Head).FontSize(11).FillWidth().Selectable()
					}
				})
				for index, status := range []struct {
					show  bool
					label func() string
				}{
					{item.Main, locales.MainWorktree},
					{item.Current, locales.CurrentWorktree},
					{item.Locked, locales.LockedWorktree},
					{item.Prunable, locales.PrunableWorktree},
					{item.Dirty, locales.DirtyWorktree},
					{item.Ignored, locales.IgnoredWorktree},
				} {
					if status.show {
						c.Key(index)
						ui.Text(c, status.label()).FontSize(12).FillWidth()
					}
				}
				for index, reason := range []string{item.LockReason, item.PruneReason} {
					if reason != "" {
						c.Key(index + 6)
						ui.Text(c, reason).FontSize(12).FillWidth().Selectable().Tooltip(reason)
					}
				}
				if item.Operation != "" {
					ui.Text(c, locales.OperationInProgress(operationName(item.Operation))).
						FontSize(12).FillWidth()
				}
				if item.Problem != "" {
					ui.Text(c, item.Problem).TextColor(c.Theme().Danger).FontSize(12).
						FillWidth().Selectable().Tooltip(item.Problem)
				}
				if app.repository.snapshot.Operation.Kind == "merge" {
					ui.Button(c, locales.FileChanges()).OnClick(func() {
						app.leaveConflict(func() { app.switchWorkspace("changes") })
					})
				}
			})
		})
	})
}
