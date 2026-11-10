// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Tag struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

func (repo *Repository) Tags(ctx context.Context) ([]Tag, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return nil, err
	}
	defer repo.mu.Unlock()
	output, err := repo.git(ctx, "for-each-ref", "--sort=refname", "--format=%(refname)%00%(objectname)", "--", "refs/tags/")
	if err != nil {
		return nil, err
	}
	tags := []Tag{}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "refs/tags/") {
			return nil, errors.New("无法解析标签")
		}
		tags = append(tags, Tag{Name: strings.TrimPrefix(fields[0], "refs/tags/"), Hash: fields[1]})
	}
	return tags, nil
}

func (repo *Repository) validateTag(ctx context.Context, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return errors.New("标签名称无效")
	}
	if _, err := repo.git(ctx, "check-ref-format", "refs/tags/"+name); err != nil {
		return fmt.Errorf("标签名称无效：%w", err)
	}
	return nil
}

func (repo *Repository) CreateTag(ctx context.Context, name, hash string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := repo.validateTag(ctx, name); err != nil {
		return err
	}
	target := hash
	if hash != "" {
		if len(hash) != 40 && len(hash) != 64 {
			return errors.New("标签目标 ID 无效")
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return errors.New("标签目标 ID 无效")
		}
	} else {
		var err error
		target, err = repo.head(ctx)
		if err != nil {
			return err
		}
		if target == "" {
			return errors.New("当前仓库没有可创建标签的提交")
		}
	}
	kind, err := repo.git(ctx, "cat-file", "-t", target)
	if err != nil {
		return fmt.Errorf("无法读取标签目标：%w", err)
	}
	if strings.TrimSpace(kind) != "commit" {
		return errors.New("标签目标必须是提交对象")
	}
	_, err = repo.git(ctx, "-c", "tag.gpgSign=false", "tag", "--no-sign", "--", name, target)
	return err
}

func (repo *Repository) DeleteTag(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := repo.validateTag(ctx, name); err != nil {
		return err
	}
	_, err := repo.git(ctx, "tag", "--delete", "--", name)
	return err
}

func (repo *Repository) RenameBranch(ctx context.Context, oldName, newName string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, name := range []string{oldName, newName} {
		if err := repo.validateBranch(ctx, name); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
	_, err := repo.git(ctx, "branch", "--move", "--", oldName, newName)
	return err
}

func (repo *Repository) DeleteBranch(ctx context.Context, name string, force bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := repo.validateBranch(ctx, name); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	current, err := repo.git(ctx, "symbolic-ref", "--quiet", "HEAD")
	if err != nil && !exitCode(err, 1) {
		return err
	}
	if strings.TrimSpace(current) == "refs/heads/"+name {
		return errors.New("不能删除当前分支，请先切换到其他分支")
	}
	option := "--delete"
	if force {
		option = "-D"
	}
	_, err = repo.git(ctx, "branch", option, "--", name)
	return err
}
