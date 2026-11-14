// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"sync"

	"github.com/ysicing/51code/internal/gitrepo"
	"github.com/ysicing/51code/internal/workspace"

	"github.com/egoist/mygo"
)

type RepositoryService struct {
	manager       *workspace.Manager
	scanMu        sync.Mutex
	cancelScan    context.CancelFunc
	historyMu     sync.Mutex
	cancelHistory context.CancelFunc
}

func (service *RepositoryService) ChooseScanRoot(ctx context.Context) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: "扫描文件夹", ButtonLabel: "扫描", Directory: true,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func (service *RepositoryService) Scan(ctx context.Context, root string) (workspace.ScanResult, error) {
	service.scanMu.Lock()
	if service.cancelScan != nil {
		service.scanMu.Unlock()
		return workspace.ScanResult{}, errors.New("已有扫描正在进行")
	}
	ctx, cancel := context.WithCancel(ctx)
	service.cancelScan = cancel
	service.scanMu.Unlock()
	defer func() {
		cancel()
		service.scanMu.Lock()
		service.cancelScan = nil
		service.scanMu.Unlock()
	}()
	return service.manager.Scan(ctx, root)
}

func (service *RepositoryService) CancelScan() {
	service.scanMu.Lock()
	defer service.scanMu.Unlock()
	if service.cancelScan != nil {
		service.cancelScan()
	}
}

func (service *RepositoryService) Library() workspace.Library {
	return service.manager.Library()
}

func (service *RepositoryService) RefreshLibrary(ctx context.Context) (workspace.Library, error) {
	return service.manager.RefreshLibrary(ctx)
}

func (service *RepositoryService) AddRepositories(ctx context.Context, paths []string, groupID string) (workspace.Library, error) {
	return service.manager.Add(ctx, paths, groupID)
}

func (service *RepositoryService) ChooseLocal(ctx context.Context, groupID string) (workspace.Library, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: "添加已有的本地仓库", ButtonLabel: "添加", Directory: true, Multiple: true,
	})
	if err != nil {
		return workspace.Library{}, err
	}
	if len(paths) == 0 {
		return service.manager.Library(), nil
	}
	return service.manager.Add(ctx, paths, groupID)
}

func (service *RepositoryService) CreateGroup(name string) (workspace.Library, error) {
	return service.manager.CreateGroup(name)
}

func (service *RepositoryService) RenameGroup(id, name string) (workspace.Library, error) {
	return service.manager.RenameGroup(id, name)
}

func (service *RepositoryService) RemoveGroup(id string) (workspace.Library, error) {
	return service.manager.RemoveGroup(id)
}

func (service *RepositoryService) MoveRepository(path, groupID string) (workspace.Library, error) {
	return service.manager.Move(path, groupID)
}

func (service *RepositoryService) RemoveRepository(path string) (workspace.Library, error) {
	return service.manager.Remove(path)
}

func (service *RepositoryService) Stashes(ctx context.Context, path string) ([]gitrepo.Stash, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return nil, err
	}
	return repo.Stashes(ctx)
}

func (service *RepositoryService) StashDiff(ctx context.Context, path, hash string) (gitrepo.Diff, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.Diff{}, err
	}
	return repo.StashDiff(ctx, hash)
}

func (service *RepositoryService) CreateStash(ctx context.Context, path, message string, includeUntracked bool) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.CreateStash(ctx, message, includeUntracked)
}

func (service *RepositoryService) ApplyStash(ctx context.Context, path, hash string, pop bool) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.ApplyStash(ctx, hash, pop)
}

func (service *RepositoryService) DropStash(ctx context.Context, path, hash string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.DropStash(ctx, hash)
}

func (service *RepositoryService) Conflict(ctx context.Context, path, file string) (gitrepo.Conflict, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.Conflict{}, err
	}
	return repo.Conflict(ctx, file)
}

func (service *RepositoryService) ResolveConflict(ctx context.Context, path, file, token, choice, content string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.ResolveConflict(ctx, file, token, choice, content)
}

func (service *RepositoryService) Merge(ctx context.Context, path, branch string, options gitrepo.MergeOptions) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.MergeWithOptions(ctx, branch, options)
}

func (service *RepositoryService) OperationAction(ctx context.Context, path, kind, token, action string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.OperationAction(ctx, kind, token, action)
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
	snapshot, err := repo.Snapshot(ctx)
	if err != nil {
		return snapshot, err
	}
	return snapshot, service.manager.Remember(snapshot)
}

func (service *RepositoryService) Diff(ctx context.Context, path, file string, staged bool) (gitrepo.Diff, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.Diff{}, err
	}
	return repo.Diff(ctx, file, staged)
}

func (service *RepositoryService) Patch(ctx context.Context, path, file string, staged bool) (gitrepo.Patch, error) {
	repo, err := service.manager.Get(path)
	if err != nil {
		return gitrepo.Patch{}, err
	}
	return repo.Patch(ctx, file, staged)
}

func (service *RepositoryService) ApplyHunk(ctx context.Context, path, file string, staged bool, token string, id int) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.ApplyHunk(ctx, file, staged, token, id)
}

func (service *RepositoryService) Stage(ctx context.Context, path string, files []string) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.Stage(ctx, files)
}

func (service *RepositoryService) ApplyLines(ctx context.Context, path, file string, staged bool, token string, ids []int) error {
	repo, err := service.manager.Get(path)
	if err != nil {
		return err
	}
	return repo.ApplyLines(ctx, file, staged, token, ids)
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
