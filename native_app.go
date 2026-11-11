// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ysicing/XSourceTree/internal/commitai"
	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/internal/workspace"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type nativePreferences struct {
	Sidebar              float32                     `json:"sidebar"`
	Files                float32                     `json:"files"`
	History              float32                     `json:"history"`
	HistoryFiles         float32                     `json:"historyFiles"`
	Split                bool                        `json:"split"`
	Wrap                 bool                        `json:"wrap"`
	FileSort             string                      `json:"fileSort"`
	FileDescending       bool                        `json:"fileDescending"`
	FileTree             bool                        `json:"fileTree"`
	Drafts               map[string]string           `json:"drafts"`
	Appearance           string                      `json:"appearance"`
	Shortcuts            nativeShortcuts             `json:"shortcuts"`
	UpdateCheckFrequency string                      `json:"updateCheckFrequency"`
	AI                   commitai.Options            `json:"ai"`
	AIConsent            bool                        `json:"aiConsent"`
	RepositoryAI         map[string]nativeAIOverride `json:"repositoryAI,omitempty"`
}

type repositoryView struct {
	snapshot gitrepo.Snapshot
	branches []gitrepo.Branch
	tags     []gitrepo.Tag
	remotes  []gitrepo.RemoteConfig
	stashes  []gitrepo.Stash
}

type nativeApp struct {
	service               *RepositoryService
	win                   *mygo.Window
	ctx                   context.Context
	cancel                context.CancelFunc
	post                  func(func())
	closed                bool
	busy                  bool
	readTask              *nativeRead
	readDone              <-chan struct{}
	writable              bool
	status                string
	problem               string
	library               workspace.Library
	group                 string
	query                 string
	screen                string
	repository            *repositoryView
	selectedFile          string
	staged                bool
	fileQuery             string
	fileFilter            string
	fileCollapsed         map[string]bool
	selectedFiles         map[string]bool
	history               []gitrepo.Commit
	historyQuery          string
	historyFile           string
	historyMore           bool
	commitHash            string
	commitFiles           []gitrepo.CommitFile
	commitFile            string
	stashHash             string
	diff                  gitrepo.Diff
	patch                 gitrepo.Patch
	diffViews             []*editor.DiffState
	diffNames             []string
	hunkOffsets           []int
	rawDiff               editor.State
	lineChoices           []nativeLine
	conflict              *gitrepo.Conflict
	conflictEdit          editor.State
	conflictBase          [3]editor.State
	hunk                  int
	dialog                *nativeDialog
	scanResult            *workspace.ScanResult
	scanSelected          map[string]bool
	prefs                 nativePreferences
	stateFile             string
	historyList           *ui.ListState
	settingsError         string
	shortcutDraft         nativeShortcuts
	settingsAutoChecks    bool
	settingsAutoDownloads bool
	availableVersion      string
	stopUpdater           func()
	ai                    *nativeCommitAI
	draftRevision         uint64

	historySelected    int
	historyGraph       []historyGraphRow
	historyGraphSource *gitrepo.Commit
	commitList         *ui.ListState
	commitSelected     int
	refCollapsed       map[nativeRefKey]bool
	refSelected        nativeRefKey
}

