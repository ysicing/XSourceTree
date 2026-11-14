// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type RemoteConfig struct {
	Name     string   `json:"name"`
	URLs     []string `json:"urls"`
	PushURLs []string `json:"pushURLs"`
	Token    string   `json:"token"`
}

type PublishBranchRequest struct {
	Remote string `json:"remote"`
	Token  string `json:"token"`
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Head   string `json:"head"`
}

type PushBranchRequest PublishBranchRequest

func (repo *Repository) validateRemoteName(ctx context.Context, name string) error {
	if name == "" || name == "." || strings.HasPrefix(name, "-") || strings.ContainsFunc(name, unicode.IsControl) {
		return errors.New("远程名称无效")
	}
	if _, err := repo.git(ctx, "check-ref-format", "refs/remotes/"+name); err != nil {
		return errors.New("远程名称无效")
	}
	return nil
}

func (repo *Repository) remoteConfig(ctx context.Context, name string) (RemoteConfig, error) {
	if err := repo.validateRemoteName(ctx, name); err != nil {
		return RemoteConfig{}, err
	}
	output, err := repo.git(ctx, "config", "--null", "--get-regexp", "^remote\\."+regexp.QuoteMeta(name)+"\\.")
	if err != nil {
		return RemoteConfig{}, fmt.Errorf("远程仓库不存在或无法读取，请刷新：%w", err)
	}
	config := RemoteConfig{Name: name, URLs: []string{}, PushURLs: []string{}}
	for _, entry := range strings.Split(output, "\x00") {
		key, value, ok := strings.Cut(entry, "\n")
		if ok && strings.EqualFold(key, "remote."+name+".url") {
			config.URLs = append(config.URLs, value)
		}
	}
	resolved := ""
	if len(config.URLs) > 0 {
		resolved, err = repo.git(ctx, "remote", "get-url", "--push", "--all", "--", name)
		if err != nil {
			return RemoteConfig{}, err
		}
		config.PushURLs = strings.Split(strings.TrimSuffix(resolved, "\n"), "\n")
	}
	// 包含解析后的推送地址，避免 URL 重写配置改变后仍沿用已确认的目标。
	sum := sha256.Sum256([]byte(name + "\x00" + output + "\x00" + resolved))
	config.Token = hex.EncodeToString(sum[:])
	return config, nil
}

func (repo *Repository) RemoteConfigs(ctx context.Context) ([]RemoteConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return nil, err
	}
	defer repo.mu.Unlock()
	output, err := repo.git(ctx, "remote")
	if err != nil {
		return nil, err
	}
	configs := []RemoteConfig{}
	for _, name := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if name == "" {
			continue
		}
		config, err := repo.remoteConfig(ctx, name)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, nil
}

func (repo *Repository) SaveRemote(ctx context.Context, name, address, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	if err := repo.validateRemoteName(ctx, name); err != nil {
		return err
	}
	if strings.TrimSpace(address) == "" || strings.HasPrefix(strings.TrimSpace(address), "-") || strings.ContainsFunc(address, unicode.IsControl) {
		return errors.New("请输入有效的仓库地址")
	}
	if token == "" {
		_, err := repo.git(ctx, "remote", "add", "--", name, address)
		return err
	}
	config, err := repo.remoteConfig(ctx, name)
	if err != nil {
		return err
	}
	if config.Token != token {
		return errors.New("远程配置已改变，请重新打开远程设置")
	}
	if len(config.URLs) != 1 {
		return errors.New("此远程使用多个地址，请在命令行中修改")
	}
	// 只更新获取地址，保留单独设置的 pushurl、refspec 等现有配置。
	_, err = repo.git(ctx, "remote", "set-url", "--", name, address)
	return err
}

func (repo *Repository) RemoveRemote(ctx context.Context, name, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	config, err := repo.remoteConfig(ctx, name)
	if err != nil {
		return err
	}
	if token == "" || config.Token != token {
		return errors.New("远程配置已改变，请重新打开远程设置")
	}
	_, err = repo.git(ctx, "remote", "remove", "--", name)
	return err
}

