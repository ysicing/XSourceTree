package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Repository struct {
	Path string
	mu   sync.Mutex
}

type File struct {
	Path         string `json:"path"`
	OriginalPath string `json:"originalPath"`
	Index        string `json:"index"`
	Working      string `json:"working"`
	Conflict     bool   `json:"conflict"`
}

type Snapshot struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Branch   string   `json:"branch"`
	Head     string   `json:"head"`
	Upstream string   `json:"upstream"`
	Ahead    int      `json:"ahead"`
	Behind   int      `json:"behind"`
	Detached bool     `json:"detached"`
	Unborn   bool     `json:"unborn"`
	Files    []File   `json:"files"`
	Remotes  []string `json:"remotes"`
}

type Branch struct {
	Name     string `json:"name"`
	Hash     string `json:"hash"`
	Current  bool   `json:"current"`
	Remote   bool   `json:"remote"`
	Upstream string `json:"upstream"`
}

type Commit struct {
	Hash    string   `json:"hash"`
	Parents []string `json:"parents"`
	Subject string   `json:"subject"`
	Author  string   `json:"author"`
	Date    string   `json:"date"`
	Refs    string   `json:"refs"`
}

func Open(ctx context.Context, path string) (*Repository, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("请选择仓库目录")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	inside, err := run(ctx, absolute, "", 15*time.Second, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return nil, errors.New("所选目录不是 Git 工作区")
	}
	root, err := run(ctx, absolute, "", 15*time.Second, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(strings.TrimSuffix(root, "\n"))
	if err != nil {
		return nil, err
	}
	return &Repository{Path: canonical}, nil
}

func (repo *Repository) git(ctx context.Context, args ...string) (string, error) {
	return run(ctx, repo.Path, "", 30*time.Second, args...)
}

func (repo *Repository) head(ctx context.Context) (string, error) {
	output, err := repo.git(ctx, "rev-parse", "--verify", "--quiet", "HEAD")
	if exitCode(err, 1) {
		return "", nil
	}
	return strings.TrimSpace(output), err
}

func (repo *Repository) files(ctx context.Context) ([]File, error) {
	output, err := repo.git(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	files := []File{}
	records := strings.Split(output, "\x00")
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" {
			continue
		}
		if len(record) < 4 || record[2] != ' ' {
			return nil, errors.New("无法解析 Git 文件状态")
		}
		file := File{Path: record[3:], Index: record[:1], Working: record[1:2]}
		status := record[:2]
		file.Conflict = strings.Contains(status, "U") || status == "AA" || status == "DD"
		if strings.ContainsAny(status, "RC") {
			index++
			if index >= len(records) || records[index] == "" {
				return nil, errors.New("Git 重命名记录不完整")
			}
			file.OriginalPath = records[index]
		}
		files = append(files, file)
	}
	return files, nil
}

func (repo *Repository) Snapshot(ctx context.Context) (Snapshot, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	state := Snapshot{Name: filepath.Base(repo.Path), Path: repo.Path, Files: []File{}, Remotes: []string{}}
	var err error
	if state.Head, err = repo.head(ctx); err != nil {
		return state, err
	}
	state.Unborn = state.Head == ""
	branch, err := repo.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil && !exitCode(err, 1) {
		return state, err
	}
	state.Detached = exitCode(err, 1)
	state.Branch = strings.TrimSpace(branch)
	if state.Detached {
		state.Branch = state.Head[:min(8, len(state.Head))]
	}
	if state.Files, err = repo.files(ctx); err != nil {
		return state, err
	}
	remotes, err := repo.git(ctx, "remote")
	if err != nil {
		return state, err
	}
	for _, remote := range strings.Split(strings.TrimSpace(remotes), "\n") {
		if remote != "" {
			state.Remotes = append(state.Remotes, remote)
		}
	}
	if !state.Unborn && !state.Detached {
		upstream, upstreamErr := repo.git(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
		if upstreamErr == nil {
			state.Upstream = strings.TrimSpace(upstream)
			counts, countErr := repo.git(ctx, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
			if countErr != nil {
				return state, countErr
			}
			fields := strings.Fields(counts)
			if len(fields) == 2 {
				state.Ahead, _ = strconv.Atoi(fields[0])
				state.Behind, _ = strconv.Atoi(fields[1])
			}
		}
	}
	return state, nil
}

func validPath(path string) error {
	if path == "" || filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return errors.New("文件路径无效")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("文件路径超出仓库范围")
	}
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if strings.EqualFold(part, ".git") {
			return errors.New("不能操作 Git 元数据")
		}
	}
	return nil
}

func (repo *Repository) changedPaths(ctx context.Context, paths []string, forUnstage bool) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("请选择文件")
	}
	files, err := repo.files(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]File{}
	for _, file := range files {
		known[file.Path] = file
	}
	result := []string{}
	seen := map[string]bool{}
	for _, path := range paths {
		if err := validPath(path); err != nil {
			return nil, err
		}
		file, exists := known[path]
		if !exists {
			return nil, fmt.Errorf("文件状态已改变，请刷新：%s", path)
		}
		names := []string{file.Path}
		if forUnstage && file.Index == "R" || !forUnstage && file.Working == "R" {
			names = append(names, file.OriginalPath)
		}
		for _, name := range names {
			if name != "" && !seen[name] {
				if err := validPath(name); err != nil {
					return nil, err
				}
				seen[name] = true
				result = append(result, name)
			}
		}
	}
	return result, nil
}

