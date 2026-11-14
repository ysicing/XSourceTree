// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Stash struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Date    string `json:"date"`
}

func (repo *Repository) Stashes(ctx context.Context) ([]Stash, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return repo.stashes(ctx)
}

func (repo *Repository) stashes(ctx context.Context) ([]Stash, error) {
	output, err := repo.git(ctx, "stash", "list", "-z", "--format=%H%x00%gs%x00%cI")
	if err != nil {
		return nil, err
	}
	stashes := []Stash{}
	if output == "" {
		return stashes, nil
	}
	fields := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	if len(fields)%3 != 0 {
		return nil, errors.New("无法解析贮藏记录")
	}
	for index := 0; index < len(fields); index += 3 {
		stashes = append(stashes, Stash{
			Hash: fields[index], Message: fields[index+1], Date: fields[index+2],
		})
	}
	return stashes, nil
}

func (repo *Repository) stashIndex(ctx context.Context, hash string) (int, error) {
	if len(hash) != 40 && len(hash) != 64 {
		return -1, errors.New("贮藏 ID 无效")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return -1, errors.New("贮藏 ID 无效")
	}
	stashes, err := repo.stashes(ctx)
	if err != nil {
		return -1, err
	}
	selected := -1
	for index, stash := range stashes {
		if stash.Hash != hash {
			continue
		}
		if selected != -1 {
			return -1, errors.New("存在相同 ID 的多条贮藏记录，请使用命令行明确选择")
		}
		selected = index
	}
	if selected == -1 {
		return -1, errors.New("贮藏记录已改变，请刷新")
	}
	return selected, nil
}

func (repo *Repository) StashDiff(ctx context.Context, hash string) (Diff, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if _, err := repo.stashIndex(ctx, hash); err != nil {
		return Diff{}, err
	}
	output, err := repo.git(ctx, "stash", "show", "--patch", "--no-color", "--include-untracked", "--no-ext-diff", "--no-textconv", hash)
	return Diff{Text: output, Binary: strings.Contains(output, "Binary files ")}, err
}

func (repo *Repository) CreateStash(ctx context.Context, message string, includeUntracked bool) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if strings.ContainsRune(message, 0) || len(message) > 64<<10 {
		return errors.New("贮藏说明无效或过长")
	}
	operation, err := repo.operation(ctx)
	if err != nil {
		return err
	}
	if operation.Kind != "" {
		return errors.New("请先完成或中止正在进行的 Git 操作，再创建贮藏")
	}
	files, err := repo.files(ctx)
	if err != nil {
		return err
	}
	changed := false
	for _, file := range files {
		if file.Conflict {
			return errors.New("请先解决冲突，再创建贮藏")
		}
		if file.Index != "?" || includeUntracked {
			changed = true
		}
	}
	if !changed {
		return errors.New("没有可贮藏的更改")
	}
	before, err := repo.stashes(ctx)
	if err != nil {
		return err
	}
	// 此命令不接收用户 pathspec；取消继承的 literal 模式，避免 stash 内部 clean 无法删除未跟踪文件。
	args := []string{"--no-literal-pathspecs", "stash", "push"}
	if includeUntracked {
		args = append(args, "--include-untracked")
	}
	if message != "" {
		args = append(args, "--message", message)
	}
	args = append(args, "--")
	if _, err := repo.git(ctx, args...); err != nil {
		return err
	}
	after, err := repo.stashes(ctx)
	if err != nil {
		return err
	}
	if len(after) <= len(before) {
		return errors.New("没有创建新的贮藏记录，请刷新检查工作区")
	}
	return nil
}

func (repo *Repository) ApplyStash(ctx context.Context, hash string, pop bool) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	operation, err := repo.operation(ctx)
	if err != nil {
		return err
	}
	if operation.Kind != "" {
		return errors.New("请先完成或中止正在进行的 Git 操作，再应用贮藏")
	}
	if _, err := repo.stashIndex(ctx, hash); err != nil {
		return err
	}
	if _, err := repo.git(ctx, "stash", "apply", hash); err != nil {
		return err
	}
	if pop {
		if err := repo.dropStash(ctx, hash); err != nil {
			return fmt.Errorf("贮藏已应用，但未能移除记录，请刷新后检查：%w", err)
		}
	}
	return nil
}

func (repo *Repository) DropStash(ctx context.Context, hash string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return repo.dropStash(ctx, hash)
}

func (repo *Repository) dropStash(ctx context.Context, hash string) error {
	index, err := repo.stashIndex(ctx, hash)
	if err != nil {
		return err
	}
	selector := "refs/stash@{" + strconv.Itoa(index) + "}"
	current, err := repo.git(ctx, "rev-parse", "--verify", "--quiet", selector)
	if err != nil {
		return err
	}
	if strings.TrimSpace(current) != hash {
		return errors.New("贮藏记录顺序已改变，未删除任何记录，请刷新")
	}
	_, err = repo.git(ctx, "stash", "drop", selector)
	return err
}
