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

func TestApplyLinesSelectsChangesAcrossHunks(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(fmt.Sprint(staged), func(t *testing.T) {
			repo, dir, base, changed := patchFixture(t, "file.txt")
			if staged {
				git(t, dir, "add", "file.txt")
			}
			view, err := repo.Patch(context.Background(), "file.txt", staged)
			if err != nil {
				t.Fatal(err)
			}
			// 只选第一个替换中的新增行，以及第二个替换的删除行。
			if err := repo.ApplyLines(context.Background(), "file.txt", staged, view.Token, []int{1, 3}); err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(base, "line 03\n", "line 03\nfirst changed\n", 1)
			want = strings.Replace(want, "line 30\n", "", 1)
			if staged {
				want = strings.Replace(changed, "first changed\n", "", 1)
				want = strings.Replace(want, "last changed\n", "line 30\nlast changed\n", 1)
			}
			if got := git(t, dir, "show", ":file.txt"); got != strings.TrimSuffix(want, "\n") {
				t.Fatalf("index = %q, want %q", got, want)
			}
			content, _ := os.ReadFile(filepath.Join(dir, "file.txt"))
			if string(content) != changed {
				t.Fatal("working tree changed")
			}
			if err := repo.ApplyLines(context.Background(), "file.txt", staged, view.Token, []int{0}); err == nil {
				t.Fatal("stale token accepted")
			}
		})
	}
}

func TestApplyLinesRejectsInvalidSelection(t *testing.T) {
	repo, dir, base, _ := patchFixture(t, "file.txt")
	view, _ := repo.Patch(context.Background(), "file.txt", false)
	for _, ids := range [][]int{nil, {}, {-1}, {5}, {0, 0}, {0, 999}} {
		if err := repo.ApplyLines(context.Background(), "file.txt", false, view.Token, ids); err == nil {
			t.Fatalf("accepted %v", ids)
		}
		if got := git(t, dir, "show", ":file.txt"); got != strings.TrimSuffix(base, "\n") {
			t.Fatal("invalid selection modified index")
		}
	}
}

func TestApplyLinesPreservesRawEndings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", ""} {
		for _, staged := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/%t", ending, staged), func(t *testing.T) {
				repo, dir := fixture(t)
				base, changed := "first\r\nold"+ending, "first\r\nnew"+ending
				write(t, dir, "file", base)
				git(t, dir, "add", "file")
				git(t, dir, "commit", "-m", "base")
				write(t, dir, "file", changed)
				if staged {
					git(t, dir, "add", "file")
				}
				view, _ := repo.Patch(context.Background(), "file", staged)
				if err := repo.ApplyLines(context.Background(), "file", staged, view.Token, []int{0, 1}); err != nil {
					t.Fatal(err)
				}
				want := changed
				if staged {
					want = base
				}
				got, err := repo.git(context.Background(), "show", ":file")
				if err != nil || got != want {
					t.Fatalf("index = %q, want %q, err %v", got, want, err)
				}
			})
		}
	}
}

func TestApplyLinesRejectsJoiningUnterminatedLines(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "file", "old")
	git(t, dir, "add", "file")
	git(t, dir, "commit", "-m", "base")
	write(t, dir, "file", "new")
	view, _ := repo.Patch(context.Background(), "file", false)
	if err := repo.ApplyLines(context.Background(), "file", false, view.Token, []int{1}); err == nil {
		t.Fatal("partial selection silently joined lines")
	}
}

