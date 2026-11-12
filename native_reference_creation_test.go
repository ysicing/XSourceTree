// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestNativeReferenceMenusAndPinnedTargets(t *testing.T) {
	app, path, finish := nativeFixture(t)
	base := app.repository.snapshot.Head
	nativeGit(t, path, "tag", "-a", "--no-sign", "-m", "release", "v1")
	nativeGit(t, path, "branch", "source")
	nativeGit(t, path, "commit", "--allow-empty", "-m", "later")
	head := nativeGit(t, path, "rev-parse", "HEAD")
	app.refresh()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.RightClick("source"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"从此分支新建分支…", "从此分支新建标签…", "重命名分支…"} {
		if !slices.Contains(tester.Menu(), label) {
			t.Fatalf("branch menu missing %s: %v", label, tester.Menu())
		}
	}
	if app.busy || app.repository.snapshot.Head != head {
		t.Fatal("right click switched branch")
	}
	if err := tester.ChooseMenuItem("从此分支新建标签…"); err != nil {
		t.Fatal(err)
	}
	finish()
	dialog := app.dialog
	if dialog == nil || !strings.Contains(dialog.detail, shortHash(base)) || !strings.Contains(dialog.detail, "分支 source") {
		t.Fatalf("wrong branch source: %+v", dialog)
	}
	nativeGit(t, path, "branch", "-f", "source", head)
	dialog.fields[0].value = "pinned"
	dialog.onChange()
	finish()
	tester.Frame()
	if err := tester.Click("创建"); err != nil {
		t.Fatal(err)
	}
	finish()
	if nativeGit(t, path, "rev-parse", "refs/tags/pinned") != base {
		t.Fatal("creation followed moved source branch")
	}
	tester.Frame()
	if err := tester.Click("标签"); err != nil {
		t.Fatal(err)
	}
	if err := tester.RightClick("v1"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("从此标签新建分支…"); err != nil {
		t.Fatal(err)
	}
	finish()
	dialog = app.dialog
	if dialog == nil || !strings.Contains(dialog.detail, "标签 v1") || !strings.Contains(dialog.detail, shortHash(base)) || dialog.check {
		t.Fatalf("annotated tag did not resolve commit or defaulted to checkout: %+v", dialog)
	}
	dialog.fields[0].value = "hotfix"
	dialog.onChange()
	finish()
	tester.Frame()
	if err := tester.Click("创建"); err != nil {
		t.Fatal(err)
	}
	finish()
	if nativeGit(t, path, "rev-parse", "refs/heads/hotfix") != base ||
		nativeGit(t, path, "rev-parse", "HEAD") != head || !strings.Contains(app.status, "hotfix") {
		t.Fatal("tag branch creation changed HEAD or lost success feedback")
	}
}

func TestNativeHistoryContextUsesClickedCommit(t *testing.T) {
	app, path, finish := nativeFixture(t)
	base := app.repository.snapshot.Head
	writeNativeFile(t, path, "hello.txt", "second\n")
	nativeGit(t, path, "commit", "-am", "second")
	app.screen = "history"
	app.searchHistory(false)
	finish()
	app.selectCommit(app.history[0].Hash)
	finish()
	selected, historyList, commitList := app.commitHash, app.historyList, app.commitList
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.RightClick("初始提交"); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("从此提交新建分支…"); err != nil {
		t.Fatal(err)
	}
	if app.readTask != nil {
		finish()
	}
	dialog := app.dialog
	if dialog == nil || !strings.Contains(dialog.detail, shortHash(base)) || dialog.check {
		t.Fatalf("context action used previous selection: %+v", dialog)
	}
	dialog.fields[0].value = "from-old-commit"
	dialog.onChange()
	finish()
	tester.Frame()
	if err := tester.Click("创建"); err != nil {
		t.Fatal(err)
	}
	finish()
	if nativeGit(t, path, "rev-parse", "from-old-commit") != base {
		t.Fatal("branch created at HEAD instead of right-clicked commit")
	}
	if app.commitHash != selected || app.historySelected != 0 || app.historyList != historyList || app.commitList != commitList ||
		app.commitFile != "hello.txt" || len(app.commitFiles) == 0 || !strings.Contains(app.diff.Text, "second") {
		t.Fatal("create-only action reset the history selection or detail")
	}
}

