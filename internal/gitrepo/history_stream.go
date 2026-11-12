// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type historyLogWriter struct {
	ctx         context.Context
	cancel      context.CancelFunc
	query       string
	skip        int
	commits     []Commit
	fields      [8]string
	field       []byte
	fieldIndex  int
	recordBytes int
	stopped     bool
	err         error
}

func newHistoryLogWriter(ctx context.Context, query string, skip int, cancel context.CancelFunc) *historyLogWriter {
	return &historyLogWriter{
		ctx: ctx, cancel: cancel, query: strings.ToLower(query), skip: skip, commits: []Commit{},
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
	for _, index := range [...]int{0, 2, 3, 6, 7} {
		if matches || strings.Contains(strings.ToLower(writer.fields[index]), writer.query) {
			matches = true
			break
		}
	}
	if !matches {
		return
	}
	if writer.skip > 0 {
		writer.skip--
		return
	}
	writer.commits = append(writer.commits, Commit{
		Hash: writer.fields[0], Parents: strings.Fields(writer.fields[1]), Subject: writer.fields[2],
		Author: writer.fields[3], Date: writer.fields[4], Refs: writer.fields[5],
	})
	if len(writer.commits) == 100 {
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

func (repo *Repository) streamHistory(ctx context.Context, query, path string, skip int, filter HistoryFilter) ([]Commit, error) {
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := []string{"--no-pager", "--literal-pathspecs", "-C", repo.Path, "-c", "color.ui=false",
		"log", "--no-show-signature", "--decorate=short", "--topo-order", "-z",
		"--format=%H%x00%P%x00%s%x00%an%x00%aI%x00%D%x00%B%x00%ae"}
	if filter.HideRemotes {
		args = append(args, "--decorate-refs-exclude=refs/remotes/*")
	}
	if filter.CurrentBranch {
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
	stdout := newHistoryLogWriter(ctx, query, skip, cancel)
	var stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = stdout, &stderr
	// runProcess 等待进程及输出复制结束，同时沿用平台进程树取消机制。
	err := runProcess(processCtx, cmd)
	if ctx.Err() != nil {
		return nil, fmt.Errorf("Git 操作已取消或超时：%w", ctx.Err())
	}
	if stdout.err != nil {
		return nil, stdout.err
	}
	if stderr.overflow {
		return nil, errors.New("Git 输出超过 4 MiB，请使用命令行查看完整结果")
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
		return nil, &commandError{code: code, message: message}
	}
	if err := stdout.finish(); err != nil {
		return nil, err
	}
	return stdout.commits, nil
}
