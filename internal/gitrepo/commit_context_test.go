// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCommitContextOnlyIncludesStagedSnapshot(t *testing.T) {
	repo, path := fixture(t)
	ctx := context.Background()
	write(t, path, "staged.txt", "staged content\n")
	write(t, path, "untracked.txt", "untracked secret\n")
	git(t, path, "add", "staged.txt")
	write(t, path, "staged.txt", "unstaged secret\n")
	staged, err := repo.CommitContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(staged.Diff, "+staged content") || strings.Contains(staged.Diff, "secret") ||
		strings.Contains(staged.Diff, "untracked.txt") {
		t.Fatalf("读取了暂存区以外的内容: %s", staged.Diff)
	}
	token, err := repo.CommitToken(ctx)
	if err != nil || token != staged.Token {
		t.Fatalf("未暂存修改不应改变令牌: %v", err)
	}
	git(t, path, "add", "staged.txt")
	token, err = repo.CommitToken(ctx)
	if err != nil || token == staged.Token {
		t.Fatalf("暂存修改必须改变令牌: %v", err)
	}
}

func TestCommitContextEmptyBinaryAndLimit(t *testing.T) {
	repo, path := fixture(t)
	ctx := context.Background()
	if _, err := repo.CommitContext(ctx); err == nil {
		t.Fatal("空暂存区应拒绝生成")
	}
	write(t, path, "binary.bin", "\x00binary secret")
	git(t, path, "add", ".")
	staged, err := repo.CommitContext(ctx)
	if err != nil || !strings.Contains(staged.Diff, "Binary files") || strings.Contains(staged.Diff, "binary secret") {
		t.Fatalf("二进制文件应只发送变化摘要: %s, %v", staged.Diff, err)
	}
	git(t, path, "commit", "-m", "initial")
	if _, err := repo.CommitContext(ctx); err == nil {
		t.Fatal("已提交内容不应再次作为暂存变更")
	}
	write(t, path, "large.txt", strings.Repeat("large content\n", 10000))
	git(t, path, "add", ".")
	limited, err := repo.CommitContext(ctx)
	if !errors.Is(err, ErrCommitContextTooLarge) || !strings.Contains(err.Error(), "96 KiB") {
		t.Fatalf("超大差异必须明确拒绝，不能静默截断: %v", err)
	}
	token, tokenErr := repo.CommitToken(ctx)
	if tokenErr != nil || limited.Token != token || limited.Diff != "" {
		t.Fatal("超限时应仅返回暂存快照令牌，不返回部分差异")
	}
}

func TestCommitContextIgnoresExternalDiffAndTextconv(t *testing.T) {
	repo, path := fixture(t)
	write(t, path, ".gitattributes", "*.txt diff=custom\n")
	write(t, path, "hello.txt", "staged\n")
	git(t, path, "add", ".")
	git(t, path, "config", "diff.custom.command", "false")
	git(t, path, "config", "diff.custom.textconv", "false")
	staged, err := repo.CommitContext(context.Background())
	if err != nil || !strings.Contains(staged.Diff, "+staged") {
		t.Fatalf("不应运行外部 diff/textconv: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.CommitContext(ctx); err == nil {
		t.Fatal("取消后不应继续读取")
	}
}
