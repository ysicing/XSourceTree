// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNativePushSelectsRemoteWithoutChangingUpstream(t *testing.T) {
	app, path, finish := nativeFixture(t)
	remotes := map[string]string{}
	for _, name := range []string{"origin", "backup"} {
		remote := filepath.Join(t.TempDir(), name+".git")
		nativeGit(t, path, "init", "--bare", remote)
		nativeGit(t, path, "remote", "add", name, remote)
		remotes[name] = remote
	}
	nativeGit(t, path, "push", "-u", "origin", "main:release")
	original := nativeGit(t, remotes["origin"], "rev-parse", "refs/heads/release")
	nativeGit(t, path, "commit", "--allow-empty", "-m", "selected remote")
	app.refresh()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("推送"); err != nil {
		t.Fatal(err)
	}
	dialog := app.dialog
	if dialog == nil || app.busy || dialog.choice != "origin" || dialog.fields[0].value != "release" {
		t.Fatalf("多远程推送未显示上游目标: %+v", dialog)
	}
	if err := tester.Click("origin"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("backup"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if dialog.choice != "backup" || dialog.fields[0].value != "main" {
		t.Fatal("切换远程后没有使用当前本地分支名")
	}
	button, found := tester.AccessByRole(ui.RoleButton, "推送")
	if !found || button.Disabled {
		t.Fatal("推送确认按钮不可用")
	}
	tester.ClickAt(button.Bounds.X+button.Bounds.W/2, button.Bounds.Y+button.Bounds.H/2)
	if !app.busy {
		t.Fatal("确认推送没有启动任务")
	}
	finish()
	if app.dialog != nil || app.problem != "" {
		t.Fatalf("推送失败: dialog=%+v problem=%s", app.dialog, app.problem)
	}
	if got := nativeGit(t, remotes["backup"], "rev-parse", "refs/heads/main"); got != app.repository.snapshot.Head {
		t.Fatal("未推送到选中的远程")
	}
	if got := nativeGit(t, remotes["origin"], "rev-parse", "refs/heads/release"); got != original {
		t.Fatal("推送修改了未选中的远程")
	}
	if got := nativeGit(t, path, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/release" {
		t.Fatal("推送改变了已有上游:", got)
	}
}

func TestNativeSingleRemotePushKeepsDirectAction(t *testing.T) {
	app, path, finish := nativeFixture(t)
	remote := t.TempDir()
	nativeGit(t, remote, "init", "--bare")
	nativeGit(t, path, "remote", "add", "origin", remote)
	nativeGit(t, path, "push", "-u", "origin", "main")
	app.refresh()
	finish()
	tester := ui.NewTester(app.view, 1024, 680)
	if err := tester.Click("推送"); err != nil {
		t.Fatal(err)
	}
	if app.dialog != nil || !app.busy {
		t.Fatal("单远程已有上游时应直接推送")
	}
	finish()
}

func TestNativePushDoesNotInferRemoteFromLocalUpstream(t *testing.T) {
	app, path, finish := nativeFixture(t)
	for _, name := range []string{"backup", "origin"} {
		nativeGit(t, path, "remote", "add", name, t.TempDir())
	}
	nativeGit(t, path, "branch", "backup/feature")
	nativeGit(t, path, "branch", "--set-upstream-to=backup/feature", "main")
	app.refresh()
	finish()
	app.remoteAction("push")
	if app.dialog == nil || app.dialog.choice != "origin" || app.dialog.fields[0].value != "main" {
		t.Fatalf("把含斜杠的本地上游误判为远程: %+v", app.dialog)
	}
}
