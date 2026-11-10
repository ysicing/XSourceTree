// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ysicing/XSourceTree/internal/workspace"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type nativeField struct {
	label    string
	value    string
	optional bool
	readOnly bool
}

type nativeDialog struct {
	title      string
	detail     string
	action     string
	fields     []nativeField
	check      bool
	checkLabel string
	options    []string
	choice     string
	body       func(*ui.Context)
	submit     func(*nativeDialog)
	danger     bool
	problem    string
	canSubmit  func() bool
	blocked    bool
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

func (app *nativeApp) dialogView(c *ui.Context) {
	dialog := app.dialog
	if dialog == nil {
		return
	}
	open := true
	width, _ := c.Size()
	ui.Modal(c, &open, func() {
		ui.Column(c).Key(dialog).Width(580).MaxWidth(max(0, width-c.Theme().Space(20))).Gap(12).Children(func() {
			ui.Text(c, dialog.title).FontSize(20).Bold()
			if dialog.detail != "" {
				ui.Text(c, dialog.detail).MaxLines(5).Selectable()
			}
			submitted := false
			focused := false
			for index := range dialog.fields {
				field := &dialog.fields[index]
				ui.Column(c).Key(index).Gap(5).FillWidth().Children(func() {
					ui.Text(c, field.label).FontSize(12).TextColor(c.Theme().TextMuted)
					input := ui.TextInput(c, &field.value).Label(field.label).ReadOnly(field.readOnly).Disabled(app.busy).FillWidth()
					if !focused && !field.readOnly {
						input.AutoFocus()
						focused = true
					}
					submitted = input.Submitted() || submitted
				})
			}
			if len(dialog.options) > 0 {
				ui.Select(c, &dialog.choice, dialog.options).Disabled(app.busy).FillWidth()
			}
			if dialog.checkLabel != "" {
				ui.Checkbox(c, &dialog.check, dialog.checkLabel).Disabled(app.busy)
			}
			if dialog.body != nil {
				ui.Column(c).FillWidth().Disabled(app.busy).Children(func() { dialog.body(c) })
			}
			if dialog.problem != "" {
				ui.Text(c, dialog.problem).TextColor(c.Theme().Danger).MaxLines(4).Selectable().Tooltip(dialog.problem)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Disabled(app.busy).Clicked() {
					app.dialog = nil
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
	if !open {
		app.dialog = nil
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
	paths, err := app.chooseDirectories("添加已有的本地仓库", "添加", true)
	if err != nil {
		app.problem = err.Error()
		return
	}
	if len(paths) > 0 {
		group := app.group
		app.libraryChange("正在添加仓库…", func(ctx context.Context) (workspace.Library, error) {
			return app.service.AddRepositories(ctx, paths, group)
		})
	}
}

func (app *nativeApp) pathDialog() {
	app.dialog = &nativeDialog{
		title: "添加本地仓库", action: "添加", fields: []nativeField{{label: "仓库完整路径"}},
		submit: func(dialog *nativeDialog) {
			path, group := dialog.fields[0].value, app.group
			app.libraryChange("正在添加仓库…", func(ctx context.Context) (workspace.Library, error) {
				return app.service.AddRepositories(ctx, []string{path}, group)
			})
		},
	}
}

func (app *nativeApp) groupDialog(id string) {
	value, title := "", "新建仓库组"
	for _, group := range app.library.Groups {
		if group.ID == id {
			value, title = group.Name, "重命名仓库组"
		}
	}
	app.dialog = &nativeDialog{
		title: title, action: "保存", fields: []nativeField{{label: "组名", value: value}},
		submit: func(dialog *nativeDialog) {
			name := dialog.fields[0].value
			app.libraryChange("正在保存分组…", func(_ context.Context) (workspace.Library, error) {
				if id == "" {
					return app.service.CreateGroup(name)
				}
				return app.service.RenameGroup(id, name)
			})
		},
	}
}

func (app *nativeApp) removeGroup(group workspace.Group) {
	app.confirm("移除仓库组？", "仓库将保留在本地列表中，不会删除磁盘文件。", "移除", func() {
		app.libraryChange("正在移除分组…", func(_ context.Context) (workspace.Library, error) {
			return app.service.RemoveGroup(group.ID)
		})
		app.group = ""
	})
}

func (app *nativeApp) groupActions(group workspace.Group) {
	app.dialog = &nativeDialog{
		title: group.Name,
		body: func(c *ui.Context) {
			if ui.Button(c, "重命名").FillWidth().Clicked() {
				app.groupDialog(group.ID)
			}
			if ui.Button(c, "移除仓库组…").FillWidth().Clicked() {
				app.removeGroup(group)
			}
		},
	}
}

func (app *nativeApp) groupChoices() ([]string, map[string]string) {
	options, ids := []string{"未分组"}, map[string]string{"未分组": ""}
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
		title: entry.Name, detail: entry.Path, options: options, choice: choice, action: "移动到分组",
		submit: func(dialog *nativeDialog) {
			group := ids[dialog.choice]
			app.libraryChange("正在移动仓库…", func(_ context.Context) (workspace.Library, error) {
				return app.service.MoveRepository(entry.Path, group)
			})
		},
		body: func(c *ui.Context) {
			if ui.Button(c, "在 Finder 中显示").FillWidth().Clicked() {
				if err := app.service.RevealRepository(entry.Path); err != nil {
					app.problem = err.Error()
				}
			}
			if ui.Button(c, "从列表移除…").FillWidth().Clicked() {
				app.confirm("移除仓库记录？", entry.Path+"\n磁盘文件不会删除。", "移除", func() {
					app.libraryChange("正在移除记录…", func(_ context.Context) (workspace.Library, error) {
						return app.service.RemoveRepository(entry.Path)
					})
				})
			}
		},
	}
}

func (app *nativeApp) chooseScan() {
	paths, err := app.chooseDirectories("扫描文件夹", "扫描", false)
	if err != nil {
		app.problem = err.Error()
		return
	}
	if len(paths) == 0 {
		return
	}
	app.run("正在扫描文件夹…", func(ctx context.Context) (func(), error) {
		result, err := app.service.Scan(ctx, paths[0])
		return func() {
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
		notices = append(notices, "扫描已达到上限")
	}
	notices = append(notices, result.Warnings...)
	app.dialog = &nativeDialog{
		title: "选择要添加的仓库", detail: strings.Join(notices, "\n"), choice: choice, action: "添加所选仓库",
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
					ui.Text(c, "添加到").FontSize(13).TextColor(c.Theme().TextMuted)
					ui.Select(c, &app.dialog.choice, options).Label("仓库组").Grow(1).MinWidth(0)
				})
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, fmt.Sprintf("已选 %d / %d 个仓库", selected, len(result.Repositories))).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
					if ui.Button(c, "全选").FontSize(12).Disabled(selected == len(result.Repositories)).Clicked() {
						for _, entry := range result.Repositories {
							app.scanSelected[entry.Path] = true
						}
					}
					if ui.Button(c, "清空选择").FontSize(12).Disabled(selected == 0).Clicked() {
						app.scanSelected = map[string]bool{}
					}
				})
				ui.Scroll(c).Height(320).FillWidth().Border(1, c.Theme().Border).Radius(c.Theme().Radius).Children(func() {
					ui.Column(c).FillWidth().Padding(4).Gap(2).Children(func() {
						if len(result.Repositories) == 0 {
							ui.Text(c, "未找到可添加的仓库").FillWidth().Padding(24).TextColor(c.Theme().TextMuted)
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
				app.problem = "请先选择仓库"
				return
			}
			group := ids[dialog.choice]
			app.libraryChange("正在添加仓库…", func(ctx context.Context) (workspace.Library, error) {
				return app.service.AddRepositories(ctx, paths, group)
			})
		},
	}
}

func (app *nativeApp) linesDialog() {
	app.dialog = &nativeDialog{
		title: "选择变更行", action: "应用所选行",
		detail: "删除行与新增行可独立选择；仅修改暂存区。",
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