func TestApplyLinesCRLFChineseLiteralPath(t *testing.T) {
	for _, normalize := range []bool{false, true} {
		for _, staged := range []bool{false, true} {
			t.Run(fmt.Sprintf("normalize=%t/staged=%t", normalize, staged), func(t *testing.T) {
				repo, dir := fixture(t)
				ctx := context.Background()
				path := "目录/中文[1] & 文件.txt"
				decoy := "目录/中文1 & 文件.txt"
				git(t, dir, "config", "core.autocrlf", "true")
				attributes := "*.txt -text\n"
				ending := "\r\n"
				if normalize {
					attributes = "*.txt text eol=crlf\n"
					ending = "\n"
				}
				write(t, dir, ".gitattributes", attributes)
				base := "原始首行\r\n" + strings.Repeat("上下文\r\n", 12) + "原始末行\r\n"
				changed := strings.ReplaceAll(base, "原始", "修改")
				write(t, dir, path, base)
				write(t, dir, decoy, "相邻文件\r\n")
				git(t, dir, "add", ".")
				git(t, dir, "commit", "-m", "CRLF Chinese paths")
				head := git(t, dir, "rev-parse", "HEAD")
				write(t, dir, path, changed)
				write(t, dir, decoy, "未选择的修改\r\n")
				if staged {
					if err := repo.Stage(ctx, []string{path}); err != nil {
						t.Fatal(err)
					}
				}
				view, err := repo.Patch(ctx, path, staged)
				if err != nil || !view.Supported || len(view.Hunks) != 2 {
					t.Fatalf("CRLF patch: %+v, %v", view, err)
				}
				before := git(t, dir, "write-tree")
				for _, unsafe := range []string{"../中文.txt", ".git/config", ".GiT/config", decoy} {
					if err := repo.ApplyLines(ctx, unsafe, staged, view.Token, []int{0, 1}); err == nil {
						t.Fatalf("token accepted for path %q", unsafe)
					}
				}
				if got := git(t, dir, "write-tree"); got != before {
					t.Fatal("rejected path changed index")
				}
				// 只处理首个替换；末行变更继续留在原来一侧。
				if err := repo.ApplyLines(ctx, path, staged, view.Token, []int{0, 1}); err != nil {
					t.Fatal(err)
				}
				want := strings.Replace(base, "原始首行", "修改首行", 1)
				if staged {
					want = strings.Replace(changed, "修改首行", "原始首行", 1)
				}
				want = strings.ReplaceAll(want, "\r\n", ending)
				got, err := repo.git(ctx, "show", ":"+path)
				if err != nil || got != want {
					t.Fatalf("index bytes = %q, want %q, err %v", got, want, err)
				}
				got, err = repo.git(ctx, "show", ":"+decoy)
				if err != nil || got != "相邻文件"+ending {
					t.Fatalf("literal path changed adjacent index entry: %q, %v", got, err)
				}
				for name, want := range map[string]string{path: changed, decoy: "未选择的修改\r\n"} {
					data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
					if err != nil || string(data) != want {
						t.Fatalf("working bytes for %q = %q, want %q, err %v", name, data, want, err)
					}
				}
				if err := repo.ApplyLines(ctx, path, staged, view.Token, []int{0, 1}); err == nil {
					t.Fatal("stale CRLF patch accepted")
				}
				if got := git(t, dir, "rev-parse", "HEAD"); got != head {
					t.Fatal("line staging changed HEAD")
				}
			})
		}
	}
}

func TestApplyLinesPreservesExecutableIndexModeWhenFileModeIgnored(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(fmt.Sprint(staged), func(t *testing.T) {
			repo, dir := fixture(t)
			ctx := context.Background()
			git(t, dir, "config", "core.filemode", "false")
			write(t, dir, "script", "old\ncontext\n")
			git(t, dir, "add", "script")
			// 直接设置 Git 模式，不依赖宿主文件系统能否提供 Unix 执行位。
			git(t, dir, "update-index", "--chmod=+x", "--", "script")
			git(t, dir, "commit", "-m", "executable index entry")
			write(t, dir, "script", "new\ncontext\n")
			if staged {
				if err := repo.Stage(ctx, []string{"script"}); err != nil {
					t.Fatal(err)
				}
			}
			view, err := repo.Patch(ctx, "script", staged)
			if err != nil || !view.Supported {
				t.Fatalf("executable content patch: %+v, %v", view, err)
			}
			if err := repo.ApplyLines(ctx, "script", staged, view.Token, []int{0, 1}); err != nil {
				t.Fatal(err)
			}
			want := "new\ncontext\n"
			if staged {
				want = "old\ncontext\n"
			}
			got, err := repo.git(ctx, "show", ":script")
			if err != nil || got != want {
				t.Fatalf("executable index bytes: %q, want %q, %v", got, want, err)
			}
			if got := git(t, dir, "ls-files", "--stage", "--", "script"); !strings.HasPrefix(got, "100755 ") {
				t.Fatalf("content staging lost executable index mode: %s", got)
			}
			working, err := os.ReadFile(filepath.Join(dir, "script"))
			if err != nil || string(working) != "new\ncontext\n" {
				t.Fatalf("working content changed: %q, %v", working, err)
			}
		})
	}
}
