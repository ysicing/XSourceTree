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

func TestDiscardPreservesIndexAndBacksUpWorkingFile(t *testing.T) {
	for _, content := range []string{"working\n", "\x00binary\r\n", ""} {
		t.Run(content, func(t *testing.T) {
			repo, dir := fixture(t)
			write(t, dir, "file", "base\n")
			git(t, dir, "add", "file")
			git(t, dir, "commit", "-m", "base")
			write(t, dir, "file", "staged\n")
			git(t, dir, "add", "file")
			write(t, dir, "file", content)
			ctx := context.Background()
			view, err := repo.DiscardPreview(ctx, "file")
			if err != nil || !view.Supported {
				t.Fatalf("preview = %+v, %v", view, err)
			}
			backup, err := repo.Discard(ctx, "file", view.Token, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(filepath.Join(dir, "file"))
			saved, _ := os.ReadFile(filepath.Join(backup, "content"))
			if string(got) != "staged\n" || string(saved) != content || git(t, dir, "show", ":file") != "staged" {
				t.Fatalf("working=%q backup=%q", got, saved)
			}
			if _, err := os.Stat(filepath.Join(backup, "metadata.json")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDiscardRestoresDeletedAndRemovesUntracked(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	path := "嵌套/[file].txt"
	write(t, dir, path, "tracked\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	if err := os.RemoveAll(filepath.Join(dir, "嵌套")); err != nil {
		t.Fatal(err)
	}
	view, err := repo.DiscardPreview(ctx, path)
	if err != nil || !view.Supported || view.Untracked {
		t.Fatalf("preview = %+v, %v", view, err)
	}
	if _, err := repo.Discard(ctx, path, view.Token, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, path))
	if string(content) != "tracked\n" {
		t.Fatal("deletion not restored")
	}
	write(t, dir, "new file", "untracked\n")
	view, _ = repo.DiscardPreview(ctx, "new file")
	if !view.Untracked {
		t.Fatal("untracked preview not identified")
	}
	backup, err := repo.Discard(ctx, "new file", view.Token, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new file")); !os.IsNotExist(err) {
		t.Fatal("untracked file not removed")
	}
	content, _ = os.ReadFile(filepath.Join(backup, "content"))
	if string(content) != "untracked\n" {
		t.Fatal("untracked content not backed up")
	}
}

func TestDiscardRejectsStalePreviewAndBackupFailure(t *testing.T) {
	for _, change := range []string{"working", "index", "head", "backup", "cancel"} {
		t.Run(change, func(t *testing.T) {
			repo, dir, _, changed := patchFixture(t, "file")
			ctx := context.Background()
			view, _ := repo.DiscardPreview(ctx, "file")
			recovery := t.TempDir()
			want := changed
			switch change {
			case "working":
				want += "external edit\n"
				write(t, dir, "file", want)
			case "index":
				git(t, dir, "add", "file")
			case "head":
				git(t, dir, "commit", "--allow-empty", "-m", "external commit")
			case "backup":
				recovery = filepath.Join(dir, "file")
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := repo.Discard(ctx, "file", view.Token, recovery); err == nil {
				t.Fatal("unsafe discard accepted")
			}
			content, _ := os.ReadFile(filepath.Join(dir, "file"))
			if string(content) != want {
				t.Fatal("failed discard changed working tree")
			}
		})
	}
}

func TestDiscardRejectsSymlinksMetadataAndOversize(t *testing.T) {
	repo, dir, _, _ := patchFixture(t, "file")
	outside := t.TempDir()
	write(t, outside, "target", "outside")
	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "ancestor")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "large", strings.Repeat("x", outputLimit+1))
	for _, path := range []string{"link", "ancestor/target", ".git/config", "../outside", "large"} {
		view, err := repo.DiscardPreview(context.Background(), path)
		if err == nil && view.Supported {
			t.Fatalf("unsafe path %q supported", path)
		}
	}
}

func TestDiscardRestoresWorktreeLineEndings(t *testing.T) {
	repo, dir := fixture(t)
	git(t, dir, "config", "core.autocrlf", "true")
	write(t, dir, "file", "base\r\n")
	git(t, dir, "add", "file")
	git(t, dir, "commit", "-m", "base")
	write(t, dir, "file", "changed\r\n")
	view, _ := repo.DiscardPreview(context.Background(), "file")
	if _, err := repo.Discard(context.Background(), "file", view.Token, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, "file"))
	if string(content) != "base\r\n" {
		t.Fatalf("line endings changed: %q", content)
	}
}

func TestDiscardPreservesExternalEditDuringCheckoutFilter(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, ".gitattributes", "file filter=edit\n")
	write(t, dir, "file", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	write(t, dir, "file", "working\n")
	git(t, dir, "config", "filter.edit.smudge", "printf 'external\\n' > file; cat")
	view, _ := repo.DiscardPreview(context.Background(), "file")
	if _, err := repo.Discard(context.Background(), "file", view.Token, t.TempDir()); err == nil {
		t.Fatal("accepted edit during preparation")
	}
	content, _ := os.ReadFile(filepath.Join(dir, "file"))
	if string(content) != "external\n" || git(t, dir, "show", ":file") != "base" {
		t.Fatal("external edit or index overwritten")
	}
}

func TestDiscardRejectsIntentToAddAndConflicts(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "intent", "new\n")
	git(t, dir, "add", "-N", "intent")
	view, err := repo.DiscardPreview(context.Background(), "intent")
	if err != nil || view.Supported {
		t.Fatalf("intent to add = %+v, %v", view, err)
	}
	git(t, dir, "add", "intent")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "checkout", "-b", "topic")
	write(t, dir, "intent", "topic\n")
	git(t, dir, "commit", "-am", "topic")
	git(t, dir, "checkout", "-")
	write(t, dir, "intent", "main\n")
	git(t, dir, "commit", "-am", "main")
	if _, err := repo.git(context.Background(), "merge", "topic"); err == nil {
		t.Fatal("expected conflict")
	}
	view, err = repo.DiscardPreview(context.Background(), "intent")
	if err != nil || view.Supported {
		t.Fatalf("conflict = %+v, %v", view, err)
	}
}

func TestDiscardRejectsIntentToAddDetectedAsRename(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "first", "nonempty original\n")
	git(t, dir, "add", "first")
	git(t, dir, "commit", "-m", "base")
	if err := os.Rename(filepath.Join(dir, "first"), filepath.Join(dir, "third")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-N", "third")
	view, err := repo.DiscardPreview(context.Background(), "third")
	if err != nil || view.Supported {
		t.Fatalf("intent to add rename = %+v, %v", view, err)
	}
}

func TestDiscardAcceptsStagedEmptyFileInUnbornRepository(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "empty", "")
	git(t, dir, "add", "empty")
	write(t, dir, "empty", "working\n")
	view, err := repo.DiscardPreview(context.Background(), "empty")
	if err != nil || !view.Supported {
		t.Fatalf("staged empty file = %+v, %v", view, err)
	}
	if _, err := repo.Discard(context.Background(), "empty", view.Token, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, "empty"))
	if len(content) != 0 {
		t.Fatal("did not restore staged empty file")
	}
}

func TestDiscardRejectsMissingIntentToAdd(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "intent", "new\n")
	git(t, dir, "add", "-N", "intent")
	if err := os.Remove(filepath.Join(dir, "intent")); err != nil {
		t.Fatal(err)
	}
	view, err := repo.DiscardPreview(context.Background(), "intent")
	if err != nil || view.Supported {
		t.Fatalf("missing intent to add = %+v, %v", view, err)
	}
}
