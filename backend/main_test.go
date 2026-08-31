package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLoadConfigRequiresMySQLDSNWhenDependenciesAreRequired(t *testing.T) {
	t.Setenv("HTTP_PORT", "8080")
	t.Setenv("MYSQL_DSN", "")
	t.Setenv("REQUIRE_DEPS", "true")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected missing MYSQL_DSN to fail configuration")
	}
}

func TestLoadConfigAllowsDependenciesToBeOptional(t *testing.T) {
	t.Setenv("MYSQL_DSN", "")
	t.Setenv("REQUIRE_DEPS", "false")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.RequireDeps {
		t.Fatal("expected REQUIRE_DEPS=false")
	}
}

func TestReadyHandlerReportsNotConfiguredDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ready", readyHandler(nil, nil, true))
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
