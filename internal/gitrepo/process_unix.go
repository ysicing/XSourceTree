// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package gitrepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func runProcess(ctx context.Context, cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return err
}