func TestNativeReferenceValidationAndPartialSuccess(t *testing.T) {
	app, path, finish := nativeFixture(t)
	base := app.repository.snapshot.Head
	writeNativeFile(t, path, "hello.txt", "next\n")
	nativeGit(t, path, "commit", "-am", "next")
	app.refresh()
	finish()
	writeNativeFile(t, path, "hello.txt", "unsaved\n")
	app.referenceDialog(false, "标签 v1", base, false)
	dialog := app.dialog
	tester := ui.NewTester(app.view, 1024, 680)
	if !tester.Focused("分支名称") {
		t.Fatal("name field was not focused")
	}
	tester.Type("main")
	finish()
	tester.Frame()
	if button, _ := tester.AccessByRole(ui.RoleButton, "创建"); !button.Disabled || !strings.Contains(dialog.fields[0].value, "main") {
		t.Fatal("duplicate name allowed creation")
	}
	dialog.fields[0].value = "fix/v1"
	dialog.onChange()
	if dialog.valid() {
		t.Fatal("pending validation allowed submission")
	}
	finish()
	tester.Frame()
	if err := tester.Click("创建后切换到新分支"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("创建并切换"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.dialog != dialog || !dialog.fields[0].readOnly || dialog.action != "重试切换" ||
		!strings.Contains(dialog.problem, "已创建") || nativeGit(t, path, "rev-parse", "refs/heads/fix/v1") != base {
		t.Fatalf("partial success not recoverable: %+v", dialog)
	}
	writeNativeFile(t, path, "hello.txt", "next\n")
	if err := tester.Click("重试切换"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.dialog != nil || app.repository.snapshot.Branch != "fix/v1" || !strings.Contains(app.status, "已创建并切换") {
		t.Fatalf("retry recreated branch instead of switching: %s", app.problem)
	}
}

func TestNativeReferenceValidationIgnoresStaleAndClosedForms(t *testing.T) {
	app, _, _ := nativeFixture(t)
	updates := make(chan func(), 4)
	app.post = func(update func()) { updates <- update }
	app.branchDialog()
	dialog := app.dialog
	next := func() func() {
		t.Helper()
		select {
		case update := <-updates:
			return update
		case <-time.After(5 * time.Second):
			t.Fatal("validation did not finish")
			return nil
		}
	}
	dialog.fields[0].value = "available"
	dialog.onChange()
	stale := next()
	dialog.fields[0].value = "main"
	dialog.onChange()
	stale()
	if dialog.valid() {
		t.Fatal("stale availability enabled a conflicting name")
	}
	next()()
	if dialog.valid() {
		t.Fatal("existing branch accepted")
	}
	dialog.fields[0].value = "another"
	dialog.onChange()
	late := next()
	app.closeDialog(dialog)
	app.branchDialog()
	replacement := app.dialog
	late()
	if app.dialog != replacement || replacement.valid() {
		t.Fatal("closed form validation affected replacement")
	}
}

func TestNativeReferenceFormAppearance(t *testing.T) {
	app, _, _ := nativeFixture(t)
	for _, appearance := range []string{appearanceLight, appearanceDark} {
		app.prefs.Appearance = appearance
		app.dialog = nil
		app.referenceDialog(false, "标签 release/"+strings.Repeat("长名称", 30), app.repository.snapshot.Head, false)
		tester := ui.NewTester(app.view, 1024, 680)
		for _, label := range []string{"分支名称", "创建后切换到新分支", "取消", "创建"} {
			info, ok := tester.Access(label)
			if !ok || info.Offscreen || info.Bounds.Y+info.Bounds.H > 680 {
				t.Fatalf("%s %s clipped: %+v", appearance, label, info)
			}
		}
		if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
			saveNativeScreenshot(t, tester, directory, "reference-form-"+appearance+".png")
		}
		tester.Key(0, ui.KeyEscape)
		if app.dialog != nil {
			t.Fatal("escape did not dismiss creation form")
		}
	}
}
