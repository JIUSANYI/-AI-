package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

type fakeQuestionRateLimiter struct {
	allowed bool
	err     error
	calls   int
}

type blockingThumbnailMirror struct {
	started  chan struct{}
	canceled chan struct{}
}

func (m *blockingThumbnailMirror) Mirror(ctx context.Context, _ string) (string, error) {
	close(m.started)
	<-ctx.Done()
	close(m.canceled)
	return "", ctx.Err()
}

func TestThumbnailMirrorsAreCanceledDuringShutdown(t *testing.T) {
	thumbnailCtx, thumbnailCancel := context.WithCancel(context.Background())
	mirror := &blockingThumbnailMirror{started: make(chan struct{}), canceled: make(chan struct{})}
	service := &questionService{thumbnailMirror: mirror, thumbnailCtx: thumbnailCtx, thumbnailCancel: thumbnailCancel}
	service.scheduleThumbnailMirrors([]thumbnailTask{{cardID: 1, sourceURL: "https://example.com/image.png"}})
	<-mirror.started

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	service.shutdownThumbnailMirrors(shutdownCtx)
	select {
	case <-mirror.canceled:
	case <-time.After(time.Second):
		t.Fatal("thumbnail mirror was not canceled")
	}
}

func (f *fakeQuestionRateLimiter) Allow(context.Context, int64, string) (bool, error) {
	f.calls++
	return f.allowed, f.err
}

func TestAllowQuestionRejectsRateLimitedRequests(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/questions", nil)
	limiter := &fakeQuestionRateLimiter{}
	service := &questionService{rateLimiter: limiter}

	if service.allowQuestion(ctx, 1) {
		t.Fatal("expected rate-limited request to be rejected")
	}
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	if recorder.Header().Get("Retry-After") != "3600" {
		t.Fatalf("Retry-After = %q, want 3600", recorder.Header().Get("Retry-After"))
	}
	if limiter.calls != 1 {
		t.Fatalf("limiter calls = %d, want 1", limiter.calls)
	}
}

func TestAllowQuestionFailsClosedWhenRateLimiterUnavailable(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/questions", nil)
	service := &questionService{rateLimiter: &fakeQuestionRateLimiter{err: errors.New("redis unavailable")}}

	if service.allowQuestion(ctx, 1) {
		t.Fatal("expected unavailable limiter to reject request")
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestRedisQuestionRateLimiterAllowsWithoutRedisClient(t *testing.T) {
	allowed, err := (redisQuestionRateLimiter{}).Allow(context.Background(), 1, "127.0.0.1")
	if err != nil || !allowed {
		t.Fatalf("Allow() = (%v, %v), want (true, nil)", allowed, err)
	}
}

func testContext(req *http.Request) *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req
	return ctx
}
