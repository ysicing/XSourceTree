// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (repo *Repository) CheckReferenceName(ctx context.Context, name string, tag bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	return repo.checkReferenceName(ctx, name, tag)
}

func (repo *Repository) checkReferenceName(ctx context.Context, name string, tag bool) error {
	namespace, label := "refs/heads/", "本地分支"
	if tag {
		namespace, label = "refs/tags/", "本地标签"
	}
	if name == "" || strings.HasPrefix(name, "-") || !tag && name == "HEAD" {
		return fmt.Errorf("%s名称无效", label)
	}
	if _, err := repo.git(ctx, "check-ref-format", namespace+name); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("名称格式无效：不能包含空格、..、~、^、:、?、*、[、反斜杠或无效的斜杠分段")
	}
	output, err := repo.git(ctx, "for-each-ref", "--format=%(refname)", "--", namespace)
	if err != nil {
		return fmt.Errorf("无法检查本地名称：%w", err)
	}
	for _, reference := range strings.Split(strings.TrimSpace(output), "\n") {
		existing := strings.TrimPrefix(reference, namespace)
		switch {
		case existing == name:
			return fmt.Errorf("%s %s 已存在", label, name)
		case existing != "" && (strings.HasPrefix(name, existing+"/") || strings.HasPrefix(existing, name+"/")):
			return fmt.Errorf("名称与%s %s 的路径冲突", label, existing)
		}
	}
	return nil
}

func (repo *Repository) ResolveCreationTarget(ctx context.Context, reference string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return "", err
	}
	defer repo.mu.Unlock()
	switch {
	case strings.HasPrefix(reference, "refs/heads/"), strings.HasPrefix(reference, "refs/tags/"):
		if _, err := repo.git(ctx, "check-ref-format", reference); err != nil {
			return "", errors.New("创建起点无效")
		}
	default:
		if err := validCommitHash(reference); err != nil {
			return "", err
		}
	}
	output, err := repo.git(ctx, "rev-parse", "--verify", "--end-of-options", reference+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("无法读取起点对应的提交：%w", err)
	}
	return strings.TrimSpace(output), nil
}

func (repo *Repository) CreateBranchAt(ctx context.Context, name, hash string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	if err := repo.checkReferenceName(ctx, name, false); err != nil {
		return err
	}
	if err := validCommitHash(hash); err != nil {
		return err
	}
	kind, err := repo.git(ctx, "cat-file", "-t", hash)
	if err != nil {
		return err
	}
	if strings.TrimSpace(kind) != "commit" {
		return errors.New("分支起点必须是提交对象")
	}
	_, err = repo.git(ctx, "branch", "--no-track", "--", name, hash)
	return err
}

func (repo *Repository) SwitchCreatedBranch(ctx context.Context, name, hash string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	if err := repo.validateBranch(ctx, name); err != nil {
		return err
	}
	if err := validCommitHash(hash); err != nil {
		return err
	}
	actual, err := repo.git(ctx, "rev-parse", "--verify", "--end-of-options", "refs/heads/"+name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(actual) != hash {
		return errors.New("新分支已被其他操作移动，请关闭表单并检查该分支")
	}
	_, err = repo.git(ctx, "switch", "--no-guess", "--no-overwrite-ignore", "--", name)
	return err
}
