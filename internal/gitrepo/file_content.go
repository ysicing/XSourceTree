// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const fileContentLimit = 1 << 20

// FileContent 描述 diff 一侧的只读完整版本；不可预览时 Text 为空，不能当作空文件。
type FileContent struct {
	Path        string `json:"path"`
	Revision    string `json:"revision"`
	Text        string `json:"text"`
	Present     bool   `json:"present"`
	Binary      bool   `json:"binary"`
	Truncated   bool   `json:"truncated"`
	Unsupported bool   `json:"unsupported"`
}

func (repo *Repository) diffFileContents(ctx context.Context, diff Diff, selected File, staged bool) (Diff, error) {
	oldPath := selected.Path
	if staged && strings.ContainsAny(selected.Index, "RC") || !staged && strings.ContainsAny(selected.Working, "RC") {
		if selected.OriginalPath != "" {
			oldPath = selected.OriginalPath
		}
	}
	var err error
	if staged {
		head, headErr := repo.head(ctx)
		if headErr != nil {
			return Diff{}, headErr
		}
		diff.Old, err = repo.treeFileContent(ctx, oldPath, "HEAD", head)
	} else {
		diff.Old, err = repo.indexFileContent(ctx, oldPath)
	}
	if err != nil {
		return Diff{}, err
	}
	if staged {
		diff.New, err = repo.indexFileContent(ctx, selected.Path)
	} else {
		diff.New, err = repo.worktreeFileContent(ctx, selected.Path)
	}
	if err != nil {
		return Diff{}, err
	}
	return diff, nil
}

func (repo *Repository) commitFileContents(ctx context.Context, diff Diff, selected CommitFile, hash string) (Diff, error) {
	// 固定实际提交及第一父提交，不把可变引用或路径表达式交给 cat-file。
	output, err := repo.git(ctx, "rev-list", "--parents", "-n", "1", hash, "--")
	if err != nil {
		return Diff{}, err
	}
	parents := strings.Fields(output)
	if len(parents) == 0 {
		return Diff{}, errors.New("无法读取提交及父提交")
	}
	oldPath := selected.Path
	if selected.OriginalPath != "" {
		oldPath = selected.OriginalPath
	}
	parent := ""
	if len(parents) > 1 {
		parent = parents[1]
	}
	diff.Old, err = repo.treeFileContent(ctx, oldPath, parent, parent)
	if err != nil {
		return Diff{}, err
	}
	diff.New, err = repo.treeFileContent(ctx, selected.Path, parents[0], parents[0])
	if err != nil {
		return Diff{}, err
	}
	return diff, nil
}

func (repo *Repository) treeFileContent(ctx context.Context, path, revision, hash string) (*FileContent, error) {
	side := &FileContent{Path: path, Revision: revision}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if hash == "" {
		return side, nil
	}
	// -l 同时给出 blob 大小，省去单独的 cat-file -s。
	output, err := repo.git(ctx, "ls-tree", "-l", "-z", hash, "--", path)
	if err != nil {
		return nil, err
	}
	for _, record := range strings.Split(output, "\x00") {
		metadata, name, ok := strings.Cut(record, "\t")
		if !ok || name != path {
			continue
		}
		fields := strings.Fields(metadata)
		if len(fields) != 4 {
			return nil, errors.New("无法解析文件树对象")
		}
		// 目录本身不是 diff 的文件一侧（例如文件改为同名目录）。
		if fields[1] == "tree" {
			return side, nil
		}
		size := int64(-1)
		if fields[1] == "blob" {
			if size, err = strconv.ParseInt(fields[3], 10, 64); err != nil || size < 0 {
				return nil, errors.New("文件对象大小无效")
			}
		}
		return repo.blobFileContentSized(ctx, side, fields[0], fields[2], size)
	}
	return side, nil
}

func (repo *Repository) indexFileContent(ctx context.Context, path string) (*FileContent, error) {
	side := &FileContent{Path: path, Revision: "INDEX"}
	output, err := repo.git(ctx, "ls-files", "--stage", "-z", "--", path)
	if err != nil {
		return nil, err
	}
	for _, record := range strings.Split(output, "\x00") {
		metadata, name, ok := strings.Cut(record, "\t")
		if !ok || name != path {
			continue
		}
		fields := strings.Fields(metadata)
		if len(fields) != 3 {
			return nil, errors.New("无法解析索引文件对象")
		}
		if fields[2] != "0" {
			// 冲突索引没有唯一完整版本，不能将某一冲突侧冒充 INDEX。
			side.Present, side.Unsupported = true, true
			return side, nil
		}
		return repo.blobFileContent(ctx, side, fields[0], fields[1])
	}
	return side, nil
}

func (repo *Repository) blobFileContent(ctx context.Context, side *FileContent, mode, hash string) (*FileContent, error) {
	return repo.blobFileContentSized(ctx, side, mode, hash, -1)
}

// blobFileContentSized 的 size 为 -1 时先查询大小；已从 ls-tree -l 得到大小时直接使用。
func (repo *Repository) blobFileContentSized(ctx context.Context, side *FileContent, mode, hash string, size int64) (*FileContent, error) {
	side.Present = true
	if mode != "100644" && mode != "100755" {
		side.Unsupported = true
		return side, nil
	}
	// 先查询大小，避免把大 blob 加载到内存；对象消失或读取失败必须向上报错。
	if size < 0 {
		output, err := repo.git(ctx, "cat-file", "-s", hash)
		if err != nil {
			return nil, err
		}
		size, err = strconv.ParseInt(strings.TrimSpace(output), 10, 64)
		if err != nil || size < 0 {
			return nil, errors.New("文件对象大小无效")
		}
	}
	if size > fileContentLimit {
		side.Truncated = true
		return side, nil
	}
	content, err := repo.git(ctx, "cat-file", "blob", hash)
	if err != nil {
		return nil, err
	}
	setFileContentText(side, content)
	return side, nil
}

func setFileContentText(side *FileContent, content string) {
	if len(content) > fileContentLimit {
		side.Truncated = true
		return
	}
	side.Binary = strings.ContainsRune(content, 0) || !utf8.ValidString(content)
	if !side.Binary {
		side.Text = content
	}
}

func (repo *Repository) worktreeFileContent(ctx context.Context, path string) (*FileContent, error) {
	side := &FileContent{Path: path, Revision: "WORKTREE"}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(repo.Path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err := repo.checkConflictAncestors(ctx, root, path); err != nil {
		if errors.Is(err, errUnsupportedAncestor) {
			// Git 将被替换目录中的跟踪项视为删除；不能穿过祖先链接读取替代内容。
			side.Unsupported = true
			return side, nil
		}
		return nil, err
	}
	info, err := root.Lstat(path)
	if os.IsNotExist(err) {
		return side, nil
	}
	if err != nil {
		return nil, err
	}
	side.Present = true
	if !info.Mode().IsRegular() {
		side.Unsupported = true
		return side, nil
	}
	if info.Size() > fileContentLimit {
		side.Truncated = true
		return side, nil
	}
	file, err := openFileContent(root, path, info)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, fileContentLimit+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	setFileContentText(side, string(content))
	return side, nil
}

func openFileContent(root *os.Root, path string, info os.FileInfo) (*os.File, error) {
	if err := validPath(path); err != nil {
		return nil, err
	}
	if filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
		return nil, errors.New("请使用仓库内的规范相对路径")
	}
	// Lstat 与打开之间可能换成 FIFO 或链接；打开本身必须不阻塞且不跟随链接。
	file, err := openFileContentPath(root, path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("读取文件时内容已被替换，请刷新后重试")
	}
	return file, nil
}
