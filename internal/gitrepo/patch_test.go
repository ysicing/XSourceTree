// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func patchFixture(t *testing.T, path string) (*Repository, string, string, string) {
	t.Helper()
	repo, dir := fixture(t)
	var lines strings.Builder
	for number := 1; number <= 40; number++ {
		fmt.Fprintf(&lines, "line %02d\n", number)
	}
	base := lines.String()
	changed := strings.Replace(base, "line 03\n", "first changed\nfirst inserted\n", 1)
	changed = strings.Replace(changed, "line 30\n", "last changed\n", 1)
	write(t, dir, path, base)
	git(t, dir, "add", "--", path)
	git(t, dir, "commit", "-m", "patch base")
	write(t, dir, path, changed)
	return repo, dir, base, changed
}

func TestApplyHunkChangesOnlySelectedIndexHunk(t *testing.T) {
	for _, staged := range []bool{false, true} {
		for _, selected := range []int{0, 1} {
			t.Run(fmt.Sprintf("staged=%t/hunk=%d", staged, selected), func(t *testing.T) {
				repo, dir, base, changed := patchFixture(t, "file.txt")
				ctx := context.Background()
				if staged {
					git(t, dir, "add", "--", "file.txt")
				}
				patch, err := repo.Patch(ctx, "file.txt", staged)
				if err != nil || !patch.Supported || len(patch.Hunks) != 2 || patch.Token == "" {
					t.Fatalf("patch: %+v, %v", patch, err)
				}
				for index, hunk := range patch.Hunks {
					if hunk.ID != index || !strings.HasPrefix(hunk.Header, "@@ -") {
						t.Fatalf("hunk: %+v", hunk)
					}
				}
				if !strings.Contains(patch.Text, "+first changed\n") || !strings.Contains(patch.Text, "+last changed\n") ||
					strings.Count(patch.Text, "\n@@ ") != len(patch.Hunks) {
					t.Fatalf("display text does not match actionable hunks: %q", patch.Text)
				}
				if err := repo.ApplyHunk(ctx, "file.txt", staged, patch.Token, patch.Hunks[selected].ID); err != nil {
					t.Fatal(err)
				}
				want := base
				if staged {
					want = changed
				}
				if selected == 0 {
					if staged {
						want = strings.Replace(want, "first changed\nfirst inserted\n", "line 03\n", 1)
					} else {
						want = strings.Replace(want, "line 03\n", "first changed\nfirst inserted\n", 1)
					}
				} else if staged {
					want = strings.Replace(want, "last changed\n", "line 30\n", 1)
				} else {
					want = strings.Replace(want, "line 30\n", "last changed\n", 1)
				}
				if got := git(t, dir, "show", ":file.txt"); got != strings.TrimSuffix(want, "\n") {
					t.Fatalf("index:\n%s\nwant:\n%s", got, want)
				}
				content, err := os.ReadFile(filepath.Join(dir, "file.txt"))
				if err != nil || string(content) != changed {
					t.Fatalf("working content changed: %q, %v", content, err)
				}
				if err := repo.ApplyHunk(ctx, "file.txt", staged, patch.Token, selected); err == nil {
					t.Fatal("reused token accepted after index changed")
				}
			})
		}
	}
}

