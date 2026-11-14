// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"

	"github.com/ysicing/XSourceTree/internal/gitrepo"

	"github.com/egoist/mygo"
)

func (service *RepositoryService) SearchHistory(ctx context.Context, path, query, file string, skip int, filter gitrepo.HistoryFilter) ([]gitrepo.Commit, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return nil, err
	}
	service.historyMu.Lock()
	if service.cancelHistory != nil {
		service.historyMu.Unlock()
		return nil, errors.New("已有历史搜索正在进行")
	}
	ctx, cancel := context.WithCancel(ctx)
	service.cancelHistory = cancel
	service.historyMu.Unlock()
	defer func() {
		cancel()
		service.historyMu.Lock()
		service.cancelHistory = nil
		service.historyMu.Unlock()
	}()
	return repo.SearchHistory(ctx, query, file, skip, filter)
}

func (service *RepositoryService) CancelHistory() {
	service.historyMu.Lock()
	defer service.historyMu.Unlock()
	if service.cancelHistory != nil {
		service.cancelHistory()
	}
}

func (service *RepositoryService) LocateHistory(ctx context.Context, path, hash, query, file string, filter gitrepo.HistoryFilter) (gitrepo.HistoryLocation, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.HistoryLocation{}, err
	}
	return repo.LocateHistory(ctx, hash, query, file, filter)
}

func (service *RepositoryService) CommitFiles(ctx context.Context, path, hash string) ([]gitrepo.CommitFile, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return nil, err
	}
	return repo.CommitFiles(ctx, hash)
}

func (service *RepositoryService) CommitFileDiff(ctx context.Context, path, hash, file string) (gitrepo.Diff, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.Diff{}, err
	}
	return repo.CommitFileDiff(ctx, hash, file)
}

func (service *RepositoryService) Compare(ctx context.Context, path, base, target string, mergeBase bool) (gitrepo.Comparison, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.Comparison{}, err
	}
	return repo.Compare(ctx, base, target, mergeBase)
}

func (service *RepositoryService) CompareFileDiff(ctx context.Context, path, from, target, file string) (gitrepo.Diff, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.Diff{}, err
	}
	return repo.CompareFileDiff(ctx, from, target, file)
}

func (service *RepositoryService) Tags(ctx context.Context, path string) ([]gitrepo.Tag, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return nil, err
	}
	return repo.Tags(ctx)
}

func (service *RepositoryService) CreateTag(ctx context.Context, path, name, hash string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.CreateTag(ctx, name, hash)
}

func (service *RepositoryService) CheckReferenceName(ctx context.Context, path, name string, tag bool) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.CheckReferenceName(ctx, name, tag)
}

func (service *RepositoryService) ResolveCreationTarget(ctx context.Context, path, reference string) (string, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return "", err
	}
	return repo.ResolveCreationTarget(ctx, reference)
}

func (service *RepositoryService) CreateBranchAt(ctx context.Context, path, name, hash string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.CreateBranchAt(ctx, name, hash)
}

func (service *RepositoryService) SwitchCreatedBranch(ctx context.Context, path, name, hash string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.SwitchCreatedBranch(ctx, name, hash)
}

func (service *RepositoryService) DeleteTag(ctx context.Context, path, name string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.DeleteTag(ctx, name)
}

func (service *RepositoryService) RenameBranch(ctx context.Context, path, oldName, newName string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.RenameBranch(ctx, oldName, newName)
}

func (service *RepositoryService) DeleteBranch(ctx context.Context, path, name string, force bool) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.DeleteBranch(ctx, name, force)
}

func (service *RepositoryService) RevealRepository(path string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return mygo.Shell.OpenPath(repo.Path)
}
