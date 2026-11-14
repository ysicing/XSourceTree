// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryPaginationSkipsUnrequestedLargeMessages(t *testing.T) {
	repo, path := fixture(t)
	git(t, path, "commit", "--allow-empty", "-m", "root")
	root := git(t, path, "rev-parse", "HEAD")
	message := filepath.Join(t.TempDir(), "message")
	if err := os.WriteFile(message, []byte("large\n\n"+strings.Repeat("x", outputLimit)), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, path, "commit", "--allow-empty", "-F", message)
	git(t, path, "commit", "--allow-empty", "-m", "head")
	commits, err := repo.SearchHistory(context.Background(), "", "", 2, HistoryFilter{})
	if err != nil || len(commits) != 1 || commits[0].Hash != root {
		t.Fatalf("未请求的前页内容阻止后页加载: %+v, %v", commits, err)
	}
	if _, err := repo.SearchHistory(context.Background(), "", "", 1, HistoryFilter{}); err == nil {
		t.Fatal("实际请求的大提交丢失大小限制")
	}
}
