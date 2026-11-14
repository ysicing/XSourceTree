// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ysicing/51code/internal/workspace"
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

type nativeField struct {
	label    string
	value    string
	optional bool
	readOnly bool
}

type nativeDialog struct {
	parent          *nativeDialog
	title           string
	titleText       func() string
	width           float32
	height          float32
	detail          string
	action          string
	dismiss         string
	fields          []nativeField
	check           bool
	checkLabel      string
	options         []string
	choice          string
	choiceChanged   func()
	body            func(*ui.Context)
	submit          func(*nativeDialog)
	danger          bool
	problem         string
	canSubmit       func() bool
	blocked         bool
	onClose         func()
	closeLabel      string
	onChange        func()
	onResult        func(error)
	cancel          func()
	preserveHistory bool
}

func (dialog *nativeDialog) valid() bool {
	if dialog.blocked {
		return false
	}
	for _, field := range dialog.fields {
		if !field.optional && strings.TrimSpace(field.value) == "" {
			return false
		}
	}
	return dialog.canSubmit == nil || dialog.canSubmit()
}

func (app *nativeApp) closeDialog(dialog *nativeDialog) {
	if app.busy || app.dialog != dialog {
		return
	}
	if dialog.cancel != nil {
		dialog.cancel()
	}
	if dialog.onClose != nil {
		dialog.onClose()
	} else {
		app.dialog = dialog.parent
	}
}

func (app *nativeApp) dialogView(c *ui.Context) {
	dialog := app.dialog
	if dialog == nil {
		return
	}
	open := true
	width, height := c.Size()
	panelWidth := dialog.width
	if panelWidth == 0 {
		panelWidth = 580
	}
	ui.Modal(c, &open, func() {
		panel := ui.Column(c).Key(dialog).Width(panelWidth).MaxWidth(max(0, width-c.Theme().Space(20))).Gap(12)
		if dialog.height > 0 {
			panel.Height(min(dialog.height, max(0, height-c.Theme().Space(20))))
		}
		panel.Children(func() {
			header := func() {
				fontSize := float32(20)
				if dialog.onClose != nil {
					fontSize = 16
				}
				title := dialog.title
				if dialog.titleText != nil {
					title = dialog.titleText()
				}
				if dialog.closeLabel != "" {
					button := ui.Button(c, "").Label(dialog.closeLabel).Tooltip(dialog.closeLabel).
						Size(28, 28).Padding(4).Disabled(app.busy)
					button.Children(func() {
						sfsymbols.Symbol(c, "xmark.circle.fill").Size(18).Color(c.Theme().TextMuted).Fallback(nativeIcons["close"])
					})
					if button.Clicked() {
						app.closeDialog(dialog)
					}
					ui.Text(c, title).FontSize(16).Bold().Grow(1).MinWidth(0).SingleLine().TextAlign(ui.Center).Tooltip(title)
					ui.Box(c).Size(28, 28)
					return
				}
				ui.Text(c, title).FontSize(fontSize).Bold().Grow(1).MinWidth(0)
				if dialog.onClose != nil {
					closeButton := ui.Button(c, "").Label(locales.CloseSettings()).Tooltip(locales.CloseSettings()).
						Size(28, 28).Padding(4).Disabled(app.busy)
					closeButton.Children(func() {
						sfsymbols.Symbol(c, "xmark.circle.fill").Size(18).Color(c.Theme().TextMuted).Fallback(nativeIcons["close"])
					})
					if closeButton.Clicked() {
						app.closeDialog(dialog)
					}
				}
			}
			if dialog.onClose != nil {
				ui.Toolbar(c, header).Label(locales.SettingsActions()).FillWidth().Padding(0)
			} else {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(header)
			}
			if dialog.detail != "" {
				ui.Text(c, dialog.detail).MaxLines(5).Selectable().Tooltip(dialog.detail)
			}
			submitted := false
			focused := false
			for index := range dialog.fields {
				field := &dialog.fields[index]
				ui.Column(c).Key(index).Gap(5).FillWidth().Children(func() {
					ui.Text(c, field.label).FontSize(12).TextColor(c.Theme().TextMuted)
					input := ui.TextInput(c, &field.value).Label(field.label).ReadOnly(field.readOnly).Disabled(app.busy).FillWidth()
					if dialog.onChange != nil {
						input.OnChange(dialog.onChange)
					}
					if !focused && !field.readOnly {
						input.AutoFocus()
						focused = true
					}
					submitted = input.Submitted() || submitted
				})
			}
			if len(dialog.options) > 0 {
				ui.Select(c, &dialog.choice, dialog.options).Disabled(app.busy).FillWidth().OnChange(dialog.choiceChanged)
			}
			if dialog.checkLabel != "" {
				ui.Checkbox(c, &dialog.check, dialog.checkLabel).Disabled(app.busy)
			}
			if dialog.body != nil {
				body := ui.Column(c).FillWidth().Disabled(app.busy)
				if dialog.height > 0 {
					body.Grow(1).MinHeight(0)
				}
				body.Children(func() { dialog.body(c) })
			}
			if dialog.problem != "" {
				ui.Text(c, dialog.problem).TextColor(c.Theme().Danger).MaxLines(4).Selectable().Tooltip(dialog.problem)
			}
			if dialog.onClose != nil && dialog.action == "" {
				return
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				dismiss := dialog.dismiss
				if dismiss == "" {
					dismiss = locales.Cancel()
				}
				if ui.Button(c, dismiss).Disabled(app.busy).Clicked() {
					// 与标题栏关闭、Esc 走同一路径，未保存草稿需经 onClose 确认。
					app.closeDialog(dialog)
				}
				if dialog.action != "" {
					button := ui.PrimaryButton(c, dialog.action).Disabled(app.busy || !dialog.valid())
					if dialog.danger {
						button.Background(c.Theme().Danger)
					}
					if (button.Clicked() || submitted && !dialog.danger) && app.dialog == dialog && !app.busy && dialog.valid() {
						dialog.problem = ""
						dialog.submit(dialog)
					}
				}
			})
		})
	})
	if !open && !app.busy {
		app.closeDialog(dialog)
	}
}

