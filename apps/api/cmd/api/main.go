package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type dependencies struct {
	db     *gorm.DB
	redis  *redis.Client
	minio  *minio.Client
	bucket string
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	port := env("API_PORT", "8080")
	db, err := gorm.Open(mysql.Open(env("MYSQL_DSN", "root:root@tcp(localhost:3306)/aigc_platform?charset=utf8mb4&parseTime=True&loc=Local")), &gorm.Config{})
	if err != nil {
		logger.Error("mysql connection failed", "error", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(&redis.Options{Addr: env("REDIS_ADDR", "localhost:6379")})
	mc, err := minio.New(env("MINIO_ENDPOINT", "localhost:9000"), &minio.Options{Creds: credentials.NewStaticV4(env("MINIO_ACCESS_KEY", "minioadmin"), env("MINIO_SECRET_KEY", "minioadmin"), ""), Secure: false})
	if err != nil {
		logger.Error("minio client failed", "error", err)
		os.Exit(1)
	}
	deps := dependencies{db: db, redis: rdb, minio: mc, bucket: env("MINIO_BUCKET", "aigc-assets")}
	r := gin.New()
	r.Use(gin.Recovery(), cors.New(cors.Config{
		AllowOrigins:     []string{env("CORS_ALLOW_ORIGIN", "http://localhost:5173")},
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Request-ID"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}), requestID())
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/readyz", readyHandler(deps))
	r.GET("/api/v1/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"service": "api", "version": "0.1.0", "environment": env("APP_ENV", "development")})
	})
	logger.Info("api started", "port", port)
	if err := r.Run(":" + port); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = fmt.Sprintf("req-%d", time.Now().UnixNano())
		}
		c.Header("X-Request-ID", id)
		c.Set("request_id", id)
		c.Next()
	}
}

func readyHandler(d dependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		checks := gin.H{}
		status := http.StatusOK
		if sqlDB, err := d.db.DB(); err != nil || sqlDB.PingContext(ctx) != nil {
			checks["mysql"] = "unavailable"
			status = http.StatusServiceUnavailable
		} else {
			checks["mysql"] = "ok"
		}
		if err := d.redis.Ping(ctx).Err(); err != nil {
			checks["redis"] = "unavailable"
			status = http.StatusServiceUnavailable
		} else {
			checks["redis"] = "ok"
		}
		if _, err := d.minio.ListBuckets(ctx); err != nil {
			checks["minio"] = "unavailable"
			status = http.StatusServiceUnavailable
		} else {
			checks["minio"] = "ok"
		}
		c.JSON(status, gin.H{"status": map[bool]string{true: "ready", false: "not_ready"}[status == http.StatusOK], "checks": checks})
	}
}