func (repo *Repository) CheckoutRemote(ctx context.Context, remoteBranch, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	for _, value := range []string{remoteBranch, name} {
		if err := repo.validateBranch(ctx, value); err != nil {
			return err
		}
	}
	if name == "HEAD" {
		return errors.New("本地分支不能命名为 HEAD")
	}
	operation, err := repo.operation(ctx)
	if err != nil {
		return err
	}
	if operation.Kind != "" {
		return errors.New("请先完成或中止当前 Git 操作")
	}
	ref := "refs/remotes/" + remoteBranch
	if _, err := repo.git(ctx, "show-ref", "--verify", "--quiet", ref); err != nil {
		return errors.New("远程分支不存在，请先获取并刷新")
	}
	if _, err := repo.git(ctx, "symbolic-ref", "--quiet", ref); err == nil {
		return errors.New("请选择具体的远程分支")
	} else if !exitCode(err, 1) {
		return err
	}
	_, err = repo.git(ctx, "switch", "--create", name, "--track=direct", "--no-overwrite-ignore", "--", ref)
	return err
}

func (repo *Repository) PublishBranch(ctx context.Context, request PublishBranchRequest) error {
	return repo.pushBranch(ctx, request, true)
}

func (repo *Repository) PushBranch(ctx context.Context, request PushBranchRequest) error {
	return repo.pushBranch(ctx, PublishBranchRequest(request), false)
}

func (repo *Repository) pushBranch(ctx context.Context, request PublishBranchRequest, publish bool) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	for _, name := range []string{request.Branch, request.Name} {
		if err := repo.validateBranch(ctx, name); err != nil {
			return err
		}
	}
	branch, err := repo.git(ctx, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return errors.New("请先切换到本地分支再推送")
	}
	head, err := repo.head(ctx)
	if err != nil {
		return err
	}
	if head == "" {
		return errors.New("请先创建首次提交再推送分支")
	}
	if strings.TrimSpace(branch) != "refs/heads/"+request.Branch || head != request.Head {
		return errors.New("当前分支或提交已改变，请重新打开推送窗口")
	}
	upstream, err := repo.git(ctx, "for-each-ref", "--format=%(upstream)", "refs/heads/"+request.Branch)
	if err != nil {
		return err
	}
	if publish && strings.TrimSpace(upstream) != "" {
		return errors.New("当前分支已有上游，请刷新后直接推送")
	}
	operation, err := repo.operation(ctx)
	if err != nil {
		return err
	}
	if operation.Kind != "" {
		return errors.New("请先完成或中止当前 Git 操作")
	}
	config, err := repo.remoteConfig(ctx, request.Remote)
	if err != nil {
		return err
	}
	if request.Token == "" || config.Token != request.Token {
		return errors.New("远程配置已改变，请重新打开推送窗口")
	}
	if len(config.PushURLs) != 1 || config.PushURLs[0] == "" {
		return errors.New("推送分支需要且只能有一个推送地址")
	}
	mirror, err := repo.git(ctx, "config", "--bool", "--get", "remote."+request.Remote+".mirror")
	if err != nil && !exitCode(err, 1) {
		return err
	}
	if strings.TrimSpace(mirror) == "true" {
		return errors.New("镜像远程不支持普通分支推送，请使用命令行操作")
	}
	// 使用明确的本地引用，外部切换 HEAD 不会把另一个分支发布出去。
	args := []string{"-c", "push.autoSetupRemote=false", "push", "--no-follow-tags", "--recurse-submodules=no"}
	if publish {
		args = append(args, "--set-upstream")
	}
	return repo.runRemote(ctx, append(args, "--", request.Remote, "refs/heads/"+request.Branch+":refs/heads/"+request.Name)...)
}
