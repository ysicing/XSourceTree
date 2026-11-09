package gitrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Patch 只包含后端生成的区块标识；Supported 为 false 时应使用整文件操作。
type Patch struct {
	Token     string      `json:"token"`
	Text      string      `json:"text"`
	Hunks     []PatchHunk `json:"hunks"`
	Supported bool        `json:"supported"`
}

// PatchHunk 的 ID 从零开始，仅对同一 Token 有效。
type PatchHunk struct {
	ID     int    `json:"id"`
	Header string `json:"header"`
}

type hunkPatch struct {
	view      Patch
	header    string
	hunks     []string
	indexMode string
	indexHash string
}

// Patch 返回工作区（staged=false）或暂存区（staged=true）的可操作区块。
func (repo *Repository) Patch(ctx context.Context, path string, staged bool) (Patch, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	patch, err := repo.hunkPatch(ctx, path, staged)
	return patch.view, err
}

// ApplyHunk 重新生成并校验补丁，只暂存或取消暂存指定区块，不修改工作区。
func (repo *Repository) ApplyHunk(ctx context.Context, path string, staged bool, token string, id int) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	patch, err := repo.hunkPatch(ctx, path, staged)
	if err != nil {
		return err
	}
	if !patch.view.Supported {
		return errors.New("此文件不支持区块操作，请使用整文件暂存或取消暂存")
	}
	if token == "" || patch.view.Token != token {
		return errors.New("文件、索引或 HEAD 已改变，请刷新后重试")
	}
	if id < 0 || id >= len(patch.hunks) {
		return errors.New("区块不存在，请刷新后重试")
	}
	input, err := repo.prepareHunkApply(ctx, patch, id, staged)
	if err != nil {
		return err
	}
	args := []string{"-c", "apply.ignoreWhitespace=no", "apply", "--cached", "--whitespace=nowarn", "-p1"}
	if _, err := run(ctx, repo.Path, input, 30*time.Second, append(args, "--check", "-")...); err != nil {
		return err
	}
	// 令牌检查尽早拒绝外部变更；最终二进制补丁还会由 Git 在持有索引锁时
	// 严格校验目标旧 blob，不能将过期文本区块偏移匹配到另一处相同上下文。
	current, err := repo.hunkPatch(ctx, path, staged)
	if err != nil {
		return err
	}
	if !current.view.Supported || current.view.Token != token {
		return errors.New("文件、索引或 HEAD 已改变，请刷新后重试")
	}
	_, err = run(ctx, repo.Path, input, 30*time.Second, append(args, "-")...)
	return err
}

func (repo *Repository) prepareHunkApply(ctx context.Context, patch hunkPatch, id int, staged bool) (string, error) {
	directory, err := os.MkdirTemp("", "xsource-hunk-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	before, err := repo.git(ctx, "cat-file", "blob", patch.indexHash)
	if err != nil {
		return "", err
	}
	after, err := applyHunkToBlob(before, patch.hunks[id], staged)
	if err != nil {
		return "", err
	}
	for name, content := range map[string]string{"before": before, "after": after} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			return "", err
		}
	}
	newHash, err := run(ctx, repo.Path, after, 30*time.Second, "hash-object", "--no-filters", "--stdin")
	if err != nil {
		return "", err
	}
	attributes := filepath.Join(directory, "attributes")
	if err := os.WriteFile(attributes, []byte("* -diff -text -filter -ident -working-tree-encoding\n"), 0600); err != nil {
		return "", err
	}
	output := filepath.Join(directory, "binary.patch")
	// 显式 git-dir 阻止向父目录发现仓库；no-index 只读取临时文件，不创建 Git 元数据。
	_, err = run(ctx, directory, "", 30*time.Second,
		"--git-dir="+directory, "-c", "core.attributesFile="+attributes, "-c", "diff.orderFile="+os.DevNull,
		"-c", "core.autocrlf=false", "-c", "core.eol=lf",
		"diff", "--no-index", "--binary", "--full-index", "--no-ext-diff", "--no-textconv",
		"--no-color", "--no-renames", "--output="+output, "--", "before", "after")
	if !exitCode(err, 1) {
		if err == nil {
			err = errors.New("选定区块没有产生内容变化，请刷新后重试")
		}
		return "", err
	}
	file, err := os.Open(output)
	if err != nil {
		return "", err
	}
	defer file.Close()
	binaryPatch, err := io.ReadAll(io.LimitReader(file, outputLimit+1))
	if err != nil {
		return "", err
	}
	if len(binaryPatch) > outputLimit {
		return "", errors.New("区块补丁超过 4 MiB，请使用整文件操作")
	}
	_, body, ok := strings.Cut(string(binaryPatch), "GIT binary patch\n")
	if !ok {
		return "", errors.New("无法生成严格校验旧版本的区块补丁")
	}
	header, _, ok := strings.Cut(patch.header, "\n")
	if !ok {
		return "", errors.New("区块补丁文件头无效，请刷新后重试")
	}
	// 临时 no-index diff 的载荷与对象哈希格式无关；使用本仓库 hash-object 的结果，
	// 兼容 SHA-1/SHA-256，最终 apply 不启用三方合并或文本回退。
	return header + "\nindex " + patch.indexHash + ".." + strings.TrimSpace(newHash) + " " + patch.indexMode + "\nGIT binary patch\n" + body, nil
}