func (repo *Repository) Stage(ctx context.Context, paths []string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	names, err := repo.changedPaths(ctx, paths, false)
	if err != nil {
		return err
	}
	_, err = repo.git(ctx, append([]string{"add", "--"}, names...)...)
	return err
}

func (repo *Repository) Unstage(ctx context.Context, paths []string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	names, err := repo.changedPaths(ctx, paths, true)
	if err != nil {
		return err
	}
	head, err := repo.head(ctx)
	if err != nil {
		return err
	}
	args := []string{"restore", "--staged", "--"}
	if head == "" {
		args = []string{"rm", "--cached", "--force", "-r", "--ignore-unmatch", "--"}
	}
	_, err = repo.git(ctx, append(args, names...)...)
	return err
}

func (repo *Repository) Commit(ctx context.Context, message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("请输入提交说明")
	}
	if len(message) > 64<<10 {
		return errors.New("提交说明过长")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	_, err := run(ctx, repo.Path, message, 2*time.Minute, "commit", "--file=-")
	return err
}

func (repo *Repository) History(ctx context.Context, skip int) ([]Commit, error) {
	if skip < 0 {
		return nil, errors.New("历史分页无效")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	head, err := repo.head(ctx)
	if err != nil {
		return nil, err
	}
	commits := []Commit{}
	if head == "" {
		return commits, nil
	}
	output, err := repo.git(ctx, "log", "--all", "--topo-order", "-n", "100", "--skip="+strconv.Itoa(skip), "-z", "--format=%H%x00%P%x00%s%x00%an%x00%aI%x00%D")
	if err != nil {
		return nil, err
	}
	if output == "" {
		return commits, nil
	}
	fields := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	if len(fields)%6 != 0 {
		return nil, errors.New("无法解析提交历史")
	}
	for index := 0; index < len(fields); index += 6 {
		commits = append(commits, Commit{
			Hash: fields[index], Parents: strings.Fields(fields[index+1]), Subject: fields[index+2],
			Author: fields[index+3], Date: fields[index+4], Refs: fields[index+5],
		})
	}
	return commits, nil
}

func (repo *Repository) Branches(ctx context.Context) ([]Branch, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	output, err := repo.git(ctx, "for-each-ref", "--format=%(refname)%00%(objectname:short)%00%(HEAD)%00%(upstream:short)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	branches := []Branch{}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 4 {
			return nil, errors.New("无法解析分支")
		}
		remote := strings.HasPrefix(fields[0], "refs/remotes/")
		name := strings.TrimPrefix(strings.TrimPrefix(fields[0], "refs/heads/"), "refs/remotes/")
		branches = append(branches, Branch{Name: name, Hash: fields[1], Current: fields[2] == "*", Remote: remote, Upstream: fields[3]})
	}
	return branches, nil
}

func (repo *Repository) validateBranch(ctx context.Context, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return errors.New("分支名称无效")
	}
	_, err := repo.git(ctx, "check-ref-format", "refs/heads/"+name)
	if err != nil {
		return errors.New("分支名称无效")
	}
	return nil
}

func (repo *Repository) SwitchBranch(ctx context.Context, name string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if err := repo.validateBranch(ctx, name); err != nil {
		return err
	}
	_, err := repo.git(ctx, "switch", "--no-guess", "--", name)
	return err
}

func (repo *Repository) CreateBranch(ctx context.Context, name string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if err := repo.validateBranch(ctx, name); err != nil {
		return err
	}
	_, err := repo.git(ctx, "switch", "-c", name)
	return err
}

func (repo *Repository) Remote(ctx context.Context, action string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	var args []string
	switch action {
	case "fetch":
		args = []string{"fetch", "--all"}
	case "pull":
		args = []string{"pull", "--ff-only"}
	case "push":
		branch, err := repo.git(ctx, "symbolic-ref", "--quiet", "HEAD")
		if err != nil {
			return errors.New("分离的 HEAD 不能推送，请先切换分支")
		}
		upstream, err := repo.git(ctx, "for-each-ref", "--format=%(upstream:remotename)%00%(upstream:remoteref)", strings.TrimSpace(branch))
		if err != nil {
			return err
		}
		fields := strings.Split(strings.TrimSuffix(upstream, "\n"), "\x00")
		if len(fields) != 2 || fields[0] == "" || !strings.HasPrefix(fields[1], "refs/heads/") {
			return errors.New("请先为当前分支设置上游")
		}
		mirror, err := repo.git(ctx, "config", "--bool", "--get", "remote."+fields[0]+".mirror")
		if err != nil && !exitCode(err, 1) {
			return err
		}
		if strings.TrimSpace(mirror) == "true" {
			return errors.New("镜像远程不支持普通分支推送，请使用命令行操作")
		}
		args = []string{"push", "--no-follow-tags", "--", fields[0], "HEAD:" + fields[1]}
	default:
		return errors.New("不支持的远程操作")
	}
	if os.Getenv("GIT_SSH_COMMAND") == "" && os.Getenv("GIT_SSH") == "" {
		ssh, err := repo.git(ctx, "config", "--get", "core.sshCommand")
		if err != nil && !exitCode(err, 1) {
			return err
		}
		if strings.TrimSpace(ssh) == "" {
			args = append([]string{"-c", "core.sshCommand=ssh -oBatchMode=yes -oConnectTimeout=15"}, args...)
		}
	}
	_, err := run(ctx, repo.Path, "", 2*time.Minute, args...)
	return err
}
