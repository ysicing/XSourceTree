// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCloneOpensAndPersistsRepository(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	source := t.TempDir()
	if output, err := exec.Command("git", "init", "-b", "main", source).CombinedOutput(); err != nil {
		t.Fatalf("init: %v: %s", err, output)
	}
	settings := filepath.Join(t.TempDir(), "settings", "recent.json")
	manager, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	state, err := manager.Clone(context.Background(), source, t.TempDir(), "clone")
	if err != nil {
		t.Fatal(err)
	}
	if state.Name != "clone" || !state.Unborn || state.Branch != "main" {
		t.Fatalf("unexpected snapshot: %+v", state)
	}
	if _, err := manager.Get(state.Path); err != nil {
		t.Fatalf("clone was not opened: %v", err)
	}
	reopened, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	recent := reopened.Recent()
	if len(recent) != 1 || recent[0].Path != state.Path {
		t.Fatalf("clone was not persisted: %+v", recent)
	}
}

func TestCloneFailureDoesNotChangeWorkspace(t *testing.T) {
	manager, err := New(filepath.Join(t.TempDir(), "recent.json"))
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	if _, err := manager.Clone(context.Background(), t.TempDir(), parent, "failed"); err == nil {
		t.Fatal("expected clone failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Clone(ctx, t.TempDir(), parent, "canceled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
	if len(manager.Recent()) != 0 {
		t.Fatal("failed clone changed recent repositories")
	}
	if _, err := manager.Get(filepath.Join(parent, "failed")); err == nil {
		t.Fatal("failed clone was opened")
	}
}
