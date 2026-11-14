// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// legacySnapshot 按改为单次 porcelain v2 之前的多条命令读取状态，作为对照基准。
func legacySnapshot(t *testing.T, dir string) Snapshot {
	t.Helper()
	state := Snapshot{}
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", "HEAD").Output(); err == nil {
		state.Head = strings.TrimSpace(string(out))
	}
	state.Unborn = state.Head == ""
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--quiet", "--short", "HEAD").Output()
	state.Detached, state.Branch = err != nil, strings.TrimSpace(string(out))
	if state.Detached {
		state.Branch = state.Head[:8]
	}
	state.Files = []File{}
	records := strings.Split(git(t, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"), "\x00")
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" {
			continue
		}
		file := File{Path: record[3:], Index: record[:1], Working: record[1:2]}
		status := record[:2]
		file.Conflict = strings.Contains(status, "U") || status == "AA" || status == "DD"
		if strings.ContainsAny(status, "RC") {
			index++
			file.OriginalPath = records[index]
		}
		state.Files = append(state.Files, file)
	}
	if !state.Unborn && !state.Detached {
		if out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}").Output(); err == nil {
			state.Upstream = strings.TrimSpace(string(out))
			fields := strings.Fields(git(t, dir, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"))
			state.Ahead, _ = strconv.Atoi(fields[0])
			state.Behind, _ = strconv.Atoi(fields[1])
		}
	}
	return state
}

func TestSnapshotMatchesLegacyStatusCommands(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	check := func(name string) {
		t.Helper()
		got, err := repo.Snapshot(ctx)
		if err != nil {
			t.Fatal(name, err)
		}
		want := legacySnapshot(t, dir)
		got.Operation, got.Name, got.Path, got.Remotes = Operation{}, "", "", nil
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s:\n got %+v\nwant %+v", name, got, want)
		}
	}
	check("unborn")
	write(t, dir, "a file.txt", "one\n")
	write(t, dir, "rename-me.txt", strings.Repeat("rename content\n", 20))
	write(t, dir, "conflict.txt", "base\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	upstream := t.TempDir()
	git(t, upstream, "init", "--bare")
	git(t, dir, "remote", "add", "origin", upstream)
	git(t, dir, "push", "-u", "origin", "main")
	check("clean with upstream")

	write(t, dir, "a file.txt", "two\n")
	git(t, dir, "mv", "rename-me.txt", "renamed with space.txt")
	write(t, dir, "new dir/untracked 文件.txt", "new\n")
	git(t, dir, "commit", "-m", "ahead", "--", "a file.txt")
	write(t, dir, "a file.txt", "three\n")
	git(t, dir, "add", "a file.txt")
	write(t, dir, "a file.txt", "four\n")
	check("ahead with staged rename, mixed and untracked changes")

	git(t, dir, "push", "origin", "HEAD:refs/heads/other")
	git(t, dir, "branch", "--set-upstream-to=origin/other")
	git(t, dir, "push", "origin", "--delete", "other")
	git(t, dir, "fetch", "--prune", "origin")
	check("upstream gone")

	git(t, dir, "stash", "push", "--include-untracked")
	git(t, dir, "branch", "--set-upstream-to=origin/main")
	git(t, dir, "switch", "-c", "side", "HEAD~1")
	write(t, dir, "conflict.txt", "side\n")
	git(t, dir, "commit", "-am", "side")
	git(t, dir, "switch", "main")
	write(t, dir, "conflict.txt", "main\n")
	git(t, dir, "commit", "-am", "main")
	if err := exec.Command("git", "-C", dir, "merge", "side").Run(); err == nil {
		t.Fatal("expected merge conflict")
	}
	check("conflict")

	git(t, dir, "merge", "--abort")
	git(t, dir, "switch", "--detach", "HEAD~1")
	check("detached")
}

func TestSnapshotBranchIgnoresSameNamedTag(t *testing.T) {
	repo, dir := fixture(t)
	git(t, dir, "commit", "--allow-empty", "-m", "base")
	git(t, dir, "switch", "-c", "a/b/c")
	git(t, dir, "tag", "a/b/c")
	// symbolic-ref --short 会消歧为 heads/a/b/c，推送时被当成分支名而无法匹配当前分支。
	state, err := repo.Snapshot(context.Background())
	if err != nil || state.Branch != "a/b/c" {
		t.Fatalf("branch = %q, %v", state.Branch, err)
	}
}

func TestPathFilesMatchFullStatus(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	for _, name := range []string{"modified.txt", "deleted.txt", "rename-me.txt", "staged.txt", "mode.sh", "dir/nested.txt"} {
		write(t, dir, name, strings.Repeat(name+"\n", 20))
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "base")
	write(t, dir, "modified.txt", "changed\n")
	write(t, dir, "dir/nested.txt", "changed\n")
	if err := exec.Command("rm", dir+"/deleted.txt").Run(); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "mv", "rename-me.txt", "renamed.txt")
	write(t, dir, "staged.txt", "staged\n")
	git(t, dir, "add", "staged.txt")
	write(t, dir, "staged.txt", "staged then edited\n")
	git(t, dir, "update-index", "--chmod=+x", "mode.sh")
	write(t, dir, "intent.txt", strings.Repeat("modified.txt\n", 20))
	git(t, dir, "add", "--intent-to-add", "intent.txt")
	write(t, dir, "new dir/untracked.txt", "new\n")
	full, err := repo.files(ctx)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"rename-me.txt", "dir", "missing.txt"}
	for _, file := range full {
		paths = append(paths, file.Path)
	}
	for _, path := range paths {
		got, err := repo.pathFiles(ctx, []string{path})
		if err != nil {
			t.Fatal(path, err)
		}
		var want, found *File
		for index := range full {
			if full[index].Path == path {
				want = &full[index]
			}
		}
		for index := range got {
			if got[index].Path == path {
				found = &got[index]
			}
		}
		if !reflect.DeepEqual(want, found) {
			t.Fatalf("%s: got %+v, want %+v", path, found, want)
		}
	}
}

func TestRemoteConfigsMatchSingleRemoteTokens(t *testing.T) {
	repo, dir := fixture(t)
	ctx := context.Background()
	if configs, err := repo.RemoteConfigs(ctx); err != nil || len(configs) != 0 {
		t.Fatalf("empty remotes: %+v %v", configs, err)
	}
	git(t, dir, "config", "remote.pushDefault", "origin")
	git(t, dir, "remote", "add", "origin", "https://example.test/a.git")
	git(t, dir, "config", "--add", "remote.origin.url", "https://example.test/b.git")
	git(t, dir, "config", "remote.origin.pushurl", "short:push")
	git(t, dir, "config", "url.ssh://push.example.test/.pushInsteadOf", "short:")
	git(t, dir, "remote", "add", "a", "/tmp/a")
	git(t, dir, "remote", "add", "a.b", "/tmp/a.b")
	git(t, dir, "config", "remote.Upper.url", "/tmp/upper")
	git(t, dir, "config", "remote.fetchonly.fetch", "+refs/heads/*:refs/remotes/fetchonly/*")
	configs, err := repo.RemoteConfigs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, config := range configs {
		names = append(names, config.Name)
		single, err := repo.remoteConfig(ctx, config.Name)
		if err != nil {
			t.Fatal(config.Name, err)
		}
		if !reflect.DeepEqual(config, single) {
			t.Fatalf("%s:\nlist   %+v\nsingle %+v", config.Name, config, single)
		}
	}
	if want := strings.Fields(git(t, dir, "remote")); !reflect.DeepEqual(names, want) {
		t.Fatalf("remote names = %v, want %v", names, want)
	}
}
