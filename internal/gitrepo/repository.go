// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

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
	Operation Operation `json:"operation"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Branch    string    `json:"branch"`
	Head      string    `json:"head"`
	Upstream  string    `json:"upstream"`
	Ahead     int       `json:"ahead"`
	Behind    int       `json:"behind"`
	Detached  bool      `json:"detached"`
	Unborn    bool      `json:"unborn"`
	Files     []File    `json:"files"`
	Remotes   []string  `json:"remotes"`
}

type Branch struct {
	Name           string `json:"name"`
	Hash           string `json:"hash"`
	Current        bool   `json:"current"`
	Default        bool   `json:"default"`
	Remote         bool   `json:"remote"`
	Upstream       string `json:"upstream"`
	UpstreamRemote string `json:"upstreamRemote"`
	UpstreamBranch string `json:"upstreamBranch"`
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
	_, files, err := repo.status(ctx, false)
	return files, err
}

// pathFiles 只读取给定路径的状态，结果与 files 中对应条目一致。
// 重命名需要成对的两端：按路径限定时新路径显示为新增、旧路径显示为删除，
// 所以出现 A 或 D 时退回完整 status，保证 OriginalPath 与重命名状态不变。
// 调用方须先校验 paths。
func (repo *Repository) pathFiles(ctx context.Context, paths []string) ([]File, error) {
	// 路径过多时完整扫描与限定扫描相差不大，也避免命令行过长。
	if len(paths) == 0 || len(paths) > 64 {
		return repo.files(ctx)
	}
	output, err := repo.git(ctx, append([]string{"status", "--porcelain=v2", "-z", "--untracked-files=all", "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	_, files, err := parseStatus(output)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if strings.ContainsAny(file.Index+file.Working, "AD") {
			return repo.files(ctx)
		}
	}
	return files, nil
}

// statusBranch 是 status --branch 头部给出的 HEAD 与上游信息。
type statusBranch struct {
	head     string // 未出生分支为空
	branch   string // 分离 HEAD 时为空
	detached bool
	upstream string // 仅在上游引用存在、可计算领先落后时设置
	ahead    int
	behind   int
}

// status 用一次 porcelain v2 调用读取文件状态；withBranch 时同时取得 HEAD、
// 当前分支和领先落后数，替代原先 rev-parse、symbolic-ref、rev-list 等多次进程调用。
func (repo *Repository) status(ctx context.Context, withBranch bool) (statusBranch, []File, error) {
	args := []string{"status", "--porcelain=v2", "-z", "--untracked-files=all"}
	if withBranch {
		// 显式要求精确计数，忽略用户的 status.aheadBehind=false。
		args = append(args, "--branch", "--ahead-behind")
	}
	output, err := repo.git(ctx, args...)
	if err != nil {
		return statusBranch{}, nil, err
	}
	return parseStatus(output)
}

func parseStatus(output string) (statusBranch, []File, error) {
	var branch statusBranch
	files := []File{}
	invalid := errors.New("无法解析 Git 文件状态")
	// v2 用 "." 表示未变化，转换为 v1 的空格以保持 File 的既有取值。
	state := func(code string) (string, string) {
		return strings.ReplaceAll(code[:1], ".", " "), strings.ReplaceAll(code[1:2], ".", " ")
	}
	upstream, counted := "", false
	records := strings.Split(output, "\x00")
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" {
			continue
		}
		switch record[0] {
		case '#':
			key, value, _ := strings.Cut(strings.TrimPrefix(record, "# "), " ")
			switch key {
			case "branch.oid":
				if value != "(initial)" {
					branch.head = value
				}
			case "branch.head":
				if value == "(detached)" {
					branch.detached = true
				} else {
					branch.branch = value
				}
			case "branch.upstream":
				upstream = value
			case "branch.ab":
				fields := strings.Fields(value)
				if len(fields) != 2 {
					return branch, nil, invalid
				}
				ahead, aheadErr := strconv.Atoi(strings.TrimPrefix(fields[0], "+"))
				behind, behindErr := strconv.Atoi(strings.TrimPrefix(fields[1], "-"))
				if aheadErr != nil || behindErr != nil {
					return branch, nil, invalid
				}
				branch.ahead, branch.behind, counted = ahead, behind, true
			}
		case '1', '2', 'u':
			// 字段数：普通 8、重命名 9、冲突 10，之后是可含空格的路径。
			count := map[byte]int{'1': 9, '2': 10, 'u': 11}[record[0]]
			fields := strings.SplitN(record, " ", count)
			if len(fields) != count || len(fields[1]) != 2 {
				return branch, nil, invalid
			}
			file := File{Path: fields[count-1], Conflict: record[0] == 'u'}
			file.Index, file.Working = state(fields[1])
			if record[0] == '2' {
				index++
				if index >= len(records) || records[index] == "" {
					return branch, nil, errors.New("Git 重命名记录不完整")
				}
				file.OriginalPath = records[index]
			}
			files = append(files, file)
		case '?':
			if len(record) < 3 {
				return branch, nil, invalid
			}
			files = append(files, File{Path: record[2:], Index: "?", Working: "?"})
		case '!':
		default:
			return branch, nil, invalid
		}
	}
	// 上游已配置但远程分支不存在时没有 branch.ab，与 rev-parse @{upstream} 失败时一致地视为无上游。
	if counted {
		branch.upstream = upstream
	}
	return branch, files, nil
}

func (repo *Repository) Snapshot(ctx context.Context) (Snapshot, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	state := Snapshot{Name: filepath.Base(repo.Path), Path: repo.Path, Files: []File{}, Remotes: []string{}}
	branch, files, err := repo.status(ctx, true)
	if err != nil {
		return state, err
	}
	state.Files, state.Head, state.Unborn, state.Detached = files, branch.head, branch.head == "", branch.detached
	state.Branch = branch.branch
	if state.Detached {
		state.Branch = state.Head[:min(8, len(state.Head))]
	}
	if !state.Unborn && !state.Detached {
		state.Upstream, state.Ahead, state.Behind = branch.upstream, branch.ahead, branch.behind
	}
	if state.Operation, err = repo.operation(ctx); err != nil {
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
	for _, path := range paths {
		if err := validPath(path); err != nil {
			return nil, err
		}
	}
	files, err := repo.pathFiles(ctx, paths)
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
		file, exists := known[path]
		if !exists {
			return nil, fmt.Errorf("文件状态已改变，请刷新：%s", path)
		}
		if file.Conflict {
			return nil, errors.New("请在冲突编辑器中解决文件后再暂存")
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
	output, err := repo.git(ctx, "log", "--no-show-signature", "--all", "--topo-order", "-n", "100", "--skip="+strconv.Itoa(skip), "-z", "--format=%H%x00%P%x00%s%x00%an%x00%aI%x00%D")
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
	output, err := repo.git(ctx, "for-each-ref", "--format=%(refname)%00%(objectname:short)%00%(HEAD)%00%(upstream:short)%00%(symref)%00%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	branches := []Branch{}
	defaults := make(map[string]bool)
	targets := make(map[string]bool)
	defaultName := ""
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 7 {
			return nil, errors.New("无法解析分支")
		}
		remote := strings.HasPrefix(fields[0], "refs/remotes/")
		name := strings.TrimPrefix(strings.TrimPrefix(fields[0], "refs/heads/"), "refs/remotes/")
		branches = append(branches, Branch{
			Name: name, Hash: fields[1], Current: fields[2] == "*", Remote: remote, Upstream: fields[3],
			UpstreamRemote: fields[5], UpstreamBranch: strings.TrimPrefix(fields[6], "refs/heads/"),
		})
		if remote && strings.HasSuffix(fields[0], "/HEAD") && fields[4] != "" {
			prefix := strings.TrimSuffix(fields[0], "HEAD")
			if strings.HasPrefix(fields[4], prefix) {
				target := strings.TrimPrefix(fields[4], prefix)
				targets[target] = true
				defaults[strings.TrimPrefix(fields[4], "refs/remotes/")] = true
				if fields[0] == "refs/remotes/origin/HEAD" {
					defaultName = target
				}
			}
		}
	}
	if defaultName == "" && len(targets) == 1 {
		for name := range targets {
			defaultName = name
		}
	}
	if defaultName == "" {
		// 未获取远程 HEAD 的仓库仍优先显示常见主分支，不按当前检出状态猜测默认分支。
		for _, name := range []string{"main", "master"} {
			for _, branch := range branches {
				if !branch.Remote && branch.Name == name {
					defaultName = name
					break
				}
			}
			if defaultName != "" {
				break
			}
		}
	}
	for index := range branches {
		branch := &branches[index]
		branch.Default = !branch.Remote && branch.Name == defaultName || branch.Remote && defaults[branch.Name]
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
	_, err := repo.git(ctx, "switch", "--no-guess", "--no-overwrite-ignore", "--", name)
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
		return repo.pull(ctx)
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
	return repo.runRemote(ctx, args...)
}

// pull 拆成 fetch 与仅快进合并：git pull 不支持 --no-overwrite-ignore，会覆盖被忽略的本地文件，
// 还会受 pull.rebase、branch.<name>.mergeOptions 等继承配置影响。
func (repo *Repository) pull(ctx context.Context) error {
	branch, err := repo.git(ctx, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return errors.New("分离的 HEAD 不能拉取，请先切换分支")
	}
	branch = strings.TrimSpace(branch)
	output, err := repo.git(ctx, "for-each-ref", "--format=%(upstream)%00%(upstream:remotename)", branch)
	if err != nil {
		return err
	}
	upstream, remote, _ := strings.Cut(strings.TrimSuffix(output, "\n"), "\x00")
	if upstream == "" {
		return errors.New("请先为当前分支设置上游")
	}
	if remote != "" && remote != "." {
		if err := repo.runRemote(ctx, "fetch", "--", remote); err != nil {
			return err
		}
	}
	if err := repo.checkMergeIgnoredPaths(ctx, upstream); err != nil {
		return err
	}
	_, err = run(ctx, repo.Path, "", 2*time.Minute, "-c", "branch."+strings.TrimPrefix(branch, "refs/heads/")+".mergeOptions=",
		"merge", "--no-edit", "--no-autostash", "--no-overwrite-ignore", "--no-squash", "--ff-only", "--strategy=ort", "--", upstream)
	return err
}

// remoteTimeout 只作兜底上限；卡住的连接由 remoteOptions 的无进度检测提前中断，
// 避免大仓库正常传输被固定时长杀掉。
const remoteTimeout = 2 * time.Hour

func (repo *Repository) runRemote(ctx context.Context, args ...string) error {
	options, err := remoteOptions(ctx, repo.Path)
	if err != nil {
		return err
	}
	_, err = run(ctx, repo.Path, "", remoteTimeout, append(options, args...)...)
	return err
}

// remoteOptions 为用户未自定义的连接补上非交互 SSH 与无进度中断，用户配置优先。
func remoteOptions(ctx context.Context, root string) ([]string, error) {
	// 一次读取相关键；匹配的是规范化后的键名（节与变量名小写）。
	output, err := run(ctx, root, "", 15*time.Second, "config", "--null", "--get-regexp",
		`^(core\.sshcommand|http\.lowspeedlimit|fetch\.writecommitgraph)$`)
	if err != nil && !exitCode(err, 1) {
		return nil, err
	}
	configured := map[string]string{}
	for _, entry := range strings.Split(output, "\x00") {
		if key, value, _ := strings.Cut(entry, "\n"); key != "" {
			configured[key] = value // 多值时与 --get 相同，以最后一个为准
		}
	}
	var options []string
	if os.Getenv("GIT_SSH_COMMAND") == "" && os.Getenv("GIT_SSH") == "" && strings.TrimSpace(configured["core.sshcommand"]) == "" {
		options = append(options, "-c", "core.sshCommand=ssh -oBatchMode=yes -oConnectTimeout=15 -oServerAliveInterval=15 -oServerAliveCountMax=4")
	}
	if strings.TrimSpace(configured["http.lowspeedlimit"]) == "" {
		options = append(options, "-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=60")
	}
	// 获取时顺带增量写入 commit-graph，大仓库按拓扑排序读取历史可快数倍；
	// 只写 objects/info 下的缓存文件，不改变仓库内容，用户显式配置优先。
	if _, set := configured["fetch.writecommitgraph"]; !set {
		options = append(options, "-c", "fetch.writeCommitGraph=true")
	}
	return options, nil
}
