// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"strings"
)

const commitContextLimit = 96 << 10

// ErrCommitContextTooLarge 只限制 AI 输入，调用方可用返回的令牌识别暂存内容是否已变化。
var ErrCommitContextTooLarge = errors.New("暂存差异超过 96 KiB，请拆分提交或手动填写提交说明")

type CommitContext struct {
	Token string
	Diff  string
}

func (repo *Repository) commitToken(ctx context.Context) (string, error) {
	head, err := repo.head(ctx)
	if err != nil {
		return "", err
	}
	tree, err := repo.git(ctx, "write-tree")
	if err != nil {
		return "", err
	}
	return head + "\n" + strings.TrimSpace(tree), nil
}

func (repo *Repository) CommitToken(ctx context.Context) (string, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return repo.commitToken(ctx)
}

func (repo *Repository) CommitContext(ctx context.Context) (CommitContext, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	token, err := repo.commitToken(ctx)
	if err != nil {
		return CommitContext{}, err
	}
	head, tree, _ := strings.Cut(token, "\n")
	if head == "" {
		head, err = repo.git(ctx, "hash-object", "-t", "tree", "--stdin")
		if err != nil {
			return CommitContext{}, err
		}
		head = strings.TrimSpace(head)
	}
	// 固定树对象保证发送的是同一份暂存快照，不执行仓库自定义 diff 或 textconv。
	diff, err := repo.git(ctx, "diff", "--no-ext-diff", "--no-textconv", "--no-renames",
		"--no-color", "--submodule=short", "--unified=3", head, tree, "--")
	if err != nil {
		return CommitContext{}, err
	}
	if strings.TrimSpace(diff) == "" {
		return CommitContext{}, errors.New("没有已暂存的变更")
	}
	if len(diff) > commitContextLimit {
		return CommitContext{Token: token}, ErrCommitContextTooLarge
	}
	current, err := repo.commitToken(ctx)
	if err != nil {
		return CommitContext{}, err
	}
	if token != current {
		return CommitContext{}, errors.New("暂存区已变化，请重新生成")
	}
	return CommitContext{Token: token, Diff: diff}, nil
}