func (app *nativeApp) confirm(title, detail, action string, fn func()) {
	app.dialog = &nativeDialog{
		title: title, detail: detail, action: action, danger: true,
		submit: func(_ *nativeDialog) {
			app.dialog = nil
			fn()
		},
	}
}

func (app *nativeApp) libraryChange(label string, task func(context.Context) (workspace.Library, error)) {
	dialog := app.dialog
	app.run(label, func(ctx context.Context) (func(), error) {
		library, err := task(ctx)
		return func() {
			if err == nil {
				app.library = library
				if app.dialog == dialog {
					app.dialog = nil
				}
			}
		}, err
	})
}

func (app *nativeApp) chooseDirectories(title, button string, multiple bool) ([]string, error) {
	busy := app.busy
	app.busy = true
	defer func() { app.busy = busy }()

	return mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: app.win, Title: title, ButtonLabel: button, Directory: true, Multiple: multiple,
	})
}

func (app *nativeApp) chooseLocal() {
	if app.busy {
		return
	}
	paths, err := app.chooseDirectories(locales.AddExistingRepository(), locales.Add(), true)
	if err != nil {
		app.problem = err.Error()
		return
	}
	if len(paths) > 0 {
		group := app.group
		app.libraryChange(locales.AddingRepositories(), func(ctx context.Context) (workspace.Library, error) {
			return app.service.AddRepositories(ctx, paths, group)
		})
	}
}

func (app *nativeApp) pathDialog() {
	app.dialog = &nativeDialog{
		title: locales.AddLocalRepository(), action: locales.Add(), fields: []nativeField{{label: locales.FullRepositoryPath()}},
		submit: func(dialog *nativeDialog) {
			path, group := dialog.fields[0].value, app.group
			app.libraryChange(locales.AddingRepositories(), func(ctx context.Context) (workspace.Library, error) {
				return app.service.AddRepositories(ctx, []string{path}, group)
			})
		},
	}
}

