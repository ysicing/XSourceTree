// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ysicing/XSourceTree/internal/aikeychain"
	"github.com/ysicing/XSourceTree/internal/commitai"
	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/internal/workspace"
	"github.com/ysicing/XSourceTree/locales"

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
	ChangesSplit         float32                     `json:"changesSplit,omitempty"`
	Split                bool                        `json:"split"`
	DiffLayout           string                      `json:"diffLayout,omitempty"`
	Wrap                 bool                        `json:"wrap"`
	FileSort             string                      `json:"fileSort"`
	FileDescending       bool                        `json:"fileDescending"`
	FileTree             bool                        `json:"fileTree"`
	Drafts               map[string]string           `json:"drafts"`
	Appearance           string                      `json:"appearance"`
	SidebarItems         nativeSidebarItems          `json:"sidebarItems"`
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
	nativeHistoryState
	browseState           nativeHistoryState
	searchState           nativeHistoryState
	searchInput           ui.Handle
	searchDateEnabled     bool
	searchSince           time.Time
	searchUntil           time.Time
	service               *RepositoryService
	win                   *mygo.Window
	ctx                   context.Context
	cancel                context.CancelFunc
	post                  func(func())
	closed                bool
	busy                  bool
	readOnlyBusy          bool
	scanning              bool
	readTask              *nativeRead
	focusRefresh          *nativeRead
	focusError            string
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
	stashHash             string
	diff                  gitrepo.Diff
	patch                 gitrepo.Patch
	diffViews             []*editor.DiffState
	diffNames             []string
	hunkOffsets           []int
	rawDiff               editor.State
	fileViewer            nativeFileViewer
	comparison            *nativeComparison
	worktrees             *nativeWorktrees
	fileMetrics           nativeFileMetrics
	diffWidth             float32
	diffAutoSplit         bool
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
	settingsError         string
	shortcutDraft         nativeShortcuts
	settingsAutoChecks    bool
	settingsAutoDownloads bool
	availableVersion      string
	stopUpdater           func()
	stopLocale            func()
	chosenLanguage        string
	chooseLanguage        func(string)
	ai                    *nativeCommitAI
	aiEditor              *nativeAISettings
	changesSplit          float32
	changesSplitTarget    float32
	aiKeys                interface {
		Load(string) (string, error)
		Save(string, string) error
		Delete(string) error
	}
	commitInput   ui.Handle
	draftRevision uint64

	refCollapsed map[nativeRefKey]bool
	refSelected  nativeRefKey
}

