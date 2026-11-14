// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/locales"

	"github.com/egoist/mygo/ui"
)

type nativeHistoryState struct {
	history              []gitrepo.Commit
	historyQuery         string
	historyFile          string
	historyMore          bool
	historyOffset        int
	historyFilter        gitrepo.HistoryFilter
	historyResult        nativeHistoryRequest
	historyLoaded        bool
	historyRepository    *repositoryView
	historySelected      int
	historyTableSelected int
	historyWorking       bool
	historyGraph         []historyGraphRow
	historyGraphSource   *gitrepo.Commit
	historyGraphWorking  bool
	historyGraphHead     string
	historyList          *ui.ListState
	commitHash           string
	viewedTag            string
	viewedCommit         gitrepo.Commit
	commitFiles          []gitrepo.CommitFile
	commitFile           string
	commitList           *ui.ListState
	commitSelected       int
	workingList          *ui.ListState
	workingSelected      int
	savedDiff            gitrepo.Diff
	savedFile            string
	savedStaged          bool
}

func newNativeHistoryState() nativeHistoryState {
	return nativeHistoryState{
		historySelected: -1, commitSelected: -1, workingSelected: -1,
		historyList: &ui.ListState{}, commitList: &ui.ListState{}, workingList: &ui.ListState{},
	}
}

func (app *nativeApp) switchWorkspace(screen string) {
	if app.screen == screen {
		return
	}
	app.cancelRead()
	if app.screen == "history" || app.screen == "search" {
		app.historyRepository = app.repository
		app.savedDiff, app.savedFile, app.savedStaged = app.diff, app.selectedFile, app.staged
		if app.screen == "history" {
			app.browseState = app.nativeHistoryState
		} else {
			app.searchState = app.nativeHistoryState
		}
	}
	app.clearDiff()
	app.selectedFile, app.stashHash = "", ""
	app.refSelected = nativeRefKey{}
	app.screen = screen
	switch screen {
	case "compare":
		app.nativeHistoryState = newNativeHistoryState()
		if app.comparison == nil {
			app.comparison = &nativeComparison{base: "HEAD", target: "HEAD", selected: -1, list: &ui.ListState{}}
		}
		app.setDiff(app.comparison.diff)
		return
	case "history":
		app.nativeHistoryState = app.browseState
	case "search":
		app.nativeHistoryState = app.searchState
	default:
		app.nativeHistoryState = newNativeHistoryState()
		return
	}
	app.selectedFile, app.staged = app.savedFile, app.savedStaged
	app.setDiff(app.savedDiff)
}

func (app *nativeApp) historyNeedsRefresh() bool {
	previous, current := app.historyRepository, app.repository
	return app.historyWorking || previous == nil || previous.snapshot.Head != current.snapshot.Head ||
		!slices.Equal(previous.branches, current.branches) || !slices.Equal(previous.tags, current.tags) ||
		!slices.Equal(previous.stashes, current.stashes)
}

