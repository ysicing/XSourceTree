// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package gitrepo

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func openFileContentPath(root *os.Root, path string) (*os.File, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(path, "/")
	for index, part := range parts {
		flags := unix.O_RDONLY | unix.O_NONBLOCK | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if index < len(parts)-1 {
			// 父目录也可能被换成 FIFO；逐层固定目录句柄，拒绝所有祖先链接。
			flags |= unix.O_DIRECTORY
		}
		var fd int
		for {
			fd, err = unix.Openat(int(directory.Fd()), part, flags, 0)
			if err != unix.EINTR {
				break
			}
		}
		directory.Close()
		if err != nil {
			return nil, &os.PathError{Op: "openat", Path: path, Err: err}
		}
		directory = os.NewFile(uintptr(fd), part)
	}
	return directory, nil
}
