package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIClientAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "test-model" || len(request.Messages) != 1 {
			t.Fatalf("unexpected request body: %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"server-model","usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20},"choices":[{"message":{"role":"assistant","content":"answer"}}]}`))
	}))
	defer server.Close()
	client := &openAIClient{baseURL: server.URL, apiKey: "secret", model: "test-model", http: server.Client()}
	answer, model, tokens, err := client.Answer(context.Background(), "question")
	if err != nil || answer != "answer" || model != "server-model" || tokens != 20 {
		t.Fatalf("answer = %q, model = %q, tokens = %d, err = %v", answer, model, tokens, err)
	}
}

func TestOpenAIClientRejectsUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream detail", http.StatusBadGateway)
	}))
	defer server.Close()
	client := &openAIClient{baseURL: server.URL, apiKey: "secret", model: "test-model", http: server.Client()}
	if _, _, _, err := client.Answer(context.Background(), "question"); err == nil {
		t.Fatal("expected upstream error")
	}
}

func TestOpenAIClientFallsBackToPromptAndCompletionTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`))
	}))
	defer server.Close()
	client := &openAIClient{baseURL: server.URL, apiKey: "secret", model: "test-model", http: server.Client()}
	_, _, tokens, err := client.Answer(context.Background(), "question")
	if err != nil || tokens != 7 {
		t.Fatalf("tokens = %d, err = %v, want 7 and nil", tokens, err)
	}
}
