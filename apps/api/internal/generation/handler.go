package generation

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aigc-3d-platform/apps/api/internal/asset"
	"aigc-3d-platform/apps/api/internal/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type Handler struct {
	service *Service
}

func New(db *gorm.DB, assets *asset.Service, rdb *redis.Client, timeout time.Duration) (*Handler, error) {
	service, err := NewService(db, assets, rdb, timeout)
	if err != nil {
		return nil, err
	}
	return &Handler{service: service}, nil
}

func (h *Handler) RegisterRoutes(group *gin.RouterGroup, authenticate gin.HandlerFunc) {
	protected := group.Group("/generation-jobs", authenticate)
	protected.POST("", h.create)
}

func (h *Handler) StartDispatcher() {
	h.service.StartDispatcher()
}

func (h *Handler) create(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req CreateJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "请求格式无效")
		return
	}
	job, err := h.service.Create(c.Request.Context(), user.ID, strings.TrimSpace(c.GetHeader("Idempotency-Key")), requestID(c), req)
	if err != nil {
		abortCreateError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job": h.service.ToResponse(*job, nil), "status_url": "/api/v1/generation-jobs/" + job.ID})
}

func abortCreateError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errInvalidArgument):
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "生成任务参数不符合要求")
	case errors.Is(err, errIdempotencyConflict):
		abort(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "同一幂等键不能提交不同的生成请求")
	case errors.Is(err, errTooManyJobs):
		abort(c, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "进行中的生成任务过多，请稍后再试")
	default:
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "创建生成任务失败")
	}
}

func requestID(c *gin.Context) string {
	if id := c.GetString("request_id"); id != "" {
		return id
	}
	return uuid.NewString()
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func abort(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message, "request_id": c.GetString("request_id")}})
}
