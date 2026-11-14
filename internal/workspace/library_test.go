// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func localRepo(t *testing.T, path string) string {
	t.Helper()
	command := exec.Command("git", "init", "-b", "main", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("init: %v: %s", err, output)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func TestLibraryMigratesRecentAndKeepsMoreThanTwelveRepositories(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "recent.json")
	legacy := []Recent{{Name: "Old", Path: "/missing/old-repository"}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(settings, data, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	root := t.TempDir()
	for index := range 13 {
		paths = append(paths, localRepo(t, filepath.Join(root, string(rune('a'+index)))))
	}
	library, err := manager.Add(context.Background(), paths, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Repositories) != 14 || library.Repositories[0].Path != legacy[0].Path {
		t.Fatalf("lost repositories: %+v", library)
	}
	if _, err := manager.Get(paths[0]); err == nil {
		t.Fatal("adding to library implicitly opened repository")
	}
	reloaded, err := New(settings)
	if err != nil || len(reloaded.Library().Repositories) != 14 {
		t.Fatalf("persistence: %v", err)
	}
	if _, err := reloaded.Open(context.Background(), paths[0]); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Library().Repositories) != 14 {
		t.Fatal("opening truncated the library")
	}
}

func TestLibraryGroupsPersistAndRemovingGroupKeepsRepositories(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "recent.json")
	manager, _ := New(settings)
	ctx := context.Background()
	library, err := manager.CreateGroup("工作")
	if err != nil || len(library.Groups) != 1 {
		t.Fatalf("create group: %v", err)
	}
	group := library.Groups[0].ID
	path := localRepo(t, filepath.Join(t.TempDir(), "project"))
	if _, err := manager.Add(ctx, []string{path, filepath.Join(path, ".git", "..")}, group); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RenameGroup(group, "个人"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	library = reloaded.Library()
	if len(library.Repositories) != 1 || library.Repositories[0].GroupID != group || library.Groups[0].Name != "个人" {
		t.Fatalf("group persistence or deduplication: %+v", library)
	}
	if _, err := reloaded.Move(path, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.Move(path, group); err != nil {
		t.Fatal(err)
	}
	library, err = reloaded.RemoveGroup(group)
	if err != nil || len(library.Groups) != 0 || len(library.Repositories) != 1 || library.Repositories[0].GroupID != "" {
		t.Fatalf("remove group: %+v, %v", library, err)
	}
	if _, err := reloaded.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		t.Fatal("removing bookmark deleted repository")
	}
	if _, err := manager.CreateGroup(" 个人 "); err == nil {
		t.Fatal("accepted duplicate trimmed group name")
	}
	if _, err := manager.Add(ctx, []string{path}, "unknown"); err == nil {
		t.Fatal("accepted missing group")
	}
}

func TestLibraryFailedBatchDoesNotPartiallyPersist(t *testing.T) {
	manager, _ := New(filepath.Join(t.TempDir(), "recent.json"))
	path := localRepo(t, filepath.Join(t.TempDir(), "valid"))
	if _, err := manager.Add(context.Background(), []string{path, t.TempDir()}, ""); err == nil {
		t.Fatal("accepted invalid repository")
	}
	if len(manager.Library().Repositories) != 0 {
		t.Fatal("failed batch left partial bookmarks")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Add(ctx, []string{path}, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestScanFindsNestedRepositoriesAndWorktreesWithoutImporting(t *testing.T) {
	root := t.TempDir()
	first := localRepo(t, filepath.Join(root, "one"))
	second := localRepo(t, filepath.Join(root, "nested", "two"))
	localRepo(t, filepath.Join(root, "node_modules", "ignored"))
	localRepo(t, filepath.Join(root, ".hidden", "ignored"))
	command := exec.Command("git", "-C", first, "-c", "user.name=Test", "-c", "user.email=test@example.test", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "initial")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, output)
	}
	worktree := filepath.Join(root, "worktree")
	command = exec.Command("git", "-C", first, "worktree", "add", "-b", "linked", worktree)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("worktree: %v: %s", err, output)
	}
	manager, _ := New(filepath.Join(t.TempDir(), "recent.json"))
	result, err := manager.Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, entry := range result.Repositories {
		found[entry.Path] = true
		if _, err := manager.Get(entry.Path); err == nil {
			t.Fatal("scan implicitly authorized repository")
		}
	}
	worktree, _ = filepath.EvalSymlinks(worktree)
	if len(found) != 3 || !found[first] || !found[second] || !found[worktree] || result.Truncated {
		t.Fatalf("scan results: %+v", result)
	}
	if len(manager.Library().Repositories) != 0 {
		t.Fatal("scan imported without selection")
	}
	own, err := manager.Scan(context.Background(), second)
	if err != nil || len(own.Repositories) != 1 || own.Repositories[0].Path != second {
		t.Fatalf("scan repository root: %+v, %v", own, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Scan(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan cancellation: %v", err)
	}
}

func TestLibraryRefreshKeepsUnavailableEntriesAndGroupAssignments(t *testing.T) {
	manager, _ := New(filepath.Join(t.TempDir(), "recent.json"))
	path := localRepo(t, filepath.Join(t.TempDir(), "project"))
	library, _ := manager.CreateGroup("项目")
	group := library.Groups[0].ID
	if _, err := manager.Add(context.Background(), []string{path}, group); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "-C", path, "symbolic-ref", "HEAD", "refs/heads/feature/library")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("branch: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(path, "new.txt"), []byte("change"), 0600); err != nil {
		t.Fatal(err)
	}
	library, err := manager.RefreshLibrary(context.Background())
	if err != nil || library.Repositories[0].Branch != "feature/library" || library.Repositories[0].Changes != 1 || library.Repositories[0].GroupID != group {
		t.Fatalf("refresh: %+v, %v", library, err)
	}
	if err := os.Rename(path, path+"-moved"); err != nil {
		t.Fatal(err)
	}
	library, err = manager.RefreshLibrary(context.Background())
	if err != nil || len(library.Repositories) != 1 || library.Repositories[0].Problem == "" || library.Repositories[0].GroupID != group {
		t.Fatalf("unavailable repository: %+v, %v", library, err)
	}
}

func TestLibraryWriteFailureDoesNotChangeMemoryAndReturnedValuesAreCopies(t *testing.T) {
	manager, _ := New(filepath.Join(t.TempDir(), "recent.json"))
	library, err := manager.CreateGroup("original")
	if err != nil {
		t.Fatal(err)
	}
	library.Groups[0].Name = "changed outside"
	if manager.Library().Groups[0].Name != "original" {
		t.Fatal("caller modified persisted group through returned slice")
	}
	manager.file = t.TempDir()
	if _, err := manager.CreateGroup("failed"); err == nil {
		t.Fatal("expected settings write failure")
	}
	if len(manager.Library().Groups) != 1 {
		t.Fatal("failed write changed memory")
	}
}

func TestScanReportsDepthLimitAndRejectsFiles(t *testing.T) {
	root := t.TempDir()
	path := root
	for range 14 {
		path = filepath.Join(path, "nested")
	}
	localRepo(t, path)
	manager, _ := New(filepath.Join(t.TempDir(), "recent.json"))
	result, err := manager.Scan(context.Background(), root)
	if err != nil || !result.Truncated || len(result.Repositories) != 0 {
		t.Fatalf("scan limit: %+v, %v", result, err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Scan(context.Background(), file); err == nil {
		t.Fatal("accepted a file as scan root")
	}
}

func TestScanDoesNotFollowSymlinkCycle(t *testing.T) {
	root := t.TempDir()
	path := localRepo(t, filepath.Join(root, "中文 仓库"))
	if err := os.Symlink(root, filepath.Join(root, "cycle")); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, errors.ErrUnsupported) ||
			runtime.GOOS == "windows" && (errors.Is(err, syscall.Errno(1314)) || errors.Is(err, syscall.Errno(50))) {
			t.Skipf("filesystem or account cannot create symlinks: %v", err)
		}
		t.Fatal(err)
	}
	manager, err := New(filepath.Join(t.TempDir(), "recent.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Scan(context.Background(), root)
	if err != nil || result.Truncated || len(result.Repositories) != 1 || result.Repositories[0].Path != path {
		t.Fatalf("scan followed cycle or lost repository: %+v, %v", result, err)
	}
}
