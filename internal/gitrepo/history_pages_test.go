// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSearchHistoryVariablePageSize(t *testing.T) {
	repo, path := fixture(t)
	var stream strings.Builder
	for index := range 1205 {
		subject := fmt.Sprintf("commit %04d", index)
		if index%2 == 0 {
			subject += " match"
		}
		fmt.Fprintf(&stream, "commit refs/heads/main\nmark :%d\ncommitter Test <test@example.test> %d +0000\ndata %d\n%s\n",
			index+1, 1800000000+index, len(subject), subject)
		if index > 0 {
			fmt.Fprintf(&stream, "from :%d\n", index)
		}
		content := fmt.Sprintf("version %d\n", index)
		fmt.Fprintf(&stream, "M 100644 inline tracked.txt\ndata %d\n%s\n", len(content), content)
	}
	process := exec.Command("git", "-C", path, "fast-import", "--quiet")
	process.Env = append(gitEnvironment(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	process.Stdin = strings.NewReader(stream.String())
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("import: %v\n%s", err, output)
	}
	day := time.Unix(1800000000, 0).In(time.Local).Format(time.DateOnly)
	for _, test := range []struct {
		name, query, file string
		filter            HistoryFilter
		count             int
	}{
		{name: "all", count: 1205},
		{name: "filtered", query: "match", file: "tracked.txt",
			filter: HistoryFilter{CurrentBranch: true, HideRemotes: true, Mode: "message", Since: day, Until: day}, count: 603},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			first, err := repo.SearchHistory(ctx, test.query, test.file, 0, test.filter)
			if err != nil || len(first) != 100 {
				t.Fatalf("default first page: count=%d err=%v", len(first), err)
			}
			seen := make(map[string]bool)
			commits := first
			for {
				for _, commit := range commits {
					if seen[commit.Hash] {
						t.Fatal("duplicate commit:", commit.Hash)
					}
					seen[commit.Hash] = true
					if test.query != "" && !strings.Contains(commit.Subject, test.query) {
						t.Fatal("pagination lost content filter")
					}
				}
				commits, err = repo.SearchHistory(ctx, test.query, test.file, len(seen), test.filter, 500)
				if err != nil {
					t.Fatal(err)
				}
				if len(commits) == 0 {
					break
				}
				if len(commits) != min(500, test.count-len(seen)) {
					t.Fatalf("unexpected batch size: %d after %d", len(commits), len(seen))
				}
			}
			if len(seen) != test.count {
				t.Fatalf("count=%d want=%d", len(seen), test.count)
			}
		})
	}
	for _, sizes := range [][]int{{0}, {-1}, {501}, {100, 500}} {
		if _, err := repo.SearchHistory(context.Background(), "", "", 0, HistoryFilter{}, sizes...); err == nil {
			t.Fatalf("accepted invalid page size: %v", sizes)
		}
	}
}
