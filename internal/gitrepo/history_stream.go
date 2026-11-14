// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type historyLogWriter struct {
	ctx         context.Context
	cancel      context.CancelFunc
	query       string
	skip        int
	limit       int
	commits     []Commit
	fields      [8]string
	field       []byte
	fieldIndex  int
	recordBytes int
	stopped     bool
	err         error
	target      string
	found       bool
	offset      int
	filter      HistoryFilter
}

func newHistoryLogWriter(ctx context.Context, query string, skip int, cancel context.CancelFunc) *historyLogWriter {
	return &historyLogWriter{
		ctx: ctx, cancel: cancel, query: strings.ToLower(query), skip: skip, limit: DefaultHistoryPageSize, commits: []Commit{},
	}
}

func (writer *historyLogWriter) Write(data []byte) (int, error) {
	size := len(data)
	if writer.err != nil {
		return 0, writer.err
	}
	if writer.stopped {
		return size, nil
	}
	for len(data) > 0 {
		if err := writer.ctx.Err(); err != nil {
			return 0, writer.fail(err)
		}
		end := bytes.IndexByte(data, 0)
		length := end
		if end < 0 {
			length = len(data)
		}
		added := length
		if end >= 0 {
			added++
		}
		if added > outputLimit-writer.recordBytes || length > outputLimit-len(writer.field) {
			return 0, writer.fail(errors.New("单条 Git 提交历史超过 4 MiB，请使用命令行查看完整结果"))
		}
		writer.field = append(writer.field, data[:length]...)
		writer.recordBytes += added
		if end < 0 {
			break
		}
		writer.fields[writer.fieldIndex] = string(writer.field)
		writer.field = nil
		writer.fieldIndex++
		data = data[end+1:]
		if writer.fieldIndex == len(writer.fields) {
			writer.accept()
			writer.fields = [8]string{}
			writer.fieldIndex = 0
			writer.recordBytes = 0
			if writer.stopped {
				break
			}
		}
	}
	return size, nil
}

func (writer *historyLogWriter) accept() {
	matches := writer.query == ""
	indices := []int{0, 2, 3, 6, 7}
	switch writer.filter.Mode {
	case "message":
		indices = []int{6}
	case "hash":
		indices = []int{0}
	case "author":
		indices = []int{3, 7}
	case "branch", "changes":
		matches = true
	}
	for _, index := range indices {
		if matches || strings.Contains(strings.ToLower(writer.fields[index]), writer.query) {
			matches = true
			break
		}
	}
	if !matches {
		return
	}
	if writer.filter.Since != "" || writer.filter.Until != "" {
		date, err := time.Parse(time.RFC3339, writer.fields[4])
		if err != nil {
			return
		}
		day := date.In(time.Local).Format(time.DateOnly)
		if writer.filter.Since != "" && day < writer.filter.Since ||
			writer.filter.Until != "" && day > writer.filter.Until {
			return
		}
	}
	if writer.skip > 0 {
		writer.skip--
		return
	}
	if writer.target != "" && len(writer.commits) == writer.limit {
		// 定位只保留目标所在的一页，避免把标签之前的全部历史装入内存。
		writer.commits = writer.commits[:0]
		writer.offset += writer.limit
	}
	writer.commits = append(writer.commits, Commit{
		Hash: writer.fields[0], Parents: strings.Fields(writer.fields[1]), Subject: writer.fields[2],
		Author: writer.fields[3], Date: writer.fields[4], Refs: writer.fields[5],
	})
	writer.found = writer.found || writer.fields[0] == writer.target
	if len(writer.commits) == writer.limit && (writer.target == "" || writer.found) {
		// 仅停止本次日志子进程；调用方 Context 的取消仍须作为失败返回。
		writer.stopped = true
		writer.cancel()
	}
}

func (writer *historyLogWriter) fail(err error) error {
	writer.err = err
	writer.cancel()
	return err
}

func (writer *historyLogWriter) finish() error {
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	if writer.err != nil {
		return writer.err
	}
	if !writer.stopped && (writer.fieldIndex != 0 || writer.recordBytes != 0) {
		return errors.New("无法解析提交历史")
	}
	return nil
}

func (repo *Repository) streamHistory(ctx context.Context, query, path string, skip, limit int, filter HistoryFilter) ([]Commit, error) {
	writer := newHistoryLogWriter(ctx, query, skip, nil)
	writer.limit = limit
	if err := repo.readHistory(ctx, path, filter, writer); err != nil {
		return nil, err
	}
	return writer.commits, nil
}

const historyFormat = "--format=%H%x00%P%x00%s%x00%an%x00%aI%x00%D%x00%B%x00%ae"

func (repo *Repository) readHistory(ctx context.Context, path string, filter HistoryFilter, stdout *historyLogWriter) error {
	stdout.filter = filter
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout.cancel = cancel
	args := []string{"--no-pager", "--literal-pathspecs", "-C", repo.Path, "-c", "color.ui=false",
		"log", "--no-show-signature", "--decorate=short", "--topo-order", "-z",
		historyFormat}
	if filter.HideRemotes {
		args = append(args, "--decorate-refs-exclude=refs/remotes/*")
	}
	if stdout.skip > 0 && stdout.query == "" && filter.Since == "" && filter.Until == "" {
		// 无内容筛选时让 Git 跳过前页，避免把不会展示的正文传输并解析到内存。
		args = append(args, "--skip="+strconv.Itoa(stdout.skip))
		stdout.skip = 0
	}
	if filter.Mode == "changes" && stdout.query != "" {
		args = append(args, "-G"+regexp.QuoteMeta(stdout.query), "--regexp-ignore-case", "--diff-merges=first-parent",
			"--no-patch", "--no-ext-diff", "--no-textconv")
	}
	if filter.Mode == "branch" && stdout.query != "" {
		refs, err := repo.git(ctx, "for-each-ref", "--format=%(refname)", "refs/heads/", "refs/remotes/")
		if err != nil {
			return err
		}
		matched := false
		for _, ref := range strings.Fields(refs) {
			if filter.HideRemotes && strings.HasPrefix(ref, "refs/remotes/") {
				continue
			}
			name := strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/remotes/")
			if strings.Contains(strings.ToLower(name), stdout.query) {
				args = append(args, ref)
				matched = true
			}
		}
		if !matched {
			return nil
		}
	} else if filter.CurrentBranch {
		args = append(args, "HEAD")
	} else {
		if filter.HideRemotes {
			args = append(args, "--exclude=refs/remotes/*")
		}
		args = append(args, "--all")
	}
	if path != "" {
		args = append(args, "--full-history")
	}
	args = append(args, "--")
	if path != "" {
		args = append(args, path)
	}
	cmd := exec.CommandContext(processCtx, "git", args...)
	cmd.Env = gitEnvironment()
	cmd.Stdin = strings.NewReader("")
	cmd.WaitDelay = 2 * time.Second
	var stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = stdout, &stderr
	// runProcess 等待进程及输出复制结束，同时沿用平台进程树取消机制。
	err := runProcess(processCtx, cmd)
	if ctx.Err() != nil {
		return fmt.Errorf("Git 操作已取消或超时：%w", ctx.Err())
	}
	if stdout.err != nil {
		return stdout.err
	}
	if stderr.overflow {
		return errors.New("Git 输出超过 4 MiB，请使用命令行查看完整结果")
	}
	if err != nil && !stdout.stopped {
		code := -1
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			code = failure.ExitCode()
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return &commandError{code: code, message: message}
	}
	if err := stdout.finish(); err != nil {
		return err
	}
	return nil
}
