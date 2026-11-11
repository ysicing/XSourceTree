// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ysicing/XSourceTree/internal/gitrepo"

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
	app.mutate("正在更新暂存区…", func(ctx context.Context, path string) error {
		if staged {
			return app.service.Unstage(ctx, path, files)
		}
		return app.service.Stage(ctx, path, files)
	})
}

func (app *nativeApp) branchDialog() {
	app.dialog = &nativeDialog{
		title: "新建分支", action: "创建并切换", fields: []nativeField{{label: "分支名称"}},
		submit: func(dialog *nativeDialog) {
			name := dialog.fields[0].value
			app.leaveConflict(func() {
				app.dialog = dialog
				app.mutateForm(dialog, "正在创建分支…", func(ctx context.Context, path string) error {
					return app.service.CreateBranch(ctx, path, name)
				})
			})
		},
	}
}

func (app *nativeApp) renameBranchDialog(old string) {
	app.dialog = &nativeDialog{
		title: "重命名分支", action: "重命名", fields: []nativeField{{label: "分支名称", value: old}},
		submit: func(dialog *nativeDialog) {
			name := dialog.fields[0].value
			app.mutateForm(dialog, "正在重命名分支…", func(ctx context.Context, path string) error {
				return app.service.RenameBranch(ctx, path, old, name)
			})
		},
	}
}

func (app *nativeApp) deleteBranch(name string, force bool) {
	title := "删除本地分支？"
	detail := name + "\n仅删除本地分支，不影响远程分支。"
	if force {
		title = "强制删除本地分支？"
		detail += "\n未合并的提交将失去此分支引用。"
	}
	app.confirm(title, detail, "删除", func() {
		app.mutate("正在删除分支…", func(ctx context.Context, path string) error {
			return app.service.DeleteBranch(ctx, path, name, force)
		})
	})
}

func (app *nativeApp) tagDialog() {
	hash := app.commitHash
	if hash == "" {
		hash = app.repository.snapshot.Head
	}
	app.dialog = &nativeDialog{
		title: "新建本地标签", detail: shortHash(hash), action: "创建", fields: []nativeField{{label: "标签名称"}},
		submit: func(dialog *nativeDialog) {
			name := dialog.fields[0].value
			app.mutateForm(dialog, "正在创建标签…", func(ctx context.Context, path string) error {
				return app.service.CreateTag(ctx, path, name, hash)
			})
		},
	}
}

func (app *nativeApp) deleteTag(name string) {
	app.confirm("删除本地标签？", name, "删除", func() {
		app.mutate("正在删除标签…", func(ctx context.Context, path string) error {
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
		app.problem = "没有可合并的其他分支"
		return
	}
	app.dialog = &nativeDialog{
		title: "合并到当前分支", detail: app.repository.snapshot.Branch, action: "合并",
		options: names, choice: names[0],
		submit: func(dialog *nativeDialog) {
			name := dialog.choice
			app.leaveConflict(func() {
				app.dialog = dialog
				app.mutateForm(dialog, "正在合并…", func(ctx context.Context, path string) error {
					return app.service.Merge(ctx, path, name)
				})
			})
		},
	}
}

func (app *nativeApp) stashDialog() {
	app.dialog = &nativeDialog{
		title: "新建储藏", action: "储藏", fields: []nativeField{{label: "说明（可选）", optional: true}},
		checkLabel: "包含未跟踪文件",
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
				app.mutateForm(dialog, "正在储藏…", func(ctx context.Context, path string) error {
					return app.service.CreateStash(ctx, path, message, untracked)
				})
			})
		},
	}
}

func (app *nativeApp) applyStash(pop bool) {
	hash := app.stashHash
	title, detail, action := "应用储藏？", "应用到 "+app.repository.snapshot.Path+"\n保留储藏记录。", "应用"
	if pop {
		title, detail, action = "应用并删除储藏？", "应用到 "+app.repository.snapshot.Path+"\n仅在应用成功后删除储藏记录；发生冲突时保留记录。", "应用并删除"
	}
	app.confirm(title, detail, action, func() {
		app.mutate("正在应用储藏…", func(ctx context.Context, path string) error {
			return app.service.ApplyStash(ctx, path, hash, pop)
		})
	})
	app.dialog.danger = pop
}

