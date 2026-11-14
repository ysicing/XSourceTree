// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ysicing/51code/internal/gitrepo"
	"github.com/ysicing/51code/locales"

	"github.com/egoist/mygo/ui"
)

type historyLane struct {
	Hash  string
	Color int
}

type historyEdge struct {
	From, To int
	Color    int
}

type historyGraphRow struct {
	Working                bool
	Column, Color, Columns int
	Incoming               []int
	Edges                  []historyEdge
}

// 轨道颜色随父提交传递，不随列压缩改变；筛选掉的提交不能被画成直接相连。
func commitGraph(commits []gitrepo.Commit) []historyGraphRow {
	positions := make(map[string]int, len(commits))
	for index, commit := range commits {
		positions[commit.Hash] = index
	}
	var lanes []historyLane
	nextColor := 0
	result := make([]historyGraphRow, len(commits))
	for index, commit := range commits {
		row := &result[index]
		for _, lane := range lanes {
			row.Incoming = append(row.Incoming, lane.Color)
		}
		column := slices.IndexFunc(lanes, func(lane historyLane) bool { return lane.Hash == commit.Hash })
		if column < 0 {
			column = len(lanes)
			lanes = append(lanes, historyLane{Hash: commit.Hash, Color: nextColor})
			nextColor++
		}
		row.Column, row.Color = column, lanes[column].Color
		after := slices.Clone(lanes)
		after = slices.Delete(after, column, column+1)
		for parentIndex, parent := range commit.Parents {
			if position, ok := positions[parent]; !ok || position <= index {
				continue
			}
			target := slices.IndexFunc(after, func(lane historyLane) bool { return lane.Hash == parent })
			if target >= 0 {
				if parentIndex == 0 {
					after[target].Color = row.Color
				}
				continue
			}
			lane := historyLane{Hash: parent, Color: row.Color}
			if parentIndex == 0 {
				after = slices.Insert(after, min(column, len(after)), lane)
			} else {
				lane.Color = nextColor
				nextColor++
				after = append(after, lane)
			}
		}
		for from, lane := range lanes {
			targets := []string{lane.Hash}
			if from == column {
				targets = commit.Parents
			}
			for _, hash := range targets {
				to := slices.IndexFunc(after, func(lane historyLane) bool { return lane.Hash == hash })
				if to >= 0 {
					row.Edges = append(row.Edges, historyEdge{From: from, To: to, Color: after[to].Color})
				}
			}
		}
		row.Columns = max(len(lanes), len(after))
		lanes = after
	}
	return result
}

func historyColor(theme *ui.Theme, index int) ui.Color {
	colors := []ui.Color{
		{R: 0, G: 113, B: 245, A: 255},
		{R: 226, G: 120, B: 22, A: 255},
		{R: 51, G: 153, B: 72, A: 255},
		{R: 160, G: 87, B: 210, A: 255},
		{R: 0, G: 151, B: 167, A: 255},
	}
	color := colors[index%len(colors)]
	if theme.Dark {
		color = color.Mix(ui.Color{R: 255, G: 255, B: 255, A: 255}, 0.22)
	}
	return color
}

func drawHistoryGraph(p *ui.Painter, r ui.Rect, row historyGraphRow, theme *ui.Theme) {
	x := func(column int) float32 { return r.X + 8 + float32(column)*16 }
	middle := r.Y + r.H/2
	for column, color := range row.Incoming {
		p.Line(x(column), r.Y, x(column), middle, 1.6, historyColor(theme, color))
	}
	for _, edge := range row.Edges {
		start := middle
		if row.Working && edge.From == row.Column {
			start += 3.6
		}
		path := new(ui.Path).MoveTo(x(edge.From), start)
		path.CubeTo(x(edge.From), middle+r.H/4, x(edge.To), middle+r.H/4, x(edge.To), r.Y+r.H)
		p.StrokePath(path, 1.6, historyColor(theme, edge.Color))
	}
	dot := new(ui.Path).Circle(x(row.Column), middle, 3.6)
	if row.Working {
		p.StrokePath(dot, 1.6, theme.TextMuted)
	} else {
		p.FillPath(dot, historyColor(theme, row.Color))
	}
}

