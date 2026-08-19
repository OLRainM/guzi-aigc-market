package catalog

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aigc-3d-platform/apps/api/internal/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	StatusDraft     = "DRAFT"
	StatusPublished = "PUBLISHED"
	StatusOffShelf  = "OFF_SHELF"
)

var (
	errInvalidProduct    = errors.New("invalid product")
	errInvalidTransition = errors.New("invalid product status transition")
)

type Product struct {
	ID              string    `gorm:"type:char(36);primaryKey" json:"id"`
	SellerID        string    `gorm:"type:char(36);not null;index" json:"seller_id"`
	Title           string    `gorm:"size:120;not null;index" json:"title"`
	Description     string    `gorm:"type:text;not null" json:"description"`
	PriceCents      int64     `gorm:"not null;index" json:"price_cents"`
	IPName          string    `gorm:"column:ip_name;size:80;not null;index" json:"ip_name"`
	Category        string    `gorm:"size:64;not null;index" json:"category"`
	Condition       string    `gorm:"size:32;not null;index" json:"condition"`
	TransactionType string    `gorm:"size:24;not null;default:SALE;index" json:"transaction_type"`
	ShippingOrigin  string    `gorm:"size:120" json:"shipping_origin,omitempty"`
	Stock           int       `gorm:"not null;default:1" json:"stock"`
	PreorderNote    string    `gorm:"size:500" json:"preorder_note,omitempty"`
	Status          string    `gorm:"size:16;not null;default:DRAFT;index" json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Handler struct {
	db *gorm.DB
}

type productRequest struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	PriceCents      int64  `json:"price_cents"`
	IPName          string `json:"ip_name"`
	Category        string `json:"category"`
	Condition       string `json:"condition"`
	TransactionType string `json:"transaction_type"`
	ShippingOrigin  string `json:"shipping_origin"`
	Stock           int    `json:"stock"`
	PreorderNote    string `json:"preorder_note"`
}

type listResponse struct {
	Items    []Product `json:"items"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
	Total    int64     `json:"total"`
}

func New(db *gorm.DB) (*Handler, error) {
	if err := db.AutoMigrate(&Product{}); err != nil {
		return nil, err
	}
	return &Handler{db: db}, nil
}

func (h *Handler) RegisterRoutes(group *gin.RouterGroup, authenticate gin.HandlerFunc) {
	group.GET("/products", h.list)
	group.GET("/products/:id", h.get)
	protected := group.Group("/products", authenticate)
	protected.POST("", h.create)
	protected.PUT("/:id", h.update)
	protected.DELETE("/:id", h.delete)
	protected.POST("/:id/publish", h.publish)
	protected.POST("/:id/off-shelf", h.offShelf)
}

func (h *Handler) create(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req productRequest
	if err := c.ShouldBindJSON(&req); err != nil || validateRequest(&req) != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "商品信息不符合要求")
		return
	}
	product := Product{
		ID: uuid.NewString(), SellerID: user.ID, Status: StatusDraft,
		Title: strings.TrimSpace(req.Title), Description: strings.TrimSpace(req.Description),
		PriceCents: req.PriceCents, IPName: strings.TrimSpace(req.IPName), Category: strings.TrimSpace(req.Category),
		Condition: strings.TrimSpace(req.Condition), TransactionType: normalizedTransactionType(req.TransactionType),
		ShippingOrigin: strings.TrimSpace(req.ShippingOrigin), Stock: req.Stock, PreorderNote: strings.TrimSpace(req.PreorderNote),
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&product).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "创建商品失败")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"product": product})
}

func (h *Handler) list(c *gin.Context) {
	page := positiveInt(c.Query("page"), 1)
	pageSize := min(positiveInt(c.Query("page_size"), 20), 100)
	query := h.db.WithContext(c.Request.Context()).Model(&Product{}).Where("status = ?", StatusPublished)
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("title LIKE ? OR description LIKE ?", like, like)
	}
	for key, column := range map[string]string{"ip_name": "ip_name", "category": "category", "condition": "`condition`", "transaction_type": "transaction_type"} {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	if value, err := strconv.ParseInt(c.Query("min_price_cents"), 10, 64); err == nil {
		query = query.Where("price_cents >= ?", value)
	}
	if value, err := strconv.ParseInt(c.Query("max_price_cents"), 10, 64); err == nil {
		query = query.Where("price_cents <= ?", value)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询商品失败")
		return
	}
	items := make([]Product, 0)
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询商品失败")
		return
	}
	c.JSON(http.StatusOK, listResponse{Items: items, Page: page, PageSize: pageSize, Total: total})
}

