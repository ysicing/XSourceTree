// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package aikeychain

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestNativeKeychainRoundTrip(t *testing.T) {
	if os.Getenv("XSOURCETREE_TEST_KEYCHAIN") != "1" {
		t.Skip("仅在显式启用时使用临时系统凭证记录")
	}
	store := Store{Service: fmt.Sprintf("com.ysicing.xsourcetree.test.%d", time.Now().UnixNano())}
	t.Cleanup(func() {
		if err := store.Delete("synthetic-account"); err != nil {
			t.Error(err)
		}
	})
	for _, key := range []string{"synthetic-test-key", "updated-synthetic-key"} {
		if err := store.Save("synthetic-account", key); err != nil {
			t.Fatal(err)
		}
		saved, err := store.Load("synthetic-account")
		if err != nil || saved != key {
			t.Fatalf("钥匙串读写未通过: %v", err)
		}
	}
	other, err := store.Load("different-account")
	if err != nil || other != "" {
		t.Fatal("不同账号不应读取到密钥")
	}
	if err := store.Delete("synthetic-account"); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.Load("synthetic-account")
	if err != nil || deleted != "" {
		t.Fatal("清除后不应读到旧密钥")
	}
}