func historyDate(value string) string {
	if date, err := time.Parse(time.RFC3339, value); err == nil {
		return date.Local().Format("2006-01-02 15:04")
	}
	return value
}

func compactHistoryRef(ref string) string {
	ref = strings.TrimPrefix(ref, "tag: ")
	label := []rune(ref)
	if len(label) > 16 {
		return string(label[:15]) + "…"
	}
	return ref
}

func historyRefs(refs string, theme *ui.Theme) ([]ui.Span, int) {
	var spans []ui.Span
	labels := strings.Split(refs, ", ")
	count := 0
	for _, ref := range labels {
		if ref == "" {
			continue
		}
		count++
		if count > 2 {
			continue
		}
		color := theme.Accent
		if strings.HasPrefix(ref, "tag: ") {
			color = theme.Warning
		} else if strings.Contains(ref, "/") && !strings.Contains(ref, "HEAD") {
			color = theme.TextMuted
		}
		spans = append(spans, ui.Span{
			Text: compactHistoryRef(ref), Size: 10, Weight: 600, Color: color,
			Background: color.Alpha(0.12), Radius: 3, PaddingX: 4, PaddingY: 1,
		}, ui.Span{Text: " "})
	}
	return spans, max(0, count-2)
}

func (app *nativeApp) historyWorkspace(c *ui.Context) {
	if app.repository == nil {
		return
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		app.operationBar(c)
		app.prefs.History = max(30, min(70, app.prefs.History))
		ui.SplitVerticalPercent(c, &app.prefs.History, func() {
			ui.Column(c).Fill().MinHeight(0).Children(func() { app.historyPanel(c) })
		}, func() {
			ui.Column(c).Fill().MinHeight(0).Children(func() {
				app.historyDetails(c)
				ui.Divider(c)
				ui.Split(c, &app.prefs.HistoryFiles, func() {
					if app.historyWorking {
						app.historyWorkingFiles(c)
					} else {
						app.historyFiles(c)
					}
				}, func() {
					app.diffPanel(c)
				}).FillWidth().Grow(1).MinHeight(0).SplitReset(280)
			})
		}).FillWidth().Grow(1).MinHeight(0).SplitReset(56)
	})
}

