package main

import (
	"context"
	"errors"
	"testing"

	tencentsms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

type fakeTencentSMSClient struct {
	request  *tencentsms.SendSmsRequest
	response *tencentsms.SendSmsResponse
	err      error
}

func (f *fakeTencentSMSClient) SendSmsWithContext(_ context.Context, request *tencentsms.SendSmsRequest) (*tencentsms.SendSmsResponse, error) {
	f.request = request
	return f.response, f.err
}

func TestNewSMSSenderFromEnvMock(t *testing.T) {
	t.Setenv("SMS_PROVIDER", "mock")
	t.Setenv("SMS_MOCK_CODE", "123456")
	sender, code, err := newSMSSenderFromEnv()
	if err != nil || sender == nil || code != "123456" {
		t.Fatalf("sender = %#v, code = %q, err = %v", sender, code, err)
	}
}

func TestNewSMSSenderFromEnvRejectsMissingTencentConfiguration(t *testing.T) {
	t.Setenv("SMS_PROVIDER", "tencent")
	for _, key := range []string{"TENCENT_SMS_SECRET_ID", "TENCENT_SMS_SECRET_KEY", "TENCENT_SMS_SDKAPPID", "TENCENT_SMS_SIGN", "TENCENT_SMS_TEMPLATE_ID"} {
		t.Setenv(key, "")
	}
	if _, _, err := newSMSSenderFromEnv(); err == nil {
		t.Fatal("missing Tencent SMS configuration should fail")
	}
}

func TestNewSMSSenderFromEnvRejectsUnknownTemplateParameters(t *testing.T) {
	t.Setenv("SMS_PROVIDER", "tencent")
	for key, value := range map[string]string{
		"TENCENT_SMS_SECRET_ID":       "secret-id",
		"TENCENT_SMS_SECRET_KEY":      "secret-key",
		"TENCENT_SMS_SDKAPPID":        "1400000000",
		"TENCENT_SMS_SIGN":            "平台",
		"TENCENT_SMS_TEMPLATE_ID":     "1000",
		"TENCENT_SMS_TEMPLATE_PARAMS": "custom",
	} {
		t.Setenv(key, value)
	}
	if _, _, err := newSMSSenderFromEnv(); err == nil {
		t.Fatal("unknown template parameters should fail")
	}
}

func TestTencentSMSSenderBuildsRequest(t *testing.T) {
	client := &fakeTencentSMSClient{response: successfulTencentSMSResponse()}
	sender := &tencentSMSSender{client: client, sdkAppID: "1400000000", sign: "平台", templateID: "1000", includeTTL: true}
	if err := sender.Send(context.Background(), "13800138000", "123456"); err != nil {
		t.Fatal(err)
	}
	request := client.request
	if request == nil || len(request.PhoneNumberSet) != 1 || *request.PhoneNumberSet[0] != "+8613800138000" {
		t.Fatalf("phone numbers = %#v", request)
	}
	if request.SmsSdkAppId == nil || *request.SmsSdkAppId != "1400000000" || request.SignName == nil || *request.SignName != "平台" || request.TemplateId == nil || *request.TemplateId != "1000" {
		t.Fatalf("request configuration = %#v", request)
	}
	if len(request.TemplateParamSet) != 2 || *request.TemplateParamSet[0] != "123456" || *request.TemplateParamSet[1] != "10" {
		t.Fatalf("template parameters = %#v", request.TemplateParamSet)
	}
}

func TestTencentSMSSenderSupportsCodeOnlyTemplate(t *testing.T) {
	client := &fakeTencentSMSClient{response: successfulTencentSMSResponse()}
	sender := &tencentSMSSender{client: client}
	if err := sender.Send(context.Background(), "13800138000", "123456"); err != nil {
		t.Fatal(err)
	}
	if len(client.request.TemplateParamSet) != 1 || *client.request.TemplateParamSet[0] != "123456" {
		t.Fatalf("template parameters = %#v", client.request.TemplateParamSet)
	}
}

func TestTencentSMSSenderClassifiesFailures(t *testing.T) {
	statusCode := "FailedOperation"
	client := &fakeTencentSMSClient{response: &tencentsms.SendSmsResponse{Response: &tencentsms.SendSmsResponseParams{SendStatusSet: []*tencentsms.SendStatus{{Code: &statusCode}}}}}
	sender := &tencentSMSSender{client: client}
	err := sender.Send(context.Background(), "13800138000", "123456")
	if err == nil || !isDefinitiveSMSSendFailure(err) {
		t.Fatalf("explicit rejection error = %v", err)
	}
	client.response, client.err = nil, errors.New("timeout")
	err = sender.Send(context.Background(), "13800138000", "123456")
	if err == nil || isDefinitiveSMSSendFailure(err) {
		t.Fatalf("transport error = %v", err)
	}
}

func successfulTencentSMSResponse() *tencentsms.SendSmsResponse {
	statusCode := "Ok"
	return &tencentsms.SendSmsResponse{Response: &tencentsms.SendSmsResponseParams{SendStatusSet: []*tencentsms.SendStatus{{Code: &statusCode}}}}
}
