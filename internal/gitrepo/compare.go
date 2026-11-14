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

// Comparison 固定比较两端的提交 ID；From 在共同祖先模式下为实际比较起点。
type Comparison struct {
	Base   string       `json:"base"`
	Target string       `json:"target"`
	From   string       `json:"from"`
	Files  []CommitFile `json:"files"`
}

// Compare 将输入固定为提交 ID，再比较快照或唯一共同祖先到目标的改动。
func (repo *Repository) Compare(ctx context.Context, base, target string, mergeBase bool) (Comparison, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return Comparison{}, err
	}
	defer repo.mu.Unlock()
	var result Comparison
	var err error
	if result.Base, err = repo.comparisonCommit(ctx, base); err != nil {
		return result, err
	}
	if result.Target, err = repo.comparisonCommit(ctx, target); err != nil {
		return result, err
	}
	result.From = result.Base
	if mergeBase {
		output, err := repo.git(ctx, "merge-base", "--all", result.Base, result.Target)
		if err != nil {
			if exitCode(err, 1) {
				return result, errors.New("两个版本没有共同祖先，请使用两端快照比较")
			}
			return result, fmt.Errorf("无法确定共同祖先：%w", err)
		}
		ancestors := strings.Fields(output)
		if len(ancestors) != 1 {
			return result, errors.New("存在多个共同祖先，请使用两端快照比较")
		}
		result.From = ancestors[0]
	}
	result.Files, err = repo.comparisonFiles(ctx, result.From, result.Target)
	return result, err
}

func (repo *Repository) comparisonCommit(ctx context.Context, revision string) (string, error) {
	revision = strings.TrimSpace(revision)
	if revision == "" || strings.ContainsRune(revision, 0) {
		return "", errors.New("比较版本不能为空或包含无效字符")
	}
	output, err := repo.git(ctx, "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("无法读取比较版本 %s：%w", revision, err)
	}
	hash := strings.TrimSpace(output)
	return hash, validCommitHash(hash)
}

func (repo *Repository) comparisonFiles(ctx context.Context, from, target string) ([]CommitFile, error) {
	output, err := repo.git(ctx, "diff", "--name-status", "-z", "--no-relative",
		"--no-ext-diff", "--no-textconv", "--find-renames", from, target, "--")
	if err != nil {
		return nil, err
	}
	return parseCommitFiles(output)
}

// CompareFileDiff 仅接受已解析的提交 ID，防止切换文件时跟随移动的引用。
func (repo *Repository) CompareFileDiff(ctx context.Context, from, target, path string) (Diff, error) {
	if err := validCommitHash(from); err != nil {
		return Diff{}, err
	}
	if err := validCommitHash(target); err != nil {
		return Diff{}, err
	}
	if err := validPath(path); err != nil {
		return Diff{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return Diff{}, err
	}
	defer repo.mu.Unlock()
	files, err := repo.comparisonFiles(ctx, from, target)
	if err != nil {
		return Diff{}, err
	}
	var selected *CommitFile
	for index := range files {
		if files[index].Path == path {
			selected = &files[index]
			break
		}
	}
	if selected == nil {
		return Diff{}, errors.New("文件不在此比较的变更中")
	}
	args := []string{"-c", "core.quotePath=true", "diff", "--patch", "--no-color",
		"--src-prefix=a/", "--dst-prefix=b/", "--no-relative", "--submodule=short",
		"--output-indicator-new=+", "--output-indicator-old=-", "--output-indicator-context= ",
		"--no-ext-diff", "--no-textconv", "--find-renames", from, target, "--", selected.Path}
	oldPath := selected.Path
	if selected.OriginalPath != "" {
		oldPath = selected.OriginalPath
		args = append(args, oldPath)
	}
	output, err := repo.git(ctx, args...)
	if err != nil {
		return Diff{}, err
	}
	output, err = selectedCommitPatch(output, *selected)
	if err != nil {
		return Diff{}, err
	}
	diff := Diff{Text: output, Binary: strings.Contains(output, "Binary files ")}
	if diff.Old, err = repo.treeFileContent(ctx, oldPath, from, from); err != nil {
		return Diff{}, err
	}
	diff.New, err = repo.treeFileContent(ctx, selected.Path, target, target)
	return diff, err
}