func TestApplyHunkUsesIndexCoordinatesWithRepeatedContexts(t *testing.T) {
	for _, staged := range []bool{false, true} {
		for _, delta := range []int{-100, 100} {
			t.Run(fmt.Sprintf("staged=%t/prefix=%d", staged, delta), func(t *testing.T) {
				repo, dir := fixture(t)
				ctx := context.Background()
				block := "same 1\nsame 2\nsame 3\nvalue old\nsame 4\nsame 5\nsame 6\n"
				from, to := "value old\n", "value new\n"
				if staged {
					block = strings.Replace(block, from, to, 1)
					from, to = to, from
				}
				prefix := strings.Repeat("prefix\n", 100)
				gap := strings.Repeat("gap\n", 93)
				source := prefix + block + gap + block + gap + block + strings.Repeat("suffix\n", 30)
				middle := len(prefix + block + gap)
				want := source[:middle] + strings.Replace(source[middle:], from, to, 1)
				other := strings.Repeat("inserted\n", 100) + want
				if delta < 0 {
					other = want[len(prefix):]
				}
				base, changed := source, other
				if staged {
					base, changed = other, source
				}
				write(t, dir, "file.txt", base)
				git(t, dir, "add", "--", "file.txt")
				git(t, dir, "commit", "-m", "repeated contexts and preceding lines")
				write(t, dir, "file.txt", changed)
				if staged {
					git(t, dir, "add", "--", "file.txt")
				}
				patch, err := repo.Patch(ctx, "file.txt", staged)
				if err != nil || !patch.Supported || len(patch.Hunks) != 2 {
					t.Fatalf("patch: %+v, %v", patch, err)
				}
				if err := repo.ApplyHunk(ctx, "file.txt", staged, patch.Token, 1); err != nil {
					t.Fatal(err)
				}
				content, err := repo.git(ctx, "show", ":file.txt")
				if err != nil || content != want {
					t.Fatalf("wrong repeated context changed:\n%q\nwant:\n%q, %v", content, want, err)
				}
				working, err := os.ReadFile(filepath.Join(dir, "file.txt"))
				if err != nil || string(working) != changed {
					t.Fatalf("working content changed: %q, %v", working, err)
				}
			})
		}
	}
}

func TestApplyHunkPreservesRawBytesWithGlobalEOLConfiguration(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(fmt.Sprintf("staged=%t", staged), func(t *testing.T) {
			repo, dir, base, changed := patchFixture(t, "file.txt")
			ctx := context.Background()
			git(t, dir, "config", "core.autocrlf", "false")
			write(t, dir, ".gitattributes", "*.txt text eol=lf\n")
			git(t, dir, "add", "--", ".gitattributes")
			git(t, dir, "commit", "-m", "repository LF attributes")
			if staged {
				git(t, dir, "add", "--", "file.txt")
			}
			global := t.TempDir()
			configuration := filepath.Join(global, "config")
			attributes := filepath.Join(global, "attributes")
			write(t, global, "attributes", "* text eol=crlf\n")
			for key, value := range map[string]string{
				"core.attributesFile": attributes,
				"core.autocrlf":       "true",
				"core.eol":            "crlf",
			} {
				git(t, dir, "config", "--file", configuration, key, value)
			}
			t.Setenv("GIT_CONFIG_GLOBAL", configuration)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("TMPDIR", t.TempDir())
			patch, err := repo.Patch(ctx, "file.txt", staged)
			if err != nil || !patch.Supported || len(patch.Hunks) != 2 {
				t.Fatalf("patch: %+v, %v", patch, err)
			}
			if err := repo.ApplyHunk(ctx, "file.txt", staged, patch.Token, 1); err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(base, "line 30\n", "last changed\n", 1)
			if staged {
				want = strings.Replace(changed, "last changed\n", "line 30\n", 1)
			}
			content, err := repo.git(ctx, "show", ":file.txt")
			if err != nil || content != want {
				t.Fatalf("index raw bytes: %q, want %q, %v", content, want, err)
			}
			working, err := os.ReadFile(filepath.Join(dir, "file.txt"))
			if err != nil || string(working) != changed {
				t.Fatalf("working content changed: %q, %v", working, err)
			}
		})
	}
}

func TestApplyHunkToBlobPreservesBoundariesAndRejectsOffsets(t *testing.T) {
	tests := []struct {
		name   string
		before string
		hunk   string
		staged bool
		want   string
		fail   bool
	}{
		{"insert into empty", "", "@@ -0,0 +1 @@\n+new\n", false, "new\n", false},
		{"reverse emptying", "", "@@ -1 +0,0 @@\n-old\n", true, "old\n", false},
		{"delete all", "old\n", "@@ -1 +0,0 @@\n-old\n", false, "", false},
		{"reverse insertion", "new\n", "@@ -0,0 +1 @@\n+new\n", true, "", false},
		{"add final newline", "old", "@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n", false, "new\n", false},
		{"reverse final newline", "new\n", "@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n", true, "old", false},
		{"CRLF bytes", "old\r\n", "@@ -1 +1 @@\n-old\r\n+new\r\n", false, "new\r\n", false},
		{"wrong position", "prefix\nold\n", "@@ -1 +1 @@\n-old\n+new\n", false, "", true},
		{"reverse wrong position", "prefix\nnew\n", "@@ -1 +1 @@\n-old\n+new\n", true, "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := applyHunkToBlob(test.before, test.hunk, test.staged)
			if test.fail {
				if err == nil {
					t.Fatalf("accepted a hunk outside its exact source position: %q", got)
				}
			} else if err != nil || got != test.want {
				t.Fatalf("raw bytes: %q, want %q, %v", got, test.want, err)
			}
		})
	}
}

