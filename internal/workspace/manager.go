// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ysicing/51code/internal/gitrepo"
)

type Recent struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Manager struct {
	mu      sync.Mutex
	file    string
	library Library
	open    map[string]*gitrepo.Repository
	// loadProblem 记录启动时已备份并跳过的损坏记录，供界面提示。
	loadProblem error
}

func New(file string) (*Manager, error) {
	manager := &Manager{file: file, library: emptyLibrary(), open: map[string]*gitrepo.Repository{}}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return manager, nil
	}
	if err != nil {
		return nil, err
	}
	if err := manager.decode(data); err != nil {
		// 损坏的记录不能阻止启动，也不能在下次保存时被空列表静默覆盖：先保留副本再以空列表继续。
		backup := file + ".corrupt-" + time.Now().Format("20060102-150405")
		if writeErr := os.WriteFile(backup, data, 0600); writeErr != nil {
			return nil, fmt.Errorf("%w；备份原文件失败：%v", err, writeErr)
		}
		manager.library = emptyLibrary()
		manager.loadProblem = fmt.Errorf("%w，原文件已备份为 %s", err, backup)
	}
	if manager.library.Repositories == nil {
		manager.library.Repositories = []LocalRepository{}
	}
	if manager.library.Groups == nil {
		manager.library.Groups = []Group{}
	}
	return manager, nil
}

func (manager *Manager) decode(data []byte) error {
	if strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
		var recent []Recent
		if err := json.Unmarshal(data, &recent); err != nil {
			return errors.New("无法读取最近仓库记录：" + err.Error())
		}
		for _, entry := range recent {
			manager.library.Repositories = append(manager.library.Repositories, LocalRepository{Name: entry.Name, Path: entry.Path})
		}
	} else if err := json.Unmarshal(data, &manager.library); err != nil {
		return errors.New("无法读取本地仓库记录：" + err.Error())
	}
	return nil
}

// LoadProblem 返回启动时被备份跳过的损坏记录说明；正常加载时为 nil。
func (manager *Manager) LoadProblem() error {
	return manager.loadProblem
}

func (manager *Manager) Recent() []Recent {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	recent := []Recent{}
	for _, entry := range manager.library.Repositories {
		recent = append(recent, Recent{Name: entry.Name, Path: entry.Path})
	}
	return recent
}

func (manager *Manager) Get(path string) (*gitrepo.Repository, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	repo, exists := manager.open[path]
	if !exists {
		return nil, errors.New("请先打开仓库")
	}
	return repo, nil
}

func (manager *Manager) Open(ctx context.Context, path string) (gitrepo.Snapshot, error) {
	repo, err := gitrepo.Open(ctx, path)
	if err != nil {
		return gitrepo.Snapshot{}, err
	}
	manager.mu.Lock()
	if existing, exists := manager.open[repo.Path]; exists {
		repo = existing
	}
	manager.mu.Unlock()
	state, err := repo.Snapshot(ctx)
	if err != nil {
		return state, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	next := copyLibrary(manager.library)
	updateEntry(&next, state)
	if err := manager.saveLocked(next); err != nil {
		return state, err
	}
	manager.open[repo.Path] = repo
	return state, nil
}
