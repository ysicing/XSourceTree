package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
)

type Recent struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Manager struct {
	mu     sync.Mutex
	file   string
	recent []Recent
	open   map[string]*gitrepo.Repository
}

func New(file string) (*Manager, error) {
	manager := &Manager{file: file, recent: []Recent{}, open: map[string]*gitrepo.Repository{}}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return manager, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &manager.recent); err != nil {
		return nil, errors.New("无法读取最近仓库记录：" + err.Error())
	}
	return manager, nil
}

func (manager *Manager) Recent() []Recent {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return append([]Recent{}, manager.recent...)
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
	recent := []Recent{{Name: state.Name, Path: state.Path}}
	for _, entry := range manager.recent {
		if entry.Path != state.Path && len(recent) < 12 {
			recent = append(recent, entry)
		}
	}
	data, err := json.MarshalIndent(recent, "", "  ")
	if err != nil {
		return state, err
	}
	if err := os.MkdirAll(filepath.Dir(manager.file), 0700); err != nil {
		return state, err
	}
	temp, err := os.CreateTemp(filepath.Dir(manager.file), ".recent-*")
	if err != nil {
		return state, err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return state, err
	}
	if err = temp.Close(); err != nil {
		return state, err
	}
	if err = os.Rename(temp.Name(), manager.file); err != nil {
		return state, err
	}
	manager.recent = recent
	manager.open[repo.Path] = repo
	return state, nil
}
