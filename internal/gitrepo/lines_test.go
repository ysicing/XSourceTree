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
