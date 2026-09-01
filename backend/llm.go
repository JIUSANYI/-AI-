package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxLLMResponseBytes = 2 << 20

type openAIClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func newOpenAIClient() (*openAIClient, error) {
	baseURL := strings.TrimRight(getenv("LLM_BASE_URL", ""), "/")
	apiKey := getenv("LLM_API_KEY", "")
	model := getenv("LLM_MODEL", "")
	if baseURL == "" || apiKey == "" || model == "" {
		return nil, errors.New("LLM_BASE_URL, LLM_API_KEY and LLM_MODEL are required")
	}
	timeout, err := time.ParseDuration(getenv("LLM_TIMEOUT", "120s"))
	if err != nil || timeout <= 0 {
		return nil, errors.New("LLM_TIMEOUT must be a positive duration")
	}
	return &openAIClient{baseURL: baseURL, apiKey: apiKey, model: model, http: &http.Client{Timeout: timeout}}, nil
}

func (c *openAIClient) Answer(ctx context.Context, content string) (string, string, error) {
	body, err := json.Marshal(chatCompletionRequest{Model: c.model, Messages: []chatMessage{{Role: "user", Content: content}}})
	if err != nil {
		return "", "", fmt.Errorf("encode llm request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("create llm request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("call llm: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxLLMResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return "", "", fmt.Errorf("read llm response: %w", err)
	}
	if len(responseBody) > maxLLMResponseBytes {
		return "", "", errors.New("llm response too large")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", "", fmt.Errorf("llm returned status %d", resp.StatusCode)
	}
	var parsed chatCompletionResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return "", "", fmt.Errorf("decode llm response: %w", err)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", "", errors.New("llm response contained no answer")
	}
	model := parsed.Model
	if model == "" {
		model = c.model
	}
	return parsed.Choices[0].Message.Content, model, nil
}
