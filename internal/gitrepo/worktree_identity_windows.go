// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"fmt"
	"os"
	"syscall"
)

func worktreeFileIdentity(path string, _ os.FileInfo) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var identity syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &identity); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d", identity.VolumeSerialNumber, identity.FileIndexHigh, identity.FileIndexLow), nil
}
