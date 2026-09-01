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

func testContext(req *http.Request) *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req
	return ctx
}
