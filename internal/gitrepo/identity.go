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
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

type IdentityValue struct {
	Value  string
	Scope  string
	Origin string
}

type IdentitySettings struct {
	Name        IdentityValue
	Email       IdentityValue
	DirectName  []string
	DirectEmail []string
	Token       string
}

func (settings IdentitySettings) Overrides() bool {
	return len(settings.DirectName) > 0 || len(settings.DirectEmail) > 0
}

type IdentityRequest struct {
	Name    string
	Email   string
	Inherit bool
	Token   string
}

type identityStore struct {
	root  string
	write []string
	read  []string
}

var globalIdentityMu sync.Mutex

func globalIdentityStore() (identityStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return identityStore{}, err
	}
	target, overridden := os.LookupEnv("GIT_CONFIG_GLOBAL")
	if !overridden {
		target = filepath.Join(home, ".gitconfig")
		if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
			configHome := os.Getenv("XDG_CONFIG_HOME")
			if configHome == "" {
				configHome = filepath.Join(home, ".config")
			}
			xdg := filepath.Join(configHome, "git", "config")
			if _, err := os.Stat(xdg); err == nil {
				target = xdg
			}
		}
	}
	if target == "" {
		return identityStore{}, errors.New("全局 Git 配置路径为空")
	}
	// 与 Git 的全局写入目标一致，避免回滚时把 XDG 文件中的值复制进另一份配置。
	return identityStore{root: home, write: []string{"--file", target}, read: []string{"--global"}}, nil
}

func GlobalIdentity(ctx context.Context) (IdentitySettings, error) {
	globalIdentityMu.Lock()
	defer globalIdentityMu.Unlock()
	store, err := globalIdentityStore()
	if err != nil {
		return IdentitySettings{}, err
	}
	return store.load(ctx)
}

func SaveGlobalIdentity(ctx context.Context, request IdentityRequest) error {
	globalIdentityMu.Lock()
	defer globalIdentityMu.Unlock()
	if request.Inherit {
		return errors.New("全局提交身份不支持仓库继承操作")
	}
	store, err := globalIdentityStore()
	if err != nil {
		return err
	}
	return store.save(ctx, request)
}

func (repo *Repository) Identity(ctx context.Context) (IdentitySettings, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return IdentitySettings{}, err
	}
	defer repo.mu.Unlock()
	return (identityStore{root: repo.Path, write: []string{"--local"}}).load(ctx)
}

func (repo *Repository) SaveIdentity(ctx context.Context, request IdentityRequest) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := repo.lockHistory(ctx); err != nil {
		return err
	}
	defer repo.mu.Unlock()
	return (identityStore{root: repo.Path, write: []string{"--local"}}).save(ctx, request)
}

func (store identityStore) config(ctx context.Context, scope []string, args ...string) (string, error) {
	parameters := append([]string{"config"}, scope...)
	return run(ctx, store.root, "", 15*time.Second, append(parameters, args...)...)
}

func (store identityStore) effective(ctx context.Context, key string) (IdentityValue, error) {
	output, err := store.config(ctx, store.read, "--includes", "--null", "--show-scope", "--show-origin", "--get", key)
	if exitCode(err, 1) {
		return IdentityValue{}, nil
	}
	if err != nil {
		return IdentityValue{}, err
	}
	parts := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	if len(parts) != 3 {
		return IdentityValue{}, errors.New("无法解析 Git 提交身份来源")
	}
	return IdentityValue{Scope: parts[0], Origin: parts[1], Value: parts[2]}, nil
}

func (store identityStore) direct(ctx context.Context, key string) ([]string, error) {
	output, err := store.config(ctx, store.write, "--no-includes", "--null", "--get-all", key)
	if exitCode(err, 1) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(output, "\x00"), "\x00"), nil
}

func (store identityStore) load(ctx context.Context) (IdentitySettings, error) {
	var settings IdentitySettings
	var err error
	if settings.Name, err = store.effective(ctx, "user.name"); err != nil {
		return settings, err
	}
	if settings.Email, err = store.effective(ctx, "user.email"); err != nil {
		return settings, err
	}
	if settings.DirectName, err = store.direct(ctx, "user.name"); err != nil {
		return settings, err
	}
	if settings.DirectEmail, err = store.direct(ctx, "user.email"); err != nil {
		return settings, err
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return settings, err
	}
	sum := sha256.Sum256(append(data, []byte(strings.Join(store.write, "\x00"))...))
	settings.Token = hex.EncodeToString(sum[:])
	return settings, nil
}

func (store identityStore) replace(ctx context.Context, key string, values []string) error {
	if len(values) == 0 {
		_, err := store.config(ctx, store.write, "--no-includes", "--unset-all", key)
		if exitCode(err, 5) {
			return nil
		}
		return err
	}
	if _, err := store.config(ctx, store.write, "--no-includes", "--replace-all", key, values[0]); err != nil {
		return err
	}
	for _, value := range values[1:] {
		if _, err := store.config(ctx, store.write, "--no-includes", "--add", key, value); err != nil {
			return err
		}
	}
	return nil
}

func (store identityStore) save(ctx context.Context, request IdentityRequest) error {
	if !request.Inherit {
		for _, value := range []string{request.Name, request.Email} {
			if strings.TrimSpace(value) == "" || strings.ContainsFunc(value, unicode.IsControl) || strings.ContainsAny(value, "<>") {
				return errors.New("请填写有效的姓名和邮箱，不能包含换行或尖括号")
			}
		}
	}
	settings, err := store.load(ctx)
	if err != nil {
		return err
	}
	if request.Token == "" || settings.Token != request.Token {
		return errors.New("Git 身份配置已改变，请重新打开设置后重试")
	}
	var names, emails []string
	if !request.Inherit {
		names, emails = []string{strings.TrimSpace(request.Name)}, []string{strings.TrimSpace(request.Email)}
	}
	changes := []struct {
		key           string
		before, after []string
	}{
		{"user.name", settings.DirectName, names},
		{"user.email", settings.DirectEmail, emails},
	}
	for index, change := range changes {
		if slices.Equal(change.before, change.after) {
			continue
		}
		if err := store.replace(ctx, change.key, change.after); err != nil {
			// Git 分别锁定每项写入。超时也可能发生在落盘之后，因此回读所有已尝试的项，
			// 仅恢复仍等于本次目标值的配置，不覆盖其他工具的新修改。
			recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer cancel()
			var rollbackErr error
			for previous := index; previous >= 0; previous-- {
				attempt := changes[previous]
				if slices.Equal(attempt.before, attempt.after) {
					continue
				}
				current, readErr := store.direct(recovery, attempt.key)
				if readErr != nil {
					rollbackErr = errors.Join(rollbackErr, readErr)
				} else if slices.Equal(current, attempt.before) {
					continue
				} else if !slices.Equal(current, attempt.after) {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("%s 已被外部修改，未回退", attempt.key))
				} else {
					rollbackErr = errors.Join(rollbackErr, store.replace(recovery, attempt.key, attempt.before))
				}
			}
			if rollbackErr != nil {
				return fmt.Errorf("身份保存失败且未能完整回退，请重新检查配置：%w", errors.Join(err, rollbackErr))
			}
			return err
		}
	}
	return nil
}
