// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRefsTagsRoundTripAndNamesakeBranch(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	tags, err := repo.Tags(ctx)
	if err != nil || tags == nil || len(tags) != 0 {
		t.Fatalf("empty tags: %+v %v", tags, err)
	}
	if err := repo.CreateTag(ctx, "unborn", ""); err == nil {
		t.Fatal("created tag without a commit")
	}
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "branch", "release/v1")
	git(t, dir, "commit", "--allow-empty", "-m", "next")
	head := git(t, dir, "rev-parse", "HEAD")
	// 仓库要求签名时，API 仍创建无需签名密钥的轻量标签。
	git(t, dir, "config", "tag.gpgSign", "true")
	if err := repo.CreateTag(ctx, "release/v1", base); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateTag(ctx, "最新版", ""); err != nil {
		t.Fatal(err)
	}
	tags, err = repo.Tags(ctx)
	want := []Tag{{Name: "release/v1", Hash: base}, {Name: "最新版", Hash: head}}
	if err != nil || !reflect.DeepEqual(tags, want) {
		t.Fatalf("tags: %+v, want %+v, error %v", tags, want, err)
	}
	if kind := git(t, dir, "cat-file", "-t", "refs/tags/release/v1"); kind != "commit" {
		t.Fatalf("created an annotated tag: %s", kind)
	}
	if err := repo.CreateTag(ctx, "release/v1", head); err == nil {
		t.Fatal("overwrote existing tag")
	}
	if got := git(t, dir, "rev-parse", "refs/tags/release/v1"); got != base {
		t.Fatal("duplicate creation changed tag:", got)
	}
	if err := repo.DeleteTag(ctx, "release/v1"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "rev-parse", "refs/heads/release/v1"); got != base {
		t.Fatal("tag deletion changed namesake branch:", got)
	}
	if err := repo.DeleteTag(ctx, "release/v1"); err == nil {
		t.Fatal("deleted missing tag")
	}
	if err := repo.DeleteTag(ctx, "最新版"); err != nil {
		t.Fatal(err)
	}
	tags, err = repo.Tags(ctx)
	if err != nil || tags == nil || len(tags) != 0 {
		t.Fatalf("remaining tags: %+v %v", tags, err)
	}
}

func TestRefsTagsReadDoesNotChangeRepository(t *testing.T) {
	repo, dir := fixture(t)
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	git(t, dir, "tag", "-a", "--no-sign", "-m", "annotated", "annotated")
	tagHash := git(t, dir, "rev-parse", "refs/tags/annotated")
	head := git(t, dir, "rev-parse", "HEAD")
	write(t, dir, "working.txt", "keep me\n")
	before := git(t, dir, "status", "--porcelain=v1")
	index, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	tags, err := repo.Tags(context.Background())
	if err != nil || !reflect.DeepEqual(tags, []Tag{{Name: "annotated", Hash: tagHash}}) {
		t.Fatalf("annotated tags: %+v %v", tags, err)
	}
	if got := git(t, dir, "status", "--porcelain=v1"); got != before {
		t.Fatalf("tag listing changed worktree: %q", got)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatal("tag listing changed HEAD:", got)
	}
	afterIndex, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if string(afterIndex) != string(index) {
		t.Fatal("tag listing changed index")
	}
}

func TestRefsRejectInvalidNamesAndTagTargets(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	hash := git(t, dir, "rev-parse", "HEAD")
	before := git(t, dir, "show-ref")
	for _, name := range []string{"", "--force", "-d", "../escape", "a..b", "bad name", "bad\nname", "bad\x00name", "name.lock", "a@{0}", "a:b", "a//b", "a/", "/a"} {
		t.Run(name, func(t *testing.T) {
			if err := repo.CreateTag(ctx, name, hash); err == nil {
				t.Fatal("accepted invalid tag name")
			}
			if err := repo.DeleteTag(ctx, name); err == nil {
				t.Fatal("accepted invalid tag deletion")
			}
			if err := repo.RenameBranch(ctx, "main", name); err == nil {
				t.Fatal("accepted invalid new branch name")
			}
			if err := repo.RenameBranch(ctx, name, "renamed"); err == nil {
				t.Fatal("accepted invalid old branch name")
			}
			if err := repo.DeleteBranch(ctx, name, true); err == nil {
				t.Fatal("accepted invalid branch deletion")
			}
		})
	}
	for _, target := range []string{"HEAD", "main", "--force", hash[:12], hash + "^", hash + "\x00", strings.Repeat("z", 40), strings.Repeat("0", 40), strings.Repeat("0", 64)} {
		if err := repo.CreateTag(ctx, "invalid-target", target); err == nil {
			t.Errorf("accepted tag target %q", target)
		}
	}
	if got := git(t, dir, "show-ref"); got != before {
		t.Fatalf("rejected operations changed refs: %s", got)
	}
}

