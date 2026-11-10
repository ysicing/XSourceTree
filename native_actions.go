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

func (app *nativeApp) checkedFiles(staged bool) []string {
	var files []string
	if app.repository == nil {
		return files
	}
	for _, file := range app.repository.snapshot.Files {
		if !file.Conflict && app.selectedFiles[fmt.Sprintf("%t:%s", staged, file.Path)] {
			files = append(files, file.Path)
		}
	}
	return files
}

func (app *nativeApp) stageFiles(staged, all bool) {
	var files []string
	if all {
		for _, file := range app.repository.snapshot.Files {
			if !file.Conflict && (staged && isStaged(file) || !staged && isWorking(file)) {
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
			app.dialog = nil
			app.leaveConflict(func() {
				app.mutate("正在创建分支…", func(ctx context.Context, path string) error {
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
			app.dialog = nil
			app.mutate("正在重命名分支…", func(ctx context.Context, path string) error {
				return app.service.RenameBranch(ctx, path, old, name)
			})
		},
	}
}

func (app *nativeApp) deleteBranch(name string, force bool) {
	title := "删除分支？"
	if force {
		title = "强制删除分支？"
	}
	app.confirm(title, name+"\n"+app.repository.snapshot.Path, "删除", func() {
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
			app.dialog = nil
			app.mutate("正在创建标签…", func(ctx context.Context, path string) error {
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
			app.dialog = nil
			app.leaveConflict(func() {
				app.mutate("正在合并…", func(ctx context.Context, path string) error {
					return app.service.Merge(ctx, path, name)
				})
			})
		},
	}
}

func (app *nativeApp) stashDialog() {
	app.dialog = &nativeDialog{
		title: "新建储藏", action: "储藏", fields: []nativeField{{label: "说明（可选）"}},
		checkLabel: "包含未跟踪文件",
		submit: func(dialog *nativeDialog) {
			message, untracked := dialog.fields[0].value, dialog.check
			app.dialog = nil
			app.leaveConflict(func() {
				app.mutate("正在储藏…", func(ctx context.Context, path string) error {
					return app.service.CreateStash(ctx, path, message, untracked)
				})
			})
		},
	}
}

func (app *nativeApp) applyStash(pop bool) {
	hash := app.stashHash
	app.confirm("应用储藏？", "应用到 "+app.repository.snapshot.Path, "应用", func() {
		app.mutate("正在应用储藏…", func(ctx context.Context, path string) error {
			return app.service.ApplyStash(ctx, path, hash, pop)
		})
	})
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
		app.confirm("使用所选版本解决冲突？", conflict.Path+"\n将替换当前工作区内容并暂存。", "确认", action)
	}
}

func (app *nativeApp) operationAction(action string) {
	operation := app.repository.snapshot.Operation
	fn := func() {
		app.leaveConflict(func() {
			app.mutate("正在处理 "+operation.Kind+"…", func(ctx context.Context, path string) error {
				return app.service.OperationAction(ctx, path, operation.Kind, operation.Token, action)
			})
		})
	}
	if action == "continue" {
		fn()
	} else {
		app.confirm("确认"+map[string]string{"abort": "中止", "skip": "跳过"}[action]+"？", operation.Kind+"\n"+app.repository.snapshot.Path, "确认", fn)
	}
}

func (app *nativeApp) remoteAction(action string) {
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
		body: func(c *ui.Context) {
			for _, remote := range app.repository.remotes {
				remote := remote
				ui.Column(c).Key(remote.Name).Gap(6).FillWidth().Children(func() {
					ui.Text(c, remote.Name).Bold()
					ui.Text(c, strings.Join(remote.URLs, "\n")).FontSize(12).Selectable()
					ui.Row(c).Gap(8).Disabled(!app.writable).Children(func() {
						if ui.Button(c, "编辑").Clicked() {
							app.editRemoteDialog(&remote)
						}
						if ui.Button(c, "移除…").Clicked() {
							app.confirm("移除远程仓库？", remote.Name, "移除", func() {
								app.mutate("正在移除远程…", func(ctx context.Context, path string) error {
									return app.service.RemoveRemote(ctx, path, remote.Name, remote.Token)
								})
							})
						}
					})
				})
				ui.Divider(c)
			}
			if ui.Button(c, "添加远程").Disabled(!app.writable).FillWidth().Clicked() {
				app.editRemoteDialog(nil)
			}
		},
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
		fields: []nativeField{{label: "名称", value: name}, {label: "获取地址", value: address}},
		submit: func(dialog *nativeDialog) {
			nextName, nextAddress := dialog.fields[0].value, dialog.fields[1].value
			if remote != nil && nextName != name {
				app.problem = "编辑地址时不能更改远程名称"
				return
			}
			app.dialog = nil
			app.mutate("正在保存远程…", func(ctx context.Context, path string) error {
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
				app.dialog = nil
				app.mutate("正在发布分支…", func(ctx context.Context, path string) error {
					return app.service.PublishBranch(ctx, path, request)
				})
				return
			}
		},
	}
}

func (app *nativeApp) checkoutDialog(remote string) {
	name := remote
	if _, after, found := strings.Cut(remote, "/"); found {
		name = after
	}
	app.dialog = &nativeDialog{
		title: "检出远程分支", detail: remote, action: "检出",
		fields: []nativeField{{label: "本地分支名称", value: name}},
		submit: func(dialog *nativeDialog) {
			name := dialog.fields[0].value
			app.dialog = nil
			app.leaveConflict(func() {
				app.mutate("正在检出分支…", func(ctx context.Context, path string) error {
					return app.service.CheckoutRemote(ctx, path, remote, name)
				})
			})
		},
	}
}

func (app *nativeApp) repositoryActions() {
	app.dialog = &nativeDialog{
		title: "仓库操作",
		body: func(c *ui.Context) {
			if ui.Button(c, "新建标签").Disabled(!app.writable).FillWidth().Clicked() {
				app.tagDialog()
			}
			if ui.Button(c, "在 Finder 中显示").FillWidth().Clicked() {
				if err := app.service.RevealRepository(app.repository.snapshot.Path); err != nil {
					app.problem = err.Error()
				}
				app.dialog = nil
			}
			if ui.Button(c, "打开恢复副本").FillWidth().Clicked() {
				if err := app.service.RevealDiscardBackups(); err != nil {
					app.problem = err.Error()
				}
				app.dialog = nil
			}
		},
	}
}