func newNativeApp(service *RepositoryService, stateFile string) *nativeApp {
	ctx, cancel := context.WithCancel(context.Background())
	app := &nativeApp{
		service: service, ctx: ctx, cancel: cancel, stateFile: stateFile,
		screen: "library", library: service.Library(), selectedFiles: map[string]bool{},
		prefs: nativePreferences{
			Sidebar: 226, Files: 360, History: 56, HistoryFiles: 280, Wrap: true, Drafts: map[string]string{},
			Appearance: appearanceSystem, Shortcuts: defaultNativeShortcuts(),
		},
		historySelected: -1, commitSelected: -1,
		historyList: &ui.ListState{}, commitList: &ui.ListState{},
		refCollapsed: make(map[nativeRefKey]bool), fileCollapsed: make(map[string]bool),
	}
	if data, err := os.ReadFile(stateFile); err == nil {
		stored := struct {
			*nativePreferences
			ByName bool `json:"byName"`
		}{nativePreferences: &app.prefs}
		if err := json.Unmarshal(data, &stored); err != nil {
			app.problem = "无法读取界面偏好，将使用默认布局"
		}
		if app.prefs.FileSort == "" && stored.ByName {
			app.prefs.FileSort = "name"
		}
	}
	switch app.prefs.FileSort {
	case "path", "name", "status":
	default:
		app.prefs.FileSort = "path"
	}
	app.prefs.Sidebar = clampSize(app.prefs.Sidebar, 226, 180, 400)
	app.prefs.Files = clampSize(app.prefs.Files, 360, 260, 640)
	app.prefs.History = clampSize(app.prefs.History, 56, 30, 70)
	app.prefs.HistoryFiles = clampSize(app.prefs.HistoryFiles, 280, 180, 440)
	if app.prefs.Drafts == nil {
		app.prefs.Drafts = map[string]string{}
	}
	normalizeNativePreferences(&app.prefs)
	commitai.Normalize(&app.prefs.AI)
	if app.prefs.RepositoryAI == nil {
		app.prefs.RepositoryAI = map[string]nativeAIOverride{}
	}
	return app
}

func clampSize(value, fallback, minimum, maximum float32) float32 {
	if value < minimum || value > maximum {
		return fallback
	}
	return value
}

func (app *nativeApp) open() {
	app.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "XSourceTree", Width: 1320, Height: 860, MinWidth: 1024, MinHeight: 680,
		TitleBarStyle: mygo.TitleBarHiddenInset, TitleBarHeight: 52,
		BackgroundColor: appearanceBackground(app.prefs.Appearance), StateKey: "main",
		Content: ui.View(app.view),
	})
	app.win.SetWindowButtonsTheme(appearanceThemeSource(app.prefs.Appearance))
	app.post = app.win.Update
	app.availableVersion = updater.AvailableVersion()
	app.stopUpdater = updater.OnChange(func() {
		app.post(func() {
			if !app.closed {
				app.availableVersion = updater.AvailableVersion()
			}
		})
	})
	protectWindowClose(app.win, app)
	app.win.OnClosed(func() {
		app.closed = true
		app.cancelRead()
		app.stopCommitAI()
		app.cancel()
		if app.stopUpdater != nil {
			app.stopUpdater()
			app.stopUpdater = nil
		}
	})
	app.win.OnFocus(func() {
		if !app.busy && app.readTask == nil && app.dialog == nil && !app.conflictDirty() {
			app.refresh()
		}
	})
}

