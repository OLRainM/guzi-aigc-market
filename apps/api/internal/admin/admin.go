package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aigc-3d-platform/apps/api/internal/auth"
	"aigc-3d-platform/apps/api/internal/catalog"
	"aigc-3d-platform/apps/api/internal/generation"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuditLog struct {
	ID         string          `gorm:"type:char(36);primaryKey" json:"id"`
	ActorID    string          `gorm:"type:char(36);not null;index" json:"actor_id"`
	Action     string          `gorm:"size:64;not null;index" json:"action"`
	TargetType string          `gorm:"size:32;not null;index" json:"target_type"`
	TargetID   string          `gorm:"size:64;not null;index" json:"target_id"`
	RequestID  string          `gorm:"size:128;not null;index" json:"request_id"`
	Before     json.RawMessage `gorm:"column:before_state;type:json" json:"before,omitempty"`
	After      json.RawMessage `gorm:"column:after_state;type:json" json:"after,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_logs" }

var errProductNotPublished = errors.New("product is not published")

type Handler struct {
	db   *gorm.DB
	jobs *generation.Service
}

func New(db *gorm.DB, jobs *generation.Service) (*Handler, error) {
	if err := db.AutoMigrate(&AuditLog{}); err != nil {
		return nil, err
	}
	return &Handler{db: db, jobs: jobs}, nil
}

func (h *Handler) RegisterRoutes(group *gin.RouterGroup, authenticate gin.HandlerFunc) {
	admin := group.Group("/admin", authenticate, auth.RequireRole("ADMIN"))
	admin.GET("/users", h.users)
	admin.GET("/products", h.products)
	admin.POST("/products/:id/off-shelf", h.offShelf)
	admin.GET("/generation-jobs", h.failedJobs)
	admin.POST("/generation-jobs/:id/retry", h.retry)
	admin.GET("/audit-logs", h.auditLogs)
}

func page(c *gin.Context) (int, int) {
	p, _ := strconv.Atoi(c.Query("page"))
	if p < 1 {
		p = 1
	}
	s, _ := strconv.Atoi(c.Query("page_size"))
	if s < 1 || s > 100 {
		s = 20
	}
	return p, s
}
func requestID(c *gin.Context) string { return c.GetString("request_id") }
func writeError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": "ADMIN_ERROR", "message": message, "request_id": requestID(c)}})
}

func (h *Handler) users(c *gin.Context) {
	p, s := page(c)
	q := h.db.WithContext(c.Request.Context()).Model(&auth.User{})
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("username LIKE ? OR email LIKE ?", like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		writeError(c, 500, "查询用户失败")
		return
	}
	var items []auth.User
	if err := q.Preload("Roles").Order("created_at DESC").Offset((p - 1) * s).Limit(s).Find(&items).Error; err != nil {
		writeError(c, 500, "查询用户失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": p, "page_size": s, "total": total})
}

func (h *Handler) products(c *gin.Context) {
	p, s := page(c)
	q := h.db.WithContext(c.Request.Context()).Model(&catalog.Product{})
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		q = q.Where("status = ?", status)
	}
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		q = q.Where("title LIKE ? OR description LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		writeError(c, 500, "查询商品失败")
		return
	}
	var items []catalog.Product
	if err := q.Order("created_at DESC").Offset((p - 1) * s).Limit(s).Find(&items).Error; err != nil {
		writeError(c, 500, "查询商品失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": p, "page_size": s, "total": total})
}

func (h *Handler) offShelf(c *gin.Context) {
	var product catalog.Product
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&product, "id = ?", c.Param("id")).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return gorm.ErrRecordNotFound
			}
			return err
		}
		before := product
		if product.Status != catalog.StatusPublished {
			return errProductNotPublished
		}
		product.Status = catalog.StatusOffShelf
		if result := tx.Model(&catalog.Product{}).Where("id = ? AND status = ?", product.ID, catalog.StatusPublished).Update("status", catalog.StatusOffShelf); result.Error != nil {
			return result.Error
		} else if result.RowsAffected != 1 {
			return errProductNotPublished
		}
		return h.recordTx(c, tx, "PRODUCT_OFF_SHELF", "PRODUCT", product.ID, before, product)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, 404, "商品不存在")
		return
	}
	if errors.Is(err, errProductNotPublished) {
		writeError(c, 409, "只有已发布商品可以下架")
		return
	}
	if err != nil {
		writeError(c, 500, "下架商品失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"product": product})
}

func (h *Handler) failedJobs(c *gin.Context) {
	p, s := page(c)
	q := h.db.WithContext(c.Request.Context()).Model(&generation.GenerationJob{}).Where("status = ?", generation.StatusFailed)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		writeError(c, 500, "查询生成任务失败")
		return
	}
	var jobs []generation.GenerationJob
	if err := q.Order("updated_at DESC").Offset((p - 1) * s).Limit(s).Find(&jobs).Error; err != nil {
		writeError(c, 500, "查询生成任务失败")
		return
	}
	items := make([]generation.JobResponse, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, h.jobs.ToResponse(job, []generation.GenerationOutput{}))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": p, "page_size": s, "total": total})
}

func (h *Handler) retry(c *gin.Context) {
	job, err := h.jobs.AdminRetryWithAudit(c.Request.Context(), c.Param("id"), requestID(c), func(tx *gorm.DB, before, after *generation.GenerationJob) error {
		return h.recordTx(c, tx, "GENERATION_RETRY", "GENERATION_JOB", after.ID, before, after)
	})
	if err != nil {
		switch {
		case errors.Is(err, generation.ErrJobNotFound):
			writeError(c, 404, "生成任务不存在")
		case errors.Is(err, generation.ErrRetryNotAllowed), errors.Is(err, generation.ErrTooManyJobs):
			writeError(c, 409, "当前任务不可重试")
		default:
			writeError(c, 500, "重试生成任务失败")
		}
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job": h.jobs.ToResponse(*job, nil)})
}

func (h *Handler) auditLogs(c *gin.Context) {
	p, s := page(c)
	var total int64
	q := h.db.WithContext(c.Request.Context()).Model(&AuditLog{})
	if err := q.Count(&total).Error; err != nil {
		writeError(c, 500, "查询审计日志失败")
		return
	}
	var items []AuditLog
	if err := q.Order("created_at DESC").Offset((p - 1) * s).Limit(s).Find(&items).Error; err != nil {
		writeError(c, 500, "查询审计日志失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": p, "page_size": s, "total": total})
}

func (h *Handler) record(c *gin.Context, action, targetType, targetID string, before, after any) error {
	return h.recordTx(c, h.db.WithContext(c.Request.Context()), action, targetType, targetID, before, after)
}

func (h *Handler) recordTx(c *gin.Context, db *gorm.DB, action, targetType, targetID string, before, after any) error {
	user, ok := auth.CurrentUser(c)
	if !ok {
		return errors.New("missing actor")
	}
	b, err := json.Marshal(before)
	if err != nil {
		return err
	}
	a, err := json.Marshal(after)
	if err != nil {
		return err
	}
	return db.Create(&AuditLog{ID: uuidString(), ActorID: user.ID, Action: action, TargetType: targetType, TargetID: targetID, RequestID: requestID(c), Before: b, After: a}).Error
}

func uuidString() string { return uuid.NewString() }
