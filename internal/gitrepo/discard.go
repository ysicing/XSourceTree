// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type DiscardPreview struct {
	Token     string `json:"token"`
	Supported bool   `json:"supported"`
	Untracked bool   `json:"untracked"`
	Reason    string `json:"reason"`
}

type discardState struct {
	view      DiscardPreview
	content   []byte
	present   bool
	mode      os.FileMode
	indexMode string
	indexHash string
}

// DiscardPreview 绑定工作区原始内容、索引和 HEAD；不支持目录、链接、冲突和超过 4 MiB 的文件。
func (repo *Repository) DiscardPreview(ctx context.Context, path string) (DiscardPreview, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	state, err := repo.discardState(ctx, path)
	return state.view, err
}

func (repo *Repository) discardState(ctx context.Context, path string) (discardState, error) {
	state := discardState{view: DiscardPreview{Reason: "此文件不支持丢弃，请使用外部 Git 工具"}}
	if err := validPath(path); err != nil {
		return state, err
	}
	if filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
		return state, errors.New("请使用仓库内的规范相对路径")
	}
	if err := ctx.Err(); err != nil {
		return state, err
	}
	files, err := repo.files(ctx)
	if err != nil {
		return state, err
	}
	found := false
	for _, file := range files {
		if file.Path != path || file.Working == " " {
			continue
		}
		if file.Conflict || found {
			return state, nil
		}
		found = true
		state.view.Untracked = file.Index == "?"
	}
	if !found {
		return state, nil
	}
	index, err := repo.git(ctx, "ls-files", "--stage", "-z")
	if err != nil {
		return state, err
	}
	state.indexMode, state.indexHash = patchIndexEntry(index, path)
	if state.view.Untracked == (state.indexHash != "") {
		return state, nil
	}
	if state.indexHash != "" {
		// 比较 ITA 在索引中可见/不可见的结果，不依赖工作区状态：
		// ITA 既可能被识别为重命名，也可能已经从工作区删除。
		visible, err := repo.git(ctx, "diff", "--cached", "--raw", "-z", "--no-abbrev", "--no-renames",
			"--no-ext-diff", "--no-textconv", "--ita-visible-in-index", "--", path)
		if err != nil {
			return state, err
		}
		invisible, err := repo.git(ctx, "diff", "--cached", "--raw", "-z", "--no-abbrev", "--no-renames",
			"--no-ext-diff", "--no-textconv", "--ita-invisible-in-index", "--", path)
		if err != nil {
			return state, err
		}
		if visible != invisible {
			state.view.Reason = "intent-to-add 文件请先暂存或取消 intent-to-add"
			return state, nil
		}
	}
	head, err := repo.head(ctx)
	if err != nil {
		return state, err
	}
	root, err := os.OpenRoot(repo.Path)
	if err != nil {
		return state, err
	}
	defer root.Close()
	if err := repo.checkConflictAncestors(ctx, root, path); err != nil {
		return state, err
	}
	info, err := root.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return state, err
	}
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > outputLimit {
			state.view.Reason = "仅支持不超过 4 MiB 的普通文件，不处理目录或符号链接"
			return state, nil
		}
		file, err := root.Open(path)
		if err != nil {
			return state, err
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
			return state, errors.New("文件已被替换，请刷新后重试")
		}
		state.content, err = io.ReadAll(io.LimitReader(file, outputLimit+1))
		if err != nil {
			return state, err
		}
		if len(state.content) > outputLimit {
			return state, nil
		}
		state.present, state.mode = true, opened.Mode().Perm()
	} else if state.view.Untracked {
		return state, nil
	}
	digest := sha256.New()
	fmt.Fprintf(digest, "%q\x00%q\x00%q\x00%q\x00%t\x00%d\x00%t\x00", repo.Path, path, head, index, state.present, state.mode, state.view.Untracked)
	digest.Write(state.content)
	state.view.Token = hex.EncodeToString(digest.Sum(nil))
	state.view.Supported, state.view.Reason = true, ""
	return state, nil
}

