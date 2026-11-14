// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	if data, err := os.ReadFile(path); err != nil || string(data) != want {
		t.Fatalf("%s = %q, %v; want %q", path, data, err, want)
	}
}

// ignoredTrackingFixture 在 main 上忽略 .env，并在 other 分支开始跟踪它。
func ignoredTrackingFixture(t *testing.T) (*Repository, string) {
	t.Helper()
	repo, dir := fixture(t)
	write(t, dir, ".gitignore", ".env\n")
	write(t, dir, "base.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "switch", "-c", "other")
	write(t, dir, ".env", "branch\n")
	git(t, dir, "add", "--force", ".env")
	git(t, dir, "commit", "-m", "track env")
	git(t, dir, "switch", "main")
	write(t, dir, ".env", "local\n")
	return repo, dir
}

func TestPullDoesNotOverwriteIgnoredFiles(t *testing.T) {
	repo, dir := ignoredTrackingFixture(t)
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-b", "main")
	git(t, dir, "remote", "add", "origin", remote)
	git(t, dir, "push", "origin", "main", "other:incoming")
	git(t, dir, "branch", "--set-upstream-to=origin/incoming")
	// 继承的 rebase 配置不能改变拉取方式。
	git(t, dir, "config", "pull.rebase", "true")
	if err := repo.Remote(context.Background(), "pull"); err == nil {
		t.Fatal("pull overwrote ignored file")
	}
	assertFile(t, filepath.Join(dir, ".env"), "local\n")
	if err := os.Remove(filepath.Join(dir, ".env")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Remote(context.Background(), "pull"); err != nil {
		t.Fatal(err)
	}
	if got, want := git(t, dir, "rev-parse", "HEAD"), git(t, dir, "rev-parse", "other"); got != want {
		t.Fatalf("pull did not fast-forward: %s != %s", got, want)
	}
}

func TestPullRejectsDivergedUpstream(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-b", "main")
	git(t, dir, "remote", "add", "origin", remote)
	git(t, dir, "push", "-u", "origin", "main")
	git(t, dir, "commit", "--allow-empty", "-m", "remote only")
	git(t, dir, "push", "origin", "main")
	git(t, dir, "reset", "--hard", "HEAD~1")
	git(t, dir, "commit", "--allow-empty", "-m", "local only")
	local := git(t, dir, "rev-parse", "HEAD")
	if err := repo.Remote(context.Background(), "pull"); err == nil {
		t.Fatal("pull merged a diverged upstream")
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != local {
		t.Fatal("diverged pull moved HEAD:", got)
	}
}

func TestSwitchingBranchesDoesNotOverwriteIgnoredFiles(t *testing.T) {
	repo, dir := ignoredTrackingFixture(t)
	if err := repo.SwitchBranch(context.Background(), "other"); err == nil {
		t.Fatal("switch overwrote ignored file")
	}
	assertFile(t, filepath.Join(dir, ".env"), "local\n")
	if branch := git(t, dir, "branch", "--show-current"); branch != "main" {
		t.Fatal("failed switch changed branch:", branch)
	}

	git(t, dir, "update-ref", "refs/remotes/origin/other", "other")
	if err := repo.CheckoutRemote(context.Background(), "origin/other", "tracked"); err == nil {
		t.Fatal("remote checkout overwrote ignored file")
	}
	assertFile(t, filepath.Join(dir, ".env"), "local\n")
}

func TestDivergedWorktreeMergeDoesNotOverwriteIgnoredTargetData(t *testing.T) {
	repo, dir, path := worktreeFixture(t)
	write(t, path, "ignored/keep", "committed")
	git(t, path, "add", "--force", "ignored/keep")
	git(t, path, "commit", "-m", "track previously ignored file")
	// 目标分支先行一步，迫使合并走非快进路径。
	write(t, dir, "target.txt", "target\n")
	git(t, dir, "add", "target.txt")
	git(t, dir, "commit", "-m", "diverge")
	write(t, dir, "ignored/keep", "precious")
	review := worktreeReview(t, repo, path)
	if !review.CanMerge {
		t.Fatalf("diverged preflight: %+v", review)
	}
	head := git(t, dir, "rev-parse", "HEAD")
	if err := repo.MergeWorktree(context.Background(), path, review.Token); err == nil {
		t.Fatal("diverged merge overwrote ignored target file")
	}
	assertFile(t, filepath.Join(dir, "ignored", "keep"), "precious")
	if got := git(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatal("rejected merge moved target HEAD:", got)
	}
}

func TestDiffsIgnoreForcedColorConfig(t *testing.T) {
	repo, dir := fixture(t)
	git(t, dir, "config", "color.diff", "always")
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	write(t, dir, "file.txt", "next\n")
	ctx := context.Background()
	working, err := repo.Diff(ctx, "file.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitDiff(ctx, git(t, dir, "rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "stash", "push")
	stash, err := repo.StashDiff(ctx, git(t, dir, "rev-parse", "stash@{0}"))
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"working": working.Text, "commit": commit.Text, "stash": stash.Text} {
		if text == "" || strings.Contains(text, "\x1b[") {
			t.Fatalf("%s diff contains color codes or is empty: %q", name, text)
		}
	}
}

func TestGlobalIdentityKeepsUnchangedIncludedValue(t *testing.T) {
	_, home := identityFixture(t)
	included := filepath.Join(home, "identity.inc")
	write(t, home, "identity.inc", "[user]\n\tname = Included User\n")
	write(t, home, ".gitconfig", "[include]\n\tpath = "+included+"\n")
	ctx := context.Background()
	settings, err := GlobalIdentity(ctx)
	if err != nil || settings.Name.Value != "Included User" || len(settings.DirectName) != 0 {
		t.Fatalf("included identity: %+v %v", settings, err)
	}
	if err := SaveGlobalIdentity(ctx, IdentityRequest{Name: "Included User", Email: "new@example.test", Token: settings.Token}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Included User") || !strings.Contains(string(data), "new@example.test") {
		t.Fatalf("global config copied inherited name or lost email:\n%s", data)
	}

	// 用户明确改名时仍然写入直接值。
	settings, err = GlobalIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveGlobalIdentity(ctx, IdentityRequest{Name: "Direct User", Email: "new@example.test", Token: settings.Token}); err != nil {
		t.Fatal(err)
	}
	if settings, err = GlobalIdentity(ctx); err != nil || len(settings.DirectName) != 1 || settings.DirectName[0] != "Direct User" {
		t.Fatalf("explicit name was not written: %+v %v", settings, err)
	}
}
