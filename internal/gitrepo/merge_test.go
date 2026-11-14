// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mergeFixture(t *testing.T, diverged bool) (*Repository, string, string, string) {
	t.Helper()
	repo, dir := fixture(t)
	write(t, dir, "base", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "checkout", "-b", "topic")
	write(t, dir, "feature", "first\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feature one")
	write(t, dir, "feature", "second\n")
	git(t, dir, "commit", "-am", "feature two")
	source := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "main")
	if diverged {
		write(t, dir, "main", "main\n")
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", "main")
	}
	return repo, dir, git(t, dir, "rev-parse", "HEAD"), source
}

func TestMergeModes(t *testing.T) {
	for _, test := range []struct {
		name      string
		mode      MergeMode
		diverged  bool
		commit    bool
		wantFF    bool
		wantError bool
		parents   int
		pending   string
		staged    bool
	}{
		{name: "auto fast forward", mode: MergeAuto, commit: true, wantFF: true},
		{name: "auto fast forward without commit", mode: MergeAuto, wantFF: true},
		{name: "auto diverged", mode: MergeAuto, diverged: true, commit: true, parents: 2},
		{name: "auto pending", mode: MergeAuto, diverged: true, pending: "merge", staged: true},
		{name: "always merge commit", mode: MergeNoFF, commit: true, parents: 2},
		{name: "always pending", mode: MergeNoFF, pending: "merge", staged: true},
		{name: "only fast forward", mode: MergeFFOnly, commit: true, wantFF: true},
		{name: "reject diverged fast forward", mode: MergeFFOnly, diverged: true, wantError: true},
		{name: "squash staged", mode: MergeSquash, staged: true},
		{name: "squash diverged staged", mode: MergeSquash, diverged: true, staged: true},
		{name: "squash commit", mode: MergeSquash, commit: true, parents: 1},
		{name: "squash diverged commit", mode: MergeSquash, diverged: true, commit: true, parents: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, dir, before, source := mergeFixture(t, test.diverged)
			err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: test.mode, Commit: test.commit})
			if (err != nil) != test.wantError {
				t.Fatalf("merge error = %v", err)
			}
			head := git(t, dir, "rev-parse", "HEAD")
			switch {
			case test.wantFF:
				if head != source {
					t.Fatal("fast forward did not move to source")
				}
			case test.parents > 0:
				parents := strings.Fields(git(t, dir, "show", "-s", "--format=%P", "HEAD"))
				if len(parents) != test.parents || parents[0] != before || head == source {
					t.Fatalf("unexpected parents: %v, original = %s", parents, before)
				}
			default:
				if head != before {
					t.Fatal("HEAD changed while merge should be pending or rejected")
				}
			}
			state, err := repo.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if state.Operation.Kind != test.pending {
				t.Fatalf("operation = %+v, want %q", state.Operation, test.pending)
			}
			staged := git(t, dir, "diff", "--cached", "--name-only")
			if (staged != "") != test.staged {
				t.Fatalf("staged files = %q", staged)
			}
			if !test.wantError {
				data, err := os.ReadFile(filepath.Join(dir, "feature"))
				if err != nil || string(data) != "second\n" {
					t.Fatalf("merged content = %q, %v", data, err)
				}
			}
			if git(t, dir, "rev-parse", "topic") != source {
				t.Fatal("merge changed source branch")
			}
		})
	}
}

func TestMergeModeOverridesRepositoryDefaults(t *testing.T) {
	for _, mode := range []MergeMode{MergeAuto, MergeNoFF, MergeFFOnly, MergeSquash} {
		t.Run(string(mode), func(t *testing.T) {
			repo, dir, _, source := mergeFixture(t, false)
			git(t, dir, "config", "merge.ff", "false")
			git(t, dir, "config", "branch.main.mergeOptions", "--squash --no-commit")
			if err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: mode, Commit: true}); err != nil {
				t.Fatal(err)
			}
			if mode == MergeAuto || mode == MergeFFOnly {
				if git(t, dir, "rev-parse", "HEAD") != source {
					t.Fatal("repository default overrode selected mode")
				}
			} else {
				want := 2
				if mode == MergeSquash {
					want = 1
				}
				if got := len(strings.Fields(git(t, dir, "show", "-s", "--format=%P", "HEAD"))); got != want {
					t.Fatalf("parent count = %d, want %d", got, want)
				}
			}
		})
	}
}

