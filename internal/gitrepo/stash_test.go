// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func stashFixture(t *testing.T) (*Repository, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo, dir := fixture(t)
	write(t, dir, "tracked.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	return repo, dir
}

func stashHashes(t *testing.T, repo *Repository) []string {
	t.Helper()
	stashes, err := repo.Stashes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hashes := make([]string, len(stashes))
	for index, stash := range stashes {
		hashes[index] = stash.Hash
	}
	return hashes
}

func TestStashTrackedUntrackedAndDiff(t *testing.T) {
	for _, includeUntracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "tracked", true: "untracked"}[includeUntracked], func(t *testing.T) {
			repo, dir := stashFixture(t)
			ctx := context.Background()
			empty, err := repo.Stashes(ctx)
			if err != nil || empty == nil || len(empty) != 0 {
				t.Fatalf("empty stashes: %+v, %v", empty, err)
			}
			write(t, dir, "tracked.txt", "staged\n")
			git(t, dir, "add", "tracked.txt")
			write(t, dir, "tracked.txt", "staged\nunstaged\n")
			write(t, dir, "new file.txt", "untracked content\n")
			if err := repo.CreateStash(ctx, "临时保存", includeUntracked); err != nil {
				t.Fatal(err)
			}
			stashes, err := repo.Stashes(ctx)
			if err != nil || len(stashes) != 1 {
				t.Fatalf("stashes: %+v, %v", stashes, err)
			}
			stash := stashes[0]
			if stash.Hash != git(t, dir, "rev-parse", "refs/stash") || !strings.Contains(stash.Message, "临时保存") {
				t.Fatalf("stash metadata: %+v", stash)
			}
			if _, err := time.Parse(time.RFC3339, stash.Date); err != nil {
				t.Fatalf("invalid stash date %q: %v", stash.Date, err)
			}
			if got := git(t, dir, "diff", "HEAD"); got != "" {
				t.Fatalf("tracked changes remain: %s", got)
			}
			_, statErr := os.Stat(filepath.Join(dir, "new file.txt"))
			if includeUntracked && !os.IsNotExist(statErr) || !includeUntracked && statErr != nil {
				t.Fatalf("untracked file status: %v", statErr)
			}
			git(t, dir, "config", "diff.external", "this-command-must-not-run")
			git(t, dir, "config", "diff.blocked.textconv", "this-command-must-not-run")
			write(t, dir, ".git/info/attributes", "*.txt diff=blocked\n")
			diff, err := repo.StashDiff(ctx, stash.Hash)
			if err != nil || !strings.Contains(diff.Text, "+staged") || !strings.Contains(diff.Text, "+unstaged") {
				t.Fatalf("tracked diff: %+v, %v", diff, err)
			}
			if strings.Contains(diff.Text, "+untracked content") != includeUntracked {
				t.Fatalf("untracked diff missing or unexpectedly included: %s", diff.Text)
			}
			if err := repo.ApplyStash(ctx, stash.Hash, false); err != nil {
				t.Fatal(err)
			}
			if got := stashHashes(t, repo); !reflect.DeepEqual(got, []string{stash.Hash}) {
				t.Fatalf("apply removed stash: %v", got)
			}
			content, err := os.ReadFile(filepath.Join(dir, "tracked.txt"))
			if err != nil || string(content) != "staged\nunstaged\n" {
				t.Fatalf("applied tracked content: %q, %v", content, err)
			}
			content, err = os.ReadFile(filepath.Join(dir, "new file.txt"))
			if err != nil || string(content) != "untracked content\n" {
				t.Fatalf("applied untracked content: %q, %v", content, err)
			}
		})
	}
}

