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
	t.Setenv("APP_ENV", "dev")
	t.Setenv("CORS_ORIGINS", "")
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

func TestLoadConfigRequiresValidProductionCORSOrigins(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("MYSQL_DSN", "")
	t.Setenv("REQUIRE_DEPS", "false")
	for _, origins := range []string{"", "*", "example.com", "https://example.com/path"} {
		t.Run(origins, func(t *testing.T) {
			t.Setenv("CORS_ORIGINS", origins)
			if _, err := loadConfig(); err == nil {
				t.Fatalf("CORS_ORIGINS=%q should fail", origins)
			}
		})
	}
	t.Setenv("CORS_ORIGINS", "https://example.com")
	if cfg, err := loadConfig(); err != nil || len(cfg.CORSOrigins) != 1 {
		t.Fatalf("valid production config = %#v, err = %v", cfg, err)
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

func TestCORSMiddlewareHandlesOriginsAndPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestID(), corsMiddleware([]string{"https://example.com"}))
	r.GET("/resource", func(c *gin.Context) { c.Status(http.StatusOK) })

	preflight := httptest.NewRequest(http.MethodOptions, "/resource", nil)
	preflight.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, preflight)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" || rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("preflight status = %d, headers = %#v", rec.Code, rec.Header())
	}

	forbidden := httptest.NewRequest(http.MethodGet, "/resource", nil)
	forbidden.Header.Set("Origin", "https://attacker.example")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, forbidden)
	if rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("forbidden status = %d, headers = %#v", rec.Code, rec.Header())
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Fatalf("forbidden Vary header = %q", rec.Header().Get("Vary"))
	}

	sameOrigin := httptest.NewRequest(http.MethodGet, "/resource", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, sameOrigin)
	if rec.Code != http.StatusOK {
		t.Fatalf("request without Origin status = %d", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(securityHeaders())
	r.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("security headers = %#v", rec.Header())
	}
}
