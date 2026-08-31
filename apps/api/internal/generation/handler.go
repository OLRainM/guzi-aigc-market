package generation

import (
	"errors"
	"io"
	"net/http"
	"os"
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
	service     *Service
	workerToken string
}

func New(db *gorm.DB, assets *asset.Service, rdb *redis.Client, timeout time.Duration) (*Handler, error) {
	service, err := NewService(db, assets, rdb, timeout)
	if err != nil {
		return nil, err
	}
	return &Handler{service: service, workerToken: os.Getenv("WORKER_INTERNAL_TOKEN")}, nil
}

func (h *Handler) RegisterRoutes(group *gin.RouterGroup, authenticate gin.HandlerFunc) {
	protected := group.Group("/generation-jobs", authenticate)
	protected.POST("", h.create)
	protected.GET("", h.list)
	protected.GET("/:id", h.get)
	protected.POST("/:id/cancel", h.cancel)
	protected.POST("/:id/retry", h.retry)
	protected.GET("/:id/outputs/:assetId/content", h.outputContent)

	internal := group.Group("/internal/generation-jobs", h.workerAuth())
	internal.POST("/:id/claim", h.claim)
	internal.POST("/:id/progress", h.progress)
	internal.POST("/:id/fail", h.fail)
	internal.POST("/:id/complete", h.complete)
}

func (h *Handler) StartDispatcher() {
	h.service.StartDispatcher()
}

func (h *Handler) StartTimeoutWatcher() {
	h.service.StartTimeoutWatcher()
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

func (h *Handler) list(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	jobs, total, err := h.service.List(c.Request.Context(), user.ID, positiveInt(c.Query("page"), 1), positiveInt(c.Query("page_size"), 20))
	if err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "加载生成任务失败")
		return
	}
	items := make([]JobResponse, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, h.service.ToResponse(job, nil))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": positiveInt(c.Query("page"), 1), "page_size": positiveInt(c.Query("page_size"), 20), "total": total})
}

func (h *Handler) get(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	job, outputs, err := h.service.Get(c.Request.Context(), user.ID, c.Param("id"))
	if err != nil {
		abortJobError(c, err, "加载生成任务失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": h.service.ToResponse(*job, outputs)})
}

func (h *Handler) cancel(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	job, outputs, err := h.service.Cancel(c.Request.Context(), user.ID, c.Param("id"))
	if err != nil {
		abortJobError(c, err, "取消生成任务失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": h.service.ToResponse(*job, outputs)})
}

func (h *Handler) retry(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	job, err := h.service.Retry(c.Request.Context(), user.ID, c.Param("id"), requestID(c))
	if err != nil {
		abortJobError(c, err, "重试生成任务失败")
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job": h.service.ToResponse(*job, nil), "status_url": "/api/v1/generation-jobs/" + job.ID})
}

func (h *Handler) outputContent(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	_, stored, err := h.service.OpenOutput(c.Request.Context(), user.ID, c.Param("id"), c.Param("assetId"))
	if err != nil {
		abortJobError(c, err, "加载生成结果失败")
		return
	}
	body, info, _, err := h.service.assets.Open(c.Request.Context(), stored.ID)
	if err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "加载生成结果失败")
		return
	}
	defer body.Close()
	c.Header("Content-Type", info.ContentType)
	c.Header("Cache-Control", "private, max-age=60")
	c.DataFromReader(http.StatusOK, info.Size, info.ContentType, body, nil)
}

func (h *Handler) claim(c *gin.Context) {
	var req WorkerProgressRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Attempt < 1 {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "认领参数无效")
		return
	}
	job, err := h.service.Claim(c.Request.Context(), c.Param("id"), req.Attempt)
	if err != nil {
		abortJobError(c, err, "认领生成任务失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": h.service.ToResponse(*job, nil)})
}

func (h *Handler) progress(c *gin.Context) {
	var req WorkerProgressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "进度参数无效")
		return
	}
	job, err := h.service.ReportProgress(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		abortJobError(c, err, "更新生成进度失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": h.service.ToResponse(*job, nil)})
}

func (h *Handler) fail(c *gin.Context) {
	var req WorkerFailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "失败参数无效")
		return
	}
	job, err := h.service.Fail(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		abortJobError(c, err, "标记生成失败失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": h.service.ToResponse(*job, nil)})
}

func (h *Handler) complete(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "缺少生成结果文件")
		return
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "读取生成结果失败")
		return
	}
	attempt := positiveInt(c.PostForm("attempt"), 0)
	if attempt < 1 {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "缺少 attempt")
		return
	}
	job, outputs, err := h.service.Complete(c.Request.Context(), c.Param("id"), WorkerCompleteRequest{
		Attempt: attempt, ProviderJobID: c.PostForm("provider_job_id"), Filename: header.Filename, MIMEType: header.Header.Get("Content-Type"), Body: body,
	})
	if err != nil {
		if errors.Is(err, asset.ErrInvalidFile) || errors.Is(err, asset.ErrUnsupportedType) || errors.Is(err, asset.ErrKindMismatch) || errors.Is(err, asset.ErrFileTooLarge) {
			abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "生成结果不是有效的 GLB")
			return
		}
		abortJobError(c, err, "保存生成结果失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": h.service.ToResponse(*job, outputs)})
}

func (h *Handler) workerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.workerToken == "" || c.GetHeader("X-Worker-Token") != h.workerToken {
			abort(c, http.StatusUnauthorized, "UNAUTHORIZED", "Worker 未授权")
			return
		}
		c.Next()
	}
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

func abortJobError(c *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, errJobNotFound):
		abort(c, http.StatusNotFound, "NOT_FOUND", "生成任务不存在")
	case errors.Is(err, errInvalidArgument):
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "生成任务参数不符合要求")
	case errors.Is(err, errInvalidTransition):
		abort(c, http.StatusConflict, "CONFLICT", "当前任务状态不允许该操作")
	case errors.Is(err, errTooManyJobs):
		abort(c, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "进行中的生成任务过多，请稍后再试")
	default:
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", fallback)
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
