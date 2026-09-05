package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tencentcommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tencentsms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

type smsSender interface {
	Send(context.Context, string, string) error
}

type mockSMSSender struct{}

func (mockSMSSender) Send(context.Context, string, string) error { return nil }

type tencentSMSClient interface {
	SendSmsWithContext(context.Context, *tencentsms.SendSmsRequest) (*tencentsms.SendSmsResponse, error)
}

type tencentSMSSender struct {
	client     tencentSMSClient
	sdkAppID   string
	sign       string
	templateID string
	includeTTL bool
}

type definitiveSMSSendError struct{ message string }

func (e definitiveSMSSendError) Error() string { return e.message }

func isDefinitiveSMSSendFailure(err error) bool {
	var target definitiveSMSSendError
	return errors.As(err, &target)
}

func newSMSSenderFromEnv() (smsSender, string, error) {
	switch strings.ToLower(strings.TrimSpace(getenv("SMS_PROVIDER", "mock"))) {
	case "mock":
		mockCode := strings.TrimSpace(os.Getenv("SMS_MOCK_CODE"))
		if mockCode != "" && !codePattern.MatchString(mockCode) {
			return nil, "", errors.New("SMS_MOCK_CODE must be a 6-digit code when set")
		}
		return mockSMSSender{}, mockCode, nil
	case "tencent":
		sender, err := newTencentSMSSenderFromEnv()
		return sender, "", err
	default:
		return nil, "", errors.New("SMS_PROVIDER must be mock or tencent")
	}
}

func newTencentSMSSenderFromEnv() (smsSender, error) {
	secretID := strings.TrimSpace(os.Getenv("TENCENT_SMS_SECRET_ID"))
	secretKey := strings.TrimSpace(os.Getenv("TENCENT_SMS_SECRET_KEY"))
	sdkAppID := strings.TrimSpace(os.Getenv("TENCENT_SMS_SDKAPPID"))
	sign := strings.TrimSpace(os.Getenv("TENCENT_SMS_SIGN"))
	templateID := strings.TrimSpace(os.Getenv("TENCENT_SMS_TEMPLATE_ID"))
	if secretID == "" || secretKey == "" || sdkAppID == "" || sign == "" || templateID == "" {
		return nil, errors.New("Tencent SMS credentials, app ID, sign and template ID are required when SMS_PROVIDER=tencent")
	}
	region := strings.TrimSpace(getenv("TENCENT_SMS_REGION", "ap-guangzhou"))
	templateParams := strings.ToLower(strings.TrimSpace(getenv("TENCENT_SMS_TEMPLATE_PARAMS", "code,ttl")))
	if templateParams != "code" && templateParams != "code,ttl" {
		return nil, errors.New("TENCENT_SMS_TEMPLATE_PARAMS must be code or code,ttl")
	}
	httpProfile := profile.NewHttpProfile()
	httpProfile.Endpoint = "sms.tencentcloudapi.com"
	httpProfile.ReqTimeout = 5
	clientProfile := profile.NewClientProfile()
	clientProfile.HttpProfile = httpProfile
	client, err := tencentsms.NewClient(tencentcommon.NewCredential(secretID, secretKey), region, clientProfile)
	if err != nil {
		return nil, fmt.Errorf("create Tencent SMS client: %w", err)
	}
	return &tencentSMSSender{client: client, sdkAppID: sdkAppID, sign: sign, templateID: templateID, includeTTL: templateParams == "code,ttl"}, nil
}

func (s *tencentSMSSender) Send(ctx context.Context, phone, code string) error {
	request := tencentsms.NewSendSmsRequest()
	request.PhoneNumberSet = []*string{smsStringPtr("+86" + phone)}
	request.SmsSdkAppId = smsStringPtr(s.sdkAppID)
	request.SignName = smsStringPtr(s.sign)
	request.TemplateId = smsStringPtr(s.templateID)
	request.TemplateParamSet = []*string{smsStringPtr(code)}
	if s.includeTTL {
		request.TemplateParamSet = append(request.TemplateParamSet, smsStringPtr("10"))
	}
	response, err := s.client.SendSmsWithContext(ctx, request)
	if err != nil {
		return fmt.Errorf("Tencent SMS request failed: %w", err)
	}
	if response == nil || response.Response == nil || len(response.Response.SendStatusSet) != 1 || response.Response.SendStatusSet[0] == nil || response.Response.SendStatusSet[0].Code == nil {
		return errors.New("Tencent SMS returned an invalid response")
	}
	if *response.Response.SendStatusSet[0].Code != "Ok" {
		return definitiveSMSSendError{message: "Tencent SMS rejected the message"}
	}
	return nil
}

func smsStringPtr(value string) *string { return &value }