func (app *nativeApp) groupDialog(id string) {
	value, title := "", locales.NewGroup()
	for _, group := range app.library.Groups {
		if group.ID == id {
			value, title = group.Name, locales.RenameGroup()
		}
	}
	app.dialog = &nativeDialog{
		title: title, action: locales.Save(), fields: []nativeField{{label: locales.GroupName(), value: value}},
		submit: func(dialog *nativeDialog) {
			name := dialog.fields[0].value
			app.libraryChange(locales.SavingGroup(), func(_ context.Context) (workspace.Library, error) {
				if id == "" {
					return app.service.CreateGroup(name)
				}
				return app.service.RenameGroup(id, name)
			})
		},
	}
}

func (app *nativeApp) removeGroup(group workspace.Group) {
	app.confirm(locales.RemoveGroupTitle(), locales.RemoveGroupDetail(), locales.Remove(), func() {
		app.libraryChange(locales.RemovingGroup(), func(_ context.Context) (workspace.Library, error) {
			return app.service.RemoveGroup(group.ID)
		})
		app.group = ""
	})
}

func (app *nativeApp) groupActions(group workspace.Group) {
	app.dialog = &nativeDialog{
		title: group.Name,
		body: func(c *ui.Context) {
			if ui.Button(c, locales.Rename()).FillWidth().Clicked() {
				app.groupDialog(group.ID)
			}
			if ui.Button(c, locales.RemoveGroupMenu()).FillWidth().Clicked() {
				app.removeGroup(group)
			}
		},
	}
}

func (app *nativeApp) groupChoices() ([]string, map[string]string) {
	options, ids := []string{locales.Ungrouped()}, map[string]string{locales.Ungrouped(): ""}
	for _, group := range app.library.Groups {
		label := group.Name
		if _, exists := ids[label]; exists {
			label += " (" + group.ID + ")"
		}
		options, ids[label] = append(options, label), group.ID
	}
	return options, ids
}

func (app *nativeApp) repositoryEntryDialog(entry workspace.LocalRepository) {
	options, ids := app.groupChoices()
	choice := options[0]
	for label, id := range ids {
		if id == entry.GroupID {
			choice = label
		}
	}
	app.dialog = &nativeDialog{
		title: entry.Name, detail: entry.Path, options: options, choice: choice, action: locales.MoveToGroup(),
		submit: func(dialog *nativeDialog) {
			group := ids[dialog.choice]
			app.libraryChange(locales.MovingRepository(), func(_ context.Context) (workspace.Library, error) {
				return app.service.MoveRepository(entry.Path, group)
			})
		},
		body: func(c *ui.Context) {
			if ui.Button(c, revealRepositoryLabel()).FillWidth().Clicked() {
				if err := app.service.RevealRepository(entry.Path); err != nil {
					app.problem = err.Error()
				}
			}
			if ui.Button(c, locales.RemoveFromListMenu()).FillWidth().Clicked() {
				app.confirm(locales.RemoveRepositoryTitle(), entry.Path+"\n"+locales.KeepDiskFiles(), locales.Remove(), func() {
					app.libraryChange(locales.RemovingRecord(), func(_ context.Context) (workspace.Library, error) {
						return app.service.RemoveRepository(entry.Path)
					})
				})
			}
		},
	}
}

func (app *nativeApp) chooseScan() {
	if app.busy || app.closed {
		return
	}
	paths, err := app.chooseDirectories(locales.ScanFolders(), locales.Scan(), false)
	if err != nil {
		app.problem = err.Error()
		return
	}
	if len(paths) == 0 {
		return
	}
	app.scanning = true
	app.runTask(locales.ScanningFolders(), true, func(ctx context.Context) (func(), error) {
		result, err := app.service.Scan(ctx, paths[0])
		return func() {
			app.scanning = false
			if err == nil {
				app.scanResult = &result
				app.scanSelected = map[string]bool{}
				app.scanDialog()
			}
		}, err
	})
}