func (app *nativeApp) savePreferences() error {
	if app.stateFile == "" {
		return nil
	}
	data, err := json.MarshalIndent(app.prefs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(app.stateFile), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(app.stateFile), ".native-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), app.stateFile)
}

// 后台任务只返回结果；界面状态统一经 Window.Update 在主线程修改。
func (app *nativeApp) run(label string, task func(context.Context) (func(), error)) {
	if app.busy || app.closed {
		return
	}
	app.cancelRead()
	previous := app.readDone
	app.busy, app.status, app.problem = true, label, ""
	dialog := app.dialog
	go func() {
		if previous != nil {
			<-previous
		}
		apply, err := task(app.ctx)
		app.post(func() {
			if app.closed {
				return
			}
			app.busy, app.status = false, ""
			if apply != nil {
				apply()
			}
			if err != nil {
				app.problem = err.Error()
				if dialog != nil && app.dialog == dialog {
					dialog.problem = err.Error()
				}
			}
		})
	}()
}

func (app *nativeApp) readRepository(ctx context.Context, path string, open bool) (*repositoryView, error) {
	result := &repositoryView{}
	var err error
	if open {
		result.snapshot, err = app.service.Open(ctx, path)
	} else {
		result.snapshot, err = app.service.Snapshot(ctx, path)
	}
	if err != nil {
		return nil, err
	}
	path = result.snapshot.Path
	if result.branches, err = app.service.Branches(ctx, path); err != nil {
		return nil, err
	}
	if result.tags, err = app.service.Tags(ctx, path); err != nil {
		return nil, err
	}
	if result.remotes, err = app.service.RemoteConfigs(ctx, path); err != nil {
		return nil, err
	}
	result.stashes, err = app.service.Stashes(ctx, path)
	return result, err
}

func (app *nativeApp) adopt(result *repositoryView) {
	if app.repository == nil || app.repository.snapshot.Path != result.snapshot.Path {
		app.stopCommitAI()
	}
	app.repository, app.writable = result, true
	app.library = app.service.Library()
	app.selectedFiles = map[string]bool{}
	app.selectedFile = ""
	app.clearDiff()
}

func (app *nativeApp) openRepository(path string) {
	app.leaveConflict(func() {
		if !app.busy {
			app.stopCommitAI()
		}
		app.run("正在打开仓库…", func(ctx context.Context) (func(), error) {
			result, err := app.readRepository(ctx, path, true)
			if err != nil {
				return nil, err
			}
			return func() {
				if app.repository == nil || app.repository.snapshot.Path != result.snapshot.Path {
					app.fileQuery, app.fileFilter = "", ""
					clear(app.fileCollapsed)
				}
				app.adopt(result)
				app.screen, app.selectedFile = "changes", ""
				app.history, app.commitHash, app.commitFiles, app.stashHash = nil, "", nil, ""
				app.historyGraph, app.historyGraphSource = nil, nil
				app.historySelected, app.commitSelected = -1, -1
			}, nil
		})
	})
}

func (app *nativeApp) refresh() {
	if app.busy {
		return
	}
	if app.screen == "library" || app.repository == nil {
		app.run("正在刷新仓库列表…", func(ctx context.Context) (func(), error) {
			library, err := app.service.RefreshLibrary(ctx)
			return func() { app.library = library }, err
		})
		return
	}
	app.leaveConflict(func() { app.mutate("正在刷新…", nil) })
}

func (app *nativeApp) mutate(label string, operation func(context.Context, string) error) {
	app.mutateForm(nil, label, operation)
}

func (app *nativeApp) mutateForm(dialog *nativeDialog, label string, operation func(context.Context, string) error) {
	if app.repository == nil || app.busy || (operation != nil && !app.writable) {
		return
	}
	if operation != nil {
		app.stopCommitAI()
	}
	path, screen, file, staged := app.repository.snapshot.Path, app.screen, app.selectedFile, app.staged
	query, historyFile := app.historyQuery, app.historyFile
	conflict, conflictEdit := app.conflict, app.conflictEdit
	conflictBase := app.conflictBase
	dirty := app.conflictDirty()
	app.run(label, func(ctx context.Context) (func(), error) {
		var operationErr error
		if operation != nil {
			operationErr = operation(ctx, path)
		}
		result, refreshErr := app.readRepository(ctx, path, false)
		var history []gitrepo.Commit
		if refreshErr == nil && screen == "history" {
			history, refreshErr = app.service.SearchHistory(ctx, path, query, historyFile, 0)
		}
		var selection *nativeSelection
		if refreshErr == nil && screen == "changes" && file != "" && fileExists(result.snapshot.Files, file, staged) {
			selection, refreshErr = app.readSelection(ctx, path, file, staged, result.snapshot.Files)
		}
		return func() {
			if result != nil {
				app.adopt(result)
			}
			app.writable = refreshErr == nil
			if screen == "history" && refreshErr == nil {
				app.history, app.historyMore = history, len(history) == 100
				app.commitHash, app.commitFile, app.commitFiles = "", "", nil
				app.historySelected, app.commitSelected = -1, -1
				app.historyList, app.commitList = &ui.ListState{}, &ui.ListState{}
			}
			if selection != nil {
				app.setSelection(selection)
			} else {
				app.selectedFile = ""
			}
			if conflict != nil && (operationErr != nil || dirty && (refreshErr != nil || selection != nil && selection.conflict != nil)) {
				// 修改分支、标签或远程配置时，仍在编辑的冲突草稿和原始校验令牌必须保留。
				app.conflict, app.conflictEdit = conflict, conflictEdit
				app.conflictBase = conflictBase
				app.selectedFile = file
			}
			if screen == "stash" {
				app.stashHash = ""
			}
			if dialog != nil && app.dialog == dialog {
				dialog.blocked = refreshErr != nil
				if operationErr == nil {
					app.dialog = dialog.parent
					if app.dialog != nil && refreshErr != nil {
						app.dialog.problem = refreshErr.Error()
					}
				}
			}
		}, errors.Join(operationErr, refreshErr)
	})
}

func fileExists(files []gitrepo.File, path string, staged bool) bool {
	for _, file := range files {
		if file.Path == path && (staged && isStaged(file) || !staged && isWorking(file)) {
			return true
		}
	}
	return false
}

func isStaged(file gitrepo.File) bool {
	return file.Index != " " && file.Index != "?" && !file.Conflict
}
func isWorking(file gitrepo.File) bool { return file.Working != " " || file.Conflict }

func (app *nativeApp) showLibrary() {
	app.leaveConflict(func() {
		app.stopCommitAI()
		app.screen = "library"
	})
}

func (app *nativeApp) commit() {
	if app.repository == nil || app.busy || !app.writable {
		return
	}
	path := app.repository.snapshot.Path
	message := app.prefs.Drafts[path]
	if app.repository.snapshot.Operation.Kind != "" {
		app.problem = "请先完成当前 Git 操作"
		return
	}
	if strings.TrimSpace(message) == "" {
		app.problem = "请填写提交说明"
		return
	}
	staged := false
	for _, file := range app.repository.snapshot.Files {
		if file.Conflict {
			app.problem = "请先解决并暂存所有冲突"
			return
		}
		staged = staged || isStaged(file)
	}
	if !staged {
		app.problem = "没有已暂存的变更"
		return
	}
	app.stopCommitAI()
	app.run("正在提交…", func(ctx context.Context) (func(), error) {
		commitErr := app.service.Commit(ctx, path, message)
		result, refreshErr := app.readRepository(ctx, path, false)
		return func() {
			if result != nil {
				app.adopt(result)
			}
			app.writable = refreshErr == nil
			if commitErr == nil {
				delete(app.prefs.Drafts, path)
				if err := app.savePreferences(); err != nil {
					app.problem = fmt.Sprintf("提交成功，但无法保存草稿状态：%v", err)
				}
			}
		}, errors.Join(commitErr, refreshErr)
	})
}

func (app *nativeApp) conflictDirty() bool {
	return app.conflict != nil && app.conflictEdit.Buffer != nil && app.conflictEdit.Buffer.String() != app.conflict.Result
}

func (app *nativeApp) leaveConflict(action func()) {
	if app.busy {
		return
	}
	if app.conflictDirty() {
		app.confirm("放弃冲突编辑？", "尚未保存的合并结果将丢失，磁盘文件不会改变。", "放弃编辑", func() {
			app.cancelRead()
			app.clearDiff()
			action()
		})
	} else {
		app.cancelRead()
		action()
	}
}

func (app *nativeApp) prepareClose() bool {
	if app.busy {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{
			Parent: app.win, Message: "当前操作尚未完成", Detail: "请等待操作结束，或先取消扫描、历史搜索。",
			Buttons: []string{"好"},
		})
		return false
	}
	if app.conflictDirty() {
		result, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: app.win, Type: mygo.MessageWarning, Message: "要丢弃冲突编辑并退出吗？",
			Detail:  "提交说明会保存；未保存的冲突编辑将丢失。",
			Buttons: []string{"取消", "丢弃编辑并退出"}, CancelButton: 0, DestructiveButtons: []int{1},
		})
		if err != nil || result.Button != 1 {
			return false
		}
	}
	if err := app.savePreferences(); err != nil {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{
			Parent: app.win, Type: mygo.MessageWarning, Message: "无法保存草稿和界面状态",
			Detail: err.Error(), Buttons: []string{"好"},
		})
		return false
	}
	return true
}
