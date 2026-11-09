package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
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
	if strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
		var recent []Recent
		if err := json.Unmarshal(data, &recent); err != nil {
			return nil, errors.New("无法读取最近仓库记录：" + err.Error())
		}
		for _, entry := range recent {
			manager.library.Repositories = append(manager.library.Repositories, LocalRepository{Name: entry.Name, Path: entry.Path})
		}
	} else if err := json.Unmarshal(data, &manager.library); err != nil {
		return nil, errors.New("无法读取本地仓库记录：" + err.Error())
	}
	if manager.library.Repositories == nil {
		manager.library.Repositories = []LocalRepository{}
	}
	if manager.library.Groups == nil {
		manager.library.Groups = []Group{}
	}
	return manager, nil
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
