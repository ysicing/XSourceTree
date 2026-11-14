// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func worktreeFixture(t *testing.T) (*Repository, string, string) {
	t.Helper()
	repo, dir := fixture(t)
	write(t, dir, "base.txt", "base\n")
	write(t, dir, ".gitignore", "ignored/\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, portableTestPath("中文\n工作区", "中文 工作区"))
	git(t, dir, "worktree", "add", "-b", "topic", path, "HEAD")
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return repo, dir, path
}

func worktreeReview(t *testing.T, repo *Repository, path string) WorktreeReview {
	t.Helper()
	review, err := repo.ReviewWorktree(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if review.Token == "" {
		t.Fatal("review has no token")
	}
	return review
}

func TestWorktreesDiscoverMainAndLinkedWithNULPaths(t *testing.T) {
	repo, _, path := worktreeFixture(t)
	ctx := context.Background()
	list, err := repo.Worktrees(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if !list[0].Main || !list[0].Current || list[0].Path != repo.Path || list[0].Branch != "refs/heads/main" {
		t.Fatalf("main: %+v", list[0])
	}
	if list[1].Main || list[1].Current || list[1].Path != path || list[1].Branch != "refs/heads/topic" || list[1].Problem != "" {
		t.Fatalf("linked: %+v", list[1])
	}
	linked, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	list, err = linked.Worktrees(ctx)
	if err != nil || !list[1].Current || list[0].Current || !list[0].Main {
		t.Fatalf("linked perspective: %+v, %v", list, err)
	}
}

func TestWorktreesRetainLockedMissingAndDetachedEntries(t *testing.T) {
	repo, dir, path := worktreeFixture(t)
	git(t, dir, "worktree", "lock", "--reason", "保留\n原因", path)
	list, err := repo.Worktrees(context.Background())
	if err != nil || !list[1].Locked || list[1].LockReason != "保留\n原因" {
		t.Fatalf("locked: %+v, %v", list, err)
	}
	review := worktreeReview(t, repo, path)
	if review.CanMerge || review.CanRemove || !strings.Contains(review.RemoveProblem, "锁定") {
		t.Fatalf("locked review: %+v", review)
	}
	git(t, dir, "worktree", "unlock", path)
	git(t, path, "checkout", "--detach")
	list, err = repo.Worktrees(context.Background())
	if err != nil || !list[1].Detached || list[1].Branch != "" {
		t.Fatalf("detached: %+v, %v", list, err)
	}
	if err := os.Rename(path, path+"-moved"); err != nil {
		t.Fatal(err)
	}
	list, err = repo.Worktrees(context.Background())
	if err != nil || len(list) != 2 || !list[1].Prunable || list[1].PruneReason == "" || list[1].Problem == "" {
		t.Fatalf("missing entry: %+v, %v", list, err)
	}
	review = worktreeReview(t, repo, path)
	if review.CanMerge || review.CanRemove {
		t.Fatalf("missing review: %+v", review)
	}
}

func TestWorktreeCreateNewExistingAndOccupiedBranches(t *testing.T) {
	repo, dir, _ := worktreeFixture(t)
	ctx := context.Background()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	start := git(t, dir, "rev-parse", "HEAD")
	created, err := repo.CreateWorktree(ctx, filepath.Join(parent, portableTestPath("new\n中文", "new 中文")), "new/topic", "main", false)
	if err != nil || created.Head != start || created.Branch != "refs/heads/new/topic" || created.Problem != "" {
		t.Fatalf("new branch: %+v, %v", created, err)
	}
	git(t, dir, "branch", "existing")
	created, err = repo.CreateWorktree(ctx, filepath.Join(parent, "existing"), "refs/heads/existing", "", true)
	if err != nil || created.Head != start || created.Branch != "refs/heads/existing" {
		t.Fatalf("existing branch: %+v, %v", created, err)
	}
	for _, branch := range []string{"main", "topic", "existing"} {
		if _, err := repo.CreateWorktree(ctx, filepath.Join(parent, branch+"-occupied"), branch, "", true); err == nil {
			t.Fatalf("accepted occupied branch %q", branch)
		}
	}
	if _, err := repo.CreateWorktree(ctx, filepath.Join(parent, "duplicate"), "existing", start, false); err == nil {
		t.Fatal("new branch creation overwrote an existing branch")
	}
	if got := git(t, dir, "rev-parse", "refs/heads/existing"); got != start {
		t.Fatal("failure changed user branch:", got)
	}
	if _, err := repo.CreateWorktree(ctx, filepath.Join(parent, "bad-start"), "bad-start", "--invalid", false); err == nil {
		t.Fatal("accepted invalid start")
	}
	if _, err := repo.CreateWorktree(ctx, filepath.Join(parent, "nonlocal"), "refs/remotes/origin/topic", "", true); err == nil {
		t.Fatal("accepted remote branch")
	}
}

func TestWorktreeCreateRejectsUnsafePathsWithoutTouchingData(t *testing.T) {
	repo, dir, linked := worktreeFixture(t)
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write(t, parent, "occupied/keep", "precious")
	write(t, parent, "file", "precious")

	for index, path := range []string{
		repo.Path, filepath.Join(repo.Path, "nested"), filepath.Join(linked, "nested"),
		filepath.Join(repo.Path, ".git", "nested"), filepath.Join(parent, "occupied"),
		filepath.Join(parent, "file"), filepath.Join(parent, "arbitrary", ".git", "nested"),
	} {
		branch := "unsafe-" + strings.Repeat("x", index+1)
		if _, err := repo.CreateWorktree(context.Background(), path, branch, "HEAD", false); err == nil {
			t.Errorf("accepted path %q", path)
		}
	}
	t.Run("symlink paths", func(t *testing.T) {
		testSymlink(t, linked, filepath.Join(parent, "link"))
		outside := t.TempDir()
		if err := os.Mkdir(filepath.Join(outside, "existing"), 0755); err != nil {
			t.Fatal(err)
		}
		testSymlink(t, outside, filepath.Join(parent, "outside-link"))
		for index, path := range []string{
			filepath.Join(parent, "link"), filepath.Join(parent, "link", "nested"),
			filepath.Join(parent, "outside-link", "nested"), filepath.Join(parent, "outside-link", "existing", "nested"),
		} {
			branch := fmt.Sprintf("unsafe-link-%d", index)
			if _, err := repo.CreateWorktree(context.Background(), path, branch, "HEAD", false); err == nil {
				t.Errorf("accepted symlink path %q", path)
			}
		}
	})
	if data, err := os.ReadFile(filepath.Join(parent, "occupied", "keep")); err != nil || string(data) != "precious" {
		t.Fatalf("existing data changed: %q %v", data, err)
	}
	if got := git(t, dir, "for-each-ref", "--format=%(refname)", "refs/heads/"); strings.Contains(got, "unsafe-") {
		t.Fatal("path rejection created branches:", got)
	}
}

func TestWorktreeReviewRejectsUnmergedAndMergesPinnedCommit(t *testing.T) {
	repo, dir, path := worktreeFixture(t)
	write(t, path, "topic.txt", "topic\n")
	git(t, path, "add", ".")
	git(t, path, "commit", "-m", "topic")
	source := git(t, path, "rev-parse", "HEAD")
	review := worktreeReview(t, repo, path)
	if !review.CanMerge || review.CanRemove || !strings.Contains(review.RemoveProblem, "未合并") || review.Worktree.Head != source {
		t.Fatalf("unmerged: %+v", review)
	}
	if err := repo.RemoveWorktree(context.Background(), path, review.Token); err == nil {
		t.Fatal("removed unmerged worktree")
	}
	if err := repo.MergeWorktree(context.Background(), path, review.Token); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != source {
		t.Fatalf("merged %s instead of %s", got, source)
	}
	review = worktreeReview(t, repo, path)
	if !review.CanRemove {
		t.Fatalf("merged worktree: %+v", review)
	}
	if err := repo.RemoveWorktree(context.Background(), path, review.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree was not removed: %v", err)
	}
	if got := git(t, dir, "rev-parse", "refs/heads/topic"); got != source {
		t.Fatal("removal deleted branch:", got)
	}
}

func TestWorktreeReviewProtectsDirtyIgnoredAndOperations(t *testing.T) {
	for _, state := range []string{"tracked", "untracked", "ignored", "merge", "assume-unchanged", "skip-worktree"} {
		t.Run(state, func(t *testing.T) {
			repo, _, path := worktreeFixture(t)
			switch state {
			case "tracked":
				write(t, path, "base.txt", "changed\n")
			case "untracked":
				write(t, path, portableTestPath("untracked\n中文", "untracked 中文"), "precious")
			case "ignored":
				write(t, path, "ignored/precious", "precious")
			case "merge":
				hash := git(t, path, "rev-parse", "HEAD")
				gitDir := git(t, path, "rev-parse", "--absolute-git-dir")
				write(t, gitDir, "MERGE_HEAD", hash+"\n")
			case "assume-unchanged":
				git(t, path, "update-index", "--assume-unchanged", "base.txt")
				write(t, path, "base.txt", "hidden precious\n")
			case "skip-worktree":
				git(t, path, "update-index", "--skip-worktree", "base.txt")
				write(t, path, "base.txt", "hidden precious\n")
			}
			review := worktreeReview(t, repo, path)
			if review.CanRemove || review.RemoveProblem == "" {
				t.Fatalf("unsafe remove review: %+v", review)
			}
			if state == "ignored" && (!review.Worktree.Ignored || !strings.Contains(review.RemoveProblem, "忽略")) {
				t.Fatalf("ignored data not explained: %+v", review)
			}
			if state != "ignored" && review.CanMerge {
				t.Fatalf("unsafe merge review: %+v", review)
			}
			if err := repo.RemoveWorktree(context.Background(), path, review.Token); err == nil {
				t.Fatal("removed protected worktree")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("rejected remove lost directory:", err)
			}
		})
	}
}

func TestWorktreeReviewProtectsMainCurrentAndTargetState(t *testing.T) {
	repo, dir, path := worktreeFixture(t)
	ctx := context.Background()
	review := worktreeReview(t, repo, repo.Path)
	if review.CanMerge || review.CanRemove || !strings.Contains(review.RemoveProblem, "主") {
		t.Fatalf("main: %+v", review)
	}
	linked, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	review = worktreeReview(t, linked, path)
	if review.CanMerge || review.CanRemove || !strings.Contains(review.RemoveProblem, "当前") {
		t.Fatalf("current: %+v", review)
	}
	write(t, dir, "untracked", "keep")
	review = worktreeReview(t, repo, path)
	if review.CanMerge || review.MergeProblem == "" {
		t.Fatalf("dirty target: %+v", review)
	}
	if err := os.Remove(filepath.Join(dir, "untracked")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "checkout", "--detach")
	review = worktreeReview(t, repo, path)
	if review.CanMerge || !strings.Contains(review.MergeProblem, "分离") {
		t.Fatalf("detached target: %+v", review)
	}
	git(t, dir, "checkout", "main")
	write(t, filepath.Join(dir, ".git"), "CHERRY_PICK_HEAD", git(t, dir, "rev-parse", "HEAD")+"\n")
	review = worktreeReview(t, repo, path)
	if review.CanMerge || review.MergeProblem == "" {
		t.Fatalf("target operation: %+v", review)
	}
}

func TestWorktreeRejectsStaleTokensForSourceAndTargetChanges(t *testing.T) {
	for _, change := range []string{"source-head", "target-head", "source-branch", "target-branch", "ignored", "locked", "untracked"} {
		t.Run(change, func(t *testing.T) {
			repo, dir, path := worktreeFixture(t)
			review := worktreeReview(t, repo, path)
			switch change {
			case "source-head":
				git(t, path, "commit", "--allow-empty", "-m", "source moved")
			case "target-head":
				git(t, dir, "commit", "--allow-empty", "-m", "target moved")
			case "source-branch":
				git(t, path, "switch", "-c", "other-source")
			case "target-branch":
				git(t, dir, "switch", "-c", "other-target")
			case "ignored":
				write(t, path, "ignored/keep", "precious")
			case "locked":
				git(t, dir, "worktree", "lock", path)
			case "untracked":
				write(t, path, "untracked", "precious")
			}
			if err := repo.MergeWorktree(context.Background(), path, review.Token); err == nil {
				t.Fatal("merge accepted stale token")
			}
			if err := repo.RemoveWorktree(context.Background(), path, review.Token); err == nil {
				t.Fatal("remove accepted stale token")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("stale remove lost directory:", err)
			}
		})
	}
}

func TestWorktreeMergePreservesRealConflictState(t *testing.T) {
	repo, dir, path := worktreeFixture(t)
	write(t, path, "base.txt", "theirs\n")
	git(t, path, "commit", "-am", "topic")
	write(t, dir, "base.txt", "ours\n")
	git(t, dir, "commit", "-am", "main")
	review := worktreeReview(t, repo, path)
	if !review.CanMerge {
		t.Fatalf("conflict preflight: %+v", review)
	}
	if err := repo.MergeWorktree(context.Background(), path, review.Token); err == nil {
		t.Fatal("expected merge conflict")
	}
	snapshot, err := repo.Snapshot(context.Background())
	if err != nil || snapshot.Operation.Kind != "merge" || len(snapshot.Files) != 1 || !snapshot.Files[0].Conflict {
		t.Fatalf("conflict state: %+v, %v", snapshot, err)
	}
	if err := repo.OperationAction(context.Background(), "merge", snapshot.Operation.Token, "abort"); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeRejectsUnrelatedOrReplacedDirectory(t *testing.T) {
	repo, dir, path := worktreeFixture(t)
	review := worktreeReview(t, repo, path)
	other, otherDir := fixture(t)
	git(t, otherDir, "commit", "--allow-empty", "-m", "other")
	if _, err := repo.ReviewWorktree(context.Background(), other.Path); err == nil {
		t.Fatal("review accepted unrelated repo")
	}
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, path, "init", "-b", "unrelated")
	write(t, path, "precious", "keep me")
	list, err := repo.Worktrees(context.Background())
	if err != nil || list[1].Problem == "" {
		t.Fatalf("replaced worktree: %+v, %v", list, err)
	}
	if err := repo.RemoveWorktree(context.Background(), path, review.Token); err == nil {
		t.Fatal("removed replacement directory")
	}
	if err := repo.MergeWorktree(context.Background(), path, review.Token); err == nil {
		t.Fatal("merged replacement directory")
	}
	if data, err := os.ReadFile(filepath.Join(path, "precious")); err != nil || string(data) != "keep me" {
		t.Fatalf("replacement data changed: %q, %v", data, err)
	}
	if got := git(t, dir, "rev-parse", "refs/heads/topic"); got != review.Worktree.Head {
		t.Fatal("rejection changed branch:", got)
	}
}

func TestWorktreeCancelledOperationsDoNotMutate(t *testing.T) {
	repo, dir, path := worktreeFixture(t)
	review := worktreeReview(t, repo, path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Worktrees(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel list: %v", err)
	}
	if _, err := repo.ReviewWorktree(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel review: %v", err)
	}
	if _, err := repo.CreateWorktree(ctx, filepath.Join(t.TempDir(), "cancelled"), "cancelled", "HEAD", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel create: %v", err)
	}
	if err := repo.MergeWorktree(ctx, path, review.Token); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel merge: %v", err)
	}
	if err := repo.RemoveWorktree(ctx, path, review.Token); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel remove: %v", err)
	}
	if got := git(t, dir, "branch", "--list", "cancelled"); got != "" {
		t.Fatal("cancelled create changed refs:", got)
	}
}

func TestWorktreeRepositoryLockWaitCanBeCancelled(t *testing.T) {
	repo, _, _ := worktreeFixture(t)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := repo.Worktrees(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock cancellation: %v", err)
	}
}

func TestWorktreeTokenRejectsReplacementEvenWithPreservedDirectoryTime(t *testing.T) {
	repo, _, path := worktreeFixture(t)
	review := worktreeReview(t, repo, path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".git", ".gitignore", "base.txt"} {
		data, err := os.ReadFile(filepath.Join(path+"-original", name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, path, name, string(data))
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveWorktree(context.Background(), path, review.Token); err == nil {
		t.Fatal("stale remove accepted a replacement with matching timestamps and Git metadata")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("replacement was deleted:", err)
	}
}

func TestWorktreesRejectSymlinkReplacement(t *testing.T) {
	repo, _, path := worktreeFixture(t)
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	testSymlink(t, path+"-original", path)
	list, err := repo.Worktrees(context.Background())
	if err != nil || len(list) != 2 || list[1].Problem == "" {
		t.Fatalf("symlink entry: %+v, %v", list, err)
	}
}

func TestWorktreeCommonLockWaitCanBeCancelled(t *testing.T) {
	repo, _, path := worktreeFixture(t)
	other, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	common, unlock, err := repo.lockWorktree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if common == "" {
		t.Fatal("empty common directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := other.Worktrees(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("common directory lock cancellation: %v", err)
	}
}

func TestWorktreeLinkedTargetCanMergeFromMain(t *testing.T) {
	_, dir, path := worktreeFixture(t)
	ctx := context.Background()
	write(t, dir, "from-main.txt", "main change\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "main change")
	mainHead := git(t, dir, "rev-parse", "HEAD")
	linked, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	review := worktreeReview(t, linked, dir)
	if !review.Worktree.Main || !review.CanMerge || review.CanRemove {
		t.Fatalf("main source: %+v", review)
	}
	if err := linked.MergeWorktree(ctx, dir, review.Token); err != nil {
		t.Fatal(err)
	}
	if got := git(t, path, "rev-parse", "HEAD"); got != mainHead {
		t.Fatalf("linked HEAD %s, want %s", got, mainHead)
	}
}

func TestWorktreesUnbornAndBareMain(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	list, err := repo.Worktrees(ctx)
	if err != nil || len(list) != 1 || list[0].Head != "" || !list[0].Current || list[0].Problem != "" {
		t.Fatalf("unborn: %+v, %v", list, err)
	}
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(parent, "bare.git")
	git(t, parent, "clone", "--bare", dir, bare)
	path := filepath.Join(parent, "linked")
	git(t, bare, "worktree", "add", path, "main")
	linked, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	list, err = linked.Worktrees(ctx)
	if err != nil || len(list) != 2 || !list[0].Bare || !list[0].Main || list[0].Problem != "" || !list[1].Current {
		t.Fatalf("bare main: %+v, %v", list, err)
	}
	review := worktreeReview(t, linked, bare)
	if review.CanMerge || review.CanRemove || review.MergeProblem == "" {
		t.Fatalf("bare main review: %+v", review)
	}
}

func TestWorktreeMergeDoesNotOverwriteIgnoredTargetData(t *testing.T) {
	repo, dir, path := worktreeFixture(t)
	write(t, path, "ignored/keep", "committed")
	git(t, path, "add", "--force", "ignored/keep")
	git(t, path, "commit", "-m", "track previously ignored file")
	write(t, dir, "ignored/keep", "precious")
	review := worktreeReview(t, repo, path)
	if !review.CanMerge {
		t.Fatalf("ignored target preflight: %+v", review)
	}
	if err := repo.MergeWorktree(context.Background(), path, review.Token); err == nil {
		t.Fatal("overwrote ignored target file")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "ignored", "keep")); err != nil || string(data) != "precious" {
		t.Fatalf("ignored target data changed: %q %v", data, err)
	}
}

func TestWorktreeCreateExistingBranchWithNamesakeTag(t *testing.T) {
	repo, dir, _ := worktreeFixture(t)
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "tag", "namesake", base)
	git(t, dir, "commit", "--allow-empty", "-m", "next")
	head := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "branch", "namesake", head)
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	created, err := repo.CreateWorktree(context.Background(), filepath.Join(parent, "namesake"), "namesake", "", true)
	if err != nil || created.Head != head || created.Branch != "refs/heads/namesake" {
		t.Fatalf("namesake branch: %+v, %v", created, err)
	}
}

func TestWorktreeInvalidTokensNeverMutate(t *testing.T) {
	repo, dir, path := worktreeFixture(t)
	for _, token := range []string{"", "invalid"} {
		if err := repo.MergeWorktree(context.Background(), path, token); err == nil {
			t.Fatal("merge accepted invalid token")
		}
		if err := repo.RemoveWorktree(context.Background(), path, token); err == nil {
			t.Fatal("remove accepted invalid token")
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("invalid token removed worktree:", err)
	}
	if got := git(t, dir, "status", "--porcelain=v1"); got != "" {
		t.Fatal("invalid token changed target:", got)
	}
}
