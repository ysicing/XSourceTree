package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ysicing/XSourceTree/internal/gitrepo"
)

type LocalRepository struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	GroupID string `json:"groupID"`
	Branch  string `json:"branch"`
	Changes int    `json:"changes"`
	Problem string `json:"problem"`
}

type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Library struct {
	Repositories []LocalRepository `json:"repositories"`
	Groups       []Group           `json:"groups"`
}

func emptyLibrary() Library {
	return Library{Repositories: []LocalRepository{}, Groups: []Group{}}
}

func copyLibrary(library Library) Library {
	return Library{
		Repositories: append([]LocalRepository{}, library.Repositories...),
		Groups:       append([]Group{}, library.Groups...),
	}
}

func (manager *Manager) Library() Library {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return copyLibrary(manager.library)
}

func (manager *Manager) saveLocked(next Library) error {
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(manager.file), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(manager.file), ".library-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), manager.file); err != nil {
		return err
	}
	manager.library = next
	return nil
}

func entryFromSnapshot(snapshot gitrepo.Snapshot) LocalRepository {
	return LocalRepository{Name: snapshot.Name, Path: snapshot.Path, Branch: snapshot.Branch, Changes: len(snapshot.Files)}
}

func updateEntry(library *Library, snapshot gitrepo.Snapshot) {
	entry := entryFromSnapshot(snapshot)
	for index, current := range library.Repositories {
		if current.Path == snapshot.Path {
			entry.GroupID = current.GroupID
			library.Repositories[index] = entry
			return
		}
	}
	library.Repositories = append(library.Repositories, entry)
}

func (manager *Manager) Remember(snapshot gitrepo.Snapshot) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, entry := range manager.library.Repositories {
		if entry.Path == snapshot.Path {
			replacement := entryFromSnapshot(snapshot)
			replacement.GroupID = entry.GroupID
			if entry == replacement {
				return nil
			}
			next := copyLibrary(manager.library)
			updateEntry(&next, snapshot)
			return manager.saveLocked(next)
		}
	}
	return nil
}

func hasGroup(library Library, id string) bool {
	if id == "" {
		return true
	}
	for _, group := range library.Groups {
		if group.ID == id {
			return true
		}
	}
	return false
}

func (manager *Manager) Add(ctx context.Context, paths []string, groupID string) (Library, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if len(paths) == 0 || len(paths) > 2000 {
		return Library{}, errors.New("请选择 1 至 2000 个本地仓库")
	}
	if !hasGroup(manager.Library(), groupID) {
		return Library{}, errors.New("仓库组不存在，请重新选择")
	}
	snapshots := []gitrepo.Snapshot{}
	seen := map[string]bool{}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return Library{}, err
		}
		repo, err := gitrepo.Open(ctx, path)
		if err != nil {
			return Library{}, fmt.Errorf("%s：%w", path, err)
		}
		if seen[repo.Path] {
			continue
		}
		snapshot, err := repo.Snapshot(ctx)
		if err != nil {
			return Library{}, err
		}
		seen[repo.Path] = true
		snapshots = append(snapshots, snapshot)
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Library{}, err
	}
	if !hasGroup(manager.library, groupID) {
		return Library{}, errors.New("仓库组不存在，请重新选择")
	}
	next := copyLibrary(manager.library)
	for _, snapshot := range snapshots {
		exists := false
		for _, entry := range next.Repositories {
			exists = exists || entry.Path == snapshot.Path
		}
		updateEntry(&next, snapshot)
		if !exists {
			next.Repositories[len(next.Repositories)-1].GroupID = groupID
		}
	}
	if err := manager.saveLocked(next); err != nil {
		return Library{}, err
	}
	return copyLibrary(next), nil
}

func (manager *Manager) RefreshLibrary(ctx context.Context) (Library, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	entries := manager.Library().Repositories
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return Library{}, err
		}
		repo, err := gitrepo.Open(ctx, entry.Path)
		if err == nil {
			var snapshot gitrepo.Snapshot
			snapshot, err = repo.Snapshot(ctx)
			if err == nil {
				entries[index] = entryFromSnapshot(snapshot)
			}
		}
		if err != nil {
			entries[index].Problem = err.Error()
		}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Library{}, err
	}
	next := copyLibrary(manager.library)
	for index, current := range next.Repositories {
		for _, entry := range entries {
			if entry.Path == current.Path {
				entry.GroupID = current.GroupID
				next.Repositories[index] = entry
				break
			}
		}
	}
	if err := manager.saveLocked(next); err != nil {
		return Library{}, err
	}
	return copyLibrary(next), nil
}

func (manager *Manager) CreateGroup(name string) (Library, error) {
	return manager.editGroup("", name)
}

func (manager *Manager) RenameGroup(id, name string) (Library, error) {
	if id == "" {
		return Library{}, errors.New("请选择仓库组")
	}
	return manager.editGroup(id, name)
}

func (manager *Manager) editGroup(id, name string) (Library, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 || strings.ContainsAny(name, "\x00\r\n") {
		return Library{}, errors.New("仓库组名称需要 1 至 80 个字符")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, group := range manager.library.Groups {
		if group.ID != id && strings.EqualFold(group.Name, name) {
			return Library{}, errors.New("已存在同名仓库组")
		}
	}
	next := copyLibrary(manager.library)
	if id == "" {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return Library{}, err
		}
		next.Groups = append(next.Groups, Group{ID: hex.EncodeToString(random), Name: name})
	} else {
		found := false
		for index, group := range next.Groups {
			if group.ID == id {
				next.Groups[index].Name = name
				found = true
				break
			}
		}
		if !found {
			return Library{}, errors.New("仓库组不存在")
		}
	}
	if err := manager.saveLocked(next); err != nil {
		return Library{}, err
	}
	return copyLibrary(next), nil
}

func (manager *Manager) RemoveGroup(id string) (Library, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if id == "" || !hasGroup(manager.library, id) {
		return Library{}, errors.New("仓库组不存在")
	}
	next := emptyLibrary()
	for _, group := range manager.library.Groups {
		if group.ID != id {
			next.Groups = append(next.Groups, group)
		}
	}
	for _, entry := range manager.library.Repositories {
		if entry.GroupID == id {
			entry.GroupID = ""
		}
		next.Repositories = append(next.Repositories, entry)
	}
	if err := manager.saveLocked(next); err != nil {
		return Library{}, err
	}
	return copyLibrary(next), nil
}

func (manager *Manager) Move(path, groupID string) (Library, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if !hasGroup(manager.library, groupID) {
		return Library{}, errors.New("仓库组不存在")
	}
	next := copyLibrary(manager.library)
	found := false
	for index, entry := range next.Repositories {
		if entry.Path == path {
			next.Repositories[index].GroupID = groupID
			found = true
			break
		}
	}
	if !found {
		return Library{}, errors.New("仓库不在本地列表中")
	}
	if err := manager.saveLocked(next); err != nil {
		return Library{}, err
	}
	return copyLibrary(next), nil
}

func (manager *Manager) Remove(path string) (Library, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	next := copyLibrary(manager.library)
	next.Repositories = []LocalRepository{}
	for _, entry := range manager.library.Repositories {
		if entry.Path != path {
			next.Repositories = append(next.Repositories, entry)
		}
	}
	if len(next.Repositories) == len(manager.library.Repositories) {
		return Library{}, errors.New("仓库不在本地列表中")
	}
	if err := manager.saveLocked(next); err != nil {
		return Library{}, err
	}
	delete(manager.open, path)
	return copyLibrary(next), nil
}