func (app *nativeApp) historyPanel(c *ui.Context) {
	if app.screen == "search" {
		app.searchControls(c)
	} else {
		ui.Row(c).FillWidth().Padding(4, 10).Gap(8).AlignItems(ui.Center).Children(func() {
			scopes := []string{locales.AllBranches(), locales.CurrentBranch()}
			scope := scopes[0]
			if app.historyFilter.CurrentBranch {
				scope = scopes[1]
			}
			c.Key("history-scope")
			ui.Select(c, &scope, scopes).Label(locales.HistoryBranchScope()).Width(140).Height(26).FontSize(12).
				Disabled(app.busy).OnChange(func() {
				app.historyFilter.CurrentBranch = scope == scopes[1]
				app.searchHistory(false)
			})
			remotes := []string{locales.ShowRemoteBranches(), locales.HideRemoteBranches()}
			remote := remotes[0]
			if app.historyFilter.HideRemotes {
				remote = remotes[1]
			}
			c.Key("history-remotes")
			ui.Select(c, &remote, remotes).Label(locales.HistoryRemoteVisibility()).Width(190).Height(26).FontSize(12).
				Disabled(app.busy).OnChange(func() {
				app.historyFilter.HideRemotes = remote == remotes[1]
				app.searchHistory(false)
			})
			app.historyReadControls(c)
		})
	}
	if app.viewedTag != "" {
		ui.Row(c).FillWidth().Padding(4, 10).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, nativeIcons["tag"]).FontSize(12).Shrink(0)
			ui.Text(c, app.viewedTag).FontSize(12).SingleLine().Tooltip(app.viewedTag).MinWidth(0).Shrink(1)
			if app.historySelected < 0 {
				ui.Text(c, locales.TagOutsideHistoryFilter()).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().MinWidth(0).Shrink(1)
				ui.Button(c, locales.ViewInAllHistory()).FontSize(12).Disabled(app.busy || app.readTask != nil).
					OnClick(func() { app.selectTag(app.viewedTag, true) })
			}
		})
	}
	if app.historyOffset > 0 {
		ui.Button(c, locales.BackToLatestCommits()).FontSize(12).Disabled(app.busy || app.readTask != nil).
			OnClick(func() { app.searchHistory(false) })
	}
	ui.Divider(c)
	workingOffset := app.historyWorkingOffset()
	if len(app.history) == 0 && workingOffset == 0 {
		app.historyGraph, app.historyGraphSource = nil, nil
		ui.Column(c).Key("empty-history").Grow(1).FillWidth().Center().Children(func() {
			message := locales.NoMatchingCommits()
			if app.screen == "search" && !app.historyLoaded {
				message = locales.SearchStartHint()
			}
			ui.Text(c, message).TextColor(c.Theme().TextMuted)
		})
		return
	}
	// 搜索和分页会替换切片或改变长度，拖动分隔线时无需反复计算整张图。
	var source *gitrepo.Commit
	if len(app.history) > 0 {
		source = &app.history[0]
	}
	head := app.repository.snapshot.Head
	if app.historyGraphSource != source || len(app.historyGraph) != len(app.history)+workingOffset ||
		app.historyGraphWorking != (workingOffset != 0) || app.historyGraphHead != head {
		app.historyGraph = historyGraphWithWorking(app.history, head, workingOffset != 0)
		app.historyGraphSource, app.historyGraphHead = source, head
		app.historyGraphWorking = workingOffset != 0
	}
	graphWidth := float32(80)
	for _, row := range app.historyGraph {
		graphWidth = max(graphWidth, 24+float32(row.Columns)*16)
	}
	app.historyTableSelected = -1
	if app.historyWorking && workingOffset != 0 {
		app.historyTableSelected = 0
	} else if app.historySelected >= 0 {
		app.historyTableSelected = app.historySelected + workingOffset
	}
	app.historyList.Selected = &app.historyTableSelected
	history := app.history
	workingLabel := locales.UncommittedFiles(fmt.Sprint(len(app.repository.snapshot.Files)))
	app.historyList.Key = func(index int) any {
		if workingOffset != 0 && index == 0 {
			return "working-tree"
		}
		return history[index-workingOffset].Hash
	}
	app.historyList.Label = func(index int) string {
		if workingOffset != 0 && index == 0 {
			return workingLabel
		}
		return history[index-workingOffset].Subject
	}
	app.historyList.CellMenu = func(menu *ui.Menu, row, _ int) {
		row -= workingOffset
		if row < 0 || row >= len(history) {
			return
		}
		commit := history[row]
		if app.screen == "search" && menu.Item(locales.ViewInCommitHistory()).Disabled(app.busy || app.readTask != nil).Chosen() {
			app.revealSearchCommit(commit.Hash)
		}
		unavailable := !app.canCreateReference() || app.readTask != nil && app.readTask.kind == nativeReadHistory
		if menu.Item(locales.BranchFromCommit()).Disabled(unavailable).Chosen() {
			app.referenceDialog(false, locales.ReferenceCommit()+" "+commit.Subject, commit.Hash, false)
		}
		if menu.Item(locales.TagFromCommit()).Disabled(unavailable).Chosen() {
			app.referenceDialog(true, locales.ReferenceCommit()+" "+commit.Subject, commit.Hash, false)
		}
	}
	theme := *c.Theme()
	tableTheme := theme
	tableTheme.Spacing, tableTheme.FontSize = 3.5, 14
	tableTheme.Accent = theme.Accent.Alpha(0.16)
	tableTheme.InactiveAccent, tableTheme.AccentText = tableTheme.Accent, theme.Text
	ui.Themed(c, &tableTheme).Key(app.historyList).FillWidth().Grow(1).MinHeight(0).Children(func() {
		columns := []ui.TableColumn{
			{Title: locales.Graph(), Width: graphWidth, Fixed: true},
			{Title: locales.CommitMessage(), MinWidth: 200},
			{Title: locales.Author(), Width: 110, MinWidth: 70},
			{Title: locales.Date(), Width: 138, MinWidth: 138},
			{Title: locales.CommitID(), Width: 88, MinWidth: 88},
		}
		label := locales.HistoryTable()
		searching := app.screen == "search"
		if searching {
			columns = columns[1:]
			label = locales.SearchResults()
		}
		if ui.Table(c, app.historyList, columns, len(app.history)+workingOffset, func(index, column int) {
			if searching {
				column++
			}
			working := workingOffset != 0 && index == 0
			var commit gitrepo.Commit
			if !working {
				commit = app.history[index-workingOffset]
			}
			switch column {
			case 0:
				ui.Box(c).Width(graphWidth - 17.5).MinHeight(17.5).AlignSelf(ui.Stretch).Draw(func(p *ui.Painter, r ui.Rect) {
					r.Y, r.H = r.Y-5.25, r.H+10.5
					drawHistoryGraph(p, r, app.historyGraph[index], &theme)
				})
			case 1:
				if working {
					ui.Text(c, workingLabel).Bold().SingleLine().Tooltip(workingLabel)
					return
				}
				ui.Row(c).FillWidth().MinWidth(0).Gap(4).AlignItems(ui.Center).Children(func() {
					if spans, overflow := historyRefs(commit.Refs, &theme); len(spans) > 0 {
						ui.Row(c).MaxWidth(180).Gap(3).MinWidth(0).AlignItems(ui.Center).Children(func() {
							ui.RichText(c, spans...).MaxWidth(150).MinWidth(0).Shrink(1).SingleLine()
							if overflow > 0 {
								ui.Text(c, fmt.Sprintf("+%d", overflow)).FontSize(10).TextColor(theme.TextMuted).SingleLine()
							}
						})
					}
					ui.Text(c, commit.Subject).Grow(1).MinWidth(0).SingleLine()
				})
			case 2:
				ui.Text(c, commit.Author).FontSize(12).TextColor(theme.TextMuted).SingleLine()
			case 3:
				ui.Text(c, historyDate(commit.Date)).FontSize(12).TextColor(theme.TextMuted).SingleLine()
			case 4:
				ui.Text(c, shortHash(commit.Hash)).Font("monospace").FontSize(12).TextColor(theme.TextMuted).SingleLine()
			}
		}).Fill().Label(label).
			Disabled(app.busy || app.readTask != nil && app.readTask.kind == nativeReadHistory).
			Changed() && app.historyTableSelected >= 0 {
			if workingOffset != 0 && app.historyTableSelected == 0 {
				app.selectHistoryWorking()
				return
			}
			index := app.historyTableSelected - workingOffset
			if index >= len(app.history) {
				return
			}
			hash := app.history[index].Hash
			if app.historyWorking || hash != app.commitHash || app.readTask != nil || app.problem != "" {
				app.selectCommit(hash)
			}
		}
	})
	if app.screen == "search" {
		ui.Text(c, app.searchSummary()).FontSize(11).TextColor(c.Theme().TextMuted).Padding(4, 10)
	}
	if app.historyMore && app.historyPagePaused {
		ui.Button(c, locales.Retry()).Disabled(app.busy || app.readTask != nil).
			OnClick(func() { app.searchHistory(true) })
	}
	app.loadHistoryNearEnd(c)
}

