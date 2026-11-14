// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 通过公开 JSON 契约断言，使新增字段尚未实现时也能运行失败测试。
type contentSide struct {
	Path, Revision, Text                    string
	Present, Binary, Truncated, Unsupported bool
}

func contentSides(t *testing.T, diff Diff, err error) (*contentSide, *contentSide) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Old, New *contentSide }
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Old == nil || result.New == nil {
		t.Fatalf("single-file diff omitted full sides (patch length %d)", len(diff.Text))
	}
	return result.Old, result.New
}

func wantContent(t *testing.T, side *contentSide, path, revision, text string, present bool) {
	t.Helper()
	want := contentSide{Path: path, Revision: revision, Text: text, Present: present}
	if *side != want {
		t.Fatalf("content = %+v, want %+v", *side, want)
	}
}

func TestFileContentIndexAndWorktree(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file.txt", "head\nunchanged\r\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	write(t, dir, "file.txt", "index\nunchanged\r\n")
	git(t, dir, "add", ".")
	write(t, dir, "file.txt", "worktree\nunchanged\r\n")
	for _, staged := range []bool{true, false} {
		diff, err := repo.Diff(ctx, "file.txt", staged)
		old, next := contentSides(t, diff, err)
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames"}
		if staged {
			args = append(args, "--cached")
		}
		patch, err := repo.git(ctx, append(args, "--", "file.txt")...)
		if err != nil || diff.Text != patch {
			t.Fatalf("Git patch changed: %v", err)
		}
		if staged {
			if old.Revision != "HEAD" && old.Revision != git(t, dir, "rev-parse", "HEAD") {
				t.Fatalf("HEAD revision: %s", old.Revision)
			}
			wantContent(t, old, "file.txt", old.Revision, "head\nunchanged\r\n", true)
			wantContent(t, next, "file.txt", "INDEX", "index\nunchanged\r\n", true)
		} else {
			wantContent(t, old, "file.txt", "INDEX", "index\nunchanged\r\n", true)
			wantContent(t, next, "file.txt", "WORKTREE", "worktree\nunchanged\r\n", true)
		}
	}
}

func TestFileContentNewRootRenameDelete(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	name := portableTestPath("目录/old\tname.txt", "目录/旧 文件.txt")
	renamed := portableTestPath("目录/new\nname.txt", "目录/新 文件.txt")
	write(t, dir, name, "first\nsecond\nthird\n")
	diff, err := repo.Diff(ctx, name, false)
	old, next := contentSides(t, diff, err)
	wantContent(t, old, name, "INDEX", "", false)
	wantContent(t, next, name, "WORKTREE", "first\nsecond\nthird\n", true)
	git(t, dir, "add", ".")
	diff, err = repo.Diff(ctx, name, true)
	old, next = contentSides(t, diff, err)
	wantContent(t, old, name, "HEAD", "", false)
	wantContent(t, next, name, "INDEX", "first\nsecond\nthird\n", true)
	git(t, dir, "commit", "-m", "root")
	root := git(t, dir, "rev-parse", "HEAD")
	diff, err = repo.CommitFileDiff(ctx, root, name)
	old, next = contentSides(t, diff, err)
	if old.Present || old.Text != "" {
		t.Fatalf("root old: %+v", old)
	}
	wantContent(t, next, name, root, "first\nsecond\nthird\n", true)
	git(t, dir, "mv", name, renamed)
	write(t, dir, renamed, "working\n")
	diff, err = repo.Diff(ctx, renamed, true)
	old, next = contentSides(t, diff, err)
	wantContent(t, old, name, old.Revision, "first\nsecond\nthird\n", true)
	wantContent(t, next, renamed, "INDEX", "first\nsecond\nthird\n", true)
	diff, err = repo.Diff(ctx, renamed, false)
	old, next = contentSides(t, diff, err)
	wantContent(t, old, renamed, "INDEX", "first\nsecond\nthird\n", true)
	wantContent(t, next, renamed, "WORKTREE", "working\n", true)
	git(t, dir, "commit", "-m", "rename")
	hash := git(t, dir, "rev-parse", "HEAD")
	for _, path := range []string{name, renamed} {
		diff, err = repo.CommitFileDiff(ctx, hash, path)
		old, next = contentSides(t, diff, err)
		wantContent(t, old, name, root, "first\nsecond\nthird\n", true)
		wantContent(t, next, renamed, hash, "first\nsecond\nthird\n", true)
	}
	if err := os.Remove(filepath.Join(dir, renamed)); err != nil {
		t.Fatal(err)
	}
	diff, err = repo.Diff(ctx, renamed, false)
	old, next = contentSides(t, diff, err)
	wantContent(t, old, renamed, "INDEX", "first\nsecond\nthird\n", true)
	wantContent(t, next, renamed, "WORKTREE", "", false)
	git(t, dir, "add", "-u")
	diff, err = repo.Diff(ctx, renamed, true)
	old, next = contentSides(t, diff, err)
	wantContent(t, next, renamed, "INDEX", "", false)
	git(t, dir, "commit", "-m", "delete")
	deleted := git(t, dir, "rev-parse", "HEAD")
	diff, err = repo.CommitFileDiff(ctx, deleted, renamed)
	old, next = contentSides(t, diff, err)
	wantContent(t, old, renamed, hash, "first\nsecond\nthird\n", true)
	wantContent(t, next, renamed, deleted, "", false)
}