func (app *nativeApp) searchControls(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(8, 10).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			labels := []string{locales.SearchAllContent(), locales.CommitMessage(), locales.CommitID(),
				locales.ReferenceBranch(), locales.SearchFileChanges(), locales.SearchUser()}
			modes := []string{"", "message", "hash", "branch", "changes", "author"}
			mode := labels[0]
			for index, value := range modes {
				if value == app.historyFilter.Mode {
					mode = labels[index]
				}
			}
			ui.Select(c, &mode, labels).Width(120).Label(locales.SearchType()).Disabled(app.busy).
				OnChange(func() {
					for index, label := range labels {
						if mode == label {
							app.historyFilter.Mode = modes[index]
						}
					}
				})
			input := ui.TextInput(c, &app.historyQuery).Search().Bind(&app.searchInput).
				Placeholder(app.searchPlaceholder()).Label(locales.SearchHistory()).Grow(2).MinWidth(100)
			if !app.historyLoaded {
				input.AutoFocus()
			}
			submitted := input.Submitted()
			pathSubmitted := ui.TextInput(c, &app.historyFile).Placeholder(locales.OptionalFilePath()).Label(locales.OptionalFilePath()).
				Grow(1).MinWidth(140).Submitted()
			ui.Checkbox(c, &app.searchDateEnabled, locales.SearchDateRange())
			if ui.PrimaryButton(c, locales.Search()).Label(locales.RunSearch()).Disabled(app.busy).Clicked() || submitted || pathSubmitted {
				app.executeSearch()
			}
			if app.readTask != nil && app.readTask.kind == nativeReadHistory {
				ui.Spinner(c).Size(14, 14).Label(locales.SearchingHistory())
				ui.Button(c, locales.CancelSearch()).OnClick(app.readTask.cancel)
			}
		})
		if app.searchDateEnabled {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				if app.searchSince.IsZero() {
					app.searchUntil = time.Now()
					app.searchSince = app.searchUntil.AddDate(0, -1, 0)
				}
				ui.DateInput(c, &app.searchSince).Width(135).Label(locales.SearchStartDate())
				ui.Text(c, locales.SearchDateTo()).FontSize(12).TextColor(c.Theme().TextMuted)
				ui.DateInput(c, &app.searchUntil).Width(135).Label(locales.SearchEndDate())
			})
		}
		if app.searchDateEnabled && app.searchSince.Format(time.DateOnly) > app.searchUntil.Format(time.DateOnly) {
			ui.Text(c, locales.SearchInvalidDates()).FontSize(12).TextColor(c.Theme().Danger)
		}
	})
}

func (app *nativeApp) executeSearch() {
	request := app.historyRequest()
	if request.filter.Since != "" && request.filter.Until != "" && request.filter.Since > request.filter.Until {
		return
	}
	app.searchHistory(false)
}

func (app *nativeApp) searchPlaceholder() string {
	switch app.historyFilter.Mode {
	case "message":
		return locales.SearchMessages()
	case "hash":
		return locales.SearchSHA()
	case "branch":
		return locales.SearchBranches()
	case "changes":
		return locales.SearchChangedLines()
	case "author":
		return locales.SearchAuthors()
	default:
		return locales.SearchHistory()
	}
}

func (app *nativeApp) revealSearchCommit(hash string) {
	if app.repository == nil || app.busy || app.readTask != nil {
		return
	}
	app.switchWorkspace("history")
	path, filter := app.repository.snapshot.Path, app.historyFilter
	app.load(locales.ReadingCommit(), nativeReadHistory, func(ctx context.Context) (func(), error) {
		location, err := app.service.LocateHistory(ctx, path, hash, "", "", filter)
		if err == nil && len(location.Commits) == 0 {
			filter = gitrepo.HistoryFilter{}
			location, err = app.service.LocateHistory(ctx, path, hash, "", "", filter)
		}
		if err != nil {
			return nil, err
		}
		files, err := app.service.CommitFiles(ctx, path, hash)
		if err != nil {
			return nil, err
		}
		file := ""
		var diff gitrepo.Diff
		if len(files) > 0 {
			file = files[0].Path
			diff, err = app.service.CommitFileDiff(ctx, path, hash, file)
		}
		return func() {
			app.history, app.historyOffset = location.Commits, location.Offset
			app.historyMore, app.historyLoaded = len(location.Commits) == 100, true
			app.historyQuery, app.historyFile, app.historyFilter = "", "", filter
			app.historyResult = app.historyRequest()
			app.viewedTag, app.viewedCommit = "", location.Commit
			app.historyList = &ui.ListState{}
			app.applyCommitSelection(hash, files, file)
			if err == nil {
				app.setDiff(diff)
			}
		}, err
	})
}

func (app *nativeApp) searchSummary() string {
	if !app.historyLoaded {
		return locales.SearchStartHint()
	}
	if app.historyRequest() != app.historyResult {
		return locales.SearchConditionsChanged()
	}
	return locales.SearchResultCount(fmt.Sprint(len(app.history)))
}