func newNativeApp(service *RepositoryService, stateFile string) *nativeApp {
	ctx, cancel := context.WithCancel(context.Background())
	app := &nativeApp{
		service: service, ctx: ctx, cancel: cancel, stateFile: stateFile,
		aiKeys: aikeychain.Store{Service: "com.ysicing.xsourcetree.ai"},
		screen: "library", library: service.Library(), selectedFiles: map[string]bool{},
		prefs: nativePreferences{
			Sidebar: 226, Files: 360, History: 56, HistoryFiles: 280, Wrap: true, DiffLayout: "auto", Drafts: map[string]string{},
			Appearance: appearanceSystem, Shortcuts: defaultNativeShortcuts(),
			SidebarItems: nativeSidebarItems{Search: true, Compare: true, Stash: true, Worktrees: true},
		},
		nativeHistoryState: newNativeHistoryState(),
		browseState:        newNativeHistoryState(), searchState: newNativeHistoryState(),
		refCollapsed: make(map[nativeRefKey]bool), fileCollapsed: make(map[string]bool),
		chooseLanguage: mygo.App.SetLocale,
	}
	if data, err := os.ReadFile(stateFile); err == nil {
		stored := struct {
			*nativePreferences
			ByName     bool    `json:"byName"`
			Split      *bool   `json:"split"`
			DiffLayout *string `json:"diffLayout"`
		}{nativePreferences: &app.prefs}
		if err := json.Unmarshal(data, &stored); err != nil {
			app.problem = locales.LoadPreferencesFailed()
		}
		if app.prefs.FileSort == "" && stored.ByName {
			app.prefs.FileSort = "name"
		}
		if stored.Split != nil {
			app.prefs.Split = *stored.Split
			if stored.DiffLayout == nil {
				app.prefs.DiffLayout = "unified"
				if *stored.Split {
					app.prefs.DiffLayout = "split"
				}
			}
		}
		if stored.DiffLayout != nil {
			app.prefs.DiffLayout = *stored.DiffLayout
		}
	}
	switch app.prefs.DiffLayout {
	case "auto", "unified", "split":
	default:
		app.prefs.DiffLayout = "auto"
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
	app.prefs.ChangesSplit = clampSize(app.prefs.ChangesSplit, 0, 0, 100)
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
	app.chosenLanguage = mygo.App.ChosenLocale()
	app.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "XSourceTree", Width: 1320, Height: 860, MinWidth: 1024, MinHeight: 680,
		TitleBarStyle: mygo.TitleBarHiddenInset, TitleBarHeight: 52,
		BackgroundColor: appearanceBackground(app.prefs.Appearance), StateKey: "main",
		Content: ui.View(app.view),
	})
	app.win.SetWindowButtonsTheme(appearanceThemeSource(app.prefs.Appearance))
	app.post = app.win.Update
	app.stopLocale = mygo.App.OnLocaleChanged(func(_ string) {
		app.post(func() {
			if !app.closed {
				app.chosenLanguage = mygo.App.ChosenLocale()
			}
		})
	})
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
		if app.stopLocale != nil {
			app.stopLocale()
			app.stopLocale = nil
		}
	})
	app.win.OnFocus(app.refreshOnFocus)
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
	app.runTask(label, false, task)
}

// 只读任务可在窗口关闭后随 app.ctx 取消；写操作必须完成后才能退出。
func (app *nativeApp) runTask(label string, readOnly bool, task func(context.Context) (func(), error)) {
	if app.busy || app.closed {
		return
	}
	app.cancelRead()
	previous := app.readDone
	app.busy, app.status, app.problem = true, label, ""
	app.readOnlyBusy = readOnly
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
			app.busy, app.readOnlyBusy, app.status = false, false, ""
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
		app.comparison = nil
		app.worktrees = nil
	}
	app.repository, app.writable = result, true
	app.fileMetrics = nativeFileMetrics{}
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
		app.run(locales.OpeningRepository(), func(ctx context.Context) (func(), error) {
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
				app.historyOffset, app.viewedTag, app.viewedCommit = 0, "", gitrepo.Commit{}
				app.historyGraph, app.historyGraphSource = nil, nil
				app.historyWorking = false
				app.historySelected, app.commitSelected = -1, -1
				app.nativeHistoryState = newNativeHistoryState()
				app.browseState, app.searchState = newNativeHistoryState(), newNativeHistoryState()
				app.searchDateEnabled = false
				app.searchSince, app.searchUntil = time.Time{}, time.Time{}
			}, nil
		})
	})
}

