// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// worktreePath 只移除 Git 输出的一次换行由调用方完成，路径本身的换行必须保留。
func worktreePath(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", errors.New("工作树路径无效")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent := absolute
	var missing []string
	for {
		_, err := os.Lstat(parent)
		if err == nil {
			canonical, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return "", err
			}
			for index := len(missing) - 1; index >= 0; index-- {
				canonical = filepath.Join(canonical, missing[index])
			}
			return filepath.Clean(canonical), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		missing = append(missing, filepath.Base(parent))
		next := filepath.Dir(parent)
		if next == parent {
			return "", errors.New("工作树路径没有可访问的父目录")
		}
		parent = next
	}
}

func worktreeCommonDir(ctx context.Context, path string) (string, error) {
	output, err := (&Repository{Path: path}).git(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return worktreePath(strings.TrimSuffix(output, "\n"))
}

func parseWorktrees(output string) ([]Worktree, error) {
	if output == "" || !strings.HasSuffix(output, "\x00\x00") {
		return nil, errors.New("Git 工作树列表不完整")
	}
	result := []Worktree{}
	var current *Worktree
	for _, record := range strings.Split(output, "\x00") {
		if record == "" {
			current = nil
			continue
		}
		key, value, _ := strings.Cut(record, " ")
		if key == "worktree" {
			if current != nil || !filepath.IsAbs(value) {
				return nil, errors.New("无法解析 Git 工作树路径")
			}
			result = append(result, Worktree{Path: value, Main: len(result) == 0})
			current = &result[len(result)-1]
			continue
		}
		if current == nil {
			return nil, errors.New("Git 工作树记录缺少路径")
		}
		switch key {
		case "HEAD":
			if err := validCommitHash(value); err != nil {
				return nil, err
			}
			if strings.Trim(value, "0") != "" {
				current.Head = value
			}
		case "branch":
			current.Branch = value
		case "detached":
			current.Detached = true
		case "bare":
			current.Bare = true
		case "locked":
			current.Locked, current.LockReason = true, value
		case "prunable":
			current.Prunable, current.PruneReason = true, value
		}
	}
	return result, nil
}

func (repo *Repository) worktrees(ctx context.Context, common string) ([]worktreeState, error) {
	output, err := repo.git(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	list, err := parseWorktrees(output)
	if err != nil {
		return nil, err
	}
	current, err := worktreePath(repo.Path)
	if err != nil {
		return nil, err
	}
	result := make([]worktreeState, 0, len(list))
	for _, worktree := range list {
		state := worktreeState{Worktree: worktree}
		canonical, pathErr := worktreePath(worktree.Path)
		if pathErr != nil {
			state.Worktree.Problem = "工作树路径无法访问：" + pathErr.Error()
		} else {
			state.Worktree.Path = canonical
			state.Worktree.Current = worktreeSamePath(canonical, current)
			info, linkErr := os.Lstat(worktree.Path)
			if linkErr == nil && info.Mode()&os.ModeSymlink != 0 {
				state.Worktree.Problem = "工作树目录已被符号链接替换，请先修复"
			} else if err := state.inspect(ctx, common); err != nil {
				state.Worktree.Problem = err.Error()
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result = append(result, state)
	}
	return result, nil
}

func (state *worktreeState) inspect(ctx context.Context, common string) error {
	root := state.Worktree.Path
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("工作树不存在或无法访问：%w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("工作树路径不是普通目录")
	}
	state.rootInfo = info
	state.RootStamp, err = worktreeFileIdentity(root, info)
	if err != nil {
		return err
	}
	state.RootStamp += fmt.Sprintf(":%d:%d", info.Mode(), info.ModTime().UnixNano())
	actualCommon, err := worktreeCommonDir(ctx, root)
	if err != nil {
		return fmt.Errorf("无法读取工作树归属：%w", err)
	}
	if !worktreeSamePath(actualCommon, common) {
		return errors.New("工作树目录已被其他仓库替换，Git common dir 不匹配")
	}
	state.commonInfo, err = os.Stat(common)
	if err != nil {
		return err
	}
	state.CommonStamp, err = worktreeFileIdentity(common, state.commonInfo)
	if err != nil {
		return err
	}
	if state.Worktree.Bare {
		return nil
	}
	source := &Repository{Path: root}
	toplevel, err := source.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	canonical, err := worktreePath(strings.TrimSuffix(toplevel, "\n"))
	if err != nil || !worktreeSamePath(canonical, root) {
		return errors.New("工作树根目录已改变，请刷新核实")
	}
	directory, err := source.git(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	state.GitDir, err = worktreePath(strings.TrimSuffix(directory, "\n"))
	if err != nil {
		return err
	}
	state.gitDirInfo, err = os.Stat(state.GitDir)
	if err != nil {
		return err
	}
	state.GitDirStamp, err = worktreeFileIdentity(state.GitDir, state.gitDirInfo)
	if err != nil {
		return err
	}
	if !state.Worktree.Main {
		if err := state.inspectLink(common); err != nil {
			return err
		}
	} else if err := state.inspectGitFile(); err != nil {
		return err
	}
	head, err := source.head(ctx)
	if err != nil {
		return err
	}
	branch, branchErr := source.git(ctx, "symbolic-ref", "--quiet", "HEAD")
	if branchErr != nil && !exitCode(branchErr, 1) {
		return branchErr
	}
	branch = strings.TrimSuffix(branch, "\n")
	detached := exitCode(branchErr, 1)
	if head != state.Worktree.Head || branch != state.Worktree.Branch || detached != state.Worktree.Detached {
		return errors.New("工作树 HEAD 或分支在读取期间已改变，请刷新")
	}
	operation, err := source.operation(ctx)
	if err != nil {
		return err
	}
	state.Worktree.Operation, state.OperationToken = operation.Kind, operation.Token
	// 禁用可选索引写入及状态缓存，读取不得刷新索引或漏掉未跟踪、忽略文件。
	state.Status, err = source.git(ctx, "--no-optional-locks", "-c", "core.fsmonitor=false",
		"-c", "core.untrackedCache=false", "-c", "core.ignoreStat=false", "status",
		"--porcelain=v1", "-z", "--untracked-files=all", "--ignored=matching", "--ignore-submodules=none")
	if err != nil {
		return err
	}
	records := strings.Split(state.Status, "\x00")
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" {
			continue
		}
		if len(record) < 4 || record[2] != ' ' {
			return errors.New("无法解析工作树文件状态")
		}
		if record[:2] == "!!" {
			state.Worktree.Ignored = true
		} else {
			state.Worktree.Dirty = true
		}
		if strings.ContainsAny(record[:2], "RC") {
			index++
			if index >= len(records) || records[index] == "" {
				return errors.New("工作树重命名状态不完整")
			}
		}
	}
	flags, err := source.git(ctx, "ls-files", "-v", "-z")
	if err != nil {
		return err
	}
	for _, record := range strings.Split(flags, "\x00") {
		// Git status 会隐藏这些索引条目的本地编辑，不能据此授权删除数据。
		if record != "" && (record[0] == 'S' || record[0] >= 'a' && record[0] <= 'z') {
			return errors.New("索引含有 assume-unchanged 或 skip-worktree 条目，无法确认工作树数据已保存")
		}
	}
	return nil
}

func (state *worktreeState) inspectLink(common string) error {
	relative, err := filepath.Rel(filepath.Join(common, "worktrees"), state.GitDir)
	if err != nil || relative == "." || filepath.Base(relative) != relative || relative == ".." {
		return errors.New("链接工作树的 Git 元数据不属于当前仓库")
	}
	if err := state.inspectGitFile(); err != nil {
		return err
	}
	gitFile := filepath.Join(state.Worktree.Path, ".git")
	backlink, err := os.ReadFile(filepath.Join(state.GitDir, "gitdir"))
	if err != nil {
		return err
	}
	backPath, err := worktreePath(strings.TrimSuffix(string(backlink), "\n"))
	if err != nil || !worktreeSamePath(backPath, gitFile) {
		return errors.New("链接工作树目录与 Git 元数据的反向关联不匹配")
	}
	return nil
}

func (state *worktreeState) inspectGitFile() error {
	gitFile := filepath.Join(state.Worktree.Path, ".git")
	info, err := os.Lstat(gitFile)
	if err != nil || !info.Mode().IsRegular() && !(state.Worktree.Main && info.IsDir()) {
		return errors.New("工作树 .git 元数据已缺失或被替换")
	}
	state.gitFileInfo = info
	state.GitFileStamp, err = worktreeFileIdentity(gitFile, info)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	state.GitFileStamp += fmt.Sprintf(":%d:%d", info.Mode(), info.ModTime().UnixNano())
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return err
	}
	state.GitFile = string(data)
	return nil
}

func (state worktreeState) checkIdentity(ctx context.Context, common string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(state.Worktree.Path)
	if err != nil || state.rootInfo == nil || !os.SameFile(info, state.rootInfo) || !info.IsDir() {
		return errors.New("工作树目录在复核后已被替换，请刷新")
	}
	actualCommon, err := worktreeCommonDir(ctx, state.Worktree.Path)
	if err != nil {
		return err
	}
	if !worktreeSamePath(actualCommon, common) {
		return errors.New("工作树 Git common dir 已改变，请刷新")
	}
	// 路径及内容可被原样复制，确认还必须绑定实际 Git 元数据目录的身份。
	commonInfo, err := os.Stat(common)
	if err != nil || state.commonInfo == nil || !os.SameFile(commonInfo, state.commonInfo) {
		return errors.New("共享 Git 元数据目录在复核后已被替换，请刷新")
	}
	gitDirInfo, err := os.Stat(state.GitDir)
	if err != nil || state.gitDirInfo == nil || !os.SameFile(gitDirInfo, state.gitDirInfo) {
		return errors.New("工作树 Git 元数据目录在复核后已被替换，请刷新")
	}
	gitFile := filepath.Join(state.Worktree.Path, ".git")
	info, err = os.Lstat(gitFile)
	if err != nil || state.gitFileInfo == nil || !os.SameFile(info, state.gitFileInfo) ||
		!info.Mode().IsRegular() && !(state.Worktree.Main && info.IsDir()) {
		return errors.New("工作树 .git 文件在复核后已被替换，请刷新")
	}
	if info.IsDir() {
		return nil
	}
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return err
	}
	if string(data) != state.GitFile {
		return errors.New("工作树 Git 关联在复核后已改变，请刷新")
	}
	return nil
}

func worktreeContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return true
	}
	// macOS 的大小写别名不能绕过包含关系；缺失的末级路径从现存祖先核对。
	for current := child; ; current = filepath.Dir(current) {
		if worktreeSamePath(parent, current) {
			return true
		}
		if filepath.Dir(current) == current {
			return false
		}
	}
}