func (h *Handler) get(c *gin.Context) {
	var product Product
	if err := h.db.WithContext(c.Request.Context()).First(&product, "id = ? AND status = ?", c.Param("id"), StatusPublished).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			abort(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "商品不存在")
			return
		}
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询商品失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"product": product})
}

func (h *Handler) update(c *gin.Context) {
	product, ok := h.ownedProduct(c)
	if !ok {
		return
	}
	if product.Status == StatusOffShelf {
		abort(c, http.StatusConflict, "INVALID_PRODUCT_STATUS", "已下架商品不能编辑")
		return
	}
	var req productRequest
	if err := c.ShouldBindJSON(&req); err != nil || validateRequest(&req) != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "商品信息不符合要求")
		return
	}
	updates := map[string]any{
		"title": strings.TrimSpace(req.Title), "description": strings.TrimSpace(req.Description), "price_cents": req.PriceCents,
		"ip_name": strings.TrimSpace(req.IPName), "category": strings.TrimSpace(req.Category), "condition": strings.TrimSpace(req.Condition),
		"transaction_type": normalizedTransactionType(req.TransactionType), "shipping_origin": strings.TrimSpace(req.ShippingOrigin),
		"stock": req.Stock, "preorder_note": strings.TrimSpace(req.PreorderNote),
	}
	if err := h.db.WithContext(c.Request.Context()).Model(product).Updates(updates).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "更新商品失败")
		return
	}
	h.db.WithContext(c.Request.Context()).First(product, "id = ?", product.ID)
	c.JSON(http.StatusOK, gin.H{"product": product})
}

func (h *Handler) delete(c *gin.Context) {
	product, ok := h.ownedProduct(c)
	if !ok {
		return
	}
	if product.Status != StatusDraft {
		abort(c, http.StatusConflict, "INVALID_PRODUCT_STATUS", "只能删除草稿商品")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(product).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "删除商品失败")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) publish(c *gin.Context) {
	h.transition(c, StatusDraft, StatusPublished)
}

func (h *Handler) offShelf(c *gin.Context) {
	h.transition(c, StatusPublished, StatusOffShelf)
}

func (h *Handler) transition(c *gin.Context, from, to string) {
	product, ok := h.ownedProduct(c)
	if !ok {
		return
	}
	if !validTransition(product.Status, to) || product.Status != from {
		abort(c, http.StatusConflict, "INVALID_PRODUCT_STATUS", "商品状态不允许此操作")
		return
	}
	result := h.db.WithContext(c.Request.Context()).Model(&Product{}).Where("id = ? AND seller_id = ? AND status = ?", product.ID, product.SellerID, from).Update("status", to)
	if result.Error != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "更新商品状态失败")
		return
	}
	if result.RowsAffected != 1 {
		abort(c, http.StatusConflict, "INVALID_PRODUCT_STATUS", "商品状态已发生变化")
		return
	}
	product.Status = to
	c.JSON(http.StatusOK, gin.H{"product": product})
}

func (h *Handler) ownedProduct(c *gin.Context) (*Product, bool) {
	user, _ := auth.CurrentUser(c)
	var product Product
	err := h.db.WithContext(c.Request.Context()).First(&product, "id = ?", c.Param("id")).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		abort(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "商品不存在")
		return nil, false
	}
	if err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询商品失败")
		return nil, false
	}
	if product.SellerID != user.ID {
		abort(c, http.StatusForbidden, "FORBIDDEN", "不能操作其他用户的商品")
		return nil, false
	}
	return &product, true
}

func validateRequest(req *productRequest) error {
	title := strings.TrimSpace(req.Title)
	if len([]rune(title)) < 2 || len([]rune(title)) > 120 || strings.TrimSpace(req.Description) == "" || req.PriceCents <= 0 || req.PriceCents > 100000000 || strings.TrimSpace(req.IPName) == "" || strings.TrimSpace(req.Category) == "" || strings.TrimSpace(req.Condition) == "" || req.Stock < 0 {
		return errInvalidProduct
	}
	transactionType := normalizedTransactionType(req.TransactionType)
	if transactionType != "SALE" && transactionType != "PREORDER" && transactionType != "EXCHANGE" {
		return errInvalidProduct
	}
	return nil
}

func normalizedTransactionType(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "SALE"
	}
	return value
}

func validTransition(from, to string) bool {
	return (from == StatusDraft && to == StatusPublished) || (from == StatusPublished && to == StatusOffShelf)
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