func (app *nativeApp) refresh() {
	if app.busy {
		return
	}
	if app.screen == "library" || app.repository == nil {
		app.runTask(locales.RefreshingLibrary(), true, func(ctx context.Context) (func(), error) {
			library, err := app.service.RefreshLibrary(ctx)
			return func() { app.library = library }, err
		})
		return
	}
	app.leaveConflict(func() { app.mutate(locales.Refreshing(), nil) })
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
	wasWorking := screen == "history" && app.historyWorking
	historyRequest := app.historyRequest()
	historyScreen := screen == "history" || screen == "search" && app.historyLoaded
	if screen == "search" {
		historyRequest = app.historyResult
	}
	historyOffset := app.historyOffset
	if historyRequest != app.historyResult {
		historyOffset = 0
	}
	conflict, conflictEdit := app.conflict, app.conflictEdit
	conflictBase := app.conflictBase
	dirty := app.conflictDirty()
	blockedAI, blockedToken := app.ai, ""
	if blockedAI != nil && blockedAI.tooLarge {
		blockedToken = blockedAI.token
	}
	app.runTask(label, operation == nil, func(ctx context.Context) (func(), error) {
		var operationErr error
		if operation != nil {
			operationErr = operation(ctx, path)
		}
		result, refreshErr := app.readRepository(ctx, path, false)
		var refreshedAIToken string
		if refreshErr == nil && blockedToken != "" {
			if repo, err := app.service.manager.Get(path); err == nil {
				refreshedAIToken, _ = repo.CommitToken(ctx)
			}
		}
		var history []gitrepo.Commit
		if refreshErr == nil && historyScreen {
			history, refreshErr = app.service.SearchHistory(ctx, path, historyRequest.query, historyRequest.file, historyOffset, historyRequest.filter)
		}
		var selection *nativeSelection
		if refreshErr == nil && screen == "changes" && file != "" && fileExists(result.snapshot.Files, file, staged) {
			selection, refreshErr = app.readSelection(ctx, path, file, staged, result.snapshot.Files)
		}
		var workingSelection *nativeSelection
		var headFiles []gitrepo.CommitFile
		var headDiff gitrepo.Diff
		var detailErr error
		var worktreeItems []gitrepo.Worktree
		if refreshErr == nil && screen == "worktrees" {
			worktreeItems, detailErr = app.service.Worktrees(ctx, path)
		}
		restoreWorking := wasWorking && refreshErr == nil && historyOffset == 0 &&
			historyRequest.query == "" && historyRequest.file == ""
		if restoreWorking {
			if len(result.snapshot.Files) > 0 {
				workingFile, workingStaged := historyWorkingFile(result.snapshot.Files, file, staged)
				workingSelection = &nativeSelection{file: workingFile, staged: workingStaged}
				workingSelection.diff, detailErr = app.service.Diff(ctx, path, workingFile, workingStaged)
			} else if result.snapshot.Head != "" {
				headVisible := false
				for _, commit := range history {
					headVisible = headVisible || commit.Hash == result.snapshot.Head
				}
				if !headVisible {
					var location gitrepo.HistoryLocation
					location, detailErr = app.service.LocateHistory(ctx, path, result.snapshot.Head, "", "", historyRequest.filter)
					if detailErr == nil && len(location.Commits) > 0 {
						history, historyOffset = location.Commits, location.Offset
					}
				}
				if detailErr == nil {
					headFiles, detailErr = app.service.CommitFiles(ctx, path, result.snapshot.Head)
				}
				if detailErr == nil && len(headFiles) > 0 {
					headDiff, detailErr = app.service.CommitFileDiff(ctx, path, result.snapshot.Head, headFiles[0].Path)
				}
			}
		}
		return func() {
			if app.ai == blockedAI && refreshedAIToken != "" && refreshedAIToken != blockedToken {
				app.stopCommitAI()
			}
			retained := -1
			preserveHistory := operation == nil || dialog != nil && dialog.preserveHistory
			if preserveHistory && historyScreen && refreshErr == nil {
				for index, commit := range history {
					if commit.Hash == app.commitHash {
						retained = index
						break
					}
				}
			}
			preserveDetails := preserveHistory && !wasWorking && historyScreen &&
				refreshErr == nil && (retained >= 0 || app.viewedTag != "" && app.viewedCommit.Hash == app.commitHash)
			if result != nil {
				if preserveDetails {
					app.repository = result
					app.library = app.service.Library()
				} else {
					app.historyWorking = false
					app.adopt(result)
					if screen == "compare" && app.comparison != nil {
						app.setDiff(app.comparison.diff)
					}
				}
			}
			app.writable = refreshErr == nil
			if screen == "worktrees" && refreshErr == nil && detailErr == nil && app.worktrees != nil {
				app.worktrees.setItems(worktreeItems)
			}
			if historyScreen && refreshErr == nil {
				app.history, app.historyMore = history, len(history) == gitrepo.DefaultHistoryPageSize
				app.historyPagePaused = false
				app.historyLoaded = true
				app.historyOffset = historyOffset
				app.historyResult = historyRequest
				if preserveDetails {
					app.historySelected = retained
				} else {
					app.viewedTag, app.viewedCommit = "", gitrepo.Commit{}
					app.commitHash, app.commitFile, app.commitFiles = "", "", nil
					app.historySelected, app.commitSelected = -1, -1
					app.historyList, app.commitList = &ui.ListState{}, &ui.ListState{}
				}
			}
			if selection != nil {
				app.setSelection(selection)
			} else {
				app.selectedFile = ""
			}
			if restoreWorking {
				if workingSelection != nil {
					app.applyHistoryWorkingSelection(workingSelection)
				} else {
					headFile := ""
					if len(headFiles) > 0 {
						headFile = headFiles[0].Path
					}
					app.applyCommitSelection(result.snapshot.Head, headFiles, headFile)
					if detailErr == nil {
						app.setDiff(headDiff)
					}
				}
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
				if dialog.onResult != nil {
					dialog.onResult(operationErr)
				}
				if operationErr == nil {
					if dialog.cancel != nil {
						dialog.cancel()
					}
					app.dialog = dialog.parent
					if app.dialog != nil && refreshErr != nil {
						app.dialog.problem = refreshErr.Error()
					}
				}
			}
		}, errors.Join(operationErr, refreshErr, detailErr)
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
		app.cancelRead()
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
		app.problem = locales.FinishOperationFirst()
		return
	}
	if strings.TrimSpace(message) == "" {
		app.problem = locales.EnterCommitMessage()
		return
	}
	staged := false
	for _, file := range app.repository.snapshot.Files {
		if file.Conflict {
			app.problem = locales.ResolveAllConflicts()
			return
		}
		staged = staged || isStaged(file)
	}
	if !staged {
		app.problem = locales.NoStagedChanges()
		return
	}
	app.stopCommitAI()
	app.run(locales.Committing(), func(ctx context.Context) (func(), error) {
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
					app.problem = locales.CommitSavedDraftFailed(err.Error())
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
		app.confirm(locales.DiscardConflictTitle(), locales.DiscardConflictDetail(), locales.DiscardConflictEdits(), func() {
			app.cancelRead()
			app.clearDiff()
			action()
		})
	} else {
		app.cancelRead()
		action()
	}
}

func (app *nativeApp) closeBlockReason() string {
	if app.aiEditor != nil && app.aiEditor.keySaving {
		return locales.SavingApiKey()
	}
	if !app.busy || app.readOnlyBusy {
		return ""
	}
	if app.status != "" {
		return app.status + "\n" + locales.OperationBusyDetail()
	}
	return locales.OperationBusyDetail()
}

func (app *nativeApp) prepareClose() bool {
	if reason := app.closeBlockReason(); reason != "" {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{
			Parent: app.win, Message: locales.OperationBusyTitle(), Detail: reason,
			Buttons: []string{locales.Ok()},
		})
		return false
	}
	if app.conflictDirty() {
		result, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: app.win, Type: mygo.MessageWarning, Message: locales.QuitWithConflictTitle(),
			Detail:  locales.QuitWithConflictDetail(),
			Buttons: []string{locales.Cancel(), locales.DiscardAndQuit()}, CancelButton: 0, DestructiveButtons: []int{1},
		})
		if err != nil || result.Button != 1 {
			return false
		}
	}
	if app.aiEditor != nil && app.aiEditor.key != "" {
		result, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: app.win, Type: mygo.MessageWarning, Message: locales.DiscardUnsavedTitle(),
			Detail: locales.DiscardApiKey(), Buttons: []string{locales.Cancel(), locales.DiscardAndQuit()},
			CancelButton: 0, DestructiveButtons: []int{1},
		})
		if err != nil || result.Button != 1 {
			return false
		}
	}
	if err := app.savePreferences(); err != nil {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{
			Parent: app.win, Type: mygo.MessageWarning, Message: locales.SaveStateFailed(),
			Detail: err.Error(), Buttons: []string{locales.Ok()},
		})
		return false
	}
	return true
}