func TestSquashAlreadyMergedDoesNotCreateCommit(t *testing.T) {
	repo, dir, _, _ := mergeFixture(t, false)
	git(t, dir, "merge", "--ff-only", "topic")
	before := git(t, dir, "rev-parse", "HEAD")
	if err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: MergeSquash, Commit: true}); err != nil {
		t.Fatal(err)
	}
	if git(t, dir, "rev-parse", "HEAD") != before {
		t.Fatal("up-to-date squash created a commit")
	}
}

func TestSquashCommitFailurePreservesStagedChanges(t *testing.T) {
	repo, dir, before, _ := mergeFixture(t, false)
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: MergeSquash, Commit: true})
	if err == nil || git(t, dir, "rev-parse", "HEAD") != before || git(t, dir, "diff", "--cached", "--name-only") != "feature" {
		t.Fatalf("failed commit lost staged result: %v", err)
	}
}

func TestMergeOptionsRejectInvalidAndDirtyState(t *testing.T) {
	repo, dir, before, _ := mergeFixture(t, false)
	if err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: "invalid"}); err == nil {
		t.Fatal("invalid mode accepted")
	}
	write(t, dir, "base", "unsaved\n")
	for _, mode := range []MergeMode{MergeAuto, MergeNoFF, MergeFFOnly, MergeSquash} {
		if err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: mode, Commit: true}); err == nil {
			t.Fatalf("%s accepted a dirty worktree", mode)
		}
	}
	if git(t, dir, "rev-parse", "HEAD") != before {
		t.Fatal("rejected merge changed HEAD")
	}
	data, err := os.ReadFile(filepath.Join(dir, "base"))
	if err != nil || string(data) != "unsaved\n" {
		t.Fatalf("worktree content lost: %q, %v", data, err)
	}
}

func TestSquashConflictPreservesBothVersions(t *testing.T) {
	repo, dir, _, _ := mergeFixture(t, false)
	write(t, dir, "feature", "main version\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "conflicting addition")
	before := git(t, dir, "rev-parse", "HEAD")
	err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: MergeSquash, Commit: true})
	if err == nil || git(t, dir, "rev-parse", "HEAD") != before {
		t.Fatalf("conflicted squash created a commit: %v", err)
	}
	if git(t, dir, "show", ":2:feature") != "main version" || git(t, dir, "show", ":3:feature") != "second" {
		t.Fatal("conflict lost one of the original versions")
	}
	state, err := repo.Snapshot(context.Background())
	if err != nil || len(state.Files) != 1 || !state.Files[0].Conflict || state.Operation.Kind != "" {
		t.Fatalf("squash conflict state = %+v, %v", state, err)
	}
}

func TestMergeDoesNotOverwriteIgnoredFiles(t *testing.T) {
	repo, dir, before, _ := mergeFixture(t, false)
	write(t, dir, ".git/info/exclude", "feature\n")
	write(t, dir, "feature", "ignored local data\n")
	for _, mode := range []MergeMode{MergeAuto, MergeNoFF, MergeFFOnly, MergeSquash} {
		err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: mode, Commit: true})
		data, readErr := os.ReadFile(filepath.Join(dir, "feature"))
		if err == nil || readErr != nil || string(data) != "ignored local data\n" || git(t, dir, "rev-parse", "HEAD") != before {
			t.Fatalf("%s overwrote ignored content: %q, %v, %v", mode, data, err, readErr)
		}
	}
}

func TestFastForwardOnlyIgnoresInheritedMergeStrategy(t *testing.T) {
	for _, diverged := range []bool{false, true} {
		t.Run(fmt.Sprint(diverged), func(t *testing.T) {
			repo, dir, before, source := mergeFixture(t, diverged)
			git(t, dir, "tag", "main")
			git(t, dir, "config", "branch.main.mergeOptions", "-s ours")
			err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: MergeFFOnly, Commit: true})
			want := source
			if diverged {
				want = before
			}
			if (err != nil) != diverged || git(t, dir, "rev-parse", "HEAD") != want {
				t.Fatalf("inherited strategy changed fast-forward semantics: %v", err)
			}
			state, stateErr := repo.Snapshot(context.Background())
			if stateErr != nil || state.Operation.Kind != "" || len(state.Files) != 0 {
				t.Fatalf("fast-forward left merge state: %+v, %v", state, stateErr)
			}
		})
	}
}

