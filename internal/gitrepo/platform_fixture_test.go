// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 复用测试二进制作为原生子进程，避免 Windows CreateProcess 不能执行 shebang 脚本。
func TestMain(m *testing.M) {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if name == "git" && os.Getenv("XSOURCE_TEST_GIT_MODE") != "" ||
		name == "gpg" && os.Getenv("XSOURCE_GPG_MARKER") != "" ||
		name == "ssh-wrapper" && os.Getenv("XSOURCE_CLONE_SOURCE") != "" {
		os.Exit(gitTestChild(name, os.Args[1:]))
	}
	os.Exit(m.Run())
}

func gitTestChild(name string, args []string) int {
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, err)
		return 99
	}
	if name == "gpg" {
		if err := os.WriteFile(os.Getenv("XSOURCE_GPG_MARKER"), []byte("called"), 0600); err != nil {
			return fail(err)
		}
		return 1
	}
	if name == "ssh-wrapper" {
		if os.Getenv("GIT_TERMINAL_PROMPT") != "0" || os.Getenv("GCM_INTERACTIVE") != "never" {
			return fail(errors.New("interactive SSH environment"))
		}
		args = []string{"upload-pack", os.Getenv("XSOURCE_CLONE_SOURCE")}
	} else {
		switch mode := os.Getenv("XSOURCE_TEST_GIT_MODE"); mode {
		case "history-count":
			if slices.Contains(args, "log") {
				file, err := os.OpenFile(os.Getenv("XSOURCE_LOG_CALLS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					return fail(err)
				}
				_, err = io.WriteString(file, "log\n")
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					return fail(errors.Join(err, closeErr))
				}
				for _, arg := range args {
					if arg == "-n" || strings.HasPrefix(arg, "--skip=") || strings.HasPrefix(arg, "--max-count=") {
						fmt.Fprintln(os.Stderr, "repeated traversal is forbidden")
						return 91
					}
				}
			}
		case "history-failure":
			fmt.Fprintln(os.Stderr, "history failure")
			return 73
		case "history-cancel":
			if err := os.WriteFile(os.Getenv("XSOURCE_STARTED"), []byte("started"), 0600); err != nil {
				return fail(err)
			}
			time.Sleep(10 * time.Second)
			return 0
		case "history-stderr":
			if _, err := io.WriteString(os.Stderr, strings.Repeat("x", outputLimit+1)); err != nil {
				return fail(err)
			}
			return 0
		case "identity-before-write", "identity-after-write":
			if len(args) >= 3 && slices.Equal(args[len(args)-3:], []string{"--replace-all", "user.email", "new@example.test"}) {
				if mode == "identity-after-write" {
					command := exec.Command(os.Getenv("XSOURCE_REAL_GIT"), args...)
					command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
					if err := command.Run(); err != nil {
						return fail(err)
					}
				}
				return 42
			}
		default:
			return fail(fmt.Errorf("unknown Git fixture mode %q", mode))
		}
	}
	command := exec.Command(os.Getenv("XSOURCE_REAL_GIT"), args...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			return failure.ExitCode()
		}
		return fail(err)
	}
	return 0
}

func gitTestExecutable(t *testing.T, name string) string {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(t.TempDir(), name)
	if err := os.Link(source, target); err == nil {
		return target
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	if err := errors.Join(copyErr, output.Close()); err != nil {
		t.Fatal(err)
	}
	return target
}

func installGitTestWrapper(t *testing.T, mode string) {
	t.Helper()
	actual, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XSOURCE_REAL_GIT", actual)
	t.Setenv("XSOURCE_TEST_GIT_MODE", mode)
	wrapper := gitTestExecutable(t, "git")
	t.Setenv("PATH", filepath.Dir(wrapper)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func portableTestPath(unix, windows string) string {
	if runtime.GOOS == "windows" {
		return windows
	}
	return unix
}

func testSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, errors.ErrUnsupported) ||
			runtime.GOOS == "windows" && (errors.Is(err, syscall.Errno(1314)) || errors.Is(err, syscall.Errno(50))) {
			t.Skipf("filesystem or account cannot create symlinks: %v", err)
		}
		t.Fatal(err)
	}
}
