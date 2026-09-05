package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPageParamsDefaultsAndBounds(t *testing.T) {
	c := httptest.NewRequest("GET", "/questions", nil)
	ctx := testContext(c)
	page, size, err := pageParams(ctx)
	if err != nil || page != 1 || size != 20 {
		t.Fatalf("defaults = (%d, %d, %v), want (1, 20, nil)", page, size, err)
	}

	for _, query := range []string{"?page=0", "?page=-1", "?size=0", "?size=21", "?size=bad"} {
		ctx := testContext(httptest.NewRequest("GET", "/questions"+query, nil))
		if _, _, err := pageParams(ctx); err == nil {
			t.Errorf("query %s should be rejected", query)
		}
	}
}

func TestRedactedQuestionContent(t *testing.T) {
	if got := visibleQuestionContent("secret", "rejected"); got != "……" {
		t.Fatalf("rejected content = %q", got)
	}
	if got := visibleQuestionContent("normal", "answered"); got != "normal" {
		t.Fatalf("answered content = %q", got)
	}
}

func TestQuestionListAndDetailHandleUnavailableDB(t *testing.T) {
	for _, name := range []string{"list", "detail"} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/questions/1", nil)
			ctx.Set(userIDContextKey, int64(1))
			service := &questionService{}
			if name == "list" {
				service.list(ctx)
			} else {
				service.detail(ctx)
			}
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
			}
		})
	}
}

func TestQuestionRetryHandlesUnavailableDB(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/questions/1/retry", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}
	ctx.Set(userIDContextKey, int64(1))
	(&questionService{}).retry(ctx)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func testContext(req *http.Request) *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req
	return ctx
}
