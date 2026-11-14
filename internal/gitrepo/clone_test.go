// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cloneEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, key := range []string{"GIT_SSH", "GIT_SSH_COMMAND"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCloneLocalRepository(t *testing.T) {
	cloneEnvironment(t)
	_, source := fixture(t)
	write(t, source, "你好 world.txt", "clone contents\n")
	git(t, source, "add", ".")
	git(t, source, "commit", "-m", "clone fixture")
	parent := t.TempDir()
	repo, err := Clone(context.Background(), source, parent, "克隆 repo")
	if err != nil {
		t.Fatal(err)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(parent, "克隆 repo"))
	if err != nil {
		t.Fatal(err)
	}
	if repo.Path != target {
		t.Fatalf("clone path: got %q, want %q", repo.Path, target)
	}
	if got := git(t, repo.Path, "rev-parse", "HEAD"); got != git(t, source, "rev-parse", "HEAD") {
		t.Fatal("cloned HEAD differs from source")
	}
	if got := git(t, repo.Path, "remote", "get-url", "origin"); got != source {
		t.Fatalf("origin: %q", got)
	}
	contents, err := os.ReadFile(filepath.Join(repo.Path, "你好 world.txt"))
	if err != nil || string(contents) != "clone contents\n" {
		t.Fatalf("checkout: %q, %v", contents, err)
	}
	if got := git(t, repo.Path, "status", "--porcelain"); got != "" {
		t.Fatalf("clone is not clean: %s", got)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary directories remain: %v, %v", entries, err)
	}
}

func TestCloneEmptyRepository(t *testing.T) {
	cloneEnvironment(t)
	_, source := fixture(t)
	repo, err := Clone(context.Background(), source, t.TempDir(), "empty")
	if err != nil {
		t.Fatal(err)
	}
	state, err := repo.Snapshot(context.Background())
	if err != nil || !state.Unborn {
		t.Fatalf("empty clone: %+v, %v", state, err)
	}
}

func TestCloneRejectsUnsafeArguments(t *testing.T) {
	cloneEnvironment(t)
	_, source := fixture(t)
	cases := []struct {
		label  string
		source string
		name   string
	}{
		{"empty source", "", "repo"},
		{"blank source", " \t", "repo"},
		{"option source", "--upload-pack=anything", "repo"},
		{"padded option source", " --bare", "repo"},
		{"nul source", "bad\x00source", "repo"},
		{"newline source", "bad\nsource", "repo"},
		{"empty name", source, ""},
		{"dot name", source, "."},
		{"parent name", source, ".."},
		{"option name", source, "-repo"},
		{"traversal", source, "../repo"},
		{"slash", source, "nested/repo"},
		{"backslash", source, `nested\repo`},
		{"drive path", source, "C:repo"},
		{"absolute", source, "/repo"},
		{"nul name", source, "bad\x00name"},
		{"control name", source, "bad\nname"},
		{"trailing dot", source, "repo."},
		{"trailing space", source, "repo "},
		{"reserved device", source, "NUL"},
		{"reserved device extension", source, "con.txt"},
		{"reserved port", source, "LPT1"},
		{"reserved superscript port", source, "COM¹"},
		{"wildcard", source, "repo*"},
	}
	for _, test := range cases {
		t.Run(test.label, func(t *testing.T) {
			parent := t.TempDir()
			if repo, err := Clone(context.Background(), test.source, parent, test.name); err == nil || repo != nil {
				t.Fatalf("unsafe arguments accepted: repo=%v, err=%v", repo, err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("validation created entries: %v, %v", entries, err)
			}
		})
	}
}

func TestCloneRequiresExistingParent(t *testing.T) {
	cloneEnvironment(t)
	_, source := fixture(t)
	root := t.TempDir()
	parentFile := filepath.Join(root, "file")
	write(t, root, "file", "keep")
	for _, parent := range []string{"", filepath.Join(root, "missing"), parentFile} {
		if _, err := Clone(context.Background(), source, parent, "repo"); err == nil {
			t.Fatalf("accepted invalid parent %q", parent)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created missing parent: %v", err)
	}
}

func TestClonePreservesExistingTarget(t *testing.T) {
	cloneEnvironment(t)
	_, source := fixture(t)
	for _, kind := range []string{"empty directory", "directory", "file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			target := filepath.Join(parent, "repo")
			switch kind {
			case "empty directory", "directory":
				if err := os.Mkdir(target, 0755); err != nil {
					t.Fatal(err)
				}
				if kind == "directory" {
					write(t, target, "keep", "external")
				}
			case "file":
				write(t, parent, "repo", "external")
			case "symlink":
				testSymlink(t, t.TempDir(), target)
			}
			before, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Clone(context.Background(), source, parent, "repo"); err == nil {
				t.Fatal("accepted existing target")
			}
			after, err := os.Lstat(target)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("existing target changed: %v", err)
			}
			if kind == "directory" {
				contents, err := os.ReadFile(filepath.Join(target, "keep"))
				if err != nil || string(contents) != "external" {
					t.Fatalf("existing content changed: %q, %v", contents, err)
				}
			}
		})
	}
}

func TestCloneFailureCleansOwnTarget(t *testing.T) {
	cloneEnvironment(t)
	parent := t.TempDir()
	if _, err := Clone(context.Background(), t.TempDir(), parent, "failed"); err == nil {
		t.Fatal("cloned a directory without a Git repository")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed clone left entries: %v, %v", entries, err)
	}
}

