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
)

func loadingBenchmarkRepo(benchmark *testing.B, commits, files int) (*Repository, string, string) {
	benchmark.Helper()
	path := benchmark.TempDir()
	command := func(input string, args ...string) string {
		benchmark.Helper()
		process := exec.Command("git", append([]string{"-C", path}, args...)...)
		process.Env = append(gitEnvironment(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		process.Stdin = strings.NewReader(input)
		output, err := process.CombinedOutput()
		if err != nil {
			benchmark.Fatalf("git %v: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	command("", "init", "-b", "main")
	var stream strings.Builder
	for index := 0; index < commits; index++ {
		fmt.Fprintf(&stream, "commit refs/heads/main\nmark :%d\ncommitter Benchmark <benchmark@example.test> %d +0000\ndata %d\ncommit %05d\n",
			index+1, 1700000000+index, len(fmt.Sprintf("commit %05d", index)), index)
		if index > 0 {
			fmt.Fprintf(&stream, "from :%d\n", index)
		}
		for file := 0; file < files; file++ {
			content := fmt.Sprintf("version %d file %d\n", index, file)
			fmt.Fprintf(&stream, "M 100644 inline file-%05d.txt\ndata %d\n%s", file, len(content), content)
		}
		stream.WriteByte('\n')
	}
	command(stream.String(), "fast-import", "--quiet")
	repo, err := Open(context.Background(), path)
	if err != nil {
		benchmark.Fatal(err)
	}
	return repo, command("", "rev-parse", "HEAD~1"), command("", "rev-parse", "HEAD")
}

func BenchmarkRevisionFileLoading(benchmark *testing.B) {
	for _, count := range []int{100, 5000} {
		benchmark.Run(fmt.Sprint(count), func(benchmark *testing.B) {
			repo, base, target := loadingBenchmarkRepo(benchmark, 2, count)
			for _, compare := range []bool{false, true} {
				name := "Commit"
				if compare {
					name = "Comparison"
				}
				benchmark.Run(name, func(benchmark *testing.B) {
					benchmark.ReportAllocs()
					benchmark.ResetTimer()
					for benchmark.Loop() {
						var err error
						if compare {
							_, err = repo.CompareFileDiff(context.Background(), base, target, "file-00000.txt")
						} else {
							_, err = repo.CommitFileDiff(context.Background(), target, "file-00000.txt")
						}
						if err != nil {
							benchmark.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func BenchmarkHistoryLoading(benchmark *testing.B) {
	repo, _, _ := loadingBenchmarkRepo(benchmark, 10000, 1)
	for _, check := range []struct {
		name, query string
		skip        int
	}{
		{"FirstPage", "", 0},
		{"LaterPage", "", 9000},
		{"Search", "commit 00000", 0},
	} {
		benchmark.Run(check.name, func(benchmark *testing.B) {
			benchmark.ReportAllocs()
			for benchmark.Loop() {
				if _, err := repo.SearchHistory(context.Background(), check.query, "", check.skip, HistoryFilter{}); err != nil {
					benchmark.Fatal(err)
				}
			}
		})
	}
}
