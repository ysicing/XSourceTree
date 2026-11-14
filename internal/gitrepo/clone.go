// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Clone 将仓库克隆到已存在父目录下的新目录，不覆盖已有目标。
func Clone(ctx context.Context, source, parent, name string) (*Repository, error) {
	if err := validateClone(source, parent, name); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parent, err := filepath.Abs(parent)
	if err != nil {
		return nil, err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return nil, fmt.Errorf("无法访问克隆父目录：%w", err)
	}
	// Git 在临时目录中运行，相对本地源必须在切换目录前解析。
	if !strings.Contains(source, ":") && !filepath.IsAbs(source) {
		source, err = filepath.Abs(source)
		if err != nil {
			return nil, err
		}
	}
	target := filepath.Join(parent, name)
	if err := os.Mkdir(target, 0755); err != nil {
		return nil, fmt.Errorf("无法创建克隆目录（目标必须不存在）：%w", err)
	}
	owned, err := os.Lstat(target)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		removeCloneReservation(target, owned)
		return nil, err
	}
	defer root.Close()
	defer removeCloneReservation(target, owned)
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(owned, opened) {
		return nil, errors.New("克隆目标已被外部替换，已保留外部内容")
	}

	// Root 将清理和发布固定在原目录；目标被移动或替换后不会递归删除新目标。
	stage := ".xsource-clone-" + rand.Text()
	if err := root.Mkdir(stage, 0700); err != nil {
		return nil, err
	}
	staging, err := root.OpenRoot(stage)
	if err != nil {
		_ = root.Remove(stage)
		return nil, err
	}
	defer staging.Close()
	staged, err := staging.Stat(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		directory, err := staging.Open(".")
		if err != nil {
			return
		}
		entries, readErr := directory.ReadDir(-1)
		_ = directory.Close()
		if readErr != nil {
			return
		}
		for _, entry := range entries {
			_ = staging.RemoveAll(entry.Name())
		}
		// 只删除空的临时目录，不沿目标路径执行 RemoveAll。
		if current, err := root.Lstat(stage); err == nil && os.SameFile(staged, current) {
			_ = root.Remove(stage)
		}
	}()

	stagePath := filepath.Join(target, stage)
	options, err := remoteOptions(ctx, stagePath)
	if err != nil {
		return nil, err
	}
	if _, err := run(ctx, stagePath, "", remoteTimeout, append(options, "clone", "--", source, ".")...); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !sameCloneDirectory(target, owned) {
		return nil, errors.New("克隆目标已被外部替换，已保留外部内容")
	}
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := directory.ReadDir(-1)
	_ = directory.Close()
	if err != nil {
		return nil, err
	}
	if len(entries) != 1 || entries[0].Name() != stage {
		return nil, errors.New("克隆目标已被外部修改，已保留外部内容")
	}
	directory, err = staging.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err = directory.ReadDir(-1)
	_ = directory.Close()
	if err != nil {
		return nil, err
	}
	// 保留独占预留目录，避免移除预留再 Rename 的竞争窗口。
	// 发布包含多次重命名；若外部干预使发布失败，保留已发布内容供用户恢复。
	for _, entry := range entries {
		if _, err := root.Lstat(entry.Name()); !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("克隆目标发生冲突，已保留目录 %s", target)
		}
		if err := root.Rename(filepath.Join(stage, entry.Name()), entry.Name()); err != nil {
			return nil, fmt.Errorf("无法完成克隆，已保留目录 %s：%w", target, err)
		}
	}
	if !sameCloneDirectory(target, owned) {
		return nil, errors.New("克隆目标已被外部替换，已保留外部内容")
	}
	repo, err := Open(ctx, target)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return repo, err
}

func validateClone(source, parent, name string) error {
	if strings.TrimSpace(source) == "" || strings.HasPrefix(strings.TrimSpace(source), "-") ||
		strings.ContainsFunc(source, unicode.IsControl) {
		return errors.New("请输入有效的仓库地址，地址不能以选项开头或包含控制字符")
	}
	if strings.TrimSpace(parent) == "" {
		return errors.New("请选择已存在的克隆父目录")
	}
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "-") ||
		strings.TrimSpace(name) != name || strings.HasSuffix(name, ".") ||
		strings.ContainsAny(name, `/\<>:"|?*`) || strings.ContainsFunc(name, unicode.IsControl) {
		return errors.New("请输入有效的单层仓库目录名")
	}
	device, _, _ := strings.Cut(strings.ToUpper(name), ".")
	device = strings.TrimRight(device, " ")
	deviceRunes := []rune(device)
	if device == "CON" || device == "PRN" || device == "AUX" || device == "NUL" ||
		device == "CONIN$" || device == "CONOUT$" ||
		(len(deviceRunes) == 4 && (strings.HasPrefix(device, "COM") || strings.HasPrefix(device, "LPT")) &&
			strings.ContainsRune("123456789¹²³", deviceRunes[3])) {
		return errors.New("仓库目录名不能使用系统保留名称")
	}
	return nil
}

func sameCloneDirectory(path string, owned os.FileInfo) bool {
	current, err := os.Lstat(path)
	return err == nil && current.IsDir() && os.SameFile(owned, current)
}

func removeCloneReservation(path string, owned os.FileInfo) {
	if sameCloneDirectory(path, owned) {
		// Remove 只移除空目录，外部添加的文件也会阻止清理。
		_ = os.Remove(path)
	}
}