func TestRefsCreateTagRejectsNonCommitObjects(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file.txt", "blob content\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "tag", "--no-sign", "-a", "-m", "annotated", "annotated")
	for _, object := range []struct {
		name     string
		revision string
	}{
		{"blob", "HEAD:file.txt"},
		{"tree", "HEAD^{tree}"},
		{"tag", "refs/tags/annotated"},
	} {
		t.Run(object.name, func(t *testing.T) {
			hash := git(t, dir, "rev-parse", object.revision)
			git(t, dir, "update-ref", "refs/tags/noncommit", hash)
			before := git(t, dir, "show-ref")
			if err := repo.CreateTag(ctx, "rejected-object", hash); err == nil {
				t.Fatal("accepted non-commit target:", object.name)
			}
			git(t, dir, "symbolic-ref", "HEAD", "refs/tags/noncommit")
			defer git(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
			if err := repo.CreateTag(ctx, "rejected-head", ""); err == nil {
				t.Fatal("accepted non-commit HEAD:", object.name)
			}
			if got := git(t, dir, "show-ref"); got != before {
				t.Fatalf("rejected tag creation changed refs: %s", got)
			}
		})
	}
}

func TestRefsRenameLocalBranches(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	hash := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "branch", "topic")
	git(t, dir, "tag", "topic")
	git(t, dir, "update-ref", "refs/remotes/origin/topic", hash)
	write(t, dir, "dirty.txt", "preserve worktree\n")
	if err := repo.RenameBranch(ctx, "topic", "feature/renamed"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "rev-parse", "refs/heads/feature/renamed"); got != hash {
		t.Fatal("rename changed target:", got)
	}
	if got := git(t, dir, "branch", "--list", "topic"); got != "" {
		t.Fatal("rename retained old branch:", got)
	}
	if got := git(t, dir, "rev-parse", "refs/tags/topic"); got != hash {
		t.Fatal("rename changed namesake tag:", got)
	}
	if err := repo.RenameBranch(ctx, "main", "trunk"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "symbolic-ref", "HEAD"); got != "refs/heads/trunk" {
		t.Fatal("current branch rename did not update HEAD:", got)
	}
	if err := repo.RenameBranch(ctx, "feature/renamed", "trunk"); err == nil {
		t.Fatal("rename overwrote existing branch")
	}
	if err := repo.RenameBranch(ctx, "origin/topic", "remote-renamed"); err == nil {
		t.Fatal("renamed a remote tracking branch")
	}
	if got := git(t, dir, "rev-parse", "refs/remotes/origin/topic"); got != hash {
		t.Fatal("rename changed remote tracking ref:", got)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "dirty.txt")); err != nil || string(content) != "preserve worktree\n" {
		t.Fatalf("rename changed working file: %q %v", content, err)
	}
}

func TestRefsDeleteBranchProtectsCurrentAndUnmerged(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")
	for _, force := range []bool{false, true} {
		if err := repo.DeleteBranch(ctx, "main", force); err == nil {
			t.Fatalf("deleted current branch with force=%v", force)
		}
	}
	git(t, dir, "switch", "-c", "topic")
	git(t, dir, "commit", "--allow-empty", "-m", "unmerged")
	topic := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "tag", "topic")
	git(t, dir, "switch", "main")
	if err := repo.DeleteBranch(ctx, "topic", false); err == nil {
		t.Fatal("deleted unmerged branch without force")
	}
	if got := git(t, dir, "rev-parse", "refs/heads/topic"); got != topic {
		t.Fatal("failed deletion changed branch:", got)
	}
	if err := repo.DeleteBranch(ctx, "topic", true); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "branch", "--list", "topic"); got != "" {
		t.Fatal("force deletion retained branch:", got)
	}
	if got := git(t, dir, "rev-parse", "refs/tags/topic"); got != topic {
		t.Fatal("branch deletion changed namesake tag:", got)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != base {
		t.Fatal("branch deletion changed HEAD:", got)
	}
	git(t, dir, "branch", "merged")
	if err := repo.DeleteBranch(ctx, "merged", false); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "branch", "--list", "merged"); got != "" {
		t.Fatal("merged deletion retained branch:", got)
	}
	if err := repo.DeleteBranch(ctx, "merged", true); err == nil {
		t.Fatal("deleted missing branch")
	}
	git(t, dir, "update-ref", "refs/remotes/origin/topic", topic)
	if err := repo.DeleteBranch(ctx, "origin/topic", true); err == nil {
		t.Fatal("deleted remote tracking branch")
	}
	if got := git(t, dir, "rev-parse", "refs/remotes/origin/topic"); got != topic {
		t.Fatal("deletion changed remote tracking ref:", got)
	}
}

