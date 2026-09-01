package main

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tencenttms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tms/v20201229"
)

type fakeModerationProvider struct {
	allowed bool
	err     error
	calls   int
}

func (f *fakeModerationProvider) Check(context.Context, string) (bool, error) {
	f.calls++
	return f.allowed, f.err
}

type fakeTencentTMSClient struct {
	request  *tencenttms.TextModerationRequest
	response *tencenttms.TextModerationResponse
	err      error
}

func (f *fakeTencentTMSClient) TextModerationWithContext(_ context.Context, request *tencenttms.TextModerationRequest) (*tencenttms.TextModerationResponse, error) {
	f.request = request
	return f.response, f.err
}

func TestLoadSensitiveWordsFromFileAndInline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte("# comment\nblocked\nblocked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	words, err := loadSensitiveWords(path, "inline")
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != 2 || words[0] != "blocked" || words[1] != "inline" {
		t.Fatalf("words = %#v", words)
	}
}

func TestLoadSensitiveWordsRejectsMissingConfiguredFile(t *testing.T) {
	if _, err := loadSensitiveWords(filepath.Join(t.TempDir(), "missing.txt"), ""); err == nil {
		t.Fatal("missing configured file should fail")
	}
}

func TestLayeredModerationRejectsSensitiveWordBeforeProvider(t *testing.T) {
	provider := &fakeModerationProvider{allowed: true}
	moderation := &layeredModeration{sensitiveWords: []string{"blocked"}, provider: provider}
	allowed, err := moderation.Check(context.Background(), "contains blocked content")
	if err != nil || allowed || provider.calls != 0 {
		t.Fatalf("allowed = %v, calls = %d, err = %v", allowed, provider.calls, err)
	}
}

func TestLayeredModerationFailureModes(t *testing.T) {
	provider := &fakeModerationProvider{err: errors.New("upstream failed")}
	moderation := &layeredModeration{provider: provider}
	if allowed, err := moderation.Check(context.Background(), "content"); err == nil || allowed {
		t.Fatalf("reject mode: allowed = %v, err = %v", allowed, err)
	}
	moderation.allowOnFailure = true
	if allowed, err := moderation.Check(context.Background(), "content"); err != nil || !allowed {
		t.Fatalf("allow mode: allowed = %v, err = %v", allowed, err)
	}
	moderation.sensitiveWords = []string{"blocked"}
	if allowed, err := moderation.Check(context.Background(), "blocked content"); err != nil || allowed {
		t.Fatalf("local rejection in allow mode: allowed = %v, err = %v", allowed, err)
	}
}

func TestNewModerationClientRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("SENSITIVE_WORDS_FILE", "")
	t.Setenv("MODERATION_PROVIDER", "unknown")
	if _, err := newModerationClientFromEnv(); err == nil {
		t.Fatal("unknown provider should fail")
	}
	t.Setenv("MODERATION_PROVIDER", "mock")
	t.Setenv("MODERATION_FAILURE_MODE", "unknown")
	if _, err := newModerationClientFromEnv(); err == nil {
		t.Fatal("unknown failure mode should fail")
	}
}

func TestNewTencentModerationProviderRequiresConfiguration(t *testing.T) {
	for _, key := range []string{"TENCENT_TMS_SECRET_ID", "TENCENT_TMS_SECRET_KEY", "TENCENT_TMS_BIZ_TYPE"} {
		t.Setenv(key, "")
	}
	if _, err := newTencentModerationProviderFromEnv(); err == nil {
		t.Fatal("missing Tencent TMS configuration should fail")
	}
}

func TestTencentModerationProviderBuildsRequest(t *testing.T) {
	client := &fakeTencentTMSClient{response: tencentModerationResponse("Pass")}
	provider := &tencentModerationProvider{client: client, bizType: "default", timeout: time.Second}
	allowed, err := provider.Check(context.Background(), "测试内容")
	if err != nil || !allowed {
		t.Fatalf("allowed = %v, err = %v", allowed, err)
	}
	request := client.request
	wantContent := base64.StdEncoding.EncodeToString([]byte("测试内容"))
	if request == nil || request.Content == nil || *request.Content != wantContent {
		t.Fatalf("content = %#v", request)
	}
	if request.BizType == nil || *request.BizType != "default" || request.SourceLanguage == nil || *request.SourceLanguage != "zh" || request.Type == nil || *request.Type != "TEXT" {
		t.Fatalf("request configuration = %#v", request)
	}
}

func TestTencentModerationProviderRejectsBlockAndReview(t *testing.T) {
	for _, suggestion := range []string{"Block", "Review"} {
		t.Run(suggestion, func(t *testing.T) {
			client := &fakeTencentTMSClient{response: tencentModerationResponse(suggestion)}
			provider := &tencentModerationProvider{client: client, timeout: time.Second}
			if allowed, err := provider.Check(context.Background(), "content"); err != nil || allowed {
				t.Fatalf("allowed = %v, err = %v", allowed, err)
			}
		})
	}
}

func TestTencentModerationProviderSanitizesFailures(t *testing.T) {
	client := &fakeTencentTMSClient{err: errors.New("vendor secret response")}
	provider := &tencentModerationProvider{client: client, timeout: time.Second}
	_, err := provider.Check(context.Background(), "content")
	if err == nil || err.Error() != "Tencent TMS request failed" {
		t.Fatalf("error = %v", err)
	}
	client.err = nil
	client.response = tencentModerationResponse("Unknown")
	if _, err = provider.Check(context.Background(), "content"); err == nil {
		t.Fatal("unknown suggestion should fail closed")
	}
}

func tencentModerationResponse(suggestion string) *tencenttms.TextModerationResponse {
	return &tencenttms.TextModerationResponse{Response: &tencenttms.TextModerationResponseParams{Suggestion: &suggestion}}
}