func TestStashHashSurvivesIndexDrift(t *testing.T) {
	for _, action := range []string{"apply", "pop", "drop"} {
		t.Run(action, func(t *testing.T) {
			repo, dir := stashFixture(t)
			ctx := context.Background()
			write(t, dir, "tracked.txt", "first\n")
			if err := repo.CreateStash(ctx, "first", false); err != nil {
				t.Fatal(err)
			}
			first := stashHashes(t, repo)[0]
			write(t, dir, "tracked.txt", "second\n")
			git(t, dir, "stash", "push", "-m", "second")
			second := git(t, dir, "rev-parse", "refs/stash")
			diff, err := repo.StashDiff(ctx, first)
			if err != nil || !strings.Contains(diff.Text, "+first") || strings.Contains(diff.Text, "+second") {
				t.Fatalf("wrong stash diff after index drift: %+v, %v", diff, err)
			}
			if action == "drop" {
				err = repo.DropStash(ctx, first)
			} else {
				err = repo.ApplyStash(ctx, first, action == "pop")
			}
			if err != nil {
				t.Fatal(err)
			}
			want := []string{second}
			if action == "apply" {
				want = append(want, first)
			}
			if got := stashHashes(t, repo); !reflect.DeepEqual(got, want) {
				t.Fatalf("wrong stash changed: got %v, want %v", got, want)
			}
			content, err := os.ReadFile(filepath.Join(dir, "tracked.txt"))
			wantContent := "first\n"
			if action == "drop" {
				wantContent = "base\n"
			}
			if err != nil || string(content) != wantContent {
				t.Fatalf("wrong applied content: %q, %v", content, err)
			}
		})
	}
}

func TestStashUntrackedBinaryDiffAndPop(t *testing.T) {
	repo, dir := stashFixture(t)
	ctx := context.Background()
	content := "binary\x00content\n"
	write(t, dir, "binary.dat", content)
	if err := repo.CreateStash(ctx, "", true); err != nil {
		t.Fatal(err)
	}
	hash := stashHashes(t, repo)[0]
	diff, err := repo.StashDiff(ctx, hash)
	if err != nil || !diff.Binary || !strings.Contains(diff.Text, "binary.dat") {
		t.Fatalf("third-parent binary diff: %+v, %v", diff, err)
	}
	if err := repo.ApplyStash(ctx, hash, true); err != nil {
		t.Fatal(err)
	}
	if got := stashHashes(t, repo); len(got) != 0 {
		t.Fatalf("successful pop retained stash: %v", got)
	}
	restored, err := os.ReadFile(filepath.Join(dir, "binary.dat"))
	if err != nil || string(restored) != content {
		t.Fatalf("untracked binary content: %q, %v", restored, err)
	}
}

func TestStashPopFailureRetainsEntry(t *testing.T) {
	for _, failure := range []string{"conflict", "dirty", "untracked-collision", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			repo, dir := stashFixture(t)
			ctx := context.Background()
			write(t, dir, "tracked.txt", "stashed\n")
			write(t, dir, "new.txt", "stash untracked\n")
			if err := repo.CreateStash(ctx, "keep on failure", true); err != nil {
				t.Fatal(err)
			}
			hash := stashHashes(t, repo)[0]
			switch failure {
			case "conflict", "dirty":
				write(t, dir, "tracked.txt", "divergent\n")
				if failure == "conflict" {
					git(t, dir, "add", ".")
					git(t, dir, "commit", "-m", "divergent")
				}
			case "untracked-collision":
				write(t, dir, "new.txt", "keep local\n")
			case "cancel":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			if err := repo.ApplyStash(ctx, hash, true); err == nil {
				t.Fatal("pop unexpectedly succeeded")
			}
			if got := stashHashes(t, repo); !reflect.DeepEqual(got, []string{hash}) {
				t.Fatalf("failed pop removed stash: %v", got)
			}
			if failure == "conflict" && git(t, dir, "ls-files", "--unmerged") == "" {
				t.Fatal("expected real apply conflict")
			}
		})
	}
}