func TestFastForwardOnlyAlreadyIntegrated(t *testing.T) {
	repo, dir, _, _ := mergeFixture(t, false)
	git(t, dir, "merge", "--ff-only", "topic")
	write(t, dir, "main", "main\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "after merge")
	before := git(t, dir, "rev-parse", "HEAD")
	err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: MergeFFOnly})
	if err != nil || git(t, dir, "rev-parse", "HEAD") != before {
		t.Fatalf("already-integrated branch should be a no-op: %v", err)
	}
}

func TestMergePreservesIgnoredDirectoryRenameTarget(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "old/base", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "checkout", "-b", "topic")
	write(t, dir, "old/local.txt", "incoming\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "incoming file")
	git(t, dir, "checkout", "main")
	git(t, dir, "mv", "old", "new")
	git(t, dir, "commit", "-m", "move directory")
	git(t, dir, "config", "merge.directoryRenames", "true")
	write(t, dir, ".git/info/exclude", "new/local.txt\n")
	write(t, dir, "new/local.txt", "local data\n")
	before := git(t, dir, "rev-parse", "HEAD")
	err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: MergeNoFF, Commit: true})
	data, readErr := os.ReadFile(filepath.Join(dir, "new/local.txt"))
	if err == nil || readErr != nil || string(data) != "local data\n" || git(t, dir, "rev-parse", "HEAD") != before {
		t.Fatalf("directory rename overwrote ignored data: %q, %v, %v", data, err, readErr)
	}
}

func TestMergePreservesIgnoredCaseAlias(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "base", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "checkout", "-b", "topic")
	write(t, dir, "cache/secret.txt", "incoming\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "incoming file")
	git(t, dir, "checkout", "main")
	write(t, dir, ".git/info/exclude", "Cache/\n")
	write(t, dir, "Cache/secret.txt", "local data\n")
	alias, err := os.Stat(filepath.Join(dir, "cache/secret.txt"))
	if os.IsNotExist(err) {
		t.Skip("filesystem is case-sensitive")
	}
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(filepath.Join(dir, "Cache/secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(alias, original) {
		t.Skip("case variants identify different files")
	}
	before := git(t, dir, "rev-parse", "HEAD")
	err = repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: MergeNoFF, Commit: true})
	data, readErr := os.ReadFile(filepath.Join(dir, "Cache/secret.txt"))
	if err == nil || readErr != nil || string(data) != "local data\n" || git(t, dir, "rev-parse", "HEAD") != before {
		t.Fatalf("case alias overwrote ignored data: %q, %v, %v", data, err, readErr)
	}
}

func TestMergeAllowsUnrelatedIgnoredFiles(t *testing.T) {
	for _, mode := range []MergeMode{MergeAuto, MergeNoFF, MergeFFOnly, MergeSquash} {
		t.Run(string(mode), func(t *testing.T) {
			repo, dir, _, _ := mergeFixture(t, false)
			write(t, dir, ".git/info/exclude", "build/\n")
			write(t, dir, "build/local", "local\n")
			err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: mode, Commit: true})
			data, readErr := os.ReadFile(filepath.Join(dir, "build/local"))
			if err != nil || readErr != nil || string(data) != "local\n" {
				t.Fatalf("unrelated ignored content blocked merge: %q, %v, %v", data, err, readErr)
			}
		})
	}
}

func TestMergePreflightKeepsConflictWorkflow(t *testing.T) {
	repo, dir, _, _ := mergeFixture(t, false)
	write(t, dir, "feature", "main\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "conflicting addition")
	write(t, dir, ".git/info/exclude", "build/\n")
	write(t, dir, "build/local", "local\n")
	err := repo.MergeWithOptions(context.Background(), "topic", MergeOptions{Mode: MergeAuto, Commit: true})
	state, stateErr := repo.Snapshot(context.Background())
	if err == nil || stateErr != nil || state.Operation.Kind != "merge" || len(state.Files) != 1 || !state.Files[0].Conflict {
		t.Fatalf("preflight prevented normal conflict handling: %+v, %v, %v", state, err, stateErr)
	}
}
