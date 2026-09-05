package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tencentcommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tencenttms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tms/v20201229"
)

const defaultModerationTimeout = 5 * time.Second

type moderationProvider interface {
	Check(context.Context, string) (bool, error)
}

type layeredModeration struct {
	sensitiveWords []string
	provider       moderationProvider
	allowOnFailure bool
}

type mockModerationProvider struct{}

func (mockModerationProvider) Check(context.Context, string) (bool, error) { return true, nil }

type tencentTMSClient interface {
	TextModerationWithContext(context.Context, *tencenttms.TextModerationRequest) (*tencenttms.TextModerationResponse, error)
}

type tencentModerationProvider struct {
	client  tencentTMSClient
	bizType string
	timeout time.Duration
}

func newModerationClientFromEnv() (moderationClient, error) {
	words, err := loadSensitiveWords(strings.TrimSpace(os.Getenv("SENSITIVE_WORDS_FILE")), os.Getenv("SENSITIVE_WORDS"))
	if err != nil {
		return nil, err
	}

	failureMode := strings.ToLower(strings.TrimSpace(getenv("MODERATION_FAILURE_MODE", "reject")))
	if failureMode != "reject" && failureMode != "allow" {
		return nil, errors.New("MODERATION_FAILURE_MODE must be reject or allow")
	}

	var provider moderationProvider
	switch strings.ToLower(strings.TrimSpace(getenv("MODERATION_PROVIDER", "mock"))) {
	case "mock":
		provider = mockModerationProvider{}
	case "tencent":
		provider, err = newTencentModerationProviderFromEnv()
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("MODERATION_PROVIDER must be mock or tencent")
	}

	return &layeredModeration{sensitiveWords: words, provider: provider, allowOnFailure: failureMode == "allow"}, nil
}

func loadSensitiveWords(path, inline string) ([]string, error) {
	words := make([]string, 0)
	seen := make(map[string]struct{})
	add := func(word string) {
		word = strings.TrimSpace(word)
		if word == "" || strings.HasPrefix(word, "#") {
			return
		}
		if _, ok := seen[word]; !ok {
			seen[word] = struct{}{}
			words = append(words, word)
		}
	}

	if path != "" {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open sensitive words file: %w", err)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			add(scanner.Text())
		}
		closeErr := file.Close()
		if err = scanner.Err(); err != nil {
			return nil, fmt.Errorf("read sensitive words file: %w", err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close sensitive words file: %w", closeErr)
		}
	}
	for _, word := range strings.Fields(inline) {
		add(word)
	}
	return words, nil
}

func (m *layeredModeration) Check(ctx context.Context, content string) (bool, error) {
	for _, word := range m.sensitiveWords {
		if strings.Contains(content, word) {
			return false, nil
		}
	}
	allowed, err := m.provider.Check(ctx, content)
	if err != nil && m.allowOnFailure {
		return true, nil
	}
	return allowed, err
}

func newTencentModerationProviderFromEnv() (moderationProvider, error) {
	secretID := strings.TrimSpace(os.Getenv("TENCENT_TMS_SECRET_ID"))
	secretKey := strings.TrimSpace(os.Getenv("TENCENT_TMS_SECRET_KEY"))
	bizType := strings.TrimSpace(os.Getenv("TENCENT_TMS_BIZ_TYPE"))
	if secretID == "" || secretKey == "" || bizType == "" {
		return nil, errors.New("Tencent TMS credentials and business type are required when MODERATION_PROVIDER=tencent")
	}
	timeout, err := time.ParseDuration(getenv("TENCENT_TMS_TIMEOUT", defaultModerationTimeout.String()))
	if err != nil || timeout <= 0 {
		return nil, errors.New("TENCENT_TMS_TIMEOUT must be a positive duration")
	}
	region := strings.TrimSpace(getenv("TENCENT_TMS_REGION", "ap-guangzhou"))
	httpProfile := profile.NewHttpProfile()
	httpProfile.Endpoint = "tms.tencentcloudapi.com"
	httpProfile.ReqTimeout = int(timeout.Seconds())
	if httpProfile.ReqTimeout < 1 {
		httpProfile.ReqTimeout = 1
	}
	clientProfile := profile.NewClientProfile()
	clientProfile.HttpProfile = httpProfile
	client, err := tencenttms.NewClient(tencentcommon.NewCredential(secretID, secretKey), region, clientProfile)
	if err != nil {
		return nil, fmt.Errorf("create Tencent TMS client: %w", err)
	}
	return &tencentModerationProvider{client: client, bizType: bizType, timeout: timeout}, nil
}

func (p *tencentModerationProvider) Check(ctx context.Context, content string) (bool, error) {
	request := tencenttms.NewTextModerationRequest()
	request.Content = moderationStringPtr(base64.StdEncoding.EncodeToString([]byte(content)))
	request.BizType = moderationStringPtr(p.bizType)
	request.SourceLanguage = moderationStringPtr("zh")
	request.Type = moderationStringPtr("TEXT")

	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	response, err := p.client.TextModerationWithContext(requestCtx, request)
	if err != nil {
		slog.Warn("Tencent TMS request failed", "error", err)
		return false, errors.New("Tencent TMS request failed")
	}
	if response == nil || response.Response == nil || response.Response.Suggestion == nil {
		return false, errors.New("Tencent TMS returned an invalid response")
	}
	switch *response.Response.Suggestion {
	case "Pass":
		return true, nil
	case "Block", "Review":
		return false, nil
	default:
		return false, errors.New("Tencent TMS returned an unknown suggestion")
	}
}

func moderationStringPtr(value string) *string { return &value }
