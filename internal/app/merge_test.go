// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ysicing/51code/internal/gitrepo"

	"github.com/egoist/mygo/ui"
)

func nativeMergeFixture(t *testing.T) (*nativeApp, string, func()) {
	t.Helper()
	app, path, finish := nativeFixture(t)
	nativeGit(t, path, "checkout", "-b", "feature/login")
	writeNativeFile(t, path, "feature.txt", "feature\n")
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "feature")
	nativeGit(t, path, "checkout", "main")
	app.refresh()
	finish()
	app.mergeDialog()
	return app, path, finish
}

func TestNativeMergeModes(t *testing.T) {
	for _, test := range []struct {
		label   string
		action  string
		parents int
		staged  bool
	}{
		{label: "自动（允许快进）", action: "合并", parents: 1},
		{label: "始终创建合并提交", action: "合并", parents: 2},
		{label: "仅允许快进", action: "合并", parents: 1},
		{label: "压缩合并（Squash）", action: "压缩合并", staged: true},
	} {
		t.Run(test.label, func(t *testing.T) {
			app, path, finish := nativeMergeFixture(t)
			before := nativeGit(t, path, "rev-parse", "HEAD")
			tester := ui.NewTester(app.view, 1024, 760)
			if err := tester.Click(test.label); err != nil {
				t.Fatal(err)
			}
			if err := tester.Click(test.action); err != nil {
				t.Fatal(err)
			}
			finish()
			tester.Frame()
			if app.dialog != nil || app.problem != "" {
				t.Fatalf("merge did not complete: %+v, %s", app.dialog, app.problem)
			}
			if test.staged {
				if nativeGit(t, path, "rev-parse", "HEAD") != before || nativeGit(t, path, "diff", "--cached", "--name-only") != "feature.txt" {
					t.Fatal("squash should leave staged content without a commit")
				}
			} else if count := len(strings.Fields(nativeGit(t, path, "show", "-s", "--format=%P", "HEAD"))); count != test.parents {
				t.Fatalf("parent count = %d, want %d", count, test.parents)
			}
		})
	}
}

func TestNativeMergeOptionsAndFailure(t *testing.T) {
	app, path, finish := nativeMergeFixture(t)
	tester := ui.NewTester(app.view, 1024, 760)
	for _, label := range []string{"始终创建合并提交", "合并后立即提交", "压缩合并（Squash）", "始终创建合并提交"} {
		if err := tester.Click(label); err != nil {
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				saveNativeScreenshot(t, tester, directory, "merge-failure.png")
			}
			t.Fatal(err)
		}
	}
	if info, ok := tester.AccessByRole(ui.RoleCheckBox, "合并后立即提交"); !ok || info.Checked {
		t.Fatal("switching modes lost the commit choice")
	}
	if err := tester.Click("仅允许快进"); err != nil {
		t.Fatal(err)
	}
	if info, ok := tester.AccessByRole(ui.RoleCheckBox, "合并后立即提交"); !ok || !info.Disabled {
		t.Fatal("fast forward exposes a meaningless commit checkbox")
	}
	writeNativeFile(t, path, "main.txt", "main\n")
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "diverged")
	if err := tester.Click("合并"); err != nil {
		t.Fatal(err)
	}
	finish()
	tester.Frame()
	if app.dialog == nil || app.dialog.problem == "" {
		t.Fatal("failed merge did not keep the dialog and error")
	}
	if info, ok := tester.AccessByRole(ui.RoleRadio, "仅允许快进"); !ok || !info.Checked {
		t.Fatal("failed merge lost selected mode")
	}
	if err := tester.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if app.dialog != nil {
		t.Fatal("cancel did not close dialog")
	}
}

func TestNativeMergeLayoutAndMotion(t *testing.T) {
	app, _, _ := nativeMergeFixture(t)
	for _, width := range []int{1024, 480} {
		for _, dark := range []bool{false, true} {
			tester := ui.NewTester(app.view, width, 680)
			tester.SetDark(dark)
			for _, label := range []string{"自动（允许快进）", "始终创建合并提交", "仅允许快进", "压缩合并（Squash）"} {
				if err := tester.Click(label); err != nil {
					t.Fatal(err)
				}
				tester.Advance(2 * time.Second)
			}
			for _, label := range []string{"合并到 main", "来源分支", "压缩合并", "取消", "关闭合并窗口", "重播演示"} {
				rect, ok := tester.Find(label)
				if !ok || rect.X < 0 || rect.Y < 0 || rect.X+rect.W > float32(width) || rect.Y+rect.H > 680 {
					t.Fatalf("control out of bounds: %s %+v", label, rect)
				}
			}
			if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
				saveNativeScreenshot(t, tester, directory, fmt.Sprintf("merge-%d-dark-%t.png", width, dark))
			}
			tester.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			if info, ok := tester.AccessByRole(ui.RoleButton, "重播演示"); !ok || !info.Disabled {
				t.Fatal("replay should be disabled under reduced motion")
			}
		}
	}
	tester := ui.NewTester(app.view, 1024, 680)
	tester.Key(0, ui.KeyEscape)
	if app.dialog != nil {
		t.Fatal("Escape did not close merge dialog")
	}
}

