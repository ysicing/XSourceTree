// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"strings"
	"time"
)

type HistoryLocation struct {
	Commit  Commit
	Commits []Commit
	Offset  int
}

// LocateHistory 返回目标所在的匹配页；目标被筛选排除时仍返回提交详情。
func (repo *Repository) LocateHistory(ctx context.Context, hash, query, path string, filter HistoryFilter) (HistoryLocation, error) {
	if err := validCommitHash(hash); err != nil {
		return HistoryLocation{}, err
	}
	if strings.ContainsRune(query, 0) {
		return HistoryLocation{}, errors.New("搜索内容无效")
	}
	if path != "" {
		if err := validPath(path); err != nil {
			return HistoryLocation{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return HistoryLocation{}, err
	}
	defer repo.mu.Unlock()
	output, err := repo.git(ctx, "show", "--no-show-signature", "--no-patch", "--decorate=short", "-z", historyFormat, hash, "--")
	if err != nil {
		return HistoryLocation{}, err
	}
	detail := newHistoryLogWriter(ctx, "", 0, cancel)
	if _, err := detail.Write([]byte(output)); err != nil {
		return HistoryLocation{}, err
	}
	if err := detail.finish(); err != nil {
		return HistoryLocation{}, err
	}
	if len(detail.commits) != 1 || detail.commits[0].Hash != hash {
		return HistoryLocation{}, errors.New("无法读取目标提交")
	}
	location := HistoryLocation{Commit: detail.commits[0]}
	if filter.CurrentBranch {
		head, err := repo.head(ctx)
		if err != nil {
			return HistoryLocation{}, err
		}
		if head == "" {
			return location, nil
		}
	}
	writer := newHistoryLogWriter(ctx, query, 0, nil)
	writer.target = hash
	if err := repo.readHistory(ctx, path, filter, writer); err != nil {
		return HistoryLocation{}, err
	}
	if writer.found {
		location.Commits, location.Offset = writer.commits, writer.offset
	}
	return location, nil
}