func (app *nativeApp) scanDialog() {
	result := app.scanResult
	options, ids := app.groupChoices()
	choice := options[0]
	for label, id := range ids {
		if id == app.group {
			choice = label
		}
	}
	var notices []string
	if result.Truncated {
		notices = append(notices, locales.ScanLimit())
	}
	notices = append(notices, result.Warnings...)
	app.dialog = &nativeDialog{
		title: locales.SelectRepositories(), detail: strings.Join(notices, "\n"), choice: choice, action: locales.AddSelectedRepositories(),
		canSubmit: func() bool {
			for _, entry := range result.Repositories {
				if app.scanSelected[entry.Path] {
					return true
				}
			}
			return false
		},
		body: func(c *ui.Context) {
			selected := 0
			for _, entry := range result.Repositories {
				if app.scanSelected[entry.Path] {
					selected++
				}
			}
			ui.Column(c).FillWidth().Gap(12).Children(func() {
				ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, locales.AddTo()).FontSize(13).TextColor(c.Theme().TextMuted)
					ui.Select(c, &app.dialog.choice, options).Label(locales.RepositoryGroups()).Grow(1).MinWidth(0)
				})
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, locales.SelectedRepositories(fmt.Sprint(selected), fmt.Sprint(len(result.Repositories)))).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
					if ui.Button(c, locales.SelectAll()).FontSize(12).Disabled(selected == len(result.Repositories)).Clicked() {
						for _, entry := range result.Repositories {
							app.scanSelected[entry.Path] = true
						}
					}
					if ui.Button(c, locales.ClearSelection()).FontSize(12).Disabled(selected == 0).Clicked() {
						app.scanSelected = map[string]bool{}
					}
				})
				ui.Scroll(c).Height(320).FillWidth().Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
					ui.Column(c).FillWidth().Padding(4).Gap(2).Children(func() {
						if len(result.Repositories) == 0 {
							ui.Text(c, locales.NoRepositoriesFound()).FillWidth().Padding(24).TextColor(c.Theme().TextMuted)
						}
						for _, entry := range result.Repositories {
							ui.Column(c).Key(entry.Path).FillWidth().Children(func() {
								value := app.scanSelected[entry.Path]
								row := ui.Checkbox(c, &value, "").Label(entry.Path).Tooltip(entry.Path).FillWidth().Padding(10, 12).Gap(12).Radius(c.Theme().Radius)
								if value {
									row.Background(c.Theme().Accent.Alpha(0.12))
								} else if row.Hovered() {
									row.Background(c.Theme().SurfaceHover)
								}
								row.Children(func() {
									ui.Icon(c, nativeIcons["folder"]).FontSize(22).TextColor(c.Theme().TextMuted)
									ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
										name := entry.Name
										if name == "" {
											name = filepath.Base(entry.Path)
										}
										ui.Text(c, name).FontSize(14).Bold().MaxLines(1)
										ui.Text(c, filepath.Dir(entry.Path)).FontSize(12).TextColor(c.Theme().TextMuted).MaxLines(1)
									})
								})
								if row.Changed() {
									app.scanSelected[entry.Path] = value
								}
							})
						}
					})
				})
				ui.Divider(c)
			})
		},
		submit: func(dialog *nativeDialog) {
			var paths []string
			for _, entry := range result.Repositories {
				if app.scanSelected[entry.Path] {
					paths = append(paths, entry.Path)
				}
			}
			if len(paths) == 0 {
				app.problem = locales.SelectRepositoryFirst()
				return
			}
			group := ids[dialog.choice]
			app.libraryChange(locales.AddingRepositories(), func(ctx context.Context) (workspace.Library, error) {
				return app.service.AddRepositories(ctx, paths, group)
			})
		},
	}
}

func (app *nativeApp) linesDialog() {
	if !app.canApplyPatch() {
		return
	}
	app.dialog = &nativeDialog{
		title: locales.SelectChangedLines(), action: locales.ApplySelectedLines(),
		detail: locales.SelectLinesDetail(),
		canSubmit: func() bool {
			for _, line := range app.lineChoices {
				if line.Checked {
					return true
				}
			}
			return false
		},
		body: func(c *ui.Context) {
			ui.Scroll(c).Height(340).FillWidth().Children(func() {
				for index := range app.lineChoices {
					line := &app.lineChoices[index]
					ui.Column(c).Key(line.ID).FillWidth().Children(func() {
						ui.Checkbox(c, &line.Checked, line.Text).Font("monospace").FillWidth()
					})
				}
			})
		},
		submit: func(_ *nativeDialog) { app.applyLines() },
	}
}
