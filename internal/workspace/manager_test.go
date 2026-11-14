// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRecentRepositoriesPersistAndRequireExplicitOpen(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings", "recent.json")
	manager, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main", dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	state, err := manager.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Get(state.Path); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	recent := reopened.Recent()
	if len(recent) != 1 || recent[0].Path != state.Path {
		t.Fatalf("recent not persisted: %+v", recent)
	}
	if _, err := reopened.Get(state.Path); err == nil {
		t.Fatal("recent path became an authorized open repository")
	}
	if _, err := manager.Get(filepath.Dir(state.Path)); err == nil {
		t.Fatal("accepted unopened path")
	}
	if _, err := manager.Open(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if len(manager.Recent()) != 1 {
		t.Fatal("duplicate recent repository")
	}
	info, err := os.Stat(settings)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("settings permissions: %v %v", info, err)
	}
}

func TestFailedOpenDoesNotChangeRecent(t *testing.T) {
	manager, err := New(filepath.Join(t.TempDir(), "recent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), t.TempDir()); err == nil {
		t.Fatal("accepted non-repository directory")
	}
	if len(manager.Recent()) != 0 {
		t.Fatal("failed open added to recent")
	}
}

func TestCorruptLibraryIsBackedUpAndStartsEmpty(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "recent.json")
	original := []byte(`{"repositories": [{"name": "keep"`)
	if err := os.WriteFile(settings, original, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := New(settings)
	if err != nil {
		t.Fatal("损坏的仓库记录阻止了启动:", err)
	}
	if manager.LoadProblem() == nil || len(manager.Library().Repositories) != 0 {
		t.Fatalf("未提示损坏记录或未以空列表启动: %v %+v", manager.LoadProblem(), manager.Library())
	}
	backups, err := filepath.Glob(settings + ".corrupt-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("未备份损坏的仓库记录: %v %v", backups, err)
	}
	if data, err := os.ReadFile(backups[0]); err != nil || string(data) != string(original) {
		t.Fatalf("备份内容不完整: %q %v", data, err)
	}
}
