package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteSettingsWorkflow(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	remote := filepath.Join(t.TempDir(), "remote with spaces.git")
	if err := repo.SaveRemote(ctx, "origin", remote, ""); err != nil {
		t.Fatal(err)
	}
	configs, err := repo.RemoteConfigs(ctx)
	if err != nil || len(configs) != 1 || configs[0].URLs[0] != remote || configs[0].PushURLs[0] != remote {
		t.Fatalf("remote settings: %+v %v", configs, err)
	}
	before := configs[0]
	git(t, dir, "config", "remote.origin.pushurl", remote+"-push")
	if err := repo.SaveRemote(ctx, "origin", remote+"-new", before.Token); err == nil {
		t.Fatal("accepted stale remote configuration")
	}
	configs, _ = repo.RemoteConfigs(ctx)
	if err := repo.SaveRemote(ctx, "origin", remote+"-new", configs[0].Token); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "config", "remote.origin.pushurl"); got != remote+"-push" {
		t.Fatal("editing URL changed independent push URL:", got)
	}
	if err := repo.RemoveRemote(ctx, "origin", configs[0].Token); err == nil {
		t.Fatal("removed remote with stale token")
	}
	configs, _ = repo.RemoteConfigs(ctx)
	if err := repo.RemoveRemote(ctx, "origin", configs[0].Token); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "remote"); got != "" {
		t.Fatal("remote remained:", got)
	}
}

func TestRemoteSettingsRejectUnsafeAndAmbiguousChanges(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	for _, name := range []string{"", "-origin", ".", "bad name", "bad\nname"} {
		if err := repo.SaveRemote(ctx, name, "/tmp/remote.git", ""); err == nil {
			t.Fatalf("accepted remote name %q", name)
		}
	}
	for _, address := range []string{"", "-upload-pack=evil", "https://example.test/repo\nextra", "a\x00b"} {
		if err := repo.SaveRemote(ctx, "origin", address, ""); err == nil {
			t.Fatalf("accepted remote address %q", address)
		}
	}
	if err := repo.SaveRemote(ctx, "origin", "/tmp/remote.git", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveRemote(ctx, "origin", "/tmp/overwrite.git", ""); err == nil {
		t.Fatal("overwrote an existing remote")
	}
	git(t, dir, "config", "--add", "remote.origin.url", "/tmp/second.git")
	configs, err := repo.RemoteConfigs(ctx)
	if err != nil || len(configs[0].URLs) != 2 {
		t.Fatalf("multiple URLs: %+v %v", configs, err)
	}
	if err := repo.SaveRemote(ctx, "origin", "/tmp/replacement.git", configs[0].Token); err == nil {
		t.Fatal("silently replaced multiple remote URLs")
	}
}

func TestPublishBranchAndCheckoutRemoteWorkflow(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-b", "main")
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	if err := repo.SaveRemote(ctx, "origin", remote, ""); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "branch", "other")
	git(t, dir, "config", "remote.origin.push", "+refs/heads/*:refs/heads/*")
	git(t, dir, "config", "push.followTags", "true")
	git(t, dir, "tag", "-a", "local-only", "-m", "local tag")
	configs, _ := repo.RemoteConfigs(ctx)
	state, _ := repo.Snapshot(ctx)
	request := PublishBranchRequest{
		Remote: "origin", Token: configs[0].Token, Name: "team/topic",
		Branch: state.Branch, Head: state.Head,
	}
	if err := repo.PublishBranch(ctx, request); err != nil {
		t.Fatal(err)
	}
	state, err := repo.Snapshot(ctx)
	if err != nil || state.Upstream != "origin/team/topic" || state.Ahead != 0 || state.Behind != 0 {
		t.Fatalf("published upstream: %+v %v", state, err)
	}
	if got := git(t, remote, "for-each-ref", "--format=%(refname)"); got != "refs/heads/team/topic" {
		t.Fatal("published additional branches or tags:", got)
	}
	if err := repo.CheckoutRemote(ctx, "origin/team/topic", "feature/local-copy"); err != nil {
		t.Fatal(err)
	}
	state, err = repo.Snapshot(ctx)
	if err != nil || state.Branch != "feature/local-copy" || state.Upstream != "origin/team/topic" {
		t.Fatalf("checked out tracking branch: %+v %v", state, err)
	}
}

func TestPublishRejectsStaleStateAndUnsafeDestinations(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-b", "main")
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	git(t, dir, "remote", "add", "origin", remote)
	request := func() PublishBranchRequest {
		t.Helper()
		configs, err := repo.RemoteConfigs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		state, err := repo.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return PublishBranchRequest{Remote: "origin", Token: configs[0].Token, Name: "main", Branch: state.Branch, Head: state.Head}
	}
	stale := request()
	git(t, dir, "commit", "--allow-empty", "-m", "new commit")
	if err := repo.PublishBranch(ctx, stale); err == nil {
		t.Fatal("published a different HEAD from the confirmed snapshot")
	}
	stale = request()
	git(t, dir, "switch", "-c", "topic")
	if err := repo.PublishBranch(ctx, stale); err == nil {
		t.Fatal("published after switching branch")
	}
	stale = request()
	git(t, dir, "config", "remote.origin.mirror", "true")
	if err := repo.PublishBranch(ctx, stale); err == nil {
		t.Fatal("published after remote configuration changed")
	}
	if err := repo.PublishBranch(ctx, request()); err == nil {
		t.Fatal("accepted mirror remote")
	}
	git(t, dir, "config", "--unset", "remote.origin.mirror")
	git(t, dir, "config", "--add", "remote.origin.pushurl", remote)
	git(t, dir, "config", "--add", "remote.origin.pushurl", filepath.Join(t.TempDir(), "unexpected.git"))
	if err := repo.PublishBranch(ctx, request()); err == nil {
		t.Fatal("accepted multiple push destinations")
	}
	if got := git(t, remote, "for-each-ref", "--format=%(refname)"); got != "" {
		t.Fatal("rejected publish changed remote refs:", got)
	}
}