func TestFileContentBinaryAndLimit(t *testing.T) {
	for _, test := range []struct {
		name, text        string
		binary, truncated bool
	}{
		{"nul", "before\x00after", true, false},
		{"utf8", "invalid\xff", true, false},
		{"empty", "", false, false},
		{"boundary", strings.Repeat("x", 1<<20), false, false},
		{"oversize", strings.Repeat("x", (1<<20)+1), false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, dir := fixture(t)
			ctx := context.Background()
			write(t, dir, "file", test.text)
			check := func(diff Diff, err error) {
				t.Helper()
				_, next := contentSides(t, diff, err)
				if !next.Present || next.Binary != test.binary || next.Truncated != test.truncated || next.Unsupported {
					t.Fatalf("flags: %+v", next)
				}
				want := test.text
				if test.binary || test.truncated {
					want = ""
				}
				if next.Text != want {
					t.Fatalf("text length = %d, want %d", len(next.Text), len(want))
				}
			}
			check(repo.Diff(ctx, "file", false))
			git(t, dir, "add", ".")
			check(repo.Diff(ctx, "file", true))
			git(t, dir, "commit", "-m", "root")
			check(repo.CommitFileDiff(ctx, git(t, dir, "rev-parse", "HEAD"), "file"))
		})
	}
}

func TestFileContentSymlinkAndGitlink(t *testing.T) {
	for _, kind := range []string{"symlink", "gitlink"} {
		t.Run(kind, func(t *testing.T) {
			repo, dir := fixture(t)
			ctx := context.Background()
			check := func(diff Diff, err error) {
				t.Helper()
				_, next := contentSides(t, diff, err)
				if !next.Present || !next.Unsupported || next.Text != "" || next.Binary {
					t.Fatalf("unsupported side: %+v", next)
				}
				if strings.Contains(diff.Text, "external secret") {
					t.Fatal("followed external symlink")
				}
			}
			path := "module"
			if kind == "symlink" {
				secret := filepath.Join(t.TempDir(), "secret")
				write(t, filepath.Dir(secret), "secret", "external secret")
				testSymlink(t, secret, filepath.Join(dir, "link"))
				path = "link"
				check(repo.Diff(ctx, path, false))
				git(t, dir, "add", ".")
			} else {
				git(t, dir, "commit", "--allow-empty", "-m", "base")
				hash := git(t, dir, "rev-parse", "HEAD")
				git(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+hash+",module")
			}
			check(repo.Diff(ctx, path, true))
			git(t, dir, "commit", "-m", kind)
			check(repo.CommitFileDiff(ctx, git(t, dir, "rev-parse", "HEAD"), path))
		})
	}
}

func TestFileContentCancellationAndSummary(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "file", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "root")
	hash := git(t, dir, "rev-parse", "HEAD")
	write(t, dir, "file", "changed\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Diff(ctx, "file", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel diff: %v", err)
	}
	if _, err := repo.CommitFileDiff(ctx, hash, "file"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel history: %v", err)
	}
	repo.mu.Lock()
	busy, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	done := make(chan error, 1)
	go func() { _, err := repo.Diff(busy, "file", false); done <- err }()
	select {
	case err := <-done:
		repo.mu.Unlock()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("busy cancellation: %v", err)
		}
	case <-time.After(time.Second):
		repo.mu.Unlock()
		<-done
		t.Fatal("Diff ignored cancellation while waiting for repository lock")
	}
	diff, err := repo.CommitDiff(context.Background(), hash)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct{ Old, New *contentSide }
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Old != nil || summary.New != nil {
		t.Fatal("whole-commit summary populated single-file sides")
	}
}

