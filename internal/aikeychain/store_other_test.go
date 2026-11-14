// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !darwin

package aikeychain

import (
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func TestCredentialStoreRejectsInvalidIdentity(t *testing.T) {
	for _, store := range []Store{{}, {Service: "test\x00service"}} {
		if _, err := store.Load("account"); err == nil {
			t.Fatal("无效服务名不应读取应用默认凭证")
		}
		if err := store.Save("account", "synthetic"); err == nil {
			t.Fatal("无效服务名不应写入凭证")
		}
		if err := store.Delete("account"); err == nil {
			t.Fatal("无效服务名不应删除凭证")
		}
	}
	store := Store{Service: "com.ysicing.xsourcetree.invalid-test"}
	for _, account := range []string{"", "account\x00", strings.Repeat("a", 257)} {
		if _, err := store.Load(account); err == nil {
			t.Fatal("无效账号不应读取凭证")
		}
		if err := store.Save(account, "synthetic"); err == nil {
			t.Fatal("无效账号不应写入凭证")
		}
		if err := store.Delete(account); err == nil {
			t.Fatal("无效账号不应删除凭证")
		}
	}
	if err := store.Save("account", strings.Repeat("a", mygo.MaxSecretSize+1)); err == nil {
		t.Fatal("过长的密钥不应截断后保存")
	}
}
