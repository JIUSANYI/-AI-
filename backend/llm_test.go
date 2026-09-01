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
		_, _ = w.Write([]byte(`{"model":"server-model","choices":[{"message":{"role":"assistant","content":"answer"}}]}`))
	}))
	defer server.Close()
	client := &openAIClient{baseURL: server.URL, apiKey: "secret", model: "test-model", http: server.Client()}
	answer, model, err := client.Answer(context.Background(), "question")
	if err != nil || answer != "answer" || model != "server-model" {
		t.Fatalf("answer = %q, model = %q, err = %v", answer, model, err)
	}
}

func TestOpenAIClientRejectsUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream detail", http.StatusBadGateway)
	}))
	defer server.Close()
	client := &openAIClient{baseURL: server.URL, apiKey: "secret", model: "test-model", http: server.Client()}
	if _, _, err := client.Answer(context.Background(), "question"); err == nil {
		t.Fatal("expected upstream error")
	}
}
