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

func conflictFixture(t *testing.T) (*Repository, string) {
	t.Helper()
	repo, dir := fixture(t)
	write(t, dir, "conflict.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "switch", "-c", "topic")
	write(t, dir, "conflict.txt", "theirs\n")
	git(t, dir, "commit", "-am", "topic change")
	git(t, dir, "switch", "main")
	write(t, dir, "conflict.txt", "ours\n")
	git(t, dir, "commit", "-am", "main change")
	return repo, dir
}

func TestMergeConflictResolution(t *testing.T) {
	repo, dir := conflictFixture(t)
	ctx := context.Background()
	if err := repo.Merge(ctx, "topic"); err == nil {
		t.Fatal("expected conflicting merge")
	}
	state, err := repo.Snapshot(ctx)
	if err != nil || state.Operation.Kind != "merge" || len(state.Files) != 1 || !state.Files[0].Conflict {
		t.Fatalf("merge state: %+v, %v", state, err)
	}
	if err := repo.Stage(ctx, []string{"conflict.txt"}); err == nil {
		t.Fatal("ordinary staging must not silently resolve conflicts")
	}
	view, err := repo.Conflict(ctx, "conflict.txt")
	if err != nil || view.Base.Text != "base\n" || view.Ours.Text != "ours\n" || view.Theirs.Text != "theirs\n" || !view.Editable {
		t.Fatalf("conflict: %+v, %v", view, err)
	}
	if err := repo.ResolveConflict(ctx, "conflict.txt", view.Token, "result", view.Result); err == nil {
		t.Fatal("must reject unresolved conflict markers")
	}
	write(t, dir, "conflict.txt", "external edit\n")
	if err := repo.ResolveConflict(ctx, "conflict.txt", view.Token, "result", "merged\n"); err == nil {
		t.Fatal("stale conflict must not overwrite external edit")
	}
	view, err = repo.Conflict(ctx, "conflict.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ResolveConflict(ctx, "conflict.txt", view.Token, "result", "merged\n"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "show", ":conflict.txt"); got != "merged" {
		t.Fatalf("resolved index: %q", got)
	}
	state, _ = repo.Snapshot(ctx)
	if err := repo.OperationAction(ctx, state.Operation.Kind, state.Operation.Token, "continue"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "log", "-1", "--format=%P"); len(strings.Fields(got)) != 2 {
		t.Fatalf("merge parents: %q", got)
	}
	state, _ = repo.Snapshot(ctx)
	if state.Operation.Kind != "" || len(state.Files) != 0 {
		t.Fatalf("after continue: %+v", state)
	}
}

func TestGitErrorIncludesConflictDiagnostics(t *testing.T) {
	repo, _ := conflictFixture(t)
	err := repo.Merge(context.Background(), "topic")
	if err == nil || !strings.Contains(err.Error(), "CONFLICT") {
		t.Fatalf("missing Git stdout diagnostics: %v", err)
	}
}

func TestAbortMergeAndRejectStaleOperation(t *testing.T) {
	repo, dir := conflictFixture(t)
	ctx := context.Background()
	_ = repo.Merge(ctx, "topic")
	state, _ := repo.Snapshot(ctx)
	if err := repo.OperationAction(ctx, "merge", "stale", "abort"); err == nil {
		t.Fatal("stale operation should be rejected")
	}
	if err := repo.OperationAction(ctx, "merge", state.Operation.Token, "abort"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "show", "HEAD:conflict.txt"); got != "ours" {
		t.Fatalf("abort changed HEAD: %s", got)
	}
	if got := git(t, dir, "status", "--porcelain"); got != "" {
		t.Fatalf("abort not clean: %s", got)
	}
}

func TestConflictSideDeletionAndBinary(t *testing.T) {
	for _, choice := range []string{"ours", "theirs", "delete"} {
		t.Run(choice, func(t *testing.T) {
			repo, dir := conflictFixture(t)
			ctx := context.Background()
			_ = repo.Merge(ctx, "topic")
			view, err := repo.Conflict(ctx, "conflict.txt")
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.ResolveConflict(ctx, "conflict.txt", view.Token, choice, ""); err != nil {
				t.Fatal(err)
			}
			if choice == "delete" {
				if _, err := os.Stat(filepath.Join(dir, "conflict.txt")); !os.IsNotExist(err) {
					t.Fatalf("file not deleted: %v", err)
				}
			} else if got := git(t, dir, "show", ":conflict.txt"); got != choice {
				t.Fatalf("selected %s: %s", choice, got)
			}
		})
	}
	t.Run("binary", func(t *testing.T) {
		repo, dir := fixture(t)
		write(t, dir, "data", "base\x00")
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "base")
		git(t, dir, "switch", "-c", "topic")
		write(t, dir, "data", "theirs\x00")
		git(t, dir, "commit", "-am", "theirs")
		git(t, dir, "switch", "main")
		write(t, dir, "data", "ours\x00")
		git(t, dir, "commit", "-am", "ours")
		_ = repo.Merge(context.Background(), "topic")
		view, err := repo.Conflict(context.Background(), "data")
		if err != nil || view.Editable || !view.Ours.Binary {
			t.Fatalf("binary view: %+v %v", view, err)
		}
		if err := repo.ResolveConflict(context.Background(), "data", view.Token, "theirs", ""); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(filepath.Join(dir, "data"))
		if string(data) != "theirs\x00" {
			t.Fatalf("binary result: %q", data)
		}
	})
}

