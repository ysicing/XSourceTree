// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestHistoryBranchFilters(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	commit := func(message string) string {
		t.Helper()
		write(t, dir, "history.txt", message+"\n")
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-m", message)
		return git(t, dir, "rev-parse", "HEAD")
	}
	base := commit("needle base")
	git(t, dir, "switch", "-c", "origin/local")
	local := commit("needle local branch")
	git(t, dir, "switch", "main")
	git(t, dir, "switch", "-c", "remote-only")
	remote := commit("needle remote branch")
	git(t, dir, "update-ref", "refs/remotes/origin/remote-only", remote)
	git(t, dir, "switch", "main")
	git(t, dir, "branch", "-D", "remote-only")
	git(t, dir, "switch", "-c", "tag-only")
	tagged := commit("needle tag")
	git(t, dir, "tag", "release/one")
	git(t, dir, "switch", "main")
	git(t, dir, "branch", "-D", "tag-only")
	head := commit("needle main")
	git(t, dir, "update-ref", "refs/remotes/origin/main", head)
	git(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	for _, check := range []struct {
		name   string
		filter HistoryFilter
		want   []string
	}{
		{"all with remotes", HistoryFilter{}, []string{head, local, remote, tagged, base}},
		{"all without remotes", HistoryFilter{HideRemotes: true}, []string{head, local, tagged, base}},
		{"current with remotes", HistoryFilter{CurrentBranch: true}, []string{head, base}},
		{"current without remotes", HistoryFilter{CurrentBranch: true, HideRemotes: true}, []string{head, base}},
	} {
		t.Run(check.name, func(t *testing.T) {
			for _, query := range []string{"", "NEEDLE"} {
				for _, path := range []string{"", "history.txt"} {
					commits, err := repo.SearchHistory(ctx, query, path, 0, check.filter)
					if err != nil {
						t.Fatal(err)
					}
					var hashes []string
					for _, entry := range commits {
						hashes = append(hashes, entry.Hash)
						if check.filter.HideRemotes && strings.Contains(entry.Refs, "origin/main") {
							t.Fatalf("隐藏远程分支后仍有远程标签: %s", entry.Refs)
						}
						if entry.Hash == local && !strings.Contains(entry.Refs, "origin/local") {
							t.Fatalf("误隐藏含 / 的本地分支: %s", entry.Refs)
						}
						if entry.Hash == head && !check.filter.HideRemotes && !strings.Contains(entry.Refs, "origin/main") {
							t.Fatalf("显示远程分支时缺少远程标签: %s", entry.Refs)
						}
					}
					slices.Sort(hashes)
					want := slices.Clone(check.want)
					slices.Sort(want)
					if !reflect.DeepEqual(hashes, want) {
						t.Fatalf("query=%q path=%q: %v, want %v", query, path, hashes, want)
					}
					page, err := repo.SearchHistory(ctx, query, path, 1, check.filter)
					if err != nil || !reflect.DeepEqual(page, commits[1:]) {
						t.Fatalf("筛选分页不一致: %v, %v", page, err)
					}
				}
			}
		})
	}
	git(t, dir, "merge", "-s", "ours", "--no-ff", "origin/local", "-m", "merged local history")
	commits, err := repo.SearchHistory(ctx, "", "", 0, HistoryFilter{CurrentBranch: true, HideRemotes: true})
	if err != nil || len(commits) != 4 {
		t.Fatalf("当前分支遗漏已合并的历史: %v, %v", commits, err)
	}
	git(t, dir, "switch", "--detach", local)
	commits, err = repo.SearchHistory(ctx, "", "", 0, HistoryFilter{CurrentBranch: true})
	if err != nil || len(commits) != 2 || commits[0].Hash != local || commits[1].Hash != base {
		t.Fatalf("游离 HEAD 历史错误: %v, %v", commits, err)
	}
	git(t, dir, "switch", "--orphan", "unborn")
	for _, filter := range []HistoryFilter{{CurrentBranch: true}, {CurrentBranch: true, HideRemotes: true}} {
		for _, query := range []string{"", "needle"} {
			commits, err := repo.SearchHistory(ctx, query, "", 0, filter)
			if err != nil || commits == nil || len(commits) != 0 {
				t.Fatalf("无提交的当前分支应为空: %v, %v", commits, err)
			}
		}
	}
	commits, err = repo.SearchHistory(ctx, "", "", 0, HistoryFilter{})
	if err != nil || len(commits) != 6 {
		t.Fatalf("未创建首提交时应仍能查看其他分支: %v, %v", commits, err)
	}
}
