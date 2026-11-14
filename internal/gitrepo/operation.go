// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	return repo.MergeWithOptions(ctx, branch, MergeOptions{Mode: MergeAuto, Commit: true})
}

type MergeMode string

const (
	MergeAuto   MergeMode = "auto"
	MergeNoFF   MergeMode = "no-ff"
	MergeFFOnly MergeMode = "ff-only"
	MergeSquash MergeMode = "squash"
)

type MergeOptions struct {
	Mode   MergeMode
	Commit bool
}

func (repo *Repository) MergeWithOptions(ctx context.Context, branch string, options MergeOptions) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args := []string{"merge", "--no-edit", "--no-autostash", "--no-overwrite-ignore"}
	switch options.Mode {
	case MergeAuto:
		args = append(args, "--no-squash", "--ff")
	case MergeNoFF:
		args = append(args, "--no-squash", "--no-ff")
	case MergeFFOnly:
		args = append(args, "--no-squash", "--ff-only", "--strategy=ort")
	case MergeSquash:
		args = append(args, "--squash", "--ff")
	default:
		return errors.New("不支持的合并方式")
	}
	if options.Commit && options.Mode != MergeSquash {
		args = append(args, "--commit")
	} else {
		args = append(args, "--no-commit")
	}
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
		return errors.New("请先完成当前操作，并提交或贮藏工作区变更")
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
	if options.Mode == MergeFFOnly {
		if _, err := repo.git(ctx, "merge-base", "--is-ancestor", "HEAD", ref); err != nil {
			if !exitCode(err, 1) {
				return err
			}
			if _, err := repo.git(ctx, "merge-base", "--is-ancestor", ref, "HEAD"); err == nil {
				return nil
			} else if !exitCode(err, 1) {
				return err
			}
			return errors.New("分支已分叉，无法仅快进合并")
		}
		current, err := repo.git(ctx, "symbolic-ref", "--quiet", "HEAD")
		if err != nil && !exitCode(err, 1) {
			return err
		}
		if current = strings.TrimPrefix(strings.TrimSpace(current), "refs/heads/"); current != "" {
			// ours 等继承策略会在参数解析后取消 --ff-only，必须隔离这次调用的分支策略。
			args = append([]string{"-c", "branch." + current + ".mergeOptions="}, args...)
		}
	}
	if err := repo.checkMergeIgnoredPaths(ctx, ref); err != nil {
		return err
	}
	if _, err = run(ctx, repo.Path, "", 2*time.Minute, append(args, "--", ref)...); err != nil {
		return err
	}
	if options.Mode != MergeSquash || !options.Commit {
		return nil
	}
	// squash 不建立 MERGE_HEAD；仅在确有暂存结果时单独提交，失败时保留暂存区供重试。
	changed, err := repo.git(ctx, "diff", "--cached", "--name-only", "-z")
	if err != nil || changed == "" {
		return err
	}
	_, err = run(ctx, repo.Path, "", 2*time.Minute, "commit", "--no-edit")
	return err
}

func (repo *Repository) checkMergeIgnoredPaths(ctx context.Context, ref string) error {
	// 部分 Git 合并策略仍会覆盖忽略文件，不能只依赖 --no-overwrite-ignore。
	ignored, err := repo.git(ctx, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil || ignored == "" {
		return err
	}
	if _, err := repo.git(ctx, "merge-base", "--is-ancestor", ref, "HEAD"); err == nil {
		return nil
	} else if !exitCode(err, 1) {
		return err
	}
	// 使用包含目录重命名结果的树检查实际写入目标，不能只比较两端路径。
	// --stdin 在冲突时仍返回成功，树中保留冲突结果，实际合并继续使用原来的冲突流程。
	output, err := run(ctx, repo.Path, "HEAD "+ref+"\n", 2*time.Minute,
		"merge-tree", "--write-tree", "--stdin", "--no-messages", "--name-only")
	if err != nil {
		return err
	}
	status, rest, _ := strings.Cut(output, "\x00")
	tree, _, _ := strings.Cut(rest, "\x00")
	hash, err := hex.DecodeString(tree)
	if err != nil || len(hash) != 20 && len(hash) != 32 || status != "0" && status != "1" {
		return errors.New("无法读取安全合并预检结果")
	}
	added, err := repo.git(ctx, "diff", "--no-ext-diff", "--no-renames", "--name-only", "--diff-filter=A", "-z", "HEAD", tree, "--")
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(repo.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	type ignoredPath struct {
		path string
		info os.FileInfo
	}
	var existing []ignoredPath
	for _, path := range strings.Split(strings.TrimSuffix(ignored, "\x00"), "\x00") {
		path = strings.TrimSuffix(path, "/")
		info, err := root.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		existing = append(existing, ignoredPath{path: path, info: info})
	}
	for _, path := range strings.Split(strings.TrimSuffix(added, "\x00"), "\x00") {
		if path == "" {
			continue
		}
		info, err := root.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("无法安全检查合并路径 %s：%w", path, err)
		}
		for _, ignored := range existing {
			collision := os.SameFile(info, ignored.info)
			if !collision && ignored.info.IsDir() {
				collision, err = mergePathInside(root, path, ignored.info)
			}
			if !collision && err == nil && info.IsDir() {
				collision, err = mergePathInside(root, ignored.path, info)
			}
			if err != nil {
				return err
			}
			if collision {
				return fmt.Errorf("合并会覆盖忽略的本地路径，请先移走或备份：%s", path)
			}
		}
	}
	return nil
}

func mergePathInside(root *os.Root, path string, directory os.FileInfo) (bool, error) {
	// 文件身份比较同时覆盖大小写不敏感卷和 Unicode 文件名别名。
	for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
		info, err := root.Stat(parent)
		if err != nil {
			return false, err
		}
		if os.SameFile(info, directory) {
			return true, nil
		}
	}
	return false, nil
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