func TestModifyDeleteConflictAndSafeSymlink(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "switch", "-c", "topic")
	git(t, dir, "rm", "file.txt")
	git(t, dir, "commit", "-m", "delete")
	git(t, dir, "switch", "main")
	write(t, dir, "file.txt", "modified\n")
	git(t, dir, "commit", "-am", "modify")
	_ = repo.Merge(ctx, "topic")
	view, err := repo.Conflict(ctx, "file.txt")
	if err != nil || view.Theirs.Present || !view.Ours.Present {
		t.Fatalf("modify/delete: %+v %v", view, err)
	}
	if err := repo.ResolveConflict(ctx, "file.txt", view.Token, "theirs", ""); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "ls-files", "--unmerged"); got != "" {
		t.Fatalf("unresolved deletion: %s", got)
	}
	outside := t.TempDir()
	write(t, outside, "secret.txt", "untouched\n")
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := repo.Conflict(ctx, "escape/secret.txt"); err == nil {
		t.Fatal("nonconflicting external file accepted")
	}
	if err := repo.ResolveConflict(ctx, "../secret.txt", "token", "result", "overwritten"); err == nil {
		t.Fatal("path escape accepted")
	}
	data, _ := os.ReadFile(filepath.Join(outside, "secret.txt"))
	if string(data) != "untouched\n" {
		t.Fatal("external file changed")
	}
}

func TestSymlinkConflictChoiceDoesNotFollowTarget(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	external := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(external, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink("base", link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "switch", "-c", "topic")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "commit", "-am", "theirs")
	git(t, dir, "switch", "main")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("ours", link); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "commit", "-am", "ours")
	_ = repo.Merge(ctx, "topic")
	view, err := repo.Conflict(ctx, "link")
	if err != nil || view.Editable {
		t.Fatalf("symlink view: %+v %v", view, err)
	}
	if err := repo.ResolveConflict(ctx, "link", view.Token, "theirs", ""); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(link); err != nil || target != external {
		t.Fatalf("symlink: %s %v", target, err)
	}
	data, _ := os.ReadFile(external)
	if string(data) != "untouched" {
		t.Fatal("symlink target changed")
	}
}

