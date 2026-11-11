// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package commitai

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMagpieLiveSyntheticDiff(t *testing.T) {
	if os.Getenv("XSOURCETREE_TEST_MAGPIE") != "1" {
		t.Skip("仅在明确启用时连接本机 Magpie，发送合成差异")
	}
	client, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	message, err := client.Generate(ctx, Options{}, "diff --git a/example.go b/example.go\n--- a/example.go\n+++ b/example.go\n@@ -1 +1 @@\n-func greeting() string { return \"helo\" }\n+func greeting() string { return \"hello\" }\n")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("合成差异生成结果: %s", message)
}
