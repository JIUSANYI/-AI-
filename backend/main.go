package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type config struct {
	AppEnv   string
	HTTPPort string
}

func loadConfig() (config, error) {
	cfg := config{
		AppEnv:   getenv("APP_ENV", "dev"),
		HTTPPort: getenv("HTTP_PORT", "8080"),
	}
	if cfg.HTTPPort == "" {
		return config{}, errors.New("HTTP_PORT must not be empty")
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

	router := gin.New()
	router.Use(requestID(), gin.Recovery(), accessLog())
	router.GET("/health", healthHandler)
	router.GET("/ready", readyHandler)

	api := router.Group("/api/v1")
	api.GET("/health", healthHandler)
	api.GET("/ready", readyHandler)

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

func readyHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"status": "ready",
			"checks": gin.H{
				"mysql": "not_configured",
				"redis": "not_configured",
			},
		},
		"request_id": c.GetString("request_id"),
	})
}

