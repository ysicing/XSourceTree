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
	if err != nil {
		t.Fatal(err)
	}
	t.Run("private permissions", func(t *testing.T) {
		// 在同一文件系统独立探测权限位，不能因目标文件权限错误而跳过安全断言。
		probe := filepath.Join(filepath.Dir(settings), "permission-probe")
		if err := os.WriteFile(probe, nil, 0666); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(probe, 0600); err != nil {
			t.Fatal(err)
		}
		probeInfo, err := os.Stat(probe)
		if err != nil {
			t.Fatal(err)
		}
		if probeInfo.Mode().Perm() != 0600 {
			t.Skipf("filesystem does not expose Unix private permission bits: %v", probeInfo.Mode())
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("settings permissions: %v", info.Mode())
		}
	})
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
