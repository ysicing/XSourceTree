// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package commitai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCustomProviderContracts(t *testing.T) {
	for _, protocol := range []string{Responses, Chat, Claude} {
		t.Run(protocol, func(t *testing.T) {
			t.Setenv("MAGPIE_API_KEY", "must-not-leak")
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests++
				if request.Method != http.MethodPost {
					t.Error("手动模型不应依赖模型列表接口")
				}
				if protocol == Claude {
					if request.URL.Path != "/v1/messages" || request.Header.Get("x-api-key") != "synthetic-key" ||
						request.Header.Get("anthropic-version") != "2023-06-01" || request.Header.Get("Authorization") != "" {
						t.Error("Claude 请求路径或认证错误")
					}
				} else if request.Header.Get("Authorization") != "Bearer synthetic-key" || request.Header.Get("x-api-key") != "" {
					t.Error("OpenAI 认证错误")
				}
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if string(payload["model"]) != `"test-model"` {
					t.Error("未使用手动填写的模型")
				}
				if protocol == Responses {
					if request.URL.Path != "/v1/responses" || string(payload["store"]) != "false" ||
						!strings.Contains(string(payload["input"]), "+synthetic diff") {
						t.Error("Responses 契约错误")
					}
					_, _ = writer.Write([]byte(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"feat(ai): 支持自定义服务"}]}]}`))
				} else if protocol == Chat {
					if request.URL.Path != "/v1/chat/completions" || !strings.Contains(string(payload["messages"]), "untrusted") {
						t.Error("Chat 契约错误")
					}
					_, _ = writer.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"feat(ai): 支持自定义服务"}}]}`))
				} else {
					if string(payload["max_tokens"]) != "2048" || !strings.Contains(string(payload["system"]), "untrusted") {
						t.Error("Claude 契约错误")
					}
					_, _ = writer.Write([]byte(`{"stop_reason":"end_turn","content":[{"type":"thinking","text":"不应显示"},{"type":"text","text":"feat(ai): 支持自定义服务"}]}`))
				}
			}))
			defer server.Close()
			options := Options{Protocol: protocol, Endpoint: server.URL, APIKey: "synthetic-key", Model: "test-model"}
			client, err := NewWithOptions(options)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			result, err := client.Generate(context.Background(), options, "+synthetic diff")
			if err != nil || result != "feat(ai): 支持自定义服务" || requests != 1 {
				t.Fatalf("协议生成失败: %q %v", result, err)
			}
			data, err := json.Marshal(options)
			if err != nil || strings.Contains(string(data), "synthetic-key") || strings.Contains(string(data), "apiKey") {
				t.Fatal("密钥不能持久化到普通配置")
			}
		})
	}
}

func TestCustomEndpointAndKeyIsolation(t *testing.T) {
	base := Options{Protocol: Responses, Endpoint: "https://EXAMPLE.com/"}
	account, err := KeyAccount(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Endpoint = "https://example.com/v1"
	equivalent, _ := KeyAccount(base)
	if account != equivalent {
		t.Fatal("同一规范化地址应复用密钥")
	}
	base.Protocol = Claude
	other, _ := KeyAccount(base)
	if account == other {
		t.Fatal("协议之间不能复用密钥")
	}
	base.Protocol, base.Endpoint = Responses, "https://another.example/v1"
	other, _ = KeyAccount(base)
	if account == other {
		t.Fatal("地址之间不能复用密钥")
	}
	for _, address := range []string{"http://example.com/v1", "https://user@example.com/v1", "https://example.com?key=secret", "https://example.com?", "file:///tmp/key", ""} {
		if _, err := CustomEndpoint(Options{Protocol: Responses, Endpoint: address}); err == nil {
			t.Fatalf("接受了不安全地址 %s", address)
		}
	}
}

func TestCustomServiceDoesNotUseMagpieKeyOrFollowRedirects(t *testing.T) {
	t.Setenv("MAGPIE_API_KEY", "must-not-leak")
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("不应跟随重定向发送密钥") }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" {
			t.Error("自定义免认证服务不应收到 Magpie 密钥")
		}
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := NewWithOptions(Options{Protocol: Responses, Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Models(context.Background()); err == nil {
		t.Fatal("重定向应明确失败")
	}
}

func TestCustomProviderRejectsIncompleteAndInvalidOutput(t *testing.T) {
	for _, scenario := range []struct {
		name, protocol, response string
	}{
		{"chat-incomplete", Chat, `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"feat: 不完整"}}]}`},
		{"chat-empty", Chat, `{"choices":[]}`},
		{"chat-wrong-role", Chat, `{"choices":[{"finish_reason":"stop","message":{"role":"user","content":"feat: 错误角色"}}]}`},
		{"claude-incomplete", Claude, `{"stop_reason":"max_tokens","content":[{"type":"text","text":"feat: 不完整"}]}`},
		{"claude-refusal", Claude, `{"stop_reason":"refusal","content":[{"type":"text","text":"无法生成"}]}`},
		{"claude-no-text", Claude, `{"stop_reason":"end_turn","content":[{"type":"thinking","text":"不应采用"}]}`},
		{"invalid-json", Chat, `not-json`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(scenario.response))
			}))
			defer server.Close()
			options := Options{Protocol: scenario.protocol, Endpoint: server.URL, Model: "test-model"}
			client, err := NewWithOptions(options)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if _, err := client.Generate(context.Background(), options, "+diff"); err == nil {
				t.Fatal("不完整或无效的生成结果不能填入提交说明")
			}
		})
	}
}

func TestCustomProviderAuthErrorDoesNotExposeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":"synthetic-key-and-private-body"}`))
	}))
	defer server.Close()
	options := Options{Protocol: Claude, Endpoint: server.URL, Model: "test-model"}
	client, err := NewWithOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.Generate(context.Background(), options, "+diff")
	var settings *SettingsError
	if !errors.As(err, &settings) || strings.Contains(err.Error(), "synthetic-key-and-private-body") {
		t.Fatal("认证失败应可前往设置修复，且不能暴露响应中的敏感内容")
	}
}