func TestCloneAlreadyCanceled(t *testing.T) {
	cloneEnvironment(t)
	_, source := fixture(t)
	parent := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Clone(ctx, source, parent, "canceled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled clone left entries: %v, %v", entries, err)
	}
}

func TestCloneCancellationPreservesExternalChanges(t *testing.T) {
	cloneEnvironment(t)
	for _, change := range []string{"none", "add file", "replace directory", "replace file", "replace symlink"} {
		t.Run(change, func(t *testing.T) {
			parent := t.TempDir()
			target := filepath.Join(parent, "repo")
			external := t.TempDir()
			write(t, external, "keep", "external")
			if change == "replace symlink" {
				probe := filepath.Join(t.TempDir(), "link")
				testSymlink(t, external, probe)
			}
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				select {
				case started <- struct{}{}:
				default:
				}
				select {
				case <-request.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := Clone(ctx, server.URL+"/repo.git", parent, "repo")
				done <- err
			}()
			select {
			case <-started:
			case err := <-done:
				t.Fatalf("clone ended before network request: %v", err)
			case <-ctx.Done():
				t.Fatal("clone never requested remote")
			}
			if _, err := os.Stat(target); err != nil {
				t.Fatalf("target was not reserved: %v", err)
			}
			if _, err := Clone(context.Background(), external, parent, "repo"); !errors.Is(err, os.ErrExist) {
				t.Fatalf("concurrent clone acquired the reserved target: %v", err)
			}
			switch change {
			case "add file":
				write(t, target, "keep", "external")
			case "replace directory", "replace file", "replace symlink":
				if err := os.Rename(target, filepath.Join(parent, "moved")); err != nil {
					t.Fatal(err)
				}
				switch change {
				case "replace directory":
					write(t, target, "keep", "external")
				case "replace file":
					write(t, parent, "repo", "external")
				case "replace symlink":
					testSymlink(t, external, target)
				}
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("expected cancellation: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("clone process did not stop after cancellation")
			}
			if change == "none" {
				entries, err := os.ReadDir(parent)
				if err != nil || len(entries) != 0 {
					t.Fatalf("cancellation left entries: %v, %v", entries, err)
				}
				return
			}
			marker := filepath.Join(target, "keep")
			if change == "replace file" {
				marker = target
			}
			contents, err := os.ReadFile(marker)
			if err != nil || string(contents) != "external" {
				t.Fatalf("external content changed: %q, %v", contents, err)
			}
		})
	}
}

func TestCloneSuccessfulTransferPreservesExternalChanges(t *testing.T) {
	cloneEnvironment(t)
	_, source := fixture(t)
	write(t, source, "file.txt", "cloned\n")
	git(t, source, "add", ".")
	git(t, source, "commit", "-m", "http fixture")
	git(t, source, "update-server-info")
	for _, change := range []string{"add file", "replace directory"} {
		t.Run(change, func(t *testing.T) {
			parent := t.TempDir()
			target := filepath.Join(parent, "repo")
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			files := http.FileServer(http.Dir(filepath.Join(source, ".git")))
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/info/refs" {
					select {
					case started <- struct{}{}:
					default:
					}
					select {
					case <-release:
					case <-request.Context().Done():
						return
					}
				}
				files.ServeHTTP(writer, request)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := Clone(ctx, server.URL, parent, "repo")
				done <- err
			}()
			select {
			case <-started:
			case err := <-done:
				t.Fatalf("clone ended before network request: %v", err)
			case <-ctx.Done():
				t.Fatal("clone never requested remote")
			}
			if change == "replace directory" {
				if err := os.Rename(target, filepath.Join(parent, "moved")); err != nil {
					t.Fatal(err)
				}
			}
			write(t, target, "file.txt", "external\n")
			close(release)
			if err := <-done; err == nil {
				t.Fatal("clone published into externally modified target")
			}
			contents, err := os.ReadFile(filepath.Join(target, "file.txt"))
			if err != nil || string(contents) != "external\n" {
				t.Fatalf("external content changed: %q, %v", contents, err)
			}
			if _, err := os.Stat(filepath.Join(target, ".git")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("published Git metadata into external target: %v", err)
			}
		})
	}
}

func TestCloneRespectsConfiguredSSH(t *testing.T) {
	for _, setting := range []string{"core.sshCommand", "GIT_SSH", "GIT_SSH_COMMAND"} {
		t.Run(setting, func(t *testing.T) {
			cloneEnvironment(t)
			_, source := fixture(t)
			actual, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("XSOURCE_REAL_GIT", actual)
			script := gitTestExecutable(t, "ssh-wrapper")
			t.Setenv("XSOURCE_CLONE_SOURCE", source)
			t.Setenv("GIT_SSH_VARIANT", "ssh")
			quoted := "'" + strings.ReplaceAll(filepath.ToSlash(script), "'", "'\"'\"'") + "'"
			if setting == "core.sshCommand" {
				config := filepath.Join(t.TempDir(), "gitconfig")
				git(t, source, "config", "--file", config, "core.sshCommand", quoted)
				t.Setenv("GIT_CONFIG_GLOBAL", config)
			} else if setting == "GIT_SSH" {
				t.Setenv(setting, script)
			} else {
				t.Setenv(setting, quoted)
			}
			if _, err := Clone(context.Background(), "ssh://fixture.invalid/repo", t.TempDir(), "ssh"); err != nil {
				t.Fatalf("configured %s was not usable: %v", setting, err)
			}
		})
	}
}
