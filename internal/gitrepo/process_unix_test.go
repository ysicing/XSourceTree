//go:build !windows

package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCancellationStopsHookDescendants(t *testing.T) {
	repo, dir := fixture(t)
	write(t, dir, "file.txt", "new\n")
	git(t, dir, "add", ".")
	started := filepath.Join(dir, "hook-started")
	survived := filepath.Join(dir, "hook-survived")
	t.Setenv("XSOURCE_STARTED", started)
	t.Setenv("XSOURCE_SURVIVED", survived)
	hook := "#!/bin/sh\nprintf started > \"$XSOURCE_STARTED\"\nsleep 1\nprintf survived > \"$XSOURCE_SURVIVED\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-commit"), []byte(hook), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- repo.Commit(ctx, "cancel me") }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hook did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("canceled operation succeeded")
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := os.Stat(survived); !os.IsNotExist(err) {
		t.Fatal("hook descendant continued after cancellation")
	}
}

func TestRemoteRespectsConfiguredSSH(t *testing.T) {
	for _, source := range []string{"core.sshCommand", "GIT_SSH"} {
		t.Run(source, func(t *testing.T) {
			repo, dir := fixture(t)
			script := filepath.Join(t.TempDir(), "ssh-wrapper")
			marker := filepath.Join(t.TempDir(), "called")
			if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf called > \"$XSOURCE_SSH_MARKER\"\nexit 1\n"), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XSOURCE_SSH_MARKER", marker)
			for _, key := range []string{"GIT_SSH_COMMAND", "GIT_SSH"} {
				t.Setenv(key, "")
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			if source == "core.sshCommand" {
				git(t, dir, "config", "core.sshCommand", "'"+strings.ReplaceAll(script, "'", "'\"'\"'")+"'")
			} else {
				t.Setenv("GIT_SSH", script)
			}
			git(t, dir, "remote", "add", "origin", "ssh://fixture.invalid/repository")
			remoteErr := repo.Remote(context.Background(), "fetch")
			if remoteErr == nil {
				t.Fatal("test SSH wrapper should fail")
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("configured SSH wrapper was ignored: %v; fetch: %v", err, remoteErr)
			}
		})
	}
}
