package workspace

import (
	"context"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
)

func (manager *Manager) Clone(ctx context.Context, source, parent, name string) (gitrepo.Snapshot, error) {
	repo, err := gitrepo.Clone(ctx, source, parent, name)
	if err != nil {
		return gitrepo.Snapshot{}, err
	}
	return manager.Open(ctx, repo.Path)
}
