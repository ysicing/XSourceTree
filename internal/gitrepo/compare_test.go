// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCompareSnapshotsAndMergeBase(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file.txt", "common\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "common")
	common := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-b", "feature")
	write(t, dir, "feature.txt", "feature\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "feature")
	feature := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "tag", "-a", "review", "-m", "review")
	git(t, dir, "checkout", "-b", "base", common)
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")

	snapshot, err := repo.Compare(ctx, "base", "refs/tags/review", false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Base != base || snapshot.Target != feature || snapshot.From != base || len(snapshot.Files) != 2 {
		t.Fatalf("snapshot comparison: %+v", snapshot)
	}
	diff, err := repo.CompareFileDiff(ctx, snapshot.From, snapshot.Target, "file.txt")
	if err != nil || diff.Old.Text != "base\n" || diff.New.Text != "common\n" ||
		diff.Old.Revision != base || diff.New.Revision != feature {
		t.Fatalf("snapshot file: %+v, %v", diff, err)
	}
	branch, err := repo.Compare(ctx, "base", "review", true)
	if err != nil || branch.From != common || branch.Base != base || branch.Target != feature ||
		len(branch.Files) != 1 || branch.Files[0].Path != "feature.txt" {
		t.Fatalf("merge-base comparison: %+v, %v", branch, err)
	}
	reverse, err := repo.Compare(ctx, "review", "base", true)
	if err != nil || reverse.From != common || reverse.Target != base ||
		len(reverse.Files) != 1 || reverse.Files[0].Path != "file.txt" {
		t.Fatalf("reversed merge-base comparison: %+v, %v", reverse, err)
	}
	empty, err := repo.Compare(ctx, "HEAD", base, false)
	if err != nil || len(empty.Files) != 0 {
		t.Fatalf("same commit: %+v, %v", empty, err)
	}
	git(t, dir, "branch", "-f", "feature", base)
	diff, err = repo.CompareFileDiff(ctx, snapshot.From, snapshot.Target, "feature.txt")
	if err != nil || diff.Old.Present || diff.New.Text != "feature\n" {
		t.Fatalf("resolved comparison changed after moving ref: %+v, %v", diff, err)
	}
}

func TestCompareFileVersionsAndPaths(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	oldPath := portableTestPath("旧\t名字.txt", "旧 名字.txt")
	newPath := portableTestPath("目录/新\n名字.txt", "目录/新 名字.txt")
	write(t, dir, oldPath, strings.Repeat("unchanged\n", 10))
	write(t, dir, "deleted.txt", "deleted\n")
	write(t, dir, "binary.bin", "old\x00bytes")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")
	if err := os.Mkdir(filepath.Join(dir, "目录"), 0700); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "mv", oldPath, newPath)
	git(t, dir, "rm", "deleted.txt")
	write(t, dir, "binary.bin", "new\x00bytes")
	write(t, dir, "large.txt", strings.Repeat("x", fileContentLimit+1))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "target")
	target := git(t, dir, "rev-parse", "HEAD")
	comparison, err := repo.Compare(ctx, base, target, false)
	if err != nil {
		t.Fatal(err)
	}
	renamed := slices.IndexFunc(comparison.Files, func(file CommitFile) bool {
		return file.Path == newPath && file.OriginalPath == oldPath && file.Status == "R"
	})
	if renamed < 0 {
		t.Fatalf("rename missing: %+v", comparison.Files)
	}
	diff, err := repo.CompareFileDiff(ctx, base, target, newPath)
	if err != nil || diff.Old.Path != oldPath || diff.New.Path != newPath ||
		diff.Old.Text != diff.New.Text || !strings.Contains(diff.Text, "rename from ") {
		t.Fatalf("rename diff: %+v, %v", diff, err)
	}
	diff, err = repo.CompareFileDiff(ctx, target, base, "deleted.txt")
	if err != nil || diff.Old.Present || !diff.New.Present || diff.New.Text != "deleted\n" {
		t.Fatalf("reverse deletion: %+v, %v", diff, err)
	}
	diff, err = repo.CompareFileDiff(ctx, base, target, "binary.bin")
	if err != nil || !diff.Binary || !diff.Old.Binary || !diff.New.Binary {
		t.Fatalf("binary diff: %+v, %v", diff, err)
	}
	diff, err = repo.CompareFileDiff(ctx, base, target, "large.txt")
	if err != nil || !diff.New.Truncated || diff.New.Text != "" {
		t.Fatalf("large file: %+v, %v", diff, err)
	}
	if _, err := repo.CompareFileDiff(ctx, base, target, "missing.txt"); err == nil {
		t.Fatal("missing changed file accepted")
	}
}

func TestCompareRejectsInvalidInputsAndCancellation(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "file.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	hash := git(t, dir, "rev-parse", "HEAD")
	for _, revision := range []string{"", "--all", "HEAD\x00other", "missing", "HEAD:file.txt"} {
		if _, err := repo.Compare(context.Background(), revision, hash, false); err == nil {
			t.Fatalf("accepted revision %q", revision)
		}
	}
	if _, err := repo.CompareFileDiff(context.Background(), "HEAD", hash, "file.txt"); err == nil {
		t.Fatal("file comparison must use resolved commit IDs")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Compare(ctx, hash, hash, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled comparison: %v", err)
	}
	git(t, dir, "checkout", "--orphan", "unrelated")
	git(t, dir, "commit", "-m", "unrelated")
	if _, err := repo.Compare(context.Background(), hash, "HEAD", true); err == nil || !strings.Contains(err.Error(), "没有共同祖先") {
		t.Fatalf("unrelated branches need an actionable error: %v", err)
	}
	if result, err := repo.Compare(context.Background(), hash, "HEAD", false); err != nil || len(result.Files) != 0 {
		t.Fatalf("unrelated snapshots should compare: %+v, %v", result, err)
	}
}
