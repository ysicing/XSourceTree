// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"os"
	"path/filepath"

	"github.com/ysicing/51code/internal/gitrepo"

	"github.com/egoist/mygo"
)

func (service *RepositoryService) DiscardPreview(ctx context.Context, path, file string) (gitrepo.DiscardPreview, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.DiscardPreview{}, err
	}
	return repo.DiscardPreview(ctx, file)
}

func (service *RepositoryService) Discard(ctx context.Context, path, file, token string) (string, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return "", err
	}
	directory, err := discardBackupDirectory()
	if err != nil {
		return "", err
	}
	return repo.Discard(ctx, file, token, directory)
}

func (service *RepositoryService) RevealDiscardBackups() error {
	directory, err := discardBackupDirectory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	return mygo.Shell.OpenPath(directory)
}

func discardBackupDirectory() (string, error) {
	directory, err := mygo.App.Path(mygo.PathUserData)
	return filepath.Join(directory, "discard-backups"), err
}
