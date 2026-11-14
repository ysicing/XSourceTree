// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package workspace

import (
	"context"

	"github.com/ysicing/51code/internal/gitrepo"
)

func (manager *Manager) Clone(ctx context.Context, source, parent, name string) (gitrepo.Snapshot, error) {
	repo, err := gitrepo.Clone(ctx, source, parent, name)
	if err != nil {
		return gitrepo.Snapshot{}, err
	}
	return manager.Open(ctx, repo.Path)
}
