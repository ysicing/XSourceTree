// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package commitai

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func CustomEndpoint(options Options) (string, error) {
	if options.Protocol != Responses && options.Protocol != Chat && options.Protocol != Claude {
		return "", &SettingsError{errors.New("请选择支持的 AI 协议")}
	}
	endpoint, err := url.Parse(strings.TrimSpace(options.Endpoint))
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery ||
		endpoint.Fragment != "" || endpoint.Opaque != "" {
		return "", &SettingsError{errors.New("请填写有效的 AI 服务 Base URL")}
	}
	endpoint.Host = strings.ToLower(endpoint.Host)
	address := net.ParseIP(endpoint.Hostname())
	local := endpoint.Hostname() == "localhost" || address != nil && address.IsLoopback()
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && local) {
		return "", &SettingsError{errors.New("远程 AI 服务必须使用 HTTPS，本机服务可使用 HTTP")}
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/")
	if endpoint.Path == "" {
		endpoint.Path = "/v1"
	}
	return endpoint.String(), nil
}

func KeyAccount(options Options) (string, error) {
	endpoint, err := CustomEndpoint(options)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(options.Protocol+"\n"+endpoint))), nil
}

func (client *Client) generateMessages(ctx context.Context, options Options, model, instructions, diff string) (string, error) {
	input := "Staged Git diff:\n" + diff
	if client.protocol == Claude {
		payload := struct {
			Model     string    `json:"model"`
			System    string    `json:"system"`
			Messages  []message `json:"messages"`
			MaxTokens int       `json:"max_tokens"`
			Stream    bool      `json:"stream"`
		}{model, instructions, []message{{"user", input}}, 2048, false}
		var response struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := client.request(ctx, http.MethodPost, "/messages", payload, &response); err != nil {
			return "", err
		}
		if response.StopReason != "end_turn" {
			return "", errors.New("模型未完成生成，请重试或切换模型")
		}
		var text strings.Builder
		for _, part := range response.Content {
			if part.Type == "text" {
				text.WriteString(part.Text)
			}
		}
		return validateMessage(text.String(), options.Format)
	}
	payload := struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
		Stream   bool      `json:"stream"`
	}{model, []message{{"system", instructions}, {"user", input}}, false}
	var response struct {
		Choices []struct {
			Message      message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := client.request(ctx, http.MethodPost, "/chat/completions", payload, &response); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 || response.Choices[0].FinishReason != "stop" || response.Choices[0].Message.Role != "assistant" {
		return "", errors.New("模型未完成生成，请重试或切换模型")
	}
	return validateMessage(response.Choices[0].Message.Content, options.Format)
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
