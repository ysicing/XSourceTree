// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package gitrepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestFileContentReadFailureIsNotMissing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file read permissions")
	}
	repo, dir := fixture(t)
	write(t, dir, "file", "private\n")
	path := filepath.Join(dir, "file")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })
	if side, err := repo.worktreeFileContent(context.Background(), "file"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("ordinary read failure became missing or unsupported: %+v %v", side, err)
	}
}

func TestFileContentOpenReplacement(t *testing.T) {
	if replacement := os.Getenv("XSOURCE_CONTENT_REPLACEMENT"); replacement != "" {
		dir := os.Getenv("XSOURCE_CONTENT_DIRECTORY")
		write(t, dir, "file", "original\n")
		info, err := os.Lstat(filepath.Join(dir, "file"))
		if err != nil {
			t.Fatal(err)
		}
		// 保留原 inode，排除复用；symlink 指回原 inode，单独依赖 SameFile 无法拒绝它。
		if err := os.Rename(filepath.Join(dir, "file"), filepath.Join(dir, "original")); err != nil {
			t.Fatal(err)
		}
		switch replacement {
		case "fifo":
			if err := unix.Mkfifo(filepath.Join(dir, "file"), 0600); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			if err := os.Symlink("original", filepath.Join(dir, "file")); err != nil {
				t.Fatal(err)
			}
		case "ancestor_fifo":
			if err := unix.Mkfifo(filepath.Join(dir, "parent"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		path := "file"
		if replacement == "ancestor_fifo" {
			path = "parent/file"
		}
		file, err := openFileContent(root, path, info)
		if file != nil {
			file.Close()
		}
		if err == nil || file != nil {
			t.Fatalf("accepted %s replacement after regular-file Lstat", replacement)
		}
		return
	}
	for _, replacement := range []string{"fifo", "symlink", "ancestor_fifo"} {
		t.Run(replacement, func(t *testing.T) {
			// 隔离可能阻塞的 open；FIFO 没有 writer，超时会杀死子进程，不留下挂起 goroutine。
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFileContentOpenReplacement$", "-test.count=1")
			command.Env = append(os.Environ(), "XSOURCE_CONTENT_REPLACEMENT="+replacement, "XSOURCE_CONTENT_DIRECTORY="+t.TempDir())
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("%s replacement blocked before fstat with no FIFO writer: %v", replacement, ctx.Err())
			}
			if err != nil {
				t.Fatalf("replacement not rejected: %v\n%s", err, output)
			}
		})
	}
}