func TestApplyHunkRejectsStaleSnapshots(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"working content", func(t *testing.T, dir string) {
			write(t, dir, "file.txt", "replacement content\n")
		}},
		{"selected index", func(t *testing.T, dir string) {
			write(t, dir, "file.txt", "replacement index\n")
			git(t, dir, "add", "--", "file.txt")
		}},
		{"other index entry", func(t *testing.T, dir string) {
			write(t, dir, "other.txt", "new index entry\n")
			git(t, dir, "add", "--", "other.txt")
		}},
		{"HEAD", func(t *testing.T, dir string) {
			git(t, dir, "commit", "--allow-empty", "-m", "advance HEAD")
		}},
	}
	for _, staged := range []bool{false, true} {
		for _, mutation := range mutations {
			t.Run(fmt.Sprintf("staged=%t/%s", staged, mutation.name), func(t *testing.T) {
				repo, dir, _, _ := patchFixture(t, "file.txt")
				ctx := context.Background()
				if staged {
					git(t, dir, "add", "--", "file.txt")
				}
				patch, err := repo.Patch(ctx, "file.txt", staged)
				if err != nil || !patch.Supported {
					t.Fatalf("patch: %+v, %v", patch, err)
				}
				mutation.mutate(t, dir)
				before := git(t, dir, "ls-files", "--stage")
				content, readErr := os.ReadFile(filepath.Join(dir, "file.txt"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				if err := repo.ApplyHunk(ctx, "file.txt", staged, patch.Token, 0); err == nil {
					t.Fatal("stale snapshot accepted")
				}
				if got := git(t, dir, "ls-files", "--stage"); got != before {
					t.Fatalf("rejected operation changed index: %s", got)
				}
				after, err := os.ReadFile(filepath.Join(dir, "file.txt"))
				if err != nil || string(after) != string(content) {
					t.Fatalf("rejected operation changed worktree: %q, %v", after, err)
				}
			})
		}
	}
}

func TestApplyHunkSerializesConcurrentRequests(t *testing.T) {
	repo, dir, base, _ := patchFixture(t, "file.txt")
	ctx := context.Background()
	patch, err := repo.Patch(ctx, "file.txt", false)
	if err != nil || !patch.Supported {
		t.Fatalf("patch: %+v, %v", patch, err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for request := 0; request < 2; request++ {
		go func() {
			<-start
			results <- repo.ApplyHunk(ctx, "file.txt", false, patch.Token, 0)
		}()
	}
	close(start)
	succeeded := 0
	for request := 0; request < 2; request++ {
		if <-results == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("same token succeeded %d times", succeeded)
	}
	want := strings.Replace(base, "line 03\n", "first changed\nfirst inserted\n", 1)
	if got := git(t, dir, "show", ":file.txt"); got != strings.TrimSuffix(want, "\n") {
		t.Fatalf("concurrent operation changed wrong hunk: %s", got)
	}
}

func TestPreparedHunkRejectsIndexReplacementAfterCheck(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(fmt.Sprintf("staged=%t", staged), func(t *testing.T) {
			repo, dir := fixture(t)
			ctx := context.Background()
			block := "same 1\nsame 2\nsame 3\nvalue old\nsame 4\nsame 5\nsame 6\n"
			base := strings.Repeat("prefix\n", 8) + block + strings.Repeat("gap\n", 12) + block + strings.Repeat("suffix\n", 8)
			first := strings.Replace(base, "value old\n", "value new\n", 1)
			position := strings.LastIndex(base, "value old\n")
			second := base[:position] + strings.Replace(base[position:], "value old\n", "value new\n", 1)
			write(t, dir, "file.txt", base)
			git(t, dir, "add", "--", "file.txt")
			git(t, dir, "commit", "-m", "repeated contexts")
			write(t, dir, "file.txt", first)
			if staged {
				git(t, dir, "add", "--", "file.txt")
			}
			snapshot, err := repo.hunkPatch(ctx, "file.txt", staged)
			if err != nil || !snapshot.view.Supported || len(snapshot.hunks) != 1 {
				t.Fatalf("snapshot: %+v, %v", snapshot.view, err)
			}
			input, err := repo.prepareHunkApply(ctx, snapshot, 0, staged)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"-c", "apply.ignoreWhitespace=no", "apply", "--cached", "--whitespace=nowarn", "-p1"}
			if _, err := run(ctx, repo.Path, input, 30*time.Second, append(args, "--check", "-")...); err != nil {
				t.Fatal(err)
			}
			// 确定性模拟最终验证之后发生的外部暂存：A 恢复，B 具有相同修改及上下文。
			write(t, dir, "file.txt", second)
			git(t, dir, "add", "--", "file.txt")
			before := git(t, dir, "ls-files", "--stage")
			legacyArgs := append([]string{}, args...)
			if staged {
				legacyArgs = append(legacyArgs, "--reverse")
			}
			if _, err := run(ctx, repo.Path, snapshot.header+snapshot.hunks[0], 30*time.Second, append(legacyArgs, "--check", "-")...); err != nil {
				t.Fatalf("fixture must expose the unguarded text-patch race: %v", err)
			}
			if _, err := run(ctx, repo.Path, input, 30*time.Second, append(args, "-")...); err == nil {
				t.Fatal("prepared patch accepted a different index blob")
			}
			if got := git(t, dir, "ls-files", "--stage"); got != before {
				t.Fatalf("rejected operation changed index: %s", got)
			}
			if got := git(t, dir, "show", ":file.txt"); got != strings.TrimSuffix(second, "\n") {
				t.Fatalf("rejected operation changed staged content: %s", got)
			}
			if got := git(t, dir, "ls-files", "--unmerged"); got != "" {
				t.Fatalf("rejected operation wrote conflict stages: %s", got)
			}
			content, err := os.ReadFile(filepath.Join(dir, "file.txt"))
			if err != nil || string(content) != second {
				t.Fatalf("rejected operation changed worktree: %q, %v", content, err)
			}
		})
	}
}

func TestPatchIgnoresDiffPresentationAndExternalCommands(t *testing.T) {
	repo, dir, _, _ := patchFixture(t, "file.txt")
	ctx := context.Background()
	before, err := repo.Patch(ctx, "file.txt", false)
	if err != nil || !before.Supported {
		t.Fatalf("initial patch: %+v, %v", before, err)
	}
	for key, value := range map[string]string{
		"diff.external":          "this-command-must-not-run",
		"diff.blocked.textconv":  "this-command-must-not-run",
		"diff.blocked.algorithm": "histogram",
		"diff.algorithm":         "patience",
		"diff.indentHeuristic":   "true",
		"diff.context":           "0",
		"diff.interHunkContext":  "10000",
		"diff.mnemonicPrefix":    "true",
		"diff.noprefix":          "true",
		"diff.relative":          "true",
		"diff.orderFile":         "this-file-must-not-be-read",
		"color.diff":             "always",
		"apply.whitespace":       "fix",
		"apply.ignoreWhitespace": "change",
	} {
		git(t, dir, "config", key, value)
	}
	t.Setenv("GIT_EXTERNAL_DIFF", "this-command-must-not-run")
	write(t, dir, ".git/info/attributes", "*.txt diff=blocked\n")
	after, err := repo.Patch(ctx, "file.txt", false)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("configuration changed patch:\nbefore %+v\nafter %+v, %v", before, after, err)
	}
	if err := repo.ApplyHunk(ctx, "file.txt", false, after.Token, 1); err != nil {
		t.Fatal(err)
	}
}