func TestPublishRejectsNonFastForwardAndKeepsUpstreamUnset(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-b", "main")
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	git(t, dir, "remote", "add", "origin", remote)
	git(t, dir, "push", "origin", "main")
	git(t, dir, "commit", "--allow-empty", "-m", "remote advance")
	git(t, dir, "push", "origin", "main")
	original := git(t, remote, "rev-parse", "refs/heads/main")
	git(t, dir, "switch", "-c", "diverged", "HEAD~1")
	git(t, dir, "commit", "--allow-empty", "-m", "local divergence")
	configs, _ := repo.RemoteConfigs(ctx)
	state, _ := repo.Snapshot(ctx)
	err := repo.PublishBranch(ctx, PublishBranchRequest{Remote: "origin", Token: configs[0].Token, Name: "main", Branch: state.Branch, Head: state.Head})
	if err == nil {
		t.Fatal("force-published diverged history")
	}
	if got := git(t, remote, "rev-parse", "refs/heads/main"); got != original {
		t.Fatal("rejected push rewrote remote")
	}
	state, _ = repo.Snapshot(ctx)
	if state.Upstream != "" {
		t.Fatal("failed push established upstream:", state.Upstream)
	}
}

func TestCheckoutRemotePreservesChangesAndRejectsSymbolicRefs(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	write(t, dir, "file", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	git(t, dir, "remote", "add", "origin", t.TempDir())
	git(t, dir, "switch", "-c", "source")
	write(t, dir, "file", "remote version\n")
	git(t, dir, "commit", "-am", "remote version")
	git(t, dir, "update-ref", "refs/remotes/origin/topic", "HEAD")
	git(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/topic")
	git(t, dir, "switch", "main")
	write(t, dir, "file", "unsaved work\n")
	for _, pair := range [][2]string{
		{"origin/topic", "local"}, {"origin/HEAD", "default"}, {"origin/missing", "missing"},
		{"origin/topic", "main"}, {"origin/topic", "-unsafe"}, {"origin/topic", "HEAD"},
		{"../heads/main", "unsafe"}, {"origin/topic", "@{-1}"},
	} {
		if err := repo.CheckoutRemote(ctx, pair[0], pair[1]); err == nil {
			t.Fatalf("accepted checkout %v", pair)
		}
	}
	content, err := os.ReadFile(filepath.Join(dir, "file"))
	if err != nil || string(content) != "unsaved work\n" {
		t.Fatalf("overwrote local changes: %q %v", content, err)
	}
	if got := git(t, dir, "branch", "--list", "local"); got != "" {
		t.Fatal("failed switch left a local branch:", got)
	}
	if got := git(t, dir, "branch", "--show-current"); got != "main" {
		t.Fatal("failed switch changed branch:", got)
	}
}

func TestRemoteTokenIncludesResolvedPushDestination(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	git(t, dir, "remote", "add", "origin", "demo:repo.git")
	configs, err := repo.RemoteConfigs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "config", "url.https://example.test/.pushInsteadOf", "demo:")
	updated, err := repo.RemoteConfigs(ctx)
	if err != nil || updated[0].PushURLs[0] != "https://example.test/repo.git" || updated[0].Token == configs[0].Token {
		t.Fatalf("resolved destination not reflected: %+v %v", updated, err)
	}
	if !strings.HasPrefix(updated[0].URLs[0], "demo:") {
		t.Fatal("configured fetch URL was replaced by resolved URL")
	}
}

func TestPublishPreservesNewlyConfiguredUpstream(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	git(t, dir, "remote", "add", "origin", t.TempDir())
	git(t, dir, "update-ref", "refs/remotes/origin/existing", "HEAD")
	configs, _ := repo.RemoteConfigs(ctx)
	state, _ := repo.Snapshot(ctx)
	git(t, dir, "branch", "--set-upstream-to=origin/existing", "main")
	err := repo.PublishBranch(ctx, PublishBranchRequest{Remote: "origin", Token: configs[0].Token, Name: "new", Branch: state.Branch, Head: state.Head})
	if err == nil {
		t.Fatal("replaced externally configured upstream")
	}
	if got := git(t, dir, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/existing" {
		t.Fatal("changed upstream:", got)
	}
}

func TestRemoteMutationsRespectCancellation(t *testing.T) {
	repo, dir := fixture(t)
	git(t, dir, "remote", "add", "origin", t.TempDir())
	configs, _ := repo.RemoteConfigs(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, mutate := range []func() error{
		func() error { return repo.SaveRemote(ctx, "new", "/tmp/remote.git", "") },
		func() error { return repo.RemoveRemote(ctx, "origin", configs[0].Token) },
		func() error { return repo.CheckoutRemote(ctx, "origin/main", "local") },
		func() error { return repo.PublishBranch(ctx, PublishBranchRequest{}) },
	} {
		if err := mutate(); err == nil {
			t.Fatal("accepted cancelled mutation")
		}
	}
	if got := git(t, dir, "remote"); got != "origin" {
		t.Fatal("cancelled operation changed configuration:", got)
	}
}
