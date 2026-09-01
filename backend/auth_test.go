package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func TestPhonePattern(t *testing.T) {
	for _, phone := range []string{"13800138000", "19912345678"} {
		if !phonePattern.MatchString(phone) {
			t.Errorf("phone %q should be accepted", phone)
		}
	}
	for _, phone := range []string{"1380013800", "12800138000", "13800138000x"} {
		if phonePattern.MatchString(phone) {
			t.Errorf("phone %q should be rejected", phone)
		}
	}
}

func TestValidCSRFOriginPolicy(t *testing.T) {
	service := &authService{allowedOrigin: map[string]struct{}{"https://example.com": {}}}
	for _, tc := range []struct {
		name   string
		origin string
		want   bool
	}{
		{name: "same origin request", want: true},
		{name: "allowed origin", origin: "https://example.com", want: true},
		{name: "forbidden origin", origin: "https://attacker.example", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testContext(httptest.NewRequest(http.MethodPost, "/auth/refresh", nil))
			ctx.Request.Header.Set("X-CSRF-Protection", "1")
			if tc.origin != "" {
				ctx.Request.Header.Set("Origin", tc.origin)
			}
			if got := service.validCSRF(ctx); got != tc.want {
				t.Fatalf("validCSRF() = %v, want %v", got, tc.want)
			}
		})
	}

	emptyPolicy := &authService{allowedOrigin: map[string]struct{}{}}
	ctx := testContext(httptest.NewRequest(http.MethodPost, "/auth/refresh", nil))
	ctx.Request.Header.Set("X-CSRF-Protection", "1")
	ctx.Request.Header.Set("Origin", "https://example.com")
	if emptyPolicy.validCSRF(ctx) {
		t.Fatal("explicit Origin should fail when the allowlist is empty")
	}
}

func TestSplitSQLStatements(t *testing.T) {
	statements := splitSQLStatements("CREATE TABLE a (id INT);\n\nCREATE TABLE b (id INT);\n")
	if len(statements) != 2 {
		t.Fatalf("got %d statements, want 2", len(statements))
	}
}

func TestHashTokenIsDeterministicAndNonEmpty(t *testing.T) {
	first := hashToken("token")
	if first == "" || first != hashToken("token") || first == hashToken("other") {
		t.Fatal("token hash is not deterministic")
	}
}

func TestSecondsUntilNextDay(t *testing.T) {
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 9, 1, 23, 59, 30, 0, zone)
	if got := secondsUntilNextDay(now); got != 30 {
		t.Fatalf("secondsUntilNextDay() = %d, want 30", got)
	}
}

func TestSplitCommaSeparated(t *testing.T) {
	got := splitCommaSeparated("127.0.0.1, 172.16.0.0/12, ")
	if len(got) != 2 || got[0] != "127.0.0.1" || got[1] != "172.16.0.0/12" {
		t.Fatalf("splitCommaSeparated() = %#v", got)
	}
}

func TestRequireAccessToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &authService{jwtSecret: []byte("01234567890123456789012345678901"), accessTTL: time.Minute}
	r := gin.New()
	r.Use(service.requireAccessToken())
	r.GET("/private", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	valid, err := service.issueAccessToken(42)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, header string
		want         int
	}{
		{name: "missing", want: http.StatusUnauthorized},
		{name: "malformed", header: "Bearer invalid", want: http.StatusUnauthorized},
		{name: "valid", header: "Bearer " + valid, want: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/private", nil)
			req.Header.Set("Authorization", tt.header)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestRequireAccessTokenRejectsWrongSigningMethod(t *testing.T) {
	service := &authService{jwtSecret: []byte("01234567890123456789012345678901")}
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{Subject: "42", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))})
	unsigned, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(service.requireAccessToken())
	r.GET("/private", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+unsigned)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
