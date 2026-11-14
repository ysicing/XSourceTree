// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Worktree 的 Branch 使用 refs/heads/ 全名；Head 为空表示尚无提交。
type Worktree struct {
	Path        string `json:"path"`
	Head        string `json:"head"`
	Branch      string `json:"branch"`
	Main        bool   `json:"main"`
	Current     bool   `json:"current"`
	Detached    bool   `json:"detached"`
	Bare        bool   `json:"bare"`
	Locked      bool   `json:"locked"`
	Prunable    bool   `json:"prunable"`
	LockReason  string `json:"lockReason"`
	PruneReason string `json:"pruneReason"`
	Dirty       bool   `json:"dirty"`
	Ignored     bool   `json:"ignored"`
	Operation   string `json:"operation"`
	Problem     string `json:"problem"`
}

// WorktreeReview 的目标始终是当前 Repository.Path。
type WorktreeReview struct {
	Worktree      Worktree `json:"worktree"`
	TargetHead    string   `json:"targetHead"`
	TargetBranch  string   `json:"targetBranch"`
	Token         string   `json:"token"`
	CanMerge      bool     `json:"canMerge"`
	CanRemove     bool     `json:"canRemove"`
	MergeProblem  string   `json:"mergeProblem"`
	RemoveProblem string   `json:"removeProblem"`
}

type worktreeState struct {
	Worktree       Worktree
	Status         string
	OperationToken string
	GitDir         string
	GitDirStamp    string
	CommonStamp    string
	GitFile        string
	GitFileStamp   string
	RootStamp      string
	rootInfo       os.FileInfo
	gitFileInfo    os.FileInfo
	gitDirInfo     os.FileInfo
	commonInfo     os.FileInfo
}

type worktreeReviewState struct {
	source worktreeState
	target worktreeState
}

type worktreeGate struct {
	ready chan struct{}
	users int
}

var worktreeGates = struct {
	sync.Mutex
	entries map[string]*worktreeGate
}{entries: make(map[string]*worktreeGate)}

// 只串行化本进程的 worktree API；外部 Git 仍可能在最后一次核对后修改状态。
func lockWorktreeCommon(ctx context.Context, common string) (func(), error) {
	info, err := os.Stat(common)
	if err != nil {
		return nil, err
	}
	identity, err := worktreeFileIdentity(common, info)
	if err != nil {
		return nil, err
	}
	common = identity
	worktreeGates.Lock()
	gate := worktreeGates.entries[common]
	if gate == nil {
		gate = &worktreeGate{ready: make(chan struct{}, 1)}
		gate.ready <- struct{}{}
		worktreeGates.entries[common] = gate
	}
	gate.users++
	worktreeGates.Unlock()
	drop := func() {
		worktreeGates.Lock()
		gate.users--
		if gate.users == 0 {
			delete(worktreeGates.entries, common)
		}
		worktreeGates.Unlock()
	}
	select {
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case <-gate.ready:
		if err := ctx.Err(); err != nil {
			gate.ready <- struct{}{}
			drop()
			return nil, err
		}
		return func() {
			gate.ready <- struct{}{}
			drop()
		}, nil
	}
}

func (repo *Repository) lockWorktree(ctx context.Context) (string, func(), error) {
	if err := repo.lockHistory(ctx); err != nil {
		return "", nil, err
	}
	common, err := worktreeCommonDir(ctx, repo.Path)
	if err != nil {
		repo.mu.Unlock()
		return "", nil, err
	}
	release, err := lockWorktreeCommon(ctx, common)
	if err != nil {
		repo.mu.Unlock()
		return "", nil, err
	}
	return common, func() {
		release()
		repo.mu.Unlock()
	}, nil
}

