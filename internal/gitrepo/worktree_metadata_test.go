// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorktreeTokenRejectsMetadataDirectoryReplacement(t *testing.T) {
	for _, replaceCommon := range []bool{false, true} {
		name := "linked-git-dir"
		if replaceCommon {
			name = "common-dir"
		}
		t.Run(name, func(t *testing.T) {
			repo, root, path := worktreeFixture(t)
			target := repo
			source := path
			if replaceCommon {
				var err error
				target, err = Open(context.Background(), path)
				if err != nil {
					t.Fatal(err)
				}
				source = filepath.Join(t.TempDir(), "another-worktree")
				git(t, root, "worktree", "add", "-b", "another", source)
			}
			review := worktreeReview(t, target, source)
			if !review.CanRemove {
				t.Fatal(review.RemoveProblem)
			}
			directory := git(t, source, "rev-parse", "--absolute-git-dir")
			if replaceCommon {
				directory = git(t, source, "rev-parse", "--path-format=absolute", "--git-common-dir")
			}
			backup := filepath.Join(t.TempDir(), "original-metadata")
			if err := os.Rename(directory, backup); err != nil {
				t.Fatal(err)
			}
			if err := os.CopyFS(directory, os.DirFS(backup)); err != nil {
				t.Fatal(err)
			}
			if err := target.RemoveWorktree(context.Background(), source, review.Token); err == nil {
				t.Fatal("元数据目录已替换，旧确认令牌仍可移除工作树")
			}
			if _, err := os.Stat(filepath.Join(source, "base.txt")); err != nil {
				t.Fatalf("旧确认破坏了工作目录: %v", err)
			}
		})
	}
}
