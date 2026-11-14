// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"

	"github.com/ysicing/51code/internal/gitrepo"
)

func (service *RepositoryService) Identity(ctx context.Context, path string) (gitrepo.IdentitySettings, error) {
	if path == "" {
		return gitrepo.GlobalIdentity(ctx)
	}
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.IdentitySettings{}, err
	}
	return repo.Identity(ctx)
}

func (service *RepositoryService) SaveIdentity(ctx context.Context, path string, request gitrepo.IdentityRequest) error {
	if path == "" {
		return gitrepo.SaveGlobalIdentity(ctx, request)
	}
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.SaveIdentity(ctx, request)
}
