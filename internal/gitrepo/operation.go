// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Operation struct {
	Kind  string `json:"kind"`
	Token string `json:"token"`
}

func (repo *Repository) operation(ctx context.Context) (Operation, error) {
	directory, err := repo.git(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return Operation{}, err
	}
	directory = strings.TrimSuffix(directory, "\n")
	for _, marker := range []struct{ path, kind string }{
		{"rebase-merge", "rebase"}, {"rebase-apply", "rebase"},
		{"MERGE_HEAD", "merge"}, {"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"},
		{"sequencer", "sequencer"},
	} {
		info, err := os.Stat(filepath.Join(directory, marker.path))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Operation{}, err
		}
		kind := marker.kind
		var data []byte
		if info.IsDir() {
			for _, name := range []string{"head", "orig-head", "head-name", "todo", "git-rebase-todo", "next", "original-commit", "stopped-sha"} {
				content, readErr := os.ReadFile(filepath.Join(directory, marker.path, name))
				if readErr != nil && !os.IsNotExist(readErr) {
					return Operation{}, readErr
				}
				if kind == "sequencer" && name == "todo" {
					if strings.HasPrefix(strings.TrimSpace(string(content)), "revert ") {
						kind = "revert"
					} else {
						kind = "cherry-pick"
					}
				}
				data = append(data, []byte(name+"\x00")...)
				data = append(data, content...)
				data = append(data, 0)
			}
			if kind == "sequencer" {
				kind = "cherry-pick"
			}
		} else {
			data, err = os.ReadFile(filepath.Join(directory, marker.path))
			if err != nil {
				return Operation{}, err
			}
		}
		head, err := repo.head(ctx)
		if err != nil {
			return Operation{}, err
		}
		sum := sha256.Sum256(append([]byte(kind+"\x00"+head+"\x00"), data...))
		return Operation{Kind: kind, Token: hex.EncodeToString(sum[:])}, nil
	}
	return Operation{}, nil
}

func (repo *Repository) Merge(ctx context.Context, branch string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	name := strings.TrimPrefix(strings.TrimPrefix(branch, "refs/heads/"), "refs/remotes/")
	if err := repo.validateBranch(ctx, name); err != nil {
		return err
	}
	operation, err := repo.operation(ctx)
	if err != nil {
		return err
	}
	files, err := repo.files(ctx)
	if err != nil {
		return err
	}
	if operation.Kind != "" || len(files) > 0 {
		return errors.New("请先完成当前操作，并提交或储藏工作区变更")
	}
	ref := branch
	if !strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/remotes/") {
		ref = "refs/heads/" + branch
	}
	if _, err := repo.git(ctx, "show-ref", "--verify", "--quiet", ref); err != nil {
		if strings.HasPrefix(branch, "refs/") {
			return errors.New("分支不存在，请刷新")
		}
		ref = "refs/remotes/" + name
		if _, err := repo.git(ctx, "show-ref", "--verify", "--quiet", ref); err != nil {
			return errors.New("分支不存在，请刷新")
		}
	}
	_, err = run(ctx, repo.Path, "", 2*time.Minute, "merge", "--no-edit", "--no-autostash", "--", ref)
	return err
}

func (repo *Repository) OperationAction(ctx context.Context, kind, token, action string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	operation, err := repo.operation(ctx)
	if err != nil {
		return err
	}
	if operation.Kind == "" || operation.Kind != kind || operation.Token != token {
		return errors.New("Git 操作已改变，请刷新后再试")
	}
	if action != "continue" && action != "abort" && action != "skip" {
		return errors.New("不支持的 Git 操作")
	}
	if action == "skip" && kind == "merge" {
		return errors.New("合并操作不能跳过")
	}
	if action == "continue" {
		files, err := repo.files(ctx)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.Conflict {
				return errors.New("请先解决所有冲突")
			}
		}
	}
	_, err = run(ctx, repo.Path, "", 2*time.Minute, kind, "--"+action)
	return err
}