func TestStashApplyRejectsOperations(t *testing.T) {
	for _, operation := range []string{"resolved-merge", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD", "sequencer"} {
		for _, pop := range []bool{false, true} {
			action := "apply"
			if pop {
				action = "pop"
			}
			t.Run(operation+"/"+action, func(t *testing.T) {
				repo, dir := stashFixture(t)
				ctx := context.Background()
				write(t, dir, "stash-only.txt", "base\n")
				git(t, dir, "add", ".")
				git(t, dir, "commit", "-m", "stash target")
				write(t, dir, "stash-only.txt", "stored\n")
				if err := repo.CreateStash(ctx, "saved", false); err != nil {
					t.Fatal(err)
				}
				hash := stashHashes(t, repo)[0]
				switch operation {
				case "resolved-merge":
					git(t, dir, "checkout", "-b", "other")
					write(t, dir, "tracked.txt", "other\n")
					git(t, dir, "add", ".")
					git(t, dir, "commit", "-m", "other")
					git(t, dir, "checkout", "main")
					write(t, dir, "tracked.txt", "main\n")
					git(t, dir, "add", ".")
					git(t, dir, "commit", "-m", "main")
					if _, err := repo.git(ctx, "merge", "other"); err == nil {
						t.Fatal("expected merge conflict")
					}
					write(t, dir, "tracked.txt", "resolved\n")
					git(t, dir, "add", "tracked.txt")
					if git(t, dir, "ls-files", "--unmerged") != "" {
						t.Fatal("merge fixture still has unresolved files")
					}
				case "rebase-merge", "rebase-apply", "sequencer":
					if err := os.Mkdir(filepath.Join(dir, ".git", operation), 0755); err != nil {
						t.Fatal(err)
					}
				default:
					write(t, dir, ".git/"+operation, git(t, dir, "rev-parse", "HEAD")+"\n")
				}
				before := git(t, dir, "status", "--porcelain=v1")
				if err := repo.ApplyStash(ctx, hash, pop); err == nil {
					t.Fatal("apply accepted an unfinished Git operation")
				}
				if after := git(t, dir, "status", "--porcelain=v1"); after != before {
					t.Fatalf("rejected apply changed status: before %q, after %q", before, after)
				}
				content, err := os.ReadFile(filepath.Join(dir, "stash-only.txt"))
				if err != nil || string(content) != "base\n" {
					t.Fatalf("rejected apply changed content: %q, %v", content, err)
				}
				if got := stashHashes(t, repo); !reflect.DeepEqual(got, []string{hash}) {
					t.Fatalf("rejected apply removed stash: %v", got)
				}
			})
		}
	}
}

