package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const outputLimit = 4 << 20

type limitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	size := len(data)
	remaining := outputLimit - buffer.Len()
	if size > remaining {
		buffer.overflow = true
		data = data[:remaining]
	}
	_, _ = buffer.Buffer.Write(data)
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
	return append(environment, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_PAGER=cat", "LC_ALL=C")
}

func run(ctx context.Context, root, input string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	options := []string{"--no-pager", "--literal-pathspecs", "-C", root, "-c", "color.ui=false"}
	cmd := exec.CommandContext(ctx, "git", append(options, args...)...)
	cmd.Env = gitEnvironment()
	cmd.Stdin = strings.NewReader(input)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := runProcess(ctx, cmd)
	if ctx.Err() != nil {
		return "", fmt.Errorf("Git 操作已取消或超时：%w", ctx.Err())
	}
	if err != nil {
		code := -1
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			code = failure.ExitCode()
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", &commandError{code: code, message: message}
	}
	if stdout.overflow || stderr.overflow {
		return "", errors.New("Git 输出超过 4 MiB，请使用命令行查看完整结果")
	}
	return stdout.String(), nil
}
