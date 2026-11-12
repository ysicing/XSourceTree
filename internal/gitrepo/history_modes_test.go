// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestHistorySearchModesAndDates(t *testing.T) {
	repo, dir := fixture(t)
	commit := func(subject, author, date string) string {
		t.Helper()
		git(t, dir, "add", ".")
		cmd := exec.Command("git", "-C", dir, "commit", "--allow-empty", "--author="+author, "-m", subject)
		cmd.Env = append(gitEnvironment(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE=2026-11-12T12:00:00Z")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit: %v %s", err, output)
		}
		return git(t, dir, "rev-parse", "HEAD")
	}
	write(t, dir, "file.txt", "old\n")
	base := commit("base", "Base User <base@example.test>", "2026-10-01T12:00:00Z")
	write(t, dir, "file.txt", "old\nNeedle [literal].*\n")
	changed := commit("ordinary title\n\nHidden Message", "Alice <needle@example.test>", "2026-11-10T12:00:00Z")
	latest := commit("Needle in title", "Bob <bob@example.test>", "2026-11-11T12:00:00Z")
	git(t, dir, "branch", "feature/Needle", changed)
	git(t, dir, "update-ref", "refs/remotes/origin/remote-only", latest)
	cases := []struct {
		mode, query, since, until, file string
		want                            []string
	}{
		{"message", "NEEDLE", "", "", "", []string{latest}},
		{"message", "hidden message", "", "", "", []string{changed}},
		{"author", "needle", "", "", "", []string{changed}},
		{"author", "BOB", "", "", "", []string{latest}},
		{"hash", strings.ToUpper(changed[:12]), "", "", "", []string{changed}},
		{"hash", "Needle", "", "", "", nil},
		{"branch", "FEATURE/needle", "", "", "", []string{changed, base}},
		{"branch", "--all", "", "", "", nil},
		{"branch", "remote-only", "", "", "", []string{latest, changed, base}},
		{"changes", "NEEDLE [literal].*", "", "", "", []string{changed}},
		{"changes", "Needle in title", "", "", "", nil},
		{"changes", "[literal].*", "", "", "missing.txt", nil},
		{"", "Needle", "", "", "", []string{latest, changed}},
		{"", "", "2026-11-10", "2026-11-10", "", []string{changed}},
		{"author", "needle", "2026-11-11", "", "", nil},
		{"message", "", "", "2026-10-01", "", []string{base}},
	}
	for index, check := range cases {
		t.Run(fmt.Sprint(index, "/", check.mode), func(t *testing.T) {
			filter := HistoryFilter{Mode: check.mode, Since: check.since, Until: check.until}
			commits, err := repo.SearchHistory(context.Background(), check.query, check.file, 0, filter)
			if err != nil || len(commits) != len(check.want) {
				t.Fatalf("search %+v: %v %v", check, commits, err)
			}
			for index, hash := range check.want {
				if commits[index].Hash != hash {
					t.Fatalf("wrong result order: %+v", commits)
				}
			}
		})
	}
	commits, err := repo.SearchHistory(context.Background(), "remote-only", "", 0, HistoryFilter{Mode: "branch", HideRemotes: true})
	if err != nil || len(commits) != 0 {
		t.Fatalf("remote visibility ignored: %+v %v", commits, err)
	}
	for _, filter := range []HistoryFilter{
		{Mode: "invalid"}, {Since: "yesterday"}, {Until: "2026-02-30"},
		{Since: "2026-11-11", Until: "2026-11-10"},
	} {
		if _, err := repo.SearchHistory(context.Background(), "", "", 0, filter); err == nil {
			t.Fatalf("invalid filter accepted: %+v", filter)
		}
	}
}

func TestHistorySearchAuthorDateLocalBoundaries(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("test", 8*60*60)
	t.Cleanup(func() { time.Local = old })
	for _, date := range []string{"2026-11-09T16:00:00Z", "2026-11-10T15:59:59Z"} {
		writer := newHistoryLogWriter(context.Background(), "", 0, func() {})
		writer.filter = HistoryFilter{Since: "2026-11-10", Until: "2026-11-10"}
		record := strings.Replace(historyStreamRecord(1, "date boundary", ""), "2026-11-10T00:00:00Z", date, 1)
		if _, err := writer.Write([]byte(record)); err != nil || len(writer.commits) != 1 {
			t.Fatalf("inclusive local date boundary %s: %+v %v", date, writer.commits, err)
		}
	}
}

func TestHistorySearchModesPagination(t *testing.T) {
	for _, mode := range []string{"message", "hash", "author"} {
		writer := newHistoryLogWriter(context.Background(), "needle", 100, func() {})
		writer.filter.Mode = mode
		for index := 1; index <= 205; index++ {
			record := historyStreamRecord(index, "needle", "needle")
			if mode == "author" {
				record = strings.Replace(record, "Archive User", "needle", 1)
			}
			if mode == "hash" {
				writer.query = "0"
			}
			if _, err := writer.Write([]byte(record)); err != nil {
				t.Fatal(err)
			}
		}
		if len(writer.commits) != 100 || writer.commits[0].Hash != fmt.Sprintf("%040x", 101) {
			t.Fatalf("pagination applied before matching: %s %+v", mode, writer.commits)
		}
	}
}

func TestHistorySearchChangesIncludesFirstParentMerge(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "base.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "checkout", "-b", "topic")
	write(t, dir, "topic.txt", "Merge [literal].* content\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "topic change")
	topic := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "main")
	git(t, dir, "merge", "--no-ff", "topic", "-m", "merge topic")
	merge := git(t, dir, "rev-parse", "HEAD")
	commits, err := repo.SearchHistory(context.Background(), "MERGE [literal].*", "topic.txt", 0, HistoryFilter{Mode: "changes"})
	if err != nil || len(commits) != 2 || commits[0].Hash != merge || commits[1].Hash != topic {
		t.Fatalf("未按第一父提交匹配合并内容: %+v %v", commits, err)
	}
}