func TestPatchUnsupportedFileChanges(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		setup func(*testing.T, string)
	}{
		{"unchanged", "file.txt", func(t *testing.T, dir string) {
			git(t, dir, "restore", "--worktree", "--", "file.txt")
		}},
		{"untracked", "new.txt", func(t *testing.T, dir string) {
			write(t, dir, "new.txt", "new\n")
		}},
		{"added", "new.txt", func(t *testing.T, dir string) {
			write(t, dir, "new.txt", "new\n")
			git(t, dir, "add", "--", "new.txt")
		}},
		{"deleted", "file.txt", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "file.txt")); err != nil {
				t.Fatal(err)
			}
		}},
		{"staged deletion", "file.txt", func(t *testing.T, dir string) {
			git(t, dir, "rm", "-f", "--", "file.txt")
		}},
		{"rename", "renamed.txt", func(t *testing.T, dir string) {
			git(t, dir, "mv", "--", "file.txt", "renamed.txt")
		}},
		{"binary", "file.txt", func(t *testing.T, dir string) {
			write(t, dir, "file.txt", "binary\x00content\n")
		}},
		{"binary forced text", "file.txt", func(t *testing.T, dir string) {
			write(t, dir, ".git/info/attributes", "*.txt diff\n")
			write(t, dir, "file.txt", "binary\x00content\n")
		}},
		{"invalid UTF-8", "file.txt", func(t *testing.T, dir string) {
			write(t, dir, "file.txt", "invalid\xffcontent\n")
		}},
		{"attributes binary", "file.txt", func(t *testing.T, dir string) {
			write(t, dir, ".git/info/attributes", "*.txt -diff\n")
		}},
		{"mode", "file.txt", func(t *testing.T, dir string) {
			git(t, dir, "config", "core.filemode", "true")
			if err := os.Chmod(filepath.Join(dir, "file.txt"), 0755); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", "file.txt", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "file.txt")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("other.txt", filepath.Join(dir, "file.txt")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		}},
		{"submodule", "module", func(t *testing.T, dir string) {
			head := git(t, dir, "rev-parse", "HEAD")
			git(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+head+",module")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo, dir, _, _ := patchFixture(t, "file.txt")
			test.setup(t, dir)
			for _, staged := range []bool{false, true} {
				patch, err := repo.Patch(context.Background(), test.path, staged)
				if err != nil || patch.Supported || len(patch.Hunks) != 0 || patch.Text != "" {
					t.Fatalf("staged=%t: %+v, %v", staged, patch, err)
				}
				before := git(t, dir, "ls-files", "--stage")
				if err := repo.ApplyHunk(context.Background(), test.path, staged, patch.Token, 0); err == nil {
					t.Fatal("unsupported patch accepted")
				}
				if got := git(t, dir, "ls-files", "--stage"); got != before {
					t.Fatalf("unsupported operation changed index: %s", got)
				}
			}
		})
	}
}

