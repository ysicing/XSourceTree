// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"time"

	"github.com/ysicing/51code/internal/gitrepo"
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo/ui"
)

func (app *nativeApp) mergeDialog() {
	if app.repository == nil || app.busy || !app.writable {
		return
	}
	var names []string
	refs := make(map[string]string)
	counts := make(map[string]int)
	for _, branch := range app.repository.branches {
		if branch.Current {
			continue
		}
		ref := "refs/heads/" + branch.Name
		if branch.Remote {
			ref = "refs/remotes/" + branch.Name
		}
		// 显式引用避免远程名称与本地分支同名时合入错误来源。
		names = append(names, ref)
		refs[ref] = branch.Name
		counts[branch.Name]++
	}
	if len(names) == 0 {
		app.problem = locales.NoMergeBranches()
		return
	}
	for _, count := range counts {
		if count > 1 {
			for _, ref := range names {
				refs[ref] = ref
			}
			break
		}
	}
	selected := names[0]
	mode := gitrepo.MergeAuto
	modes := []gitrepo.MergeMode{gitrepo.MergeAuto, gitrepo.MergeNoFF, gitrepo.MergeFFOnly, gitrepo.MergeSquash}
	choices := map[gitrepo.MergeMode]*gitrepo.MergeOptions{}
	for _, value := range modes {
		choices[value] = &gitrepo.MergeOptions{Mode: value, Commit: value == gitrepo.MergeAuto || value == gitrepo.MergeNoFF}
	}
	var replay uint64
	dialog := &nativeDialog{
		title: locales.MergeIntoBranch(app.repository.snapshot.Branch), action: locales.Merge(),
		width: 500, closeLabel: locales.CloseMergeDialog(),
		canSubmit: func() bool { return app.writable && selected != "" },
	}
	dialog.body = func(c *ui.Context) {
		_, height := c.Size()
		contentHeight := max(0, height-148)
		if dialog.problem != "" {
			contentHeight = max(0, contentHeight-80)
		}
		ui.Scroll(c).FillWidth().MinHeight(min(380, contentHeight)).MaxHeight(contentHeight).Children(func() {
			ui.Column(c).FillWidth().Gap(14).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, locales.MergeSource()).Width(86)
					ui.MenuButton(c, refs[selected], func(menu *ui.Menu) {
						for _, ref := range names {
							if menu.Item(refs[ref]).Radio(ref == selected).Chosen() {
								selected = ref
							}
						}
					}).Label(locales.MergeSource()).Grow(1).MinWidth(0).Justify(ui.SpaceBetween).Tooltip(selected)
				})
				ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(12).Children(func() {
					ui.Text(c, locales.MergeModeLabel()).Width(86).Padding(5, 0)
					ui.RadioGroup(c, func() {
						for _, value := range modes {
							c.Key(value)
							ui.Radio(c, &mode, value, mergeModeLabel(value)).OnChange(func() { replay++ })
						}
					}).Label(locales.MergeModeLabel()).Grow(1).MinWidth(0).Gap(6)
				})
				ui.Text(c, mergeModeDetail(mode)).FontSize(12).TextColor(c.Theme().TextMuted).FillWidth()
				mergePreview(c, mode, &replay)
				ui.Checkbox(c, &choices[mode].Commit, locales.MergeAutoCommit()).Disabled(mode == gitrepo.MergeFFOnly)
				if !choices[mode].Commit && mode != gitrepo.MergeFFOnly {
					hint := locales.MergePendingHint()
					if mode == gitrepo.MergeAuto {
						hint = locales.MergeFastForwardHint()
					}
					ui.Text(c, hint).FontSize(12).TextColor(c.Theme().TextMuted).FillWidth()
				}
			})
		})
		ui.Divider(c)
		dialog.action = locales.Merge()
		if mode == gitrepo.MergeSquash {
			dialog.action = locales.SquashMerge()
		}
	}
	dialog.submit = func(dialog *nativeDialog) {
		ref := selected
		options := *choices[mode]
		app.leaveConflict(func() {
			app.dialog = dialog
			app.mutateForm(dialog, locales.Merging(), func(ctx context.Context, path string) error {
				return app.service.Merge(ctx, path, ref, options)
			})
		})
	}
	app.dialog = dialog
}

func mergeModeLabel(mode gitrepo.MergeMode) string {
	switch mode {
	case gitrepo.MergeNoFF:
		return locales.MergeAlwaysCommit()
	case gitrepo.MergeFFOnly:
		return locales.MergeFastForwardOnly()
	case gitrepo.MergeSquash:
		return locales.MergeSquashLabel()
	default:
		return locales.MergeAutomatic()
	}
}

