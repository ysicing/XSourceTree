// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPushBranchRejectsStaleAndUnsafeRequests(t *testing.T) {
	for _, change := range []string{"head", "branch", "remote", "mirror", "multiple-urls", "invalid-name", "canceled"} {
		t.Run(change, func(t *testing.T) {
			repo, path := fixture(t)
			ctx := context.Background()
			remote := t.TempDir()
			git(t, remote, "init", "--bare", "-b", "main")
			git(t, path, "commit", "--allow-empty", "-m", "base")
			git(t, path, "remote", "add", "origin", remote)
			if change == "mirror" {
				git(t, path, "config", "remote.origin.mirror", "true")
			}
			if change == "multiple-urls" {
				git(t, path, "config", "--add", "remote.origin.pushurl", remote)
				git(t, path, "config", "--add", "remote.origin.pushurl", filepath.Join(t.TempDir(), "unexpected.git"))
			}
			configs, err := repo.RemoteConfigs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			request := PushBranchRequest{
				Remote: "origin", Token: configs[0].Token, Name: "main",
				Branch: "main", Head: git(t, path, "rev-parse", "HEAD"),
			}
			switch change {
			case "head":
				git(t, path, "commit", "--allow-empty", "-m", "new")
			case "branch":
				git(t, path, "switch", "-c", "other")
			case "remote":
				git(t, path, "config", "remote.origin.url", t.TempDir())
			case "invalid-name":
				request.Name = "-unsafe"
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := repo.PushBranch(ctx, request); err == nil {
				t.Fatal("accepted unsafe request")
			}
			if got := git(t, remote, "for-each-ref", "--format=%(refname)"); got != "" {
				t.Fatal("rejected push changed remote:", got)
			}
		})
	}
}

func TestPushBranchUsesExplicitRefAndPreservesTracking(t *testing.T) {
	repo, path := fixture(t)
	ctx := context.Background()
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-b", "main")
	git(t, path, "commit", "--allow-empty", "-m", "base")
	git(t, path, "remote", "add", "origin", remote)
	git(t, path, "branch", "other")
	git(t, path, "tag", "-a", "local-only", "-m", "local tag")
	git(t, path, "config", "remote.origin.push", "+refs/heads/*:refs/heads/*")
	git(t, path, "config", "push.followTags", "true")
	git(t, path, "config", "push.autoSetupRemote", "true")
	configs, _ := repo.RemoteConfigs(ctx)
	request := PushBranchRequest{Remote: "origin", Token: configs[0].Token, Name: "release", Branch: "main", Head: git(t, path, "rev-parse", "HEAD")}
	if err := repo.PushBranch(ctx, request); err != nil {
		t.Fatal(err)
	}
	if got := git(t, remote, "for-each-ref", "--format=%(refname)"); got != "refs/heads/release" {
		t.Fatal("push affected unexpected refs:", got)
	}
	if got := git(t, path, "for-each-ref", "--format=%(upstream)", "refs/heads/main"); got != "" {
		t.Fatal("push silently established tracking:", got)
	}
	git(t, path, "commit", "--allow-empty", "-m", "remote advance")
	git(t, path, "push", "origin", "main:release")
	remoteHead := git(t, remote, "rev-parse", "release")
	git(t, path, "commit", "--allow-empty", "--amend", "-m", "local divergence")
	request.Head = git(t, path, "rev-parse", "HEAD")
	if err := repo.PushBranch(ctx, request); err == nil {
		t.Fatal("force-pushed non-fast-forward history")
	}
	if got := git(t, remote, "rev-parse", "release"); got != remoteHead {
		t.Fatal("push rewrote remote history")
	}
}