func (app *nativeApp) historyDetails(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(9, 14).Gap(5).Children(func() {
		if app.historyWorking {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.UncommittedChanges()).Bold().FontSize(15).Grow(1)
				ui.Button(c, locales.GoToFileChanges()).Disabled(app.busy || app.readTask != nil).OnClick(app.openHistoryWorkingChanges)
			})
			return
		}
		if app.commitHash == "" {
			ui.Text(c, locales.SelectCommit()).TextColor(c.Theme().TextMuted)
			return
		}
		var selected *gitrepo.Commit
		if app.viewedCommit.Hash == app.commitHash {
			selected = &app.viewedCommit
		}
		for index := range app.history {
			if app.history[index].Hash == app.commitHash {
				selected = &app.history[index]
				break
			}
		}
		if selected != nil {
			ui.Text(c, selected.Subject).Bold().FontSize(15).MaxLines(1).Selectable().Tooltip(selected.Subject)
		}
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			if selected != nil {
				ui.Text(c, selected.Author).FontSize(11).MaxWidth(120).SingleLine().Selectable()
				ui.Text(c, historyDate(selected.Date)).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
			}
			ui.Text(c, app.commitHash).Font("monospace").FontSize(11).MinWidth(0).Grow(1).SingleLine().Selectable().Label(locales.FullCommitID())
			if app.screen == "search" {
				hash := app.commitHash
				ui.Button(c, locales.ViewInCommitHistory()).FontSize(12).Disabled(app.busy || app.readTask != nil).
					OnClick(func() { app.revealSearchCommit(hash) })
			}
		})
		if selected != nil && selected.Refs != "" {
			ui.Text(c, selected.Refs).FontSize(11).TextColor(c.Theme().TextMuted).MaxLines(1).Selectable().Tooltip(selected.Refs)
		}
	})
}