func worktreeSamePath(first, second string) bool {
	if first == second {
		return true
	}
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	return firstErr == nil && secondErr == nil && os.SameFile(firstInfo, secondInfo)
}

func worktreeCreatePath(path, common string, states []worktreeState) (string, error) {
	original, err := filepath.Abs(path)
	if path == "" || err != nil || strings.ContainsRune(path, 0) {
		return "", errors.New("工作树路径无效")
	}
	for current := original; ; current = filepath.Dir(current) {
		if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
			// 系统根目录下的路径别名（如 /var）可规范化，其余链接不作为创建入口。
			root := filepath.VolumeName(current) + string(filepath.Separator)
			if current == original || filepath.Dir(current) != root {
				return "", errors.New("不能在符号链接位置创建工作树")
			}
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	canonical, err := worktreePath(original)
	if err != nil {
		return "", err
	}
	for _, candidate := range []string{original, canonical} {
		for _, part := range strings.Split(filepath.ToSlash(candidate), "/") {
			if strings.EqualFold(part, ".git") {
				return "", errors.New("不能在 Git 元数据中创建工作树")
			}
		}
	}
	if worktreeContains(common, canonical) || worktreeContains(canonical, common) {
		return "", errors.New("工作树路径与 Git 元数据目录重叠")
	}
	for _, state := range states {
		if worktreeContains(state.Worktree.Path, canonical) || worktreeContains(canonical, state.Worktree.Path) {
			return "", errors.New("工作树路径不能与当前或其他已知工作树重叠")
		}
	}
	info, err := os.Lstat(canonical)
	if os.IsNotExist(err) {
		return canonical, nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("工作树路径已被文件或符号链接占用")
	}
	directory, err := os.Open(canonical)
	if err != nil {
		return "", err
	}
	defer directory.Close()
	_, err = directory.Readdirnames(1)
	if !errors.Is(err, io.EOF) {
		if err != nil {
			return "", err
		}
		return "", errors.New("工作树路径已存在且不是空目录")
	}
	return canonical, nil
}
