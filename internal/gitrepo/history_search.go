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

type CommitFile struct {
	Path         string `json:"path"`
	Status       string `json:"status"`
	OriginalPath string `json:"originalPath"`
}

type HistoryFilter struct {
	CurrentBranch bool
	HideRemotes   bool
}

func (repo *Repository) SearchHistory(ctx context.Context, query string, path string, skip int, filter HistoryFilter) ([]Commit, error) {
	if skip < 0 || skip > 1<<31-1 {
		return nil, errors.New("历史分页无效")
	}
	if strings.ContainsRune(query, 0) {
		return nil, errors.New("搜索内容无效")
	}
	if path != "" {
		if err := validPath(path); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return nil, err
	}
	defer repo.mu.Unlock()
	if filter.CurrentBranch {
		head, err := repo.head(ctx)
		if err != nil {
			return nil, err
		}
		if head == "" {
			return []Commit{}, nil
		}
	}
	return repo.streamHistory(ctx, query, path, skip, filter)
}

func validCommitHash(hash string) error {
	if len(hash) != 40 && len(hash) != 64 {
		return errors.New("提交 ID 无效")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return errors.New("提交 ID 无效")
	}
	return nil
}

func (repo *Repository) lockHistory(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if repo.mu.TryLock() {
		return nil
	}
	// 等待共享仓库锁也必须受 Context 限制，不能等到获取锁后才响应取消。
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := ctx.Err(); err != nil {
				return err
			}
			if repo.mu.TryLock() {
				return nil
			}
		}
	}
}

// CommitFiles 对首提交使用空树，对合并提交只比较第一父提交。
func (repo *Repository) CommitFiles(ctx context.Context, hash string) ([]CommitFile, error) {
	if err := validCommitHash(hash); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return nil, err
	}
	defer repo.mu.Unlock()
	return repo.commitFiles(ctx, hash)
}

func (repo *Repository) commitFiles(ctx context.Context, hash string) ([]CommitFile, error) {
	kind, err := repo.git(ctx, "cat-file", "-t", hash)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(kind) != "commit" {
		return nil, errors.New("提交 ID 未指向提交对象")
	}
	output, err := repo.git(ctx, "diff-tree", "--root", "--diff-merges=first-parent", "--no-commit-id",
		"--name-status", "-r", "-z", "--no-ext-diff", "--no-textconv", "--find-renames", hash, "--")
	if err != nil {
		return nil, err
	}
	files := []CommitFile{}
	if output == "" {
		return files, nil
	}
	if !strings.HasSuffix(output, "\x00") {
		return nil, errors.New("无法解析提交文件")
	}
	fields := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	for index := 0; index < len(fields); {
		status := fields[index]
		index++
		if status == "" || !strings.ContainsRune("ACDMRTUXB", rune(status[0])) || index >= len(fields) {
			return nil, errors.New("无法解析提交文件")
		}
		file := CommitFile{Status: status[:1], Path: fields[index]}
		index++
		if file.Status == "R" || file.Status == "C" {
			file.OriginalPath = file.Path
			if index >= len(fields) {
				return nil, errors.New("Git 重命名记录不完整")
			}
			file.Path = fields[index]
			index++
			if err := validPath(file.OriginalPath); err != nil {
				return nil, err
			}
		}
		if err := validPath(file.Path); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// CommitFileDiff 使用与 CommitFiles 相同的首提交及第一父提交规则。
func (repo *Repository) CommitFileDiff(ctx context.Context, hash string, path string) (Diff, error) {
	if err := validCommitHash(hash); err != nil {
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
	files, err := repo.commitFiles(ctx, hash)
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
		for index := range files {
			if files[index].OriginalPath == path {
				selected = &files[index]
				break
			}
		}
	}
	if selected == nil {
		return Diff{}, errors.New("文件不在此提交的变更中")
	}
	args := []string{"-c", "core.quotePath=true", "show", "--no-show-signature", "--format=", "--root",
		"--diff-merges=first-parent", "--patch", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "--no-relative",
		"--submodule=short", "--output-indicator-new=+", "--output-indicator-old=-", "--output-indicator-context= ",
		"--no-ext-diff", "--no-textconv", "--find-renames", hash, "--", selected.Path}
	// 保留重命名两端后再精确筛选 patch；字面 pathspec 仍会包含同名目录的后代。
	if selected.OriginalPath != "" {
		args = append(args, selected.OriginalPath)
	}
	output, err := repo.git(ctx, args...)
	if err != nil {
		return Diff{}, err
	}
	output, err = selectedCommitPatch(output, *selected)
	if err != nil {
		return Diff{}, err
	}
	return Diff{Text: output, Binary: strings.Contains(output, "Binary files ")}, nil
}

func selectedCommitPatch(output string, file CommitFile) (string, error) {
	original := file.Path
	if file.OriginalPath != "" {
		original = file.OriginalPath
	}
	header := "diff --git " + quoteCommitDiffPath("a/"+original) + " " + quoteCommitDiffPath("b/"+file.Path)
	var selected strings.Builder
	start := -1
	for cursor := 0; cursor < len(output); {
		end := strings.IndexByte(output[cursor:], '\n')
		if end < 0 {
			end = len(output)
		} else {
			end += cursor
		}
		line := output[cursor:end]
		if strings.HasPrefix(line, "diff --git ") {
			if start >= 0 {
				selected.WriteString(output[start:cursor])
			}
			start = -1
			if line == header {
				start = cursor
			}
		}
		cursor = end + 1
	}
	if start >= 0 {
		selected.WriteString(output[start:])
	}
	if selected.Len() == 0 {
		return "", errors.New("无法定位提交文件差异")
	}
	return selected.String(), nil
}

func quoteCommitDiffPath(path string) string {
	var quoted strings.Builder
	quoted.WriteByte('"')
	escaped := false
	for index := range len(path) {
		value := path[index]
		switch {
		case value == '"' || value == '\\':
			quoted.WriteByte('\\')
			quoted.WriteByte(value)
			escaped = true
		case value < ' ' || value >= 0x7f:
			special := strings.IndexByte("\a\b\t\n\v\f\r", value)
			if special >= 0 {
				quoted.WriteByte('\\')
				quoted.WriteByte("abtnvfr"[special])
			} else {
				fmt.Fprintf(&quoted, "\\%03o", value)
			}
			escaped = true
		default:
			quoted.WriteByte(value)
		}
	}
	if !escaped {
		return path
	}
	quoted.WriteByte('"')
	return quoted.String()
}
