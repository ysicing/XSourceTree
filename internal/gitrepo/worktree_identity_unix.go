// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package gitrepo

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func worktreeFileIdentity(_ string, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("无法读取工作树文件系统标识")
	}
	// 时间戳可被复制或还原，令牌还必须绑定设备与 inode，拒绝目录替换。
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