func TestRefsDeleteBranchRejectsWorktreeOccupancy(t *testing.T) {
	repo, dir := fixture(t)
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	worktree := filepath.Join(t.TempDir(), "linked")
	git(t, dir, "worktree", "add", "-b", "occupied", worktree)
	for _, force := range []bool{false, true} {
		if err := repo.DeleteBranch(context.Background(), "occupied", force); err == nil {
			t.Fatalf("deleted occupied branch with force=%v", force)
		}
	}
	if got := git(t, worktree, "symbolic-ref", "HEAD"); got != "refs/heads/occupied" {
		t.Fatal("changed occupied worktree branch:", got)
	}
}

func TestRefsCancelledOperationsDoNotChangeRefs(t *testing.T) {
	repo, dir := fixture(t)
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	git(t, dir, "branch", "topic")
	git(t, dir, "tag", "v1")
	before := git(t, dir, "show-ref")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Tags(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("tag listing cancellation: %v", err)
	}
	for name, operation := range map[string]func() error{
		"create tag":    func() error { return repo.CreateTag(ctx, "v2", "") },
		"delete tag":    func() error { return repo.DeleteTag(ctx, "v1") },
		"rename branch": func() error { return repo.RenameBranch(ctx, "topic", "renamed") },
		"delete branch": func() error { return repo.DeleteBranch(ctx, "topic", true) },
	} {
		if err := operation(); !errors.Is(err, context.Canceled) {
			t.Errorf("%s cancellation: %v", name, err)
		}
	}
	if got := git(t, dir, "show-ref"); got != before {
		t.Fatal("cancelled operations changed refs:", got)
	}
}

func TestRefsCancellationWhileRepositoryBusy(t *testing.T) {
	repo, dir := fixture(t)
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	git(t, dir, "branch", "topic")
	git(t, dir, "tag", "v1")
	before := git(t, dir, "show-ref")
	for _, operation := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"tags", func(ctx context.Context) error {
			_, err := repo.Tags(ctx)
			return err
		}},
		{"create tag", func(ctx context.Context) error {
			return repo.CreateTag(ctx, "v2", "")
		}},
		{"delete tag", func(ctx context.Context) error {
			return repo.DeleteTag(ctx, "v1")
		}},
		{"rename branch", func(ctx context.Context) error {
			return repo.RenameBranch(ctx, "topic", "renamed")
		}},
		{"delete branch", func(ctx context.Context) error {
			return repo.DeleteBranch(ctx, "topic", true)
		}},
	} {
		for _, mode := range []string{"cancel", "deadline"} {
			t.Run(operation.name+"/"+mode, func(t *testing.T) {
				var ctx context.Context
				var cancel context.CancelFunc
				want := context.Canceled
				if mode == "deadline" {
					ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
					want = context.DeadlineExceeded
				} else {
					ctx, cancel = context.WithCancel(context.Background())
					timer := time.AfterFunc(20*time.Millisecond, cancel)
					defer timer.Stop()
				}
				defer cancel()
				repo.mu.Lock()
				result := make(chan error, 1)
				go func() { result <- operation.run(ctx) }()
				select {
				case err := <-result:
					repo.mu.Unlock()
					if !errors.Is(err, want) {
						t.Fatalf("waiting for repository lock: %v, want %v", err, want)
					}
				case <-time.After(time.Second):
					repo.mu.Unlock()
					cancel()
					<-result
					t.Fatal("operation ignored context while waiting for repository lock")
				}
				if !repo.mu.TryLock() {
					t.Fatal("operation retained repository lock")
				}
				repo.mu.Unlock()
				if got := git(t, dir, "show-ref"); got != before {
					t.Fatal("cancelled operation changed refs:", got)
				}
			})
		}
	}
}
