package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type config struct {
	AppEnv      string
	HTTPPort    string
	MySQLDSN    string
	RedisAddr   string
	RedisPass   string
	RedisDB     string
	RequireDeps bool
}

func loadConfig() (config, error) {
	cfg := config{
		AppEnv:      getenv("APP_ENV", "dev"),
		HTTPPort:    getenv("HTTP_PORT", "8080"),
		MySQLDSN:    os.Getenv("MYSQL_DSN"),
		RedisAddr:   getenv("REDIS_ADDR", "redis:6379"),
		RedisPass:   os.Getenv("REDIS_PASSWORD"),
		RedisDB:     getenv("REDIS_DB", "0"),
		RequireDeps: getenv("REQUIRE_DEPS", "true") == "true",
	}
	if cfg.HTTPPort == "" {
		return config{}, errors.New("HTTP_PORT must not be empty")
	}
	if cfg.RequireDeps && cfg.MySQLDSN == "" {
		return config{}, errors.New("MYSQL_DSN must not be empty when REQUIRE_DEPS=true")
	}
	redisDB, err := strconv.Atoi(cfg.RedisDB)
	if err != nil || redisDB < 0 {
		return config{}, errors.New("REDIS_DB must be a non-negative integer")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	var db *sql.DB
	var rdb *redis.Client
	if cfg.MySQLDSN != "" {
		db, err = sql.Open("mysql", cfg.MySQLDSN)
		if err != nil {
			slog.Error("mysql open failed", "error", err)
			os.Exit(1)
		}
		defer db.Close()
		db.SetConnMaxLifetime(5 * time.Minute)
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		if err = runMigrations(context.Background(), db); err != nil && cfg.RequireDeps {
			slog.Error("database migration failed", "error", err)
			os.Exit(1)
		}
	}
	if cfg.RedisAddr != "" {
		redisDB, _ := strconv.Atoi(cfg.RedisDB)
		rdb = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPass, DB: redisDB})
		defer rdb.Close()
	}

	router := gin.New()
	router.Use(requestID(), gin.Recovery(), accessLog())
	router.GET("/health", healthHandler)
	router.GET("/ready", readyHandler(db, rdb, cfg.RequireDeps))

	api := router.Group("/api/v1")
	api.GET("/health", healthHandler)
	api.GET("/ready", readyHandler(db, rdb, cfg.RequireDeps))

	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      180 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			slog.Error("server shutdown failed", "error", err)
		}
	}()

	slog.Info("server starting", "env", cfg.AppEnv, "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = fmt.Sprintf("req_%d", time.Now().UnixNano())
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		requestID, _ := c.Get("request_id")
		slog.Info("http request",
			"request_id", requestID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"data":       gin.H{"status": "ok"},
		"request_id": c.GetString("request_id"),
	})
}

func readyHandler(db *sql.DB, rdb *redis.Client, requireDeps bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		checks := gin.H{"mysql": "not_configured", "redis": "not_configured"}
		status := http.StatusOK
		if db != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			err := db.PingContext(ctx)
			cancel()
			checks["mysql"] = dependencyStatus(err)
			if err != nil && requireDeps {
				status = http.StatusServiceUnavailable
			}
		}
		if rdb != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			err := rdb.Ping(ctx).Err()
			cancel()
			checks["redis"] = dependencyStatus(err)
			if err != nil && requireDeps {
				status = http.StatusServiceUnavailable
			}
		}
		responseStatus := "ready"
		if status != http.StatusOK {
			responseStatus = "not_ready"
		}
		c.JSON(status, gin.H{
			"data": gin.H{
				"status": responseStatus,
				"checks": checks,
			},
			"request_id": c.GetString("request_id"),
		})
	}
}

func dependencyStatus(err error) string {
	if err == nil {
		return "ok"
	}
	return "unavailable"
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(64) NOT NULL PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var applied int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}
		script, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err = tx.ExecContext(ctx, string(script)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		slog.Info("migration applied", "version", name)
	}
	return nil
}