func TestConflictRejectsSymlinkedAncestorToGitMetadata(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "directory/config", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "switch", "-c", "topic")
	write(t, dir, "directory/config", "theirs\n")
	git(t, dir, "commit", "-am", "topic")
	git(t, dir, "switch", "main")
	write(t, dir, "directory/config", "ours\n")
	git(t, dir, "commit", "-am", "main")
	_ = repo.Merge(ctx, "topic")
	before, err := os.ReadFile(filepath.Join(dir, ".git/config"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "directory/config")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "directory")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".git", filepath.Join(dir, "directory")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if view, err := repo.Conflict(ctx, "directory/config"); err == nil {
		t.Fatalf("Git metadata exposed as conflict result: %+v", view)
	}
	after, err := os.ReadFile(filepath.Join(dir, ".git/config"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("Git metadata changed: %v", err)
	}
}

func TestApplyRebaseRejectsTokenFromPreviousPatch(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "switch", "-c", "topic")
	write(t, dir, "file", "upstream\n")
	git(t, dir, "commit", "-am", "upstream")
	git(t, dir, "switch", "main")
	write(t, dir, "file", "first\n")
	git(t, dir, "commit", "-am", "first")
	write(t, dir, "file", "second\n")
	git(t, dir, "commit", "-am", "second")
	if _, err := repo.git(ctx, "rebase", "--apply", "topic"); err == nil {
		t.Fatal("expected first patch conflict")
	}
	first, err := repo.Snapshot(ctx)
	if err != nil || first.Operation.Kind != "rebase" {
		t.Fatalf("first patch: %+v %v", first, err)
	}
	if _, err := repo.git(ctx, "rebase", "--skip"); err == nil {
		t.Fatal("expected second patch conflict")
	}
	second, err := repo.Snapshot(ctx)
	if err != nil || first.Head != second.Head {
		t.Fatalf("fixture HEAD must remain unchanged: %+v %v", second, err)
	}
	if first.Operation.Token == second.Operation.Token {
		t.Fatal("operation token ignored current patch identity")
	}
	if err := repo.OperationAction(ctx, "rebase", first.Operation.Token, "skip"); err == nil {
		t.Fatal("stale skip accepted")
	}
	if err := repo.OperationAction(ctx, "rebase", second.Operation.Token, "skip"); err != nil {
		t.Fatal(err)
	}
	final, _ := repo.Snapshot(ctx)
	if final.Operation.Kind != "" {
		t.Fatal("rebase did not finish")
	}
}

func TestRebaseConflictContinue(t *testing.T) {
	repo, dir := conflictFixture(t)
	ctx := context.Background()
	_, _ = repo.git(ctx, "rebase", "topic")
	state, err := repo.Snapshot(ctx)
	if err != nil || state.Operation.Kind != "rebase" {
		t.Fatalf("rebase state: %+v, %v", state, err)
	}
	view, err := repo.Conflict(ctx, "conflict.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ResolveConflict(ctx, "conflict.txt", view.Token, "result", "rebased\n"); err != nil {
		t.Fatal(err)
	}
	if err := repo.OperationAction(ctx, "rebase", state.Operation.Token, "continue"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "show", "HEAD:conflict.txt"); got != "rebased" {
		t.Fatal(got)
	}
	state, _ = repo.Snapshot(ctx)
	if state.Operation.Kind != "" {
		t.Fatalf("rebase still active: %+v", state.Operation)
	}
}

func TestMergeRemoteRefDoesNotSelectLocalNamesake(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "base", "base")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "switch", "-c", "topic")
	write(t, dir, "remote-file", "remote")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "remote change")
	remote := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "update-ref", "refs/remotes/origin/topic", remote)
	git(t, dir, "switch", "main")
	git(t, dir, "switch", "-c", "origin/topic")
	write(t, dir, "local-file", "local")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "local namesake")
	git(t, dir, "switch", "main")
	if err := repo.Merge(ctx, "refs/remotes/origin/topic"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != remote {
		t.Fatalf("merged local namesake instead: %s", got)
	}
}
