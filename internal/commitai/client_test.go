// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package commitai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryAndEndpointValidation(t *testing.T) {
	t.Setenv("MAGPIE_ADDR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".config", "magpie")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gateway.json"), []byte(`{"addr":"127.0.0.1:4567"}`), 0600); err != nil {
		t.Fatal(err)
	}
	endpoint, err := DiscoverEndpoint()
	if err != nil || endpoint != "http://127.0.0.1:4567/v1" {
		t.Fatalf("自动发现失败: %s, %v", endpoint, err)
	}
	t.Setenv("MAGPIE_ADDR", "[::1]:9876")
	endpoint, err = DiscoverEndpoint()
	if err != nil || endpoint != "http://[::1]:9876/v1" {
		t.Fatalf("环境覆盖失败: %s, %v", endpoint, err)
	}
	for _, invalid := range []string{"https://example.com/v1", "file:///tmp/secret", "http://user:secret@localhost/v1", "http://127.0.0.1/v1?key=secret"} {
		if _, err := ValidateEndpoint(invalid); err == nil {
			t.Fatalf("接受了非本机或带凭证地址: %s", invalid)
		}
	}
}

func TestResponsesContract(t *testing.T) {
	t.Setenv("MAGPIE_API_KEY", "test-local-key")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-local-key" {
			t.Error("未使用本机环境凭证")
		}
		switch request.URL.Path {
		case "/v1/models":
			_, _ = writer.Write([]byte(`{"data":[{"id":"other"},{"id":"group/codex"}]}`))
		case "/v1/responses":
			var payload struct {
				Model        string `json:"model"`
				Instructions string `json:"instructions"`
				Input        string `json:"input"`
				Store        bool   `json:"store"`
				Stream       bool   `json:"stream"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload.Model != "group/codex" || payload.Store || payload.Stream ||
				!strings.Contains(payload.Instructions, "untrusted") || !strings.Contains(payload.Input, "+staged") {
				t.Errorf("请求契约错误: %+v", payload)
			}
			_, _ = writer.Write([]byte(`{"status":"completed","output":[{"type":"reasoning","content":[{"type":"output_text","text":"private reasoning"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"feat(ai): 添加提交说明生成"}]}]}`))
		default:
			t.Errorf("未知请求路径: %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client, err := New(server.URL + "/v1")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	message, err := client.Generate(context.Background(), Options{}, "+staged")
	if err != nil || message != "feat(ai): 添加提交说明生成" {
		t.Fatalf("生成失败: %q, %v", message, err)
	}
	if _, err := SelectModel([]string{"other"}, "missing"); err == nil {
		t.Fatal("配置模型不存在时不能静默使用其他模型")
	}
}

func TestFailuresAreBoundedAndDoNotExposeResponse(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		body   string
	}{
		{"auth", 401, "upstream-secret-token"},
		{"invalid-json", 200, "upstream-secret-token"},
		{"large", 200, strings.Repeat("s", responseLimit+1)},
		{"incomplete", 200, `{"status":"incomplete","output":[]}`},
		{"empty", 200, `{"status":"completed","output":[]}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/models" {
					_, _ = writer.Write([]byte(`{"data":[{"id":"test"}]}`))
					return
				}
				writer.WriteHeader(scenario.status)
				_, _ = writer.Write([]byte(scenario.body))
			}))
			defer server.Close()
			client, err := New(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_, err = client.Generate(context.Background(), Options{}, "diff")
			if err == nil || strings.Contains(err.Error(), "upstream-secret-token") {
				t.Fatalf("错误缺失或泄露响应正文: %v", err)
			}
		})
	}
}

func TestCancelRequestAndRejectRedirect(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	client, _ := New(server.URL)
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.Models(ctx); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("未传播取消")
		}
	case <-time.After(time.Second):
		t.Fatal("取消未及时结束请求")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "https://example.com", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	redirectClient, _ := New(redirect.URL)
	defer redirectClient.Close()
	if _, err := redirectClient.Models(context.Background()); err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("不应跟随重定向: %v", err)
	}
}

func TestValidateMessage(t *testing.T) {
	for _, invalid := range []string{
		"", "```feat: 添加功能```", "feat: 添加功能\n并声称测试通过", "随便写一个摘要",
		"feat: " + strings.Repeat("长", 51), "fix: 修复功能。",
		"feat(" + strings.Repeat("scope", 150) + "): 添加功能",
	} {
		if _, err := validateMessage(invalid, Conventional); err == nil {
			t.Fatalf("接受了无效生成结果: %q", invalid)
		}
	}
	if _, err := validateMessage("添加提交说明生成", Plain); err != nil {
		t.Fatal(err)
	}
}
