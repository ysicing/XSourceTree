package main

import (
	"context"

	"github.com/egoist/mygo"
	"github.com/ysicing/XSourceTree/internal/gitrepo"
	"github.com/ysicing/XSourceTree/internal/workspace"
)

type RepositoryService struct {
	manager *workspace.Manager
}

func (service *RepositoryService) Recent() []workspace.Recent {
	return service.manager.Recent()
}

func (service *RepositoryService) Choose(ctx context.Context) (*gitrepo.Snapshot, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: "打开 Git 仓库", ButtonLabel: "打开仓库", Directory: true,
	})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	state, err := service.manager.Open(ctx, paths[0])
	return &state, err
}

func (service *RepositoryService) Open(ctx context.Context, path string) (gitrepo.Snapshot, error) {
	return service.manager.Open(ctx, path)
}

func (service *RepositoryService) Snapshot(ctx context.Context, path string) (gitrepo.Snapshot, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.Snapshot{}, err
	}
	return repo.Snapshot(ctx)
}

func (service *RepositoryService) Diff(ctx context.Context, path, file string, staged bool) (gitrepo.Diff, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.Diff{}, err
	}
	return repo.Diff(ctx, file, staged)
}

func (service *RepositoryService) Stage(ctx context.Context, path string, files []string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.Stage(ctx, files)
}

func (service *RepositoryService) Unstage(ctx context.Context, path string, files []string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.Unstage(ctx, files)
}

func (service *RepositoryService) Commit(ctx context.Context, path, message string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.Commit(ctx, message)
}

func (service *RepositoryService) History(ctx context.Context, path string, skip int) ([]gitrepo.Commit, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return nil, err
	}
	return repo.History(ctx, skip)
}

func (service *RepositoryService) CommitDiff(ctx context.Context, path, hash string) (gitrepo.Diff, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.Diff{}, err
	}
	return repo.CommitDiff(ctx, hash)
}

func (service *RepositoryService) Branches(ctx context.Context, path string) ([]gitrepo.Branch, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return nil, err
	}
	return repo.Branches(ctx)
}

func (service *RepositoryService) SwitchBranch(ctx context.Context, path, name string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.SwitchBranch(ctx, name)
}

func (service *RepositoryService) CreateBranch(ctx context.Context, path, name string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.CreateBranch(ctx, name)
}

func (service *RepositoryService) Remote(ctx context.Context, path, action string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.Remote(ctx, action)
}