func applyHunkToBlob(before, hunk string, staged bool) (string, error) {
	invalid := errors.New("区块与索引原始内容不一致，请刷新后重试")
	header, body, ok := strings.Cut(hunk, "\n")
	if !ok || !patchHunkHeader.MatchString(header+"\n") {
		return "", invalid
	}
	side := 1
	if staged {
		side = 2
	}
	coordinate, countText, explicitCount := strings.Cut(strings.Fields(header)[side][1:], ",")
	start, err := strconv.Atoi(coordinate)
	if err != nil {
		return "", invalid
	}
	count := 1
	if explicitCount {
		count, err = strconv.Atoi(countText)
		if err != nil {
			return "", invalid
		}
	}
	var source, target strings.Builder
	sourceLines := 0
	lines := strings.SplitAfter(body, "\n")
	for index, line := range lines {
		if line == "" || line == "\\ No newline at end of file\n" {
			continue
		}
		content := line[1:]
		if index+1 < len(lines) && lines[index+1] == "\\ No newline at end of file\n" {
			content = strings.TrimSuffix(content, "\n")
		}
		switch line[0] {
		case ' ':
			source.WriteString(content)
			target.WriteString(content)
			sourceLines++
		case '-', '+':
			if (line[0] == '+') == staged {
				source.WriteString(content)
				sourceLines++
			} else {
				target.WriteString(content)
			}
		default:
			return "", invalid
		}
	}
	if sourceLines != count {
		return "", invalid
	}
	// 单区块只使用当前索引一侧的坐标；不搜索相似上下文，也不经过 attributes/EOL 转换。
	if count > 0 {
		start--
	}
	if start < 0 {
		return "", invalid
	}
	position := 0
	for line := 0; line < start; line++ {
		end := strings.IndexByte(before[position:], '\n')
		if end < 0 {
			return "", invalid
		}
		position += end + 1
	}
	old := source.String()
	if !strings.HasPrefix(before[position:], old) ||
		(old != "" && !strings.HasSuffix(old, "\n") && position+len(old) != len(before)) {
		return "", invalid
	}
	return before[:position] + target.String() + before[position+len(old):], nil
}

func (repo *Repository) hunkPatch(ctx context.Context, path string, staged bool) (hunkPatch, error) {
	patch := hunkPatch{view: Patch{Hunks: []PatchHunk{}}}
	if err := validPath(path); err != nil {
		return patch, err
	}
	if filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
		return patch, errors.New("请使用仓库内的规范相对路径")
	}
	if err := ctx.Err(); err != nil {
		return patch, err
	}
	head, err := repo.head(ctx)
	if err != nil || head == "" {
		return patch, err
	}
	index, err := repo.git(ctx, "ls-files", "--stage", "-z")
	if err != nil {
		return patch, err
	}
	indexMode, indexHash := patchIndexEntry(index, path)
	if indexHash == "" {
		return patch, nil
	}
	tree, err := repo.git(ctx, "ls-tree", "--full-tree", "-z", head, "--", path)
	if err != nil {
		return patch, err
	}
	metadata, name, ok := strings.Cut(strings.TrimSuffix(tree, "\x00"), "\t")
	fields := strings.Fields(metadata)
	if !ok || name != path || len(fields) != 3 || fields[0] != indexMode || fields[1] != "blob" {
		return patch, nil
	}
	root, err := os.OpenRoot(repo.Path)
	if err != nil {
		return patch, err
	}
	defer root.Close()
	if err := repo.checkConflictAncestors(ctx, root, path); err != nil {
		return patch, err
	}
	content, mode, supported, err := readPatchFile(root, path)
	if err != nil || !supported {
		return patch, err
	}
	if (mode.Perm()&0111 != 0) != (indexMode == "100755") {
		return patch, nil
	}
	for _, hash := range []string{fields[2], indexHash} {
		supported, err := repo.patchTextBlob(ctx, hash)
		if err != nil || !supported {
			return patch, err
		}
	}
	workingDiff, err := repo.stableHunkDiff(ctx, path, false)
	if err != nil {
		return patch, err
	}
	stagedDiff, err := repo.stableHunkDiff(ctx, path, true)
	if err != nil {
		return patch, err
	}
	text := workingDiff
	if staged {
		text = stagedDiff
	}
	header, hunks := splitHunkPatch(text)
	if len(hunks) == 0 {
		return patch, nil
	}
	// 不使用索引文件的时间戳：索引刷新不应失效，任意条目的内容或冲突阶段变化则必须失效。
	currentHead, err := repo.head(ctx)
	if err != nil {
		return patch, err
	}
	currentIndex, err := repo.git(ctx, "ls-files", "--stage", "-z")
	if err != nil {
		return patch, err
	}
	currentContent, currentMode, supported, err := readPatchFile(root, path)
	if err != nil {
		return patch, err
	}
	if !supported || head != currentHead || index != currentIndex || content != currentContent || mode != currentMode {
		return patch, errors.New("生成补丁时文件、索引或 HEAD 已改变，请刷新后重试")
	}
	digest := sha256.New()
	for _, value := range []string{repo.Path, path, strconv.FormatBool(staged), head, index, content, mode.String(), workingDiff, stagedDiff} {
		fmt.Fprintf(digest, "%d:", len(value))
		io.WriteString(digest, value)
	}
	patch.view.Token = hex.EncodeToString(digest.Sum(nil))
	patch.view.Text = text
	patch.view.Supported = true
	patch.header, patch.hunks = header, hunks
	patch.indexMode, patch.indexHash = indexMode, indexHash
	for id, hunk := range hunks {
		title, _, _ := strings.Cut(hunk, "\n")
		patch.view.Hunks = append(patch.view.Hunks, PatchHunk{ID: id, Header: title})
	}
	return patch, nil
}