func TestFileContentMergeUsesFirstParent(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "checkout", "-b", "side")
	write(t, dir, "file", "side\n")
	git(t, dir, "commit", "-am", "side")
	git(t, dir, "checkout", "main")
	write(t, dir, "other", "main\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "main")
	parent := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "merge", "--no-ff", "side", "-m", "merge")
	hash := git(t, dir, "rev-parse", "HEAD")
	diff, err := repo.CommitFileDiff(ctx, hash, "file")
	old, next := contentSides(t, diff, err)
	wantContent(t, old, "file", parent, "base\n", true)
	wantContent(t, next, "file", hash, "side\n", true)
}

func TestFileContentObjectFailureIsNotMissing(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	side, err := repo.indexFileContent(ctx, "missing")
	if err != nil || side.Present {
		t.Fatalf("missing index side: %+v %v", side, err)
	}
	git(t, dir, "update-index", "--add", "--cacheinfo", "100644,"+strings.Repeat("1", 40)+",broken")
	if side, err := repo.indexFileContent(ctx, "broken"); err == nil {
		t.Fatalf("unreadable object became missing: %+v", side)
	}
}

func TestFileContentRejectsSymlinkAncestor(t *testing.T) {
	repo, dir := fixture(t)
	outside := t.TempDir()
	write(t, outside, "file", "external secret\n")
	testSymlink(t, outside, filepath.Join(dir, "linked"))
	if side, err := repo.worktreeFileContent(context.Background(), "linked/file"); err != nil || side.Present || !side.Unsupported || side.Text != "" {
		t.Fatalf("symlink ancestor was followed or hid the deleted side: %+v %v", side, err)
	}
}

func TestFileContentLargeBlobAndStashSummary(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file", "base\n")
	write(t, dir, ".gitattributes", "file -diff\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	write(t, dir, "file", strings.Repeat("large\n", 1000000))
	git(t, dir, "add", "file")
	diff, err := repo.Diff(ctx, "file", true)
	old, next := contentSides(t, diff, err)
	wantContent(t, old, "file", old.Revision, "base\n", true)
	if !next.Present || !next.Truncated || next.Text != "" {
		t.Fatalf("large blob loaded or represented as complete: %+v", next)
	}
	git(t, dir, "stash", "push", "-m", "large")
	hash := git(t, dir, "rev-parse", "refs/stash")
	summary, err := repo.StashDiff(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Old != nil || summary.New != nil {
		t.Fatal("stash summary populated single-file content")
	}
}

func TestFileContentDirectoryReplacementKeepsDeletedDiff(t *testing.T) {
	for _, replacement := range []string{"file", "symlink"} {
		t.Run(replacement, func(t *testing.T) {
			repo, dir := fixture(t)
			ctx := context.Background()
			write(t, dir, "dir/file.txt", "indexed body\n")
			git(t, dir, "add", ".")
			git(t, dir, "commit", "-m", "base")
			if err := os.Rename(filepath.Join(dir, "dir"), filepath.Join(dir, "saved")); err != nil {
				t.Fatal(err)
			}
			if replacement == "file" {
				write(t, dir, "dir", "replacement\n")
			} else {
				outside := t.TempDir()
				write(t, outside, "file.txt", "external secret\n")
				testSymlink(t, outside, filepath.Join(dir, "dir"))
			}
			files, err := repo.files(ctx)
			if err != nil {
				t.Fatal(err)
			}
			deleted := false
			for _, file := range files {
				deleted = deleted || file.Path == "dir/file.txt" && file.Working == "D"
			}
			if !deleted {
				t.Fatalf("Git did not report deleted child: %+v", files)
			}
			patch, err := repo.git(ctx, "diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--", "dir/file.txt")
			if err != nil {
				t.Fatal(err)
			}
			diff, err := repo.Diff(ctx, "dir/file.txt", false)
			old, next := contentSides(t, diff, err)
			wantContent(t, old, "dir/file.txt", "INDEX", "indexed body\n", true)
			if next.Present || next.Text != "" || next.Revision != "WORKTREE" || next.Path != "dir/file.txt" {
				t.Fatalf("deleted child read through replaced ancestor: %+v", next)
			}
			if diff.Text != patch || !strings.Contains(diff.Text, "-indexed body") || strings.Contains(diff.Text, "external secret") {
				t.Fatalf("deleted child patch lost or external content read: %q", diff.Text)
			}
		})
	}
}
