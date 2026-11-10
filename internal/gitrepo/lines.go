package gitrepo

import (
	"context"
	"errors"
	"strings"
)

// ApplyLines 的行 ID 是 Patch.Text 中增删行从零开始的序号，仅对同一 Token 有效。
// 替换的删除行和新增行独立选择；工作区始终保持原样。
func (repo *Repository) ApplyLines(ctx context.Context, path string, staged bool, token string, ids []int) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	patch, err := repo.hunkPatch(ctx, path, staged)
	if err != nil {
		return err
	}
	if !patch.view.Supported || token == "" || patch.view.Token != token {
		return errors.New("文件不支持逐行操作或版本已改变，请刷新后重试")
	}
	selected := make(map[int]bool, len(ids))
	for _, id := range ids {
		if id < 0 || selected[id] {
			return errors.New("所选行无效或重复")
		}
		selected[id] = true
	}
	hunkSelections := make([]map[int]bool, len(patch.hunks))
	change := 0
	for index, hunk := range patch.hunks {
		hunkSelections[index] = map[int]bool{}
		local := 0
		for _, line := range strings.Split(hunk, "\n") {
			if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
				if selected[change] {
					hunkSelections[index][local] = true
				}
				change++
				local++
			}
		}
	}
	if len(selected) == 0 {
		return errors.New("请选择至少一行变更")
	}
	for id := range selected {
		if id >= change {
			return errors.New("所选行不存在，请刷新后重试")
		}
	}
	before, err := repo.git(ctx, "cat-file", "blob", patch.indexHash)
	if err != nil {
		return err
	}
	after := before
	// 从后往前应用，保留前面区块在当前索引中的原始坐标。
	for index := len(patch.hunks) - 1; index >= 0; index-- {
		if len(hunkSelections[index]) == 0 {
			continue
		}
		after, err = applyHunkLinesToBlob(after, patch.hunks[index], staged, hunkSelections[index])
		if err != nil {
			return err
		}
	}
	input, err := repo.prepareBlobApply(ctx, patch, before, after)
	if err != nil {
		return err
	}
	return repo.applyIndexPatch(ctx, path, staged, token, input)
}
