// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"slices"
	"testing"
)

func TestBranchesIdentifyDefault(t *testing.T) {
	for _, test := range []struct {
		name     string
		main     string
		heads    map[string]string
		expected []string
	}{
		{"main-fallback", "main", nil, []string{"main"}},
		{"master-fallback", "master", nil, []string{"master"}},
		{"custom-without-metadata", "trunk", nil, nil},
		{"origin-head", "main", map[string]string{"origin": "release/stable", "backup": "main"}, []string{"release/stable", "origin/release/stable", "backup/main"}},
		{"custom-remote-name", "main", map[string]string{"team/mirror": "release/stable"}, []string{"release/stable", "team/mirror/release/stable"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, path := fixture(t)
			git(t, path, "commit", "--allow-empty", "-m", "base")
			if test.main != "main" {
				git(t, path, "branch", "-m", test.main)
			}
			git(t, path, "branch", "release/stable")
			git(t, path, "switch", "-c", "a-topic")
			for remote, branch := range test.heads {
				git(t, path, "update-ref", "refs/remotes/"+remote+"/"+branch, "HEAD")
				git(t, path, "symbolic-ref", "refs/remotes/"+remote+"/HEAD", "refs/remotes/"+remote+"/"+branch)
			}
			branches, err := repo.Branches(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var defaults []string
			for _, branch := range branches {
				if branch.Default {
					defaults = append(defaults, branch.Name)
				}
				if branch.Current != (branch.Name == "a-topic") {
					t.Fatalf("default detection changed current branch: %+v", branch)
				}
			}
			slices.Sort(defaults)
			slices.Sort(test.expected)
			if !slices.Equal(defaults, test.expected) {
				t.Fatalf("defaults=%v expected=%v", defaults, test.expected)
			}
		})
	}
}

func TestBranchesReadConfiguredUpstreamTarget(t *testing.T) {
	repo, path := fixture(t)
	git(t, path, "commit", "--allow-empty", "-m", "base")
	git(t, path, "remote", "add", "team/mirror", t.TempDir())
	git(t, path, "config", "remote.team/mirror.fetch", "+refs/heads/*:refs/remotes/cache/*")
	git(t, path, "update-ref", "refs/remotes/cache/release", "HEAD")
	git(t, path, "config", "branch.main.remote", "team/mirror")
	git(t, path, "config", "branch.main.merge", "refs/heads/release")
	branches, err := repo.Branches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range branches {
		if branch.Current {
			if branch.UpstreamRemote != "team/mirror" || branch.UpstreamBranch != "release" {
				t.Fatalf("lost configured upstream target: %+v", branch)
			}
			return
		}
	}
	t.Fatal("missing current branch")
}