// Worktrees 保留缺失、锁定及可清理条目，单项故障由 Problem 表达。
func (repo *Repository) Worktrees(ctx context.Context) ([]Worktree, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	common, unlock, err := repo.lockWorktree(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	states, err := repo.worktrees(ctx, common)
	if err != nil {
		return nil, err
	}
	result := make([]Worktree, 0, len(states))
	for _, state := range states {
		result = append(result, state.Worktree)
	}
	return result, nil
}

// ReviewWorktree 仅检查列表中的条目，不把任意路径当成链接工作树。
func (repo *Repository) ReviewWorktree(ctx context.Context, path string) (WorktreeReview, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	common, unlock, err := repo.lockWorktree(ctx)
	if err != nil {
		return WorktreeReview{}, err
	}
	defer unlock()
	review, _, err := repo.reviewWorktree(ctx, common, path)
	return review, err
}

func (repo *Repository) reviewWorktree(ctx context.Context, common, path string) (WorktreeReview, worktreeReviewState, error) {
	canonical, err := worktreePath(path)
	if err != nil {
		return WorktreeReview{}, worktreeReviewState{}, err
	}
	states, err := repo.worktrees(ctx, common)
	if err != nil {
		return WorktreeReview{}, worktreeReviewState{}, err
	}
	var source, target *worktreeState
	for index := range states {
		state := &states[index]
		if worktreeSamePath(state.Worktree.Path, canonical) {
			source = state
		}
		if state.Worktree.Current {
			target = state
		}
	}
	if source == nil {
		return WorktreeReview{}, worktreeReviewState{}, errors.New("路径不在当前仓库的工作树列表中")
	}
	if target == nil {
		return WorktreeReview{}, worktreeReviewState{}, errors.New("当前目标不在仓库工作树列表中，请重新打开仓库")
	}
	review := WorktreeReview{
		Worktree: source.Worktree, TargetHead: target.Worktree.Head, TargetBranch: target.Worktree.Branch,
		MergeProblem:  worktreeMergeProblem(source.Worktree, target.Worktree),
		RemoveProblem: worktreeRemoveProblem(source.Worktree, target.Worktree),
	}
	if review.RemoveProblem == "" {
		_, err := repo.git(ctx, "merge-base", "--is-ancestor", source.Worktree.Head, target.Worktree.Head)
		if exitCode(err, 1) {
			review.RemoveProblem = "工作树含有未合并到当前目标 HEAD 的提交，不能移除"
		} else if err != nil {
			return WorktreeReview{}, worktreeReviewState{}, err
		}
	}
	review.CanMerge = review.MergeProblem == ""
	review.CanRemove = review.RemoveProblem == ""
	data, err := json.Marshal(struct {
		Common string
		Source worktreeState
		Target worktreeState
	}{common, *source, *target})
	if err != nil {
		return WorktreeReview{}, worktreeReviewState{}, err
	}
	sum := sha256.Sum256(data)
	review.Token = hex.EncodeToString(sum[:])
	return review, worktreeReviewState{source: *source, target: *target}, nil
}

func worktreeUnavailable(worktree Worktree) string {
	switch {
	case worktree.Locked:
		return "工作树已锁定，请先解锁"
	case worktree.Prunable:
		return "工作树已失效并标记为可清理，请先修复"
	case worktree.Bare:
		return "裸仓库没有可操作的工作树"
	case worktree.Problem != "":
		return worktree.Problem
	}
	return ""
}

func worktreeMergeProblem(source, target Worktree) string {
	switch {
	case source.Current:
		return "不能将当前工作树合并到自身"
	}
	if problem := worktreeUnavailable(source); problem != "" {
		return problem
	}
	if problem := worktreeUnavailable(target); problem != "" {
		return "目标工作树：" + problem
	}
	switch {
	case source.Head == "":
		return "源工作树尚无提交"
	case source.Operation != "":
		return "请先完成源工作树正在进行的 Git 操作"
	case source.Dirty:
		return "请先提交或贮藏源工作树的未提交及未跟踪文件"
	case target.Detached || !strings.HasPrefix(target.Branch, "refs/heads/"):
		return "目标工作树处于分离 HEAD，请先切换到本地分支"
	case target.Operation != "":
		return "请先完成目标工作树正在进行的 Git 操作"
	case target.Dirty:
		return "请先提交或贮藏目标工作树的未提交及未跟踪文件"
	}
	return ""
}

func worktreeRemoveProblem(source, target Worktree) string {
	switch {
	case source.Main:
		return "不能移除主工作树"
	case source.Current:
		return "不能移除当前工作树"
	}
	if problem := worktreeUnavailable(source); problem != "" {
		return problem
	}
	switch {
	case source.Dirty:
		return "工作树含有未提交或未跟踪文件，不能移除"
	case source.Ignored:
		return "工作树含有被忽略的文件，移除会丢失数据"
	case source.Operation != "":
		return "工作树正在进行 Git 操作，不能移除"
	case target.Problem != "":
		return "无法核实目标工作树：" + target.Problem
	case source.Head == "" || target.Head == "":
		return "无法确认工作树提交已合并到当前目标 HEAD，不能移除"
	}
	return ""
}

// CreateWorktree 不覆盖路径，不强制检出；失败后不删除分支或用户目录。
func (repo *Repository) CreateWorktree(ctx context.Context, path, branch, start string, existing bool) (Worktree, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	common, unlock, err := repo.lockWorktree(ctx)
	if err != nil {
		return Worktree{}, err
	}
	defer unlock()
	states, err := repo.worktrees(ctx, common)
	if err != nil {
		return Worktree{}, err
	}
	path, err = worktreeCreatePath(path, common, states)
	if err != nil {
		return Worktree{}, err
	}
	if strings.HasPrefix(branch, "refs/") && !strings.HasPrefix(branch, "refs/heads/") {
		return Worktree{}, errors.New("工作树只能使用本地分支")
	}
	branch = strings.TrimPrefix(branch, "refs/heads/")
	if err := repo.validateBranch(ctx, branch); err != nil {
		if ctx.Err() != nil {
			return Worktree{}, ctx.Err()
		}
		return Worktree{}, err
	}
	ref := "refs/heads/" + branch
	for _, state := range states {
		if state.Worktree.Branch == ref {
			return Worktree{}, errors.New("分支已被其他工作树占用")
		}
	}
	_, refErr := repo.git(ctx, "show-ref", "--verify", "--quiet", ref)
	if refErr != nil && !exitCode(refErr, 1) {
		return Worktree{}, refErr
	}
	if existing && refErr != nil {
		return Worktree{}, errors.New("本地分支不存在")
	}
	if !existing && refErr == nil {
		return Worktree{}, errors.New("本地分支已存在，请选择使用已有分支")
	}
	if existing {
		start = ref
	} else if start == "" {
		start = "HEAD"
	}
	head, err := repo.comparisonCommit(ctx, start)
	if err != nil {
		return Worktree{}, err
	}
	if checked, err := worktreeCreatePath(path, common, states); err != nil || checked != path {
		if err != nil {
			return Worktree{}, err
		}
		return Worktree{}, errors.New("工作树目标路径在检查后已改变")
	}
	args := []string{"worktree", "add"}
	if existing {
		args = append(args, "--", path, branch)
	} else {
		args = append(args, "--no-track", "-b", branch, "--", path, head)
	}
	if _, err := run(ctx, repo.Path, "", 2*time.Minute, args...); err != nil {
		return Worktree{}, err
	}
	states, err = repo.worktrees(ctx, common)
	if err != nil {
		return Worktree{}, err
	}
	for _, state := range states {
		if worktreeSamePath(state.Worktree.Path, path) {
			if state.Worktree.Problem != "" || state.Worktree.Head != head || state.Worktree.Branch != ref {
				return state.Worktree, errors.New("工作树已创建，但状态发生变化，请刷新核实")
			}
			return state.Worktree, nil
		}
	}
	return Worktree{}, fmt.Errorf("工作树创建后未出现在列表中，请刷新核实：%s", path)
}

// MergeWorktree 合并复核后的固定提交；冲突状态留给现有操作界面处理。
func (repo *Repository) MergeWorktree(ctx context.Context, path, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	common, unlock, err := repo.lockWorktree(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	review, state, err := repo.reviewWorktree(ctx, common, path)
	if err != nil {
		return err
	}
	if token == "" || token != review.Token {
		return errors.New("工作树状态已改变，请刷新后重新确认")
	}
	if !review.CanMerge {
		return errors.New(review.MergeProblem)
	}
	if err := state.source.checkIdentity(ctx, common); err != nil {
		return err
	}
	if err := state.target.checkIdentity(ctx, common); err != nil {
		return err
	}
	_, err = run(ctx, repo.Path, "", 2*time.Minute, "merge", "--no-edit", "--no-autostash",
		"--no-overwrite-ignore", "--", review.Worktree.Head)
	return err
}

// RemoveWorktree 不强制移除，不删除分支，也不清理其他工作树元数据。
func (repo *Repository) RemoveWorktree(ctx context.Context, path, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	common, unlock, err := repo.lockWorktree(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	review, state, err := repo.reviewWorktree(ctx, common, path)
	if err != nil {
		return err
	}
	if token == "" || token != review.Token {
		return errors.New("工作树状态已改变，请刷新后重新确认")
	}
	if !review.CanRemove {
		return errors.New(review.RemoveProblem)
	}
	if err := state.target.checkIdentity(ctx, common); err != nil {
		return err
	}
	if err := state.source.checkIdentity(ctx, common); err != nil {
		return err
	}
	_, err = run(ctx, repo.Path, "", 2*time.Minute, "worktree", "remove", "--", review.Worktree.Path)
	return err
}