// Discard 只修改工作区。recoveryDir 由桌面层提供，核心不依赖平台数据目录。
// 恢复副本持久化成功且版本仍匹配后才写文件；外部编辑器不参与此进程的锁。
func (repo *Repository) Discard(ctx context.Context, path, token, recoveryDir string) (backup string, err error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	state, err := repo.discardState(ctx, path)
	if err != nil {
		return "", err
	}
	if !state.view.Supported || token == "" || token != state.view.Token {
		return "", errors.New("文件、索引或 HEAD 已改变，或此文件不支持丢弃，请刷新后重试")
	}
	var restored string
	if !state.view.Untracked {
		// 固定索引 blob，使用 Git 的工作区转换（包括 CRLF），不重新读取可变化的索引引用。
		restored, err = repo.git(ctx, "cat-file", "--filters", "--path="+path, state.indexHash)
		if err != nil {
			return "", err
		}
	}
	backup, err = saveDiscardBackup(recoveryDir, repo.Path, path, state)
	if err != nil {
		return "", fmt.Errorf("无法保存恢复副本，已取消丢弃：%w", err)
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("%w；恢复副本：%s", err, backup)
		}
	}()
	root, err := os.OpenRoot(repo.Path)
	if err != nil {
		return backup, err
	}
	defer root.Close()
	if !state.view.Untracked {
		if err := root.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return backup, err
		}
	}
	if err := repo.checkConflictAncestors(ctx, root, path); err != nil {
		return backup, err
	}
	parent, err := root.OpenRoot(filepath.Dir(path))
	if err != nil {
		return backup, err
	}
	defer parent.Close()
	temp := ".xsourcetree-discard-" + rand.Text()
	if !state.view.Untracked {
		mode := state.mode
		if !state.present {
			mode = 0644
		}
		mode &^= 0111
		if state.indexMode == "100755" {
			mode |= 0111
		}
		file, err := parent.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return backup, err
		}
		defer parent.Remove(temp)
		writeErr := writeDiscardFile(file, []byte(restored), mode)
		closeErr := file.Close()
		if writeErr != nil {
			return backup, writeErr
		}
		if closeErr != nil {
			return backup, closeErr
		}
	}
	current, err := repo.discardState(ctx, path)
	if err != nil {
		return backup, err
	}
	if !current.view.Supported || current.view.Token != token {
		return backup, errors.New("文件、索引或 HEAD 已改变，已取消丢弃")
	}
	expectedParent, err := root.Stat(filepath.Dir(path))
	if err != nil {
		return backup, err
	}
	openedParent, err := parent.Stat(".")
	if err != nil || !os.SameFile(expectedParent, openedParent) {
		return backup, errors.New("文件父目录已改变，已取消丢弃")
	}
	if err := ctx.Err(); err != nil {
		return backup, err
	}
	if state.view.Untracked {
		err = parent.Remove(filepath.Base(path))
	} else {
		err = parent.Rename(temp, filepath.Base(path))
	}
	return backup, err
}

func saveDiscardBackup(directory, repository, path string, state discardState) (string, error) {
	if !filepath.IsAbs(directory) {
		return "", errors.New("恢复目录必须是绝对路径")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	backup, err := os.MkdirTemp(directory, "discard-")
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(backup)
		}
	}()
	metadata, err := json.MarshalIndent(struct {
		Repository string      `json:"repository"`
		Path       string      `json:"path"`
		Created    time.Time   `json:"created"`
		Present    bool        `json:"present"`
		Mode       os.FileMode `json:"mode"`
		Untracked  bool        `json:"untracked"`
	}{repository, path, time.Now().UTC(), state.present, state.mode, state.view.Untracked}, "", "  ")
	if err != nil {
		return "", err
	}
	for name, content := range map[string][]byte{"content": state.content, "metadata.json": metadata} {
		file, err := os.OpenFile(filepath.Join(backup, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", err
		}
		writeErr := writeDiscardFile(file, content, 0600)
		closeErr := file.Close()
		if writeErr != nil {
			return "", writeErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	complete = true
	return backup, nil
}

func writeDiscardFile(file *os.File, content []byte, mode os.FileMode) error {
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	return file.Sync()
}
