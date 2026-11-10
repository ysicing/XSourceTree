// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ysicing/XSourceTree/internal/workspace"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type nativeField struct {
	label string
	value string
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
}

func (app *nativeApp) dialogView(c *ui.Context) {
	dialog := app.dialog
	if dialog == nil {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Key(dialog).Width(580).MaxWidthPercent(90).Gap(12).Children(func() {
			ui.Text(c, dialog.title).FontSize(20).Bold()
			if dialog.detail != "" {
				ui.Text(c, dialog.detail).MaxLines(5).Selectable()
			}
			for index := range dialog.fields {
				field := &dialog.fields[index]
				ui.Column(c).Key(index).Gap(5).FillWidth().Children(func() {
					ui.Text(c, field.label).FontSize(12).TextColor(c.Theme().TextMuted)
					ui.TextInput(c, &field.value).Label(field.label).FillWidth()
				})
			}
			if len(dialog.options) > 0 {
				ui.Select(c, &dialog.choice, dialog.options).FillWidth()
			}
			if dialog.checkLabel != "" {
				ui.Checkbox(c, &dialog.check, dialog.checkLabel)
			}
			if dialog.body != nil {
				dialog.body(c)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Clicked() {
					app.dialog = nil
				}
				if dialog.action != "" {
					button := ui.PrimaryButton(c, dialog.action)
					if dialog.danger {
						button.Background(c.Theme().Danger)
					}
					if button.Clicked() && app.dialog == dialog {
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
	app.dialog = nil
	app.run(label, func(ctx context.Context) (func(), error) {
		library, err := task(ctx)
		return func() {
			if err == nil {
				app.library = library
			}
		}, err
	})
}

func (app *nativeApp) chooseDirectories(title, button string, multiple bool) ([]string, error) {
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
	detail := fmt.Sprintf("找到 %d 个仓库", len(result.Repositories))
	if result.Truncated {
		detail += "，扫描已达到上限"
	}
	if len(result.Warnings) > 0 {
		detail += "\n" + strings.Join(result.Warnings, "\n")
	}
	app.dialog = &nativeDialog{
		title: "选择要添加的仓库", detail: detail, options: options, choice: choice, action: "添加所选仓库",
		body: func(c *ui.Context) {
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "全选").Clicked() {
					for _, entry := range result.Repositories {
						app.scanSelected[entry.Path] = true
					}
				}
				if ui.Button(c, "清空选择").Clicked() {
					app.scanSelected = map[string]bool{}
				}
			})
			ui.Scroll(c).Height(300).FillWidth().Children(func() {
				for _, entry := range result.Repositories {
					value := app.scanSelected[entry.Path]
					if ui.Checkbox(c, &value, entry.Path).Key(entry.Path).FillWidth().Changed() {
						app.scanSelected[entry.Path] = value
					}
				}
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
		body: func(c *ui.Context) {
			ui.Scroll(c).Height(340).FillWidth().Children(func() {
				for index := range app.lineChoices {
					line := &app.lineChoices[index]
					ui.Checkbox(c, &line.Checked, line.Text).Key(line.ID).Font("monospace").FillWidth()
				}
			})
		},
		submit: func(_ *nativeDialog) { app.applyLines() },
	}
}
