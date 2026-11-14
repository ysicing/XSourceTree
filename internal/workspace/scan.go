// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ysicing/51code/internal/gitrepo"
)

type ScanResult struct {
	Repositories []LocalRepository `json:"repositories"`
	Warnings     []string          `json:"warnings"`
	Truncated    bool              `json:"truncated"`
}

func (manager *Manager) Scan(ctx context.Context, root string) (ScanResult, error) {
	result := ScanResult{Repositories: []LocalRepository{}, Warnings: []string{}}
	if strings.TrimSpace(root) == "" {
		return result, errors.New("请选择扫描文件夹")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return result, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return result, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, errors.New("请选择文件夹")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	visited := 0
	seen := map[string]bool{}
	warn := func(path string, err error) {
		if len(result.Warnings) < 20 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s：%s", path, err))
		}
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			warn(path, walkErr)
			return fs.SkipDir
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return fs.SkipDir
			}
			relative, _ := filepath.Rel(root, path)
			if len(strings.Split(relative, string(filepath.Separator))) > 12 {
				result.Truncated = true
				return fs.SkipDir
			}
		}
		visited++
		if visited > 20000 || len(result.Repositories) >= 2000 {
			result.Truncated = true
			return fs.SkipAll
		}
		if _, err := os.Lstat(filepath.Join(path, ".git")); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				warn(path, err)
			}
			return nil
		}
		repo, err := gitrepo.Open(ctx, path)
		if err != nil {
			warn(path, err)
			return nil
		}
		if repo.Path != path || seen[repo.Path] {
			return nil
		}
		snapshot, err := repo.Snapshot(ctx)
		if err != nil {
			warn(path, err)
			return nil
		}
		seen[repo.Path] = true
		result.Repositories = append(result.Repositories, entryFromSnapshot(snapshot))
		return nil
	})
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	sort.Slice(result.Repositories, func(first, second int) bool {
		return result.Repositories[first].Path < result.Repositories[second].Path
	})
	return result, nil
}