func TestStashRejectsInvalidAndMissingHashes(t *testing.T) {
	repo, dir := stashFixture(t)
	ctx := context.Background()
	write(t, dir, "tracked.txt", "saved\n")
	if err := repo.CreateStash(ctx, "saved", false); err != nil {
		t.Fatal(err)
	}
	hash := stashHashes(t, repo)[0]
	invalid := []string{"", "HEAD", "refs/stash", "stash@{0}", "0", "-1", "--all", hash[:12], hash + "^1", strings.Repeat("z", 40), strings.Repeat("0", 40), git(t, dir, "rev-parse", "HEAD")}
	for _, value := range invalid {
		t.Run(value, func(t *testing.T) {
			if _, err := repo.StashDiff(ctx, value); err == nil {
				t.Fatal("diff accepted invalid hash")
			}
			if err := repo.ApplyStash(ctx, value, false); err == nil {
				t.Fatal("apply accepted invalid hash")
			}
			if err := repo.ApplyStash(ctx, value, true); err == nil {
				t.Fatal("pop accepted invalid hash")
			}
			if err := repo.DropStash(ctx, value); err == nil {
				t.Fatal("drop accepted invalid hash")
			}
		})
	}
	if got := stashHashes(t, repo); !reflect.DeepEqual(got, []string{hash}) {
		t.Fatalf("invalid operation changed stashes: %v", got)
	}
	if err := repo.DropStash(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if got := stashHashes(t, repo); len(got) != 0 {
		t.Fatalf("last stash remains: %v", got)
	}
	if _, err := repo.StashDiff(ctx, hash); err == nil {
		t.Fatal("diff accepted a removed stash object")
	}
	if err := repo.ApplyStash(ctx, hash, true); err == nil {
		t.Fatal("pop accepted a removed stash object")
	}
	if err := repo.DropStash(ctx, hash); err == nil {
		t.Fatal("drop accepted an already removed stash")
	}
}

func TestStashRejectsDuplicateHashes(t *testing.T) {
	repo, dir := stashFixture(t)
	ctx := context.Background()
	write(t, dir, "tracked.txt", "saved\n")
	if err := repo.CreateStash(ctx, "saved", false); err != nil {
		t.Fatal(err)
	}
	hash := stashHashes(t, repo)[0]
	git(t, dir, "reflog", "write", "refs/stash", hash, hash, "duplicate")
	if got := stashHashes(t, repo); !reflect.DeepEqual(got, []string{hash, hash}) {
		t.Fatalf("fixture did not create duplicate entries: %v", got)
	}
	before := git(t, dir, "reflog", "show", "--format=%H %gs", "refs/stash")
	if err := repo.DropStash(ctx, hash); err == nil {
		t.Fatal("drop accepted ambiguous duplicate hash")
	}
	if err := repo.ApplyStash(ctx, hash, true); err == nil {
		t.Fatal("pop accepted ambiguous duplicate hash")
	}
	if after := git(t, dir, "reflog", "show", "--format=%H %gs", "refs/stash"); before != after {
		t.Fatalf("duplicate entry removed: %s", after)
	}
}

func TestStashCreateRejectsNoChangesAndOperations(t *testing.T) {
	for _, operation := range []string{"clean", "untracked-only", "MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD", "sequencer", "conflict"} {
		t.Run(operation, func(t *testing.T) {
			repo, dir := stashFixture(t)
			ctx := context.Background()
			if operation != "clean" && operation != "untracked-only" {
				write(t, dir, "tracked.txt", "changed\n")
			}
			switch operation {
			case "clean":
			case "untracked-only":
				write(t, dir, "new.txt", "untracked\n")
			case "rebase-merge", "rebase-apply", "sequencer":
				if err := os.Mkdir(filepath.Join(dir, ".git", operation), 0755); err != nil {
					t.Fatal(err)
				}
			case "conflict":
				git(t, dir, "checkout", "-b", "other")
				git(t, dir, "add", ".")
				git(t, dir, "commit", "-m", "other")
				git(t, dir, "checkout", "main")
				write(t, dir, "tracked.txt", "main\n")
				git(t, dir, "add", ".")
				git(t, dir, "commit", "-m", "main")
				if _, err := repo.git(ctx, "merge", "other"); err == nil {
					t.Fatal("expected merge conflict")
				}
				if err := os.Remove(filepath.Join(dir, ".git", "MERGE_HEAD")); err != nil {
					t.Fatal(err)
				}
			default:
				write(t, dir, ".git/"+operation, git(t, dir, "rev-parse", "HEAD")+"\n")
			}
			before := git(t, dir, "status", "--porcelain=v1")
			if err := repo.CreateStash(ctx, "must fail", false); err == nil {
				t.Fatal("create unexpectedly succeeded")
			}
			if after := git(t, dir, "status", "--porcelain=v1"); before != after {
				t.Fatalf("rejected create changed status: before %q, after %q", before, after)
			}
			if got := stashHashes(t, repo); len(got) != 0 {
				t.Fatalf("rejected create added stash: %v", got)
			}
			if operation == "untracked-only" {
				if err := repo.CreateStash(ctx, "", true); err != nil {
					t.Fatal(err)
				}
				if got := stashHashes(t, repo); len(got) != 1 {
					t.Fatalf("untracked-only stash missing: %v", got)
				}
			}
		})
	}
}
