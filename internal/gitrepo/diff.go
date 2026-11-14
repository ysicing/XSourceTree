// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

type Diff struct {
	Text      string       `json:"text"`
	Binary    bool         `json:"binary"`
	Truncated bool         `json:"truncated"`
	Old       *FileContent `json:"old,omitempty"`
	New       *FileContent `json:"new,omitempty"`
}

func (repo *Repository) Diff(ctx context.Context, path string, staged bool) (Diff, error) {
	if err := validPath(path); err != nil {
		return Diff{}, err
	}
	if err := repo.lockHistory(ctx); err != nil {
		return Diff{}, err
	}
	defer repo.mu.Unlock()
	files, err := repo.pathFiles(ctx, []string{path})
	if err != nil {
		return Diff{}, err
	}
	var selected *File
	for _, file := range files {
		if file.Path == path {
			selected = &file
			break
		}
	}
	if selected == nil {
		return Diff{}, errors.New("文件状态已改变，请刷新")
	}
	if !staged && selected.Index == "?" {
		diff, err := repo.untrackedDiff(path)
		if err != nil {
			return Diff{}, err
		}
		return repo.diffFileContents(ctx, diff, *selected, staged)
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--find-renames"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	if selected.OriginalPath != "" {
		args = append(args, selected.OriginalPath)
	}
	output, err := repo.git(ctx, args...)
	if err != nil {
		return Diff{}, err
	}
	return repo.diffFileContents(ctx, Diff{Text: output, Binary: strings.Contains(output, "Binary files ")}, *selected, staged)
}

func (repo *Repository) untrackedDiff(path string) (Diff, error) {
	root, err := os.OpenRoot(repo.Path)
	if err != nil {
		return Diff{}, err
	}
	defer root.Close()
	info, err := root.Lstat(path)
	if err != nil {
		return Diff{}, err
	}
	var content []byte
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := root.Readlink(path)
		if err != nil {
			return Diff{}, err
		}
		content = []byte(target)
	} else {
		if !info.Mode().IsRegular() {
			return Diff{}, errors.New("此文件类型不支持预览")
		}
		file, err := root.Open(path)
		if err != nil {
			return Diff{}, err
		}
		defer file.Close()
		content, err = io.ReadAll(io.LimitReader(file, (256<<10)+1))
		if err != nil {
			return Diff{}, err
		}
	}
	truncated := len(content) > 256<<10
	if truncated {
		content = content[:256<<10]
	}
	if strings.ContainsRune(string(content), 0) || !utf8.Valid(content) {
		return Diff{Text: "二进制文件不提供文本预览", Binary: true, Truncated: truncated}, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(content) == 0 {
		lines = nil
	}
	var preview strings.Builder
	fmt.Fprintf(&preview, "diff --git a/%s b/%s\nnew file\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", path, path, path, len(lines))
	for _, line := range lines {
		preview.WriteString("+" + line + "\n")
	}
	if len(content) > 0 && content[len(content)-1] != '\n' && !truncated {
		preview.WriteString("\\ No newline at end of file\n")
	}
	return Diff{Text: preview.String(), Truncated: truncated}, nil
}

func (repo *Repository) CommitDiff(ctx context.Context, hash string) (Diff, error) {
	if len(hash) != 40 && len(hash) != 64 {
		return Diff{}, errors.New("提交 ID 无效")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return Diff{}, errors.New("提交 ID 无效")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	output, err := repo.git(ctx, "show", "--no-show-signature", "--format=fuller", "--root", "--diff-merges=first-parent", "--stat", "--patch", "--no-color", "--no-ext-diff", "--no-textconv", "--find-renames", hash, "--")
	return Diff{Text: output, Binary: strings.Contains(output, "Binary files ")}, err
}
