package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

const refreshCookieName = "refresh_token"
const userIDContextKey = "user_id"

var phonePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)
var codePattern = regexp.MustCompile(`^\d{6}$`)

type authService struct {
	db            *sql.DB
	rdb           *redis.Client
	jwtSecret     []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
	cookieSecure  bool
	allowedOrigin map[string]struct{}
	smsSender     smsSender
	mockCode      string
}

type loginRequest struct {
	Phone string `json:"phone" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

type smsCodeRequest struct {
	Phone   string `json:"phone" binding:"required"`
	Purpose string `json:"purpose"`
}

func newAuthService(cfg config, db *sql.DB, rdb *redis.Client) (*authService, error) {
	secret := getenv("JWT_SECRET", "")
	if len(secret) < 32 {
		return nil, errors.New("JWT_SECRET must contain at least 32 characters")
	}
	accessTTL, err := time.ParseDuration(getenv("JWT_ACCESS_TTL", "30m"))
	if err != nil || accessTTL <= 0 {
		return nil, errors.New("JWT_ACCESS_TTL must be a positive duration")
	}
	refreshTTL, err := time.ParseDuration(getenv("JWT_REFRESH_TTL", "720h"))
	if err != nil || refreshTTL <= 0 {
		return nil, errors.New("JWT_REFRESH_TTL must be a positive duration")
	}
	origins := make(map[string]struct{})
	for _, origin := range strings.Split(os.Getenv("CORS_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins[origin] = struct{}{}
		}
	}
	sender, mockCode, err := newSMSSenderFromEnv()
	if err != nil {
		return nil, err
	}
	return &authService{db: db, rdb: rdb, jwtSecret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL, cookieSecure: cfg.AppEnv == "prod", allowedOrigin: origins, smsSender: sender, mockCode: mockCode}, nil
}

func (s *authService) registerRoutes(api *gin.RouterGroup) {
	auth := api.Group("/auth")
	auth.POST("/sms-code", s.sendSMSCode)
	auth.POST("/login", s.login)
	auth.POST("/refresh", s.refresh)
	auth.POST("/logout", s.logout)
}

func (s *authService) requireAccessToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
			c.Abort()
			return
		}
		token, err := jwt.ParseWithClaims(strings.TrimSpace(parts[1]), &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("unexpected signing method")
			}
			return s.jwtSecret, nil
		})
		if err != nil || !token.Valid {
			writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
			c.Abort()
			return
		}
		claims, ok := token.Claims.(*jwt.RegisteredClaims)
		if !ok || claims.Subject == "" {
			writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
			c.Abort()
			return
		}
		userID, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil || userID <= 0 {
			writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
			c.Abort()
			return
		}
		c.Set(userIDContextKey, userID)
		c.Next()
	}
}

func (s *authService) sendSMSCode(c *gin.Context) {
	var req smsCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil || !phonePattern.MatchString(req.Phone) {
		writeError(c, http.StatusBadRequest, "INVALID_PHONE", "手机号格式不正确")
		return
	}
	if req.Purpose == "" {
		req.Purpose = "login"
	}
	if req.Purpose != "login" {
		writeError(c, http.StatusBadRequest, "INVALID_PURPOSE", "不支持的验证码用途")
		return
	}
	if s.rdb == nil {
		writeError(c, http.StatusServiceUnavailable, "SMS_PROVIDER_ERROR", "短信服务暂时不可用，请稍后再试")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	key := "sms:login:" + req.Phone
	cooldownKey := "sms:cooldown:login:" + req.Phone
	reserved, err := s.rdb.SetNX(ctx, cooldownKey, "1", time.Minute).Result()
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "SMS_PROVIDER_ERROR", "短信服务暂时不可用，请稍后再试")
		return
	}
	if !reserved {
		writeError(c, http.StatusTooManyRequests, "SMS_TOO_FREQUENT", "验证码发送过于频繁，请稍后再试")
		return
	}
	now := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	date := now.Format("20060102")
	phoneDailyKey := "sms:daily:phone:" + req.Phone + ":" + date
	ipDailyKey := "sms:daily:ip:" + hashToken(c.ClientIP()) + ":" + date
	dailyReserved, err := reserveDailySMS(ctx, s.rdb, phoneDailyKey, ipDailyKey, secondsUntilNextDay(now))
	if err != nil {
		_ = s.rdb.Del(ctx, cooldownKey)
		writeError(c, http.StatusServiceUnavailable, "SMS_PROVIDER_ERROR", "短信服务暂时不可用，请稍后再试")
		return
	}
	if !dailyReserved {
		_ = s.rdb.Del(ctx, cooldownKey)
		writeError(c, http.StatusTooManyRequests, "SMS_DAILY_LIMIT", "今日验证码发送次数已达上限")
		return
	}
	code := s.mockCode
	if code == "" {
		generated, err := randomDigits(6)
		if err != nil {
			_ = s.rdb.Del(ctx, cooldownKey)
			_ = releaseDailySMS(ctx, s.rdb, phoneDailyKey, ipDailyKey)
			writeError(c, http.StatusInternalServerError, "SMS_PROVIDER_ERROR", "短信服务暂时不可用，请稍后再试")
			return
		}
		code = generated
	}
	if err := s.rdb.Set(ctx, key, hashToken(code), 10*time.Minute).Err(); err != nil {
		_ = s.rdb.Del(ctx, cooldownKey)
		_ = releaseDailySMS(ctx, s.rdb, phoneDailyKey, ipDailyKey)
		writeError(c, http.StatusServiceUnavailable, "SMS_PROVIDER_ERROR", "短信服务暂时不可用，请稍后再试")
		return
	}
	smsCtx, smsCancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	err = s.smsSender.Send(smsCtx, req.Phone, code)
	smsCancel()
	if err != nil {
		if isDefinitiveSMSSendFailure(err) {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = rollbackSMSReservation(cleanupCtx, s.rdb, key, cooldownKey, phoneDailyKey, ipDailyKey, hashToken(code))
			cleanupCancel()
		}
		writeError(c, http.StatusBadGateway, "SMS_PROVIDER_ERROR", "短信服务暂时不可用，请稍后再试")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"expires_in": 600, "resend_after": 60}, "request_id": c.GetString("request_id")})
}

func (s *authService) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil || !phonePattern.MatchString(req.Phone) || !codePattern.MatchString(req.Code) {
		writeError(c, http.StatusBadRequest, "INVALID_SMS_CODE", "验证码错误或已过期")
		return
	}
	if s.db == nil || s.rdb == nil {
		writeError(c, http.StatusServiceUnavailable, "AUTH_FAILED", "登录服务暂时不可用，请稍后再试")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	key := "sms:login:" + req.Phone
	consumed, err := consumeSMSCode(ctx, s.rdb, key, hashToken(req.Code))
	if err != nil && err != redis.Nil {
		writeError(c, http.StatusServiceUnavailable, "AUTH_FAILED", "登录服务暂时不可用，请稍后再试")
		return
	}
	if !consumed {
		writeError(c, http.StatusBadRequest, "INVALID_SMS_CODE", "验证码错误或已过期")
		return
	}
	var user userRecord
	err = s.db.QueryRowContext(ctx, "SELECT id, phone, nickname, status, created_at FROM users WHERE phone = ?", req.Phone).Scan(&user.ID, &user.Phone, &user.Nickname, &user.Status, &user.CreatedAt)
	if err == sql.ErrNoRows {
		user.Nickname = "用户" + req.Phone[len(req.Phone)-4:]
		result, insertErr := s.db.ExecContext(ctx, "INSERT INTO users(phone, nickname, status) VALUES (?, ?, 'active')", req.Phone, user.Nickname)
		if insertErr != nil {
			writeError(c, http.StatusInternalServerError, "AUTH_FAILED", "登录失败，请稍后再试")
			return
		}
		user.ID, _ = result.LastInsertId()
		user.Phone, user.Status, user.CreatedAt = req.Phone, "active", time.Now()
	} else if err != nil {
		writeError(c, http.StatusInternalServerError, "AUTH_FAILED", "登录失败，请稍后再试")
		return
	}
	if user.Status != "active" {
		writeError(c, http.StatusForbidden, "USER_DISABLED", "当前账号不可用")
		return
	}
	access, err := s.issueAccessToken(user.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "AUTH_FAILED", "登录失败，请稍后再试")
		return
	}
	refresh, err := s.issueRefreshToken(ctx, user.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "AUTH_FAILED", "登录失败，请稍后再试")
		return
	}
	s.setRefreshCookie(c, refresh)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"access_token": access, "token_type": "Bearer", "expires_in": int(s.accessTTL.Seconds()), "user": user.response()}, "request_id": c.GetString("request_id")})
}

func consumeSMSCode(ctx context.Context, rdb *redis.Client, key, expectedHash string) (bool, error) {
	result, err := rdb.Eval(ctx, `
		if redis.call('GET', KEYS[1]) == ARGV[1] then
			redis.call('DEL', KEYS[1])
			return 1
		end
		return 0
	`, []string{key}, expectedHash).Int()
	return result == 1, err
}

func reserveDailySMS(ctx context.Context, rdb *redis.Client, phoneKey, ipKey string, ttlSeconds int64) (bool, error) {
	result, err := rdb.Eval(ctx, `
		local phoneCount = tonumber(redis.call('GET', KEYS[1]) or '0')
		local ipCount = tonumber(redis.call('GET', KEYS[2]) or '0')
		if phoneCount >= 5 or ipCount >= 10 then return 0 end
		phoneCount = redis.call('INCR', KEYS[1])
		ipCount = redis.call('INCR', KEYS[2])
		if phoneCount == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
		if ipCount == 1 then redis.call('EXPIRE', KEYS[2], ARGV[1]) end
		return 1
	`, []string{phoneKey, ipKey}, ttlSeconds).Int()
	return result == 1, err
}

func releaseDailySMS(ctx context.Context, rdb *redis.Client, phoneKey, ipKey string) error {
	return rdb.Eval(ctx, `
		for _, key in ipairs(KEYS) do
			local count = tonumber(redis.call('GET', key) or '0')
			if count > 0 then redis.call('DECR', key) end
		end
		return 1
	`, []string{phoneKey, ipKey}).Err()
}

func rollbackSMSReservation(ctx context.Context, rdb *redis.Client, codeKey, cooldownKey, phoneKey, ipKey, expectedHash string) error {
	return rdb.Eval(ctx, `
		if redis.call('GET', KEYS[1]) == ARGV[1] then redis.call('DEL', KEYS[1]) end
		redis.call('DEL', KEYS[2])
		for index = 3, 4 do
			local count = tonumber(redis.call('GET', KEYS[index]) or '0')
			if count > 0 then redis.call('DECR', KEYS[index]) end
		end
		return 1
	`, []string{codeKey, cooldownKey, phoneKey, ipKey}, expectedHash).Err()
}

func secondsUntilNextDay(now time.Time) int64 {
	nextDay := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	return int64(nextDay.Sub(now).Seconds())
}

type userRecord struct {
	ID                      int64
	Phone, Nickname, Status string
	CreatedAt               time.Time
}

func (u userRecord) response() gin.H {
	return gin.H{"id": u.ID, "phone_masked": u.Phone[:3] + "****" + u.Phone[len(u.Phone)-4:], "nickname": u.Nickname, "created_at": u.CreatedAt.Format(time.RFC3339Nano)}
}

func (s *authService) issueAccessToken(userID int64) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{Subject: fmt.Sprint(userID), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL))}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}
func (s *authService) issueRefreshToken(ctx context.Context, userID int64) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO refresh_tokens(user_id, token_hash, expires_at) VALUES (?, ?, ?)", userID, hashToken(token), time.Now().Add(s.refreshTTL))
	return token, err
}
func (s *authService) refresh(c *gin.Context) {
	if !s.validCSRF(c) {
		writeError(c, http.StatusForbidden, "CSRF_REJECTED", "请求来源校验失败")
		return
	}
	token, err := c.Cookie(refreshCookieName)
	if err != nil || token == "" {
		writeError(c, http.StatusUnauthorized, "REFRESH_TOKEN_MISSING", "登录状态已失效，请重新登录")
		return
	}
	if s.db == nil {
		writeError(c, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID", "登录状态已失效，请重新登录")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "REFRESH_TOKEN_REVOKED", "登录状态已失效，请重新登录")
		return
	}
	var userID int64
	var expires time.Time
	err = tx.QueryRowContext(ctx, "SELECT user_id, expires_at FROM refresh_tokens WHERE token_hash = ? AND revoked_at IS NULL FOR UPDATE", hashToken(token)).Scan(&userID, &expires)
	if err != nil || time.Now().After(expires) {
		_ = tx.Rollback()
		writeError(c, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID", "登录状态已失效，请重新登录")
		return
	}
	if _, err = tx.ExecContext(ctx, "UPDATE refresh_tokens SET revoked_at = CURRENT_TIMESTAMP(3) WHERE token_hash = ? AND revoked_at IS NULL", hashToken(token)); err != nil {
		_ = tx.Rollback()
		writeError(c, http.StatusUnauthorized, "REFRESH_TOKEN_REVOKED", "登录状态已失效，请重新登录")
		return
	}
	access, err := s.issueAccessToken(userID)
	if err != nil {
		_ = tx.Rollback()
		writeError(c, http.StatusInternalServerError, "AUTH_FAILED", "登录失败，请稍后再试")
		return
	}
	newToken, err := randomToken(32)
	if err != nil {
		_ = tx.Rollback()
		writeError(c, http.StatusInternalServerError, "AUTH_FAILED", "登录失败，请稍后再试")
		return
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO refresh_tokens(user_id, token_hash, expires_at) VALUES (?, ?, ?)", userID, hashToken(newToken), time.Now().Add(s.refreshTTL)); err != nil {
		_ = tx.Rollback()
		writeError(c, http.StatusInternalServerError, "AUTH_FAILED", "登录失败，请稍后再试")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(c, http.StatusInternalServerError, "AUTH_FAILED", "登录失败，请稍后再试")
		return
	}
	s.setRefreshCookie(c, newToken)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"access_token": access, "token_type": "Bearer", "expires_in": int(s.accessTTL.Seconds())}, "request_id": c.GetString("request_id")})
}
func (s *authService) logout(c *gin.Context) {
	if !s.validCSRF(c) {
		writeError(c, http.StatusForbidden, "CSRF_REJECTED", "请求来源校验失败")
		return
	}
	if token, err := c.Cookie(refreshCookieName); err == nil && s.db != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		_, _ = s.db.ExecContext(ctx, "UPDATE refresh_tokens SET revoked_at = CURRENT_TIMESTAMP(3) WHERE token_hash = ? AND revoked_at IS NULL", hashToken(token))
	}
	s.clearRefreshCookie(c)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"logged_out": true}, "request_id": c.GetString("request_id")})
}
func (s *authService) validCSRF(c *gin.Context) bool {
	if c.GetHeader("X-CSRF-Protection") != "1" {
		return false
	}
	origin := c.GetHeader("Origin")
	if origin == "" || len(s.allowedOrigin) == 0 {
		return true
	}
	_, ok := s.allowedOrigin[origin]
	return ok
}
func (s *authService) setRefreshCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{Name: refreshCookieName, Value: token, Path: "/api/v1/auth", HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: int(s.refreshTTL.Seconds())})
}
func (s *authService) clearRefreshCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: refreshCookieName, Value: "", Path: "/api/v1/auth", HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func randomDigits(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = '0' + b[i]%10
	}
	return string(b), nil
}
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}, "request_id": c.GetString("request_id")})
}
