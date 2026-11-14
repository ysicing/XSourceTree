// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"

	"github.com/ysicing/51code/internal/gitrepo"
)

func (service *RepositoryService) Worktrees(ctx context.Context, path string) ([]gitrepo.Worktree, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return nil, err
	}
	return repo.Worktrees(ctx)
}

func (service *RepositoryService) CreateWorktree(ctx context.Context, repository, path, branch, start string, existing bool) (gitrepo.Worktree, error) {
	repo, err := service.manager.Get(repository)
	if err != nil {
		return gitrepo.Worktree{}, err
	}
	return repo.CreateWorktree(ctx, path, branch, start, existing)
}

func (service *RepositoryService) ReviewWorktree(ctx context.Context, repository, path string) (gitrepo.WorktreeReview, error) {
	repo, err := service.manager.Get(repository)
	if err != nil {
		return gitrepo.WorktreeReview{}, err
	}
	return repo.ReviewWorktree(ctx, path)
}

func (service *RepositoryService) MergeWorktree(ctx context.Context, repository, path, token string) error {
	repo, err := service.manager.Get(repository)
	if err != nil {
		return err
	}
	return repo.MergeWorktree(ctx, path, token)
}

func (service *RepositoryService) RemoveWorktree(ctx context.Context, repository, path, token string) error {
	repo, err := service.manager.Get(repository)
	if err != nil {
		return err
	}
	return repo.RemoveWorktree(ctx, path, token)
}