func TestPatchRejectsSymlinkedAncestor(t *testing.T) {
	repo, dir, _, _ := patchFixture(t, "directory/file.txt")
	outside := filepath.Join(t.TempDir(), "directory")
	if err := os.Rename(filepath.Join(dir, "directory"), outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "directory")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ctx := context.Background()
	before := git(t, dir, "ls-files", "--stage")
	if patch, err := repo.Patch(ctx, "directory/file.txt", false); err == nil {
		t.Fatalf("symlinked ancestor accepted: %+v", patch)
	}
	if err := repo.ApplyHunk(ctx, "directory/file.txt", false, "forged", 0); err == nil {
		t.Fatal("apply accepted symlinked ancestor")
	}
	if got := git(t, dir, "ls-files", "--stage"); got != before {
		t.Fatal("rejected symlink changed index")
	}
}

func TestPatchUnbornRepositoryIsUnsupported(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "file.txt", "initial content\n")
	git(t, dir, "add", "--", "file.txt")
	for _, staged := range []bool{false, true} {
		patch, err := repo.Patch(context.Background(), "file.txt", staged)
		if err != nil || patch.Supported || patch.Text != "" || len(patch.Hunks) != 0 {
			t.Fatalf("unborn patch: %+v, %v", patch, err)
		}
		if err := repo.ApplyHunk(context.Background(), "file.txt", staged, patch.Token, 0); err == nil {
			t.Fatal("unborn patch accepted")
		}
	}
}