func patchIndexEntry(index, path string) (mode, hash string) {
	found := false
	for _, entry := range strings.Split(index, "\x00") {
		metadata, name, ok := strings.Cut(entry, "\t")
		if !ok || name != path {
			continue
		}
		fields := strings.Fields(metadata)
		if found || len(fields) != 3 || fields[2] != "0" || (fields[0] != "100644" && fields[0] != "100755") {
			return "", ""
		}
		found = true
		mode, hash = fields[0], fields[1]
	}
	return mode, hash
}

func readPatchFile(root *os.Root, path string) (string, os.FileMode, bool, error) {
	info, err := root.Lstat(path)
	if os.IsNotExist(err) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > outputLimit {
		return "", 0, false, nil
	}
	file, err := root.Open(path)
	if err != nil {
		return "", 0, false, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", 0, false, err
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return "", 0, false, errors.New("读取补丁时文件已被替换，请刷新后重试")
	}
	content, err := io.ReadAll(io.LimitReader(file, outputLimit+1))
	if err != nil {
		return "", 0, false, err
	}
	if len(content) > outputLimit || !utf8.Valid(content) || strings.ContainsRune(string(content), 0) {
		return "", 0, false, nil
	}
	return string(content), opened.Mode(), true, nil
}

func (repo *Repository) patchTextBlob(ctx context.Context, hash string) (bool, error) {
	size, err := repo.git(ctx, "cat-file", "-s", hash)
	if err != nil {
		return false, err
	}
	length, err := strconv.ParseInt(strings.TrimSpace(size), 10, 64)
	if err != nil || length < 0 || length > outputLimit {
		return false, err
	}
	content, err := repo.git(ctx, "cat-file", "blob", hash)
	return err == nil && utf8.ValidString(content) && !strings.ContainsRune(content, 0), err
}

func (repo *Repository) stableHunkDiff(ctx context.Context, path string, staged bool) (string, error) {
	args := []string{
		"-c", "core.quotePath=true", "-c", "diff.suppressBlankEmpty=false", "-c", "diff.orderFile=" + os.DevNull,
		"diff", "--patch", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames",
		"--diff-algorithm=myers", "--no-indent-heuristic", "--unified=3", "--inter-hunk-context=0",
		"--no-function-context", "--no-relative", "--word-diff=none", "--full-index",
		"--src-prefix=a/", "--dst-prefix=b/", "--output-indicator-new=+",
		"--output-indicator-old=-", "--output-indicator-context= ", "--ignore-submodules=none",
	}
	if staged {
		args = append(args, "--cached")
	}
	return repo.git(ctx, append(args, "--", path)...)
}

var patchHunkHeader = regexp.MustCompile(`^@@ -[0-9]+(,[0-9]+)? \+[0-9]+(,[0-9]+)? @@[^\n]*\n$`)

func splitHunkPatch(text string) (string, []string) {
	headerEnd := -1
	start := -1
	offset := 0
	files := 0
	hunks := []string{}
	for _, line := range strings.SplitAfter(text, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			files++
			if files != 1 {
				return "", nil
			}
		}
		if start < 0 {
			for _, prefix := range []string{"old mode ", "new mode ", "new file mode ", "deleted file mode ", "rename ", "copy ", "Binary files ", "GIT binary patch"} {
				if strings.HasPrefix(line, prefix) {
					return "", nil
				}
			}
		}
		if strings.HasPrefix(line, "@@ ") {
			if !patchHunkHeader.MatchString(line) {
				return "", nil
			}
			if start >= 0 {
				hunks = append(hunks, text[start:offset])
			} else {
				headerEnd = offset
			}
			start = offset
		}
		offset += len(line)
	}
	if files != 1 || start < 0 {
		return "", nil
	}
	hunks = append(hunks, text[start:])
	return text[:headerEnd], hunks
}
