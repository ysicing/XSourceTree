// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const outputLimit = 4 << 20

type limitedBuffer struct {
	// 不嵌入 bytes.Buffer，避免 io.Copy 通过提升的 ReadFrom 绕过 Write 限制。
	content  bytes.Buffer
	overflow bool
}

func (buffer *limitedBuffer) String() string {
	return buffer.content.String()
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	size := len(data)
	remaining := outputLimit - buffer.content.Len()
	if size > remaining {
		buffer.overflow = true
		data = data[:remaining]
	}
	_, _ = buffer.content.Write(data)
	return size, nil
}

type commandError struct {
	code    int
	message string
}

func (err *commandError) Error() string { return err.message }

func exitCode(err error, code int) bool {
	var failure *commandError
	return errors.As(err, &failure) && failure.code == code
}

func gitEnvironment() []string {
	local := map[string]bool{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_CONFIG": true,
		"GIT_CONFIG_PARAMETERS": true, "GIT_CONFIG_COUNT": true,
		"GIT_OBJECT_DIRECTORY": true, "GIT_DIR": true, "GIT_WORK_TREE": true,
		"GIT_IMPLICIT_WORK_TREE": true, "GIT_GRAFT_FILE": true,
		"GIT_INDEX_FILE": true, "GIT_NO_REPLACE_OBJECTS": true,
		"GIT_REPLACE_REF_BASE": true, "GIT_PREFIX": true,
		"GIT_SHALLOW_FILE": true, "GIT_COMMON_DIR": true,
	}
	environment := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if !local[key] && !strings.HasPrefix(key, "GIT_CONFIG_KEY_") && !strings.HasPrefix(key, "GIT_CONFIG_VALUE_") {
			environment = append(environment, entry)
		}
	}
	// 只读刷新（status、diff）默认会顺带回写索引并抢占 index.lock，
	// 与用户在终端中的提交、暂存冲突；必要的写操作不受此变量影响。
	return append(environment, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_PAGER=cat",
		"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "GIT_MERGE_AUTOEDIT=no", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
}

func run(ctx context.Context, root, input string, timeout time.Duration, args ...string) (string, error) {
	var stdout limitedBuffer
	if err := runTo(ctx, root, input, timeout, &stdout, func() string { return stdout.String() }, args...); err != nil {
		return "", err
	}
	if stdout.overflow {
		return "", errors.New("Git 输出超过 4 MiB，请使用命令行查看完整结果")
	}
	return stdout.String(), nil
}

// runDigest 将完整输出流式计入 SHA-256，用于大仓库索引等只需判断是否变化、
// 不需要保留内容且可能超过输出上限的场景。
func runDigest(ctx context.Context, root string, timeout time.Duration, args ...string) (string, error) {
	digest := sha256.New()
	if err := runTo(ctx, root, "", timeout, digest, func() string { return "" }, args...); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func runTo(ctx context.Context, root, input string, timeout time.Duration, stdout io.Writer, details func() string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	options := []string{"--no-pager", "--literal-pathspecs", "-C", root, "-c", "color.ui=false"}
	cmd := exec.CommandContext(ctx, "git", append(options, args...)...)
	cmd.Env = gitEnvironment()
	cmd.Stdin = strings.NewReader(input)
	cmd.WaitDelay = 2 * time.Second
	var stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = stdout, &stderr
	err := runProcess(ctx, cmd)
	if ctx.Err() != nil {
		return fmt.Errorf("Git 操作已取消或超时：%w", ctx.Err())
	}
	if err != nil {
		code := -1
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			code = failure.ExitCode()
		}
		message := strings.TrimSpace(stderr.String())
		if details := strings.TrimSpace(details()); details != "" {
			message = strings.TrimSpace(details + "\n" + message)
		}
		if message == "" {
			message = err.Error()
		}
		return &commandError{code: code, message: message}
	}
	if stderr.overflow {
		return errors.New("Git 输出超过 4 MiB，请使用命令行查看完整结果")
	}
	return nil
}