func TestPatchRejectsConflict(t *testing.T) {
	repo, dir := conflictFixture(t)
	ctx := context.Background()
	if err := repo.Merge(ctx, "topic"); err == nil {
		t.Fatal("expected conflict")
	}
	before := git(t, dir, "ls-files", "--stage")
	for _, staged := range []bool{false, true} {
		patch, err := repo.Patch(ctx, "conflict.txt", staged)
		if err != nil || patch.Supported {
			t.Fatalf("conflicted patch: %+v, %v", patch, err)
		}
		if err := repo.ApplyHunk(ctx, "conflict.txt", staged, patch.Token, 0); err == nil {
			t.Fatal("conflicted patch accepted")
		}
	}
	if got := git(t, dir, "ls-files", "--stage"); got != before {
		t.Fatal("conflict stages changed")
	}
}

func TestPatchValidatesPathAndBindsToken(t *testing.T) {
	repo, dir, base, changed := patchFixture(t, "file.txt")
	ctx := context.Background()
	patch, err := repo.Patch(ctx, "file.txt", false)
	if err != nil || !patch.Supported {
		t.Fatalf("patch: %+v, %v", patch, err)
	}
	for _, path := range []string{"", ".", "..", "../file.txt", filepath.Join(dir, "file.txt"), ".git/config", ".GIT/config", "file\x00.txt", "dir/../file.txt"} {
		if _, err := repo.Patch(ctx, path, false); err == nil {
			t.Errorf("invalid path accepted: %q", path)
		}
		if err := repo.ApplyHunk(ctx, path, false, patch.Token, 0); err == nil {
			t.Errorf("invalid apply path accepted: %q", path)
		}
	}
	for _, id := range []int{-1, 2, 100} {
		if err := repo.ApplyHunk(ctx, "file.txt", false, patch.Token, id); err == nil {
			t.Errorf("invalid hunk accepted: %d", id)
		}
	}
	for _, token := range []string{"", "--cached", "forged", patch.Token + "0"} {
		if err := repo.ApplyHunk(ctx, "file.txt", false, token, 0); err == nil {
			t.Errorf("invalid token accepted: %q", token)
		}
	}
	if err := repo.ApplyHunk(ctx, "file.txt", true, patch.Token, 0); err == nil {
		t.Fatal("token accepted for other direction")
	}
	write(t, dir, "other.txt", base)
	git(t, dir, "add", "--", "other.txt")
	git(t, dir, "commit", "-m", "other file")
	write(t, dir, "other.txt", changed)
	patch, err = repo.Patch(ctx, "file.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyHunk(ctx, "other.txt", false, patch.Token, 0); err == nil {
		t.Fatal("token accepted for another path")
	}
	if got := git(t, dir, "diff", "--cached"); got != "" {
		t.Fatalf("rejected input changed index: %s", got)
	}
}

func TestPatchLiteralPathsAndMissingNewline(t *testing.T) {
	for _, path := range []string{"--cached", ":(glob)*.txt", "你好 space.txt", "quote\"tab\tname.txt", "line\nname.txt"} {
		t.Run(path, func(t *testing.T) {
			repo, dir, _, _ := patchFixture(t, path)
			ctx := context.Background()
			patch, err := repo.Patch(ctx, path, false)
			if err != nil || !patch.Supported || len(patch.Hunks) != 2 {
				t.Fatalf("literal path patch: %+v, %v", patch, err)
			}
			if err := repo.ApplyHunk(ctx, path, false, patch.Token, 1); err != nil {
				t.Fatal(err)
			}
			if got := git(t, dir, "show", ":"+path); !strings.Contains(got, "last changed") || strings.Contains(got, "first changed") {
				t.Fatalf("wrong literal path staged: %s", got)
			}
		})
	}
	repo, dir := fixture(t)
	write(t, dir, "file.txt", "old")
	git(t, dir, "add", "--", "file.txt")
	git(t, dir, "commit", "-m", "no newline")
	write(t, dir, "file.txt", "new")
	for _, staged := range []bool{false, true} {
		patch, err := repo.Patch(context.Background(), "file.txt", staged)
		if err != nil || !patch.Supported || len(patch.Hunks) != 1 {
			t.Fatalf("missing newline: %+v, %v", patch, err)
		}
		if err := repo.ApplyHunk(context.Background(), "file.txt", staged, patch.Token, 0); err != nil {
			t.Fatal(err)
		}
		want := "new"
		if staged {
			want = "old"
		}
		if got := git(t, dir, "show", ":file.txt"); got != want {
			t.Fatalf("missing newline index: %q, want %q", got, want)
		}
	}
}