func TestMergePreviewAnimationSettlesAndRespectsReducedMotion(t *testing.T) {
	for _, mode := range []gitrepo.MergeMode{gitrepo.MergeAuto, gitrepo.MergeNoFF, gitrepo.MergeFFOnly, gitrepo.MergeSquash} {
		t.Run(string(mode), func(t *testing.T) {
			var replay uint64
			view := func(c *ui.Context) { mergePreview(c, mode, &replay) }
			tester := ui.NewTester(view, 420, 160)
			first := bytes.Clone(tester.Image().Pix)
			tester.Advance(2 * time.Second)
			last := bytes.Clone(tester.Image().Pix)
			if bytes.Equal(first, last) {
				t.Fatal("preview did not animate")
			}
			tester.Advance(time.Second)
			if !bytes.Equal(last, tester.Image().Pix) {
				t.Fatal("preview keeps animating after completion")
			}
			still := ui.NewTesterWith(view, 420, 160, ui.TesterOptions{
				Preferences: ui.Preferences{ReduceMotion: true, TextScale: 1},
			})
			before := bytes.Clone(still.Image().Pix)
			still.Advance(2 * time.Second)
			if !bytes.Equal(before, still.Image().Pix) {
				t.Fatal("reduced motion still animates")
			}
		})
	}
}

func TestNativeMergeEmptyBusyAndLongNames(t *testing.T) {
	app, _, _ := nativeFixture(t)
	app.mergeDialog()
	if app.dialog != nil || app.problem == "" {
		t.Fatal("missing empty branch message")
	}
	app, _, _ = nativeMergeFixture(t)
	tester := ui.NewTester(app.view, 1024, 680)
	app.busy = true
	tester.Frame()
	for _, label := range []string{"合并", "取消", "关闭合并窗口", "重播演示"} {
		if info, ok := tester.AccessByRole(ui.RoleButton, label); !ok || !info.Disabled {
			t.Fatalf("busy operation still enables %q", label)
		}
	}
	tester.Key(0, ui.KeyEscape)
	if app.dialog == nil {
		t.Fatal("busy operation dismissed dialog")
	}
	app.busy = false
	app.dialog = nil
	app.repository.snapshot.Branch = strings.Repeat("long-current-branch-", 8)
	app.repository.branches[0].Name = strings.Repeat("long-source-branch-", 8)
	t.Setenv("MYGO_LOCALE", "en")
	app.mergeDialog()
	compact := ui.NewTester(app.view, 480, 560)
	compact.SetLocale("en")
	for _, label := range []string{"Squash merge", "Cancel", "Merge"} {
		rect, ok := compact.Find(label)
		if !ok || rect.X < 0 || rect.X+rect.W > 480 || rect.Y+rect.H > 560 {
			t.Fatalf("long names or English overflow: %s %+v", label, rect)
		}
	}
	if directory := os.Getenv("XSOURCETREE_TEST_SCREENSHOTS"); directory != "" {
		saveNativeScreenshot(t, compact, directory, "merge-long-english.png")
	}
}

func TestNativeMergeSourceUsesUnambiguousRef(t *testing.T) {
	app, path, finish := nativeFixture(t)
	nativeGit(t, path, "branch", "origin/topic")
	nativeGit(t, path, "update-ref", "refs/remotes/origin/topic", "HEAD")
	nativeGit(t, path, "checkout", "-b", "refs/heads/origin/topic")
	writeNativeFile(t, path, "chosen.txt", "chosen\n")
	nativeGit(t, path, "add", ".")
	nativeGit(t, path, "commit", "-m", "chosen")
	want := nativeGit(t, path, "rev-parse", "HEAD")
	nativeGit(t, path, "checkout", "main")
	app.refresh()
	finish()
	app.mergeDialog()
	tester := ui.NewTester(app.view, 1024, 680)
	source, ok := tester.AccessByRole(ui.RoleMenuButton, "来源分支")
	if !ok {
		t.Fatal("missing source menu")
	}
	tester.ClickAt(source.Bounds.X+source.Bounds.W/2, source.Bounds.Y+source.Bounds.H/2)
	if err := tester.ChooseMenuItem("refs/heads/refs/heads/origin/topic"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("合并"); err != nil {
		t.Fatal(err)
	}
	finish()
	if app.problem != "" || app.dialog != nil || nativeGit(t, path, "rev-parse", "HEAD") != want {
		t.Fatalf("selected a different branch: %s", app.problem)
	}
}
