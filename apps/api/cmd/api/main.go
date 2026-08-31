package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"aigc-3d-platform/apps/api/internal/asset"
	"aigc-3d-platform/apps/api/internal/auth"
	"aigc-3d-platform/apps/api/internal/catalog"
	"aigc-3d-platform/apps/api/internal/generation"
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

func durationEnv(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(env(key, fallback.String()))
	if err != nil {
		return fallback
	}
	return value
}

func boolEnv(key string, fallback bool) bool {
	value, err := strconv.ParseBool(env(key, strconv.FormatBool(fallback)))
	if err != nil {
		return fallback
	}
	return value
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	appEnv := env("APP_ENV", "development")
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" && appEnv == "development" {
		jwtSecret = "local-development-secret-change-before-deployment"
	}
	port := env("API_PORT", "8080")
	db, err := gorm.Open(mysql.Open(env("MYSQL_DSN", "root:root@tcp(localhost:3306)/aigc_platform?charset=utf8mb4&parseTime=True&loc=Local")), &gorm.Config{TranslateError: true})
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
	authHandler, err := auth.New(db, auth.Config{
		JWTSecret:     jwtSecret,
		Issuer:        env("JWT_ISSUER", "aigc-3d-platform"),
		AccessTTL:     durationEnv("JWT_ACCESS_TTL", 15*time.Minute),
		RefreshTTL:    durationEnv("JWT_REFRESH_TTL", 30*24*time.Hour),
		CookieSecure:  boolEnv("COOKIE_SECURE", false),
		RefreshCookie: "refresh_token",
	})
	if err != nil {
		logger.Error("auth initialization failed", "error", err)
		os.Exit(1)
	}
	assetService, err := asset.New(db, asset.NewMinIOStore(mc, deps.bucket), deps.bucket)
	if err != nil {
		logger.Error("asset initialization failed", "error", err)
		os.Exit(1)
	}
	catalogHandler, err := catalog.New(db, assetService)
	if err != nil {
		logger.Error("catalog initialization failed", "error", err)
		os.Exit(1)
	}
	generationHandler, err := generation.New(db, assetService, rdb, durationEnv("GENERATION_JOB_TIMEOUT", 2*time.Minute))
	if err != nil {
		logger.Error("generation initialization failed", "error", err)
		os.Exit(1)
	}
	r := gin.New()
	r.Use(gin.Recovery(), cors.New(cors.Config{
		AllowOrigins:     []string{env("CORS_ALLOW_ORIGIN", "http://localhost:5173")},
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Request-ID", "Idempotency-Key"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}), requestID(), securityHeaders())
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/readyz", readyHandler(deps))
	api := r.Group("/api/v1")
	api.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"service": "api", "version": "0.3.0", "environment": env("APP_ENV", "development")})
	})
	authHandler.RegisterRoutes(api)
	catalogHandler.RegisterRoutes(api, authHandler.Authenticate(), authHandler.ResolveUser)
	generationHandler.RegisterRoutes(api, authHandler.Authenticate())
	generationHandler.StartDispatcher()
	logger.Info("api started", "port", port)
	if err := r.Run(":" + port); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
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