func (app *nativeApp) historyFiles(c *ui.Context) {
	empty := ""
	if app.commitHash != "" {
		empty = locales.EmptyCommit()
	}
	app.revisionFileList(c, app.commitFiles, app.commitList, &app.commitSelected, locales.CommitFileList(), empty, func(file string) {
		if file != app.commitFile || app.readTask != nil || app.problem != "" {
			app.selectCommitFile(file)
		}
	})
}

func (app *nativeApp) revisionFileList(c *ui.Context, files []gitrepo.CommitFile, list *ui.ListState, selected *int, label, empty string, choose func(string)) {
	theme := *c.Theme()
	theme.Accent = theme.Accent.Alpha(0.16)
	theme.InactiveAccent, theme.AccentText = theme.Accent, theme.Text
	ui.Themed(c, &theme).Fill().MinWidth(0).Children(func() {
		ui.Row(c).FillWidth().Height(49).Padding(0, 12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.ChangedFiles(fmt.Sprint(len(files)))).FontSize(12).Bold()
		})
		ui.Divider(c)
		if len(files) == 0 {
			if empty != "" {
				ui.Text(c, empty).Padding(12).FontSize(12).TextColor(c.Theme().TextMuted)
			}
			return
		}
		list.Selected = selected
		list.Key = func(index int) any { return files[index].Path }
		list.Label = func(index int) string { return files[index].Path }
		if ui.List(c, list, len(files), func(index int) {
			file := files[index]
			ui.Row(c).FillWidth().Height(30).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				nativeFileIcon(c, file.Path)
				ui.Text(c, file.Path).FontSize(12).Grow(1).MinWidth(0).SingleLine().Tooltip(file.Path)
				ui.Text(c, file.Status).FontSize(10).TextColor(c.Theme().TextMuted)
			})
		}).FillWidth().Grow(1).MinHeight(0).Disabled(app.readTask != nil && app.readTask.kind != nativeReadDiff).Label(label).Changed() && *selected >= 0 && *selected < len(files) {
			choose(files[*selected].Path)
		}
	})
}