func mergeModeDetail(mode gitrepo.MergeMode) string {
	switch mode {
	case gitrepo.MergeNoFF:
		return locales.MergeAlwaysHint()
	case gitrepo.MergeFFOnly:
		return locales.MergeOnlyHint()
	case gitrepo.MergeSquash:
		return locales.MergeSquashHint()
	default:
		return locales.MergeAutomaticHint()
	}
}

func mergePreview(c *ui.Context, mode gitrepo.MergeMode, replay *uint64) {
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.MergePreview()).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
			ui.Button(c, locales.ReplayMergePreview()).FontSize(12).Disabled(c.Preferences().ReduceMotion).
				OnClick(func() { *replay++ })
		})
		c.Key(mode)
		canvas := ui.Box(c).FillWidth().Height(112).Role(ui.RoleImage).
			Label(locales.MergePreview()).Description(mergeModeDetail(mode))
		timeline := canvas.Timeline("merge", ui.TimelineOptions{Run: *replay, Delay: 200 * time.Millisecond})
		progress := timeline.Tween(ui.Tween{
			From: 0, To: 1, Duration: 1200 * time.Millisecond, Ease: ui.CubicBezier(0.77, 0, 0.175, 1),
		})
		theme := *c.Theme()
		canvas.Draw(func(painter *ui.Painter, bounds ui.Rect) {
			drawMergePreview(painter, bounds, mode, progress, theme)
		})
	})
}

func drawMergePreview(painter *ui.Painter, bounds ui.Rect, mode gitrepo.MergeMode, progress float32, theme ui.Theme) {
	start, end := bounds.X+24, bounds.X+bounds.W-48
	step := (end - start) / 4
	top, bottom := bounds.Y+34, bounds.Y+82
	mainColor, sourceColor := theme.Accent, historyColor(&theme, 1)
	node := func(x, y float32, label string, color ui.Color) {
		painter.FillPath(new(ui.Path).Circle(x, y, 4.5), color)
		painter.Text(x-4, y+8, label, 11, theme.TextMuted)
	}
	edge := func(fromX, fromY, toX, toY float32, color ui.Color) {
		path := new(ui.Path).MoveTo(fromX, fromY)
		path.CubeTo((fromX+toX)/2, fromY, (fromX+toX)/2, toY, toX, toY)
		painter.StrokePath(path, 1.8, color)
	}
	if mode == gitrepo.MergeFFOnly {
		edge(start, top, end, top, mainColor)
		node(start, top, "A", mainColor)
		node(start+step*2, top, "B", sourceColor)
		node(end, top, "C", sourceColor)
		pointer := start + (end-start)*progress
		painter.Text(pointer-14, top-28, "main", 12, mainColor)
		painter.Line(pointer, top-12, pointer, top-6, 2, mainColor)
		painter.Text(end-14, top+28, "topic", 12, sourceColor)
		return
	}
	head := start + step
	if mode == gitrepo.MergeNoFF {
		head = start
	}
	edge(start, top, start+step, bottom, sourceColor)
	edge(start+step, bottom, start+step*2, bottom, sourceColor)
	if head != start {
		edge(start, top, head, top, mainColor)
		node(head, top, "B", mainColor)
	}
	node(start, top, "A", mainColor)
	node(start+step, bottom, "C", sourceColor)
	node(start+step*2, bottom, "D", sourceColor)
	painter.Text(start+step*2+12, bottom+8, "topic", 12, sourceColor)
	painter.Text(head+(end-head)*progress-14, top-28, "main", 12, mainColor)
	if progress <= 0 {
		return
	}
	edge(head, top, head+(end-head)*progress, top, mainColor)
	if mode == gitrepo.MergeSquash {
		// 移动的是变更副本，来源提交仍然保留；S 只有当前分支这一个父提交。
		for _, from := range []float32{start + step, start + step*2} {
			color := sourceColor.Alpha(1 - progress)
			painter.FillPath(new(ui.Path).Circle(from+(end-from)*progress, bottom+(top-bottom)*progress, 4), color)
		}
	} else {
		edge(start+step*2, bottom, start+step*2+(end-start-step*2)*progress, bottom+(top-bottom)*progress, sourceColor)
	}
	if progress >= 1 {
		label := "M"
		if mode == gitrepo.MergeSquash {
			label = "S"
		}
		node(end, top, label, mainColor)
	}
}