func (app *nativeApp) dropStash() {
	hash := app.stashHash
	app.confirm("删除储藏？", shortHash(hash)+"\n此操作不能直接撤销。", "删除", func() {
		app.mutate("正在删除储藏…", func(ctx context.Context, path string) error {
			return app.service.DropStash(ctx, path, hash)
		})
	})
}

func (app *nativeApp) discardFile() {
	path, file := app.repository.snapshot.Path, app.selectedFile
	app.run("正在检查恢复副本…", func(ctx context.Context) (func(), error) {
		preview, err := app.service.DiscardPreview(ctx, path, file)
		return func() {
			if err != nil {
				return
			}
			if !preview.Supported {
				app.problem = preview.Reason
				return
			}
			app.confirm("丢弃文件修改？", file+"\n操作前会保存恢复副本；工作区恢复为暂存区版本。", "丢弃修改", func() {
				app.mutate("正在保存副本并丢弃…", func(ctx context.Context, path string) error {
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
		app.mutate("正在解决冲突…", func(ctx context.Context, path string) error {
			return app.service.ResolveConflict(ctx, path, conflict.Path, conflict.Token, choice, content)
		})
	}
	if choice == "result" {
		action()
	} else {
		title, detail, label := "使用当前版本解决冲突？", "将使用当前版本替换工作区内容并暂存。", "使用当前版本"
		switch choice {
		case "theirs":
			title, detail, label = "使用传入版本解决冲突？", "将使用传入版本替换工作区内容并暂存。", "使用传入版本"
		case "delete":
			title, detail, label = "删除冲突文件？", "将删除工作区文件，并暂存此删除以解决冲突。", "删除文件"
		}
		app.confirm(title, conflict.Path+"\n"+detail, label, action)
	}
}

func (app *nativeApp) operationAction(action string) {
	operation := app.repository.snapshot.Operation
	fn := func() {
		app.leaveConflict(func() {
			app.mutate("正在处理"+operationName(operation.Kind)+"…", func(ctx context.Context, path string) error {
				return app.service.OperationAction(ctx, path, operation.Kind, operation.Token, action)
			})
		})
	}
	if action == "continue" {
		fn()
	} else {
		title, detail, label := "中止"+operationName(operation.Kind)+"？", "将丢弃本次操作中尚未保存的冲突解决结果，恢复到操作开始前的状态。", "中止"+operationName(operation.Kind)
		if action == "skip" {
			title, detail, label = "跳过当前提交？", "将丢弃当前提交的修改和冲突解决结果，并继续处理下一个提交。", "跳过当前提交"
		}
		app.confirm(title, app.repository.snapshot.Path+"\n"+detail, label, fn)
	}
}

func (app *nativeApp) remoteAction(action string) {
	if len(app.repository.remotes) == 0 {
		app.editRemoteDialog(nil)
		app.dialog.detail = "此仓库尚未配置远程地址。"
		return
	}
	if action == "pull" && app.repository.snapshot.Upstream == "" {
		app.dialog = &nativeDialog{
			title: "当前分支未设置上游", detail: "先发布当前分支并设置上游，再拉取远程变更。",
			action: "发布当前分支…", submit: func(_ *nativeDialog) { app.publishDialog() },
		}
		return
	}
	if action == "push" && app.repository.snapshot.Upstream == "" {
		app.publishDialog()
		return
	}
	app.leaveConflict(func() {
		app.mutate("正在"+map[string]string{"fetch": "获取", "pull": "拉取", "push": "推送"}[action]+"…", func(ctx context.Context, path string) error {
			return app.service.Remote(ctx, path, action)
		})
	})
}

func (app *nativeApp) remoteDialog() {
	app.dialog = &nativeDialog{
		title: "远程仓库",
		body:  app.remoteSettingsView,
	}
}

func (app *nativeApp) remoteSettingsView(c *ui.Context) {
	parent := app.dialog
	if len(app.repository.remotes) == 0 {
		ui.Text(c, "尚未配置远程仓库").TextColor(c.Theme().TextMuted)
	}
	for _, remote := range app.repository.remotes {
		remote := remote
		ui.Column(c).Key(remote.Name).Gap(6).FillWidth().Children(func() {
			ui.Text(c, remote.Name).Bold()
			ui.Text(c, strings.Join(remote.URLs, "\n")).FontSize(12).MaxLines(2).Selectable()
			ui.Row(c).Gap(8).Disabled(!app.writable).Children(func() {
				if ui.Button(c, "编辑").Label("编辑远程 " + remote.Name).Clicked() {
					app.editRemoteDialog(&remote)
					app.dialog.parent = parent
				}
				if ui.Button(c, "移除…").Label("移除远程 " + remote.Name).Clicked() {
					app.dialog = &nativeDialog{
						parent: parent, title: "移除远程仓库？",
						detail: remote.Name + "\n仅移除此仓库的远程配置，不会删除服务器上的仓库。",
						action: "移除", danger: true,
						submit: func(dialog *nativeDialog) {
							app.mutateForm(dialog, "正在移除远程…", func(ctx context.Context, path string) error {
								return app.service.RemoveRemote(ctx, path, remote.Name, remote.Token)
							})
						},
					}
				}
			})
		})
		ui.Divider(c)
	}
	if ui.Button(c, "添加远程").Disabled(!app.writable).FillWidth().Clicked() {
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
		title: "远程地址", action: "保存",
		fields: []nativeField{{label: "名称", value: name, readOnly: remote != nil}, {label: "获取地址", value: address}},
		submit: func(dialog *nativeDialog) {
			nextName, nextAddress := dialog.fields[0].value, dialog.fields[1].value
			if remote != nil && nextName != name {
				dialog.problem = "编辑地址时不能更改远程名称"
				return
			}
			app.mutateForm(dialog, "正在保存远程…", func(ctx context.Context, path string) error {
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
		title: "发布当前分支", action: "发布",
		options: names, choice: names[0],
		fields: []nativeField{{label: "远程分支名称", value: snapshot.Branch}},
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
				app.mutateForm(dialog, "正在发布分支…", func(ctx context.Context, path string) error {
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
		title: "检出远程分支", detail: remote, action: "检出",
		fields: []nativeField{{label: "本地分支名称", value: name}},
		submit: func(dialog *nativeDialog) {
			name := dialog.fields[0].value
			app.leaveConflict(func() {
				app.dialog = dialog
				app.mutateForm(dialog, "正在检出分支…", func(ctx context.Context, path string) error {
					return app.service.CheckoutRemote(ctx, path, remote, name)
				})
			})
		},
	}
}

func operationName(kind string) string {
	switch kind {
	case "merge":
		return "合并"
	case "rebase":
		return "变基"
	case "cherry-pick":
		return "拣选"
	default:
		return kind
	}
}

func (app *nativeApp) repositoryActions(menu *ui.Menu) {
	unavailable := app.busy || app.repository == nil
	if menu.Item("新建标签…").Disabled(unavailable || !app.writable).Chosen() {
		app.tagDialog()
	}
	if menu.Item("储藏修改…").Disabled(unavailable || !app.writable ||
		len(app.repository.snapshot.Files) == 0 || app.repository.snapshot.Operation.Kind != "").Chosen() {
		app.stashDialog()
	}
	menu.Separator()
	if menu.Item("在 Finder 中显示").Disabled(unavailable).Chosen() {
		if err := app.service.RevealRepository(app.repository.snapshot.Path); err != nil {
			app.problem = err.Error()
		}
	}
	if menu.Item("打开恢复副本文件夹").Disabled(unavailable).Chosen() {
		if err := app.service.RevealDiscardBackups(); err != nil {
			app.problem = err.Error()
		}
	}
}
