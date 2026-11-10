package main

import (
	"context"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
)

func (service *RepositoryService) RemoteConfigs(ctx context.Context, path string) ([]gitrepo.RemoteConfig, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return nil, err
	}
	return repo.RemoteConfigs(ctx)
}

func (service *RepositoryService) SaveRemote(ctx context.Context, path, name, address, token string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.SaveRemote(ctx, name, address, token)
}

func (service *RepositoryService) RemoveRemote(ctx context.Context, path, name, token string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.RemoveRemote(ctx, name, token)
}

func (service *RepositoryService) CheckoutRemote(ctx context.Context, path, remoteBranch, name string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.CheckoutRemote(ctx, remoteBranch, name)
}

func (service *RepositoryService) PublishBranch(ctx context.Context, path string, request gitrepo.PublishBranchRequest) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.PublishBranch(ctx, request)
}
