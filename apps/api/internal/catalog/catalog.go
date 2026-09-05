package catalog

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aigc-3d-platform/apps/api/internal/asset"
	"aigc-3d-platform/apps/api/internal/auth"
	"aigc-3d-platform/apps/api/internal/generation"
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
	CoverAssetID    *string   `gorm:"type:char(36);index" json:"cover_asset_id,omitempty"`
	ModelAssetID    *string   `gorm:"type:char(36);index" json:"model_asset_id,omitempty"`
	Status          string    `gorm:"size:16;not null;default:DRAFT;index" json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ProductAsset struct {
	ProductID string    `gorm:"type:char(36);primaryKey" json:"product_id"`
	AssetID   string    `gorm:"type:char(36);primaryKey" json:"asset_id"`
	Kind      string    `gorm:"size:16;not null;index" json:"kind"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

func (ProductAsset) TableName() string { return "product_assets" }

type Handler struct {
	db          *gorm.DB
	assets      *asset.Service
	jobs        *generation.Service
	resolveUser func(*gin.Context) (*auth.User, bool)
}

type productView struct {
	Product
	Images []asset.Public `json:"images"`
	Model  *asset.Public  `json:"model,omitempty"`
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
	GenerationJobID string `json:"generation_job_id"`
}

func New(db *gorm.DB, assets *asset.Service, jobs *generation.Service) (*Handler, error) {
	if err := db.AutoMigrate(&Product{}, &ProductAsset{}); err != nil {
		return nil, err
	}
	return &Handler{db: db, assets: assets, jobs: jobs}, nil
}

func (h *Handler) RegisterRoutes(group *gin.RouterGroup, authenticate gin.HandlerFunc, resolveUser func(*gin.Context) (*auth.User, bool)) {
	h.resolveUser = resolveUser
	group.GET("/products/mine", authenticate, h.listMine)
	group.GET("/products", h.list)
	group.GET("/products/:id", h.get)
	group.GET("/products/:id/assets/:asset_id/content", h.getContent)
	protected := group.Group("/products", authenticate)
	protected.POST("", h.create)
	protected.PUT("/:id", h.update)
	protected.DELETE("/:id", h.delete)
	protected.POST("/:id/publish", h.publish)
	protected.POST("/:id/off-shelf", h.offShelf)
	protected.POST("/:id/images", h.uploadImage)
	protected.POST("/:id/model", h.uploadModel)
	protected.DELETE("/:id/assets/:asset_id", h.deleteAsset)
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
	if jobID := strings.TrimSpace(req.GenerationJobID); jobID != "" {
		if err := h.attachGenerationModel(c, &product, jobID); err != nil {
			_ = h.db.WithContext(c.Request.Context()).Delete(&Product{}, "id = ?", product.ID).Error
			abortAttachError(c, err)
			return
		}
	}
	c.JSON(http.StatusCreated, gin.H{"product": h.view(c, product)})
}

func (h *Handler) list(c *gin.Context) {
	page := positiveInt(c.Query("page"), 1)
	pageSize := min(positiveInt(c.Query("page_size"), 20), 100)
	query := h.db.WithContext(c.Request.Context()).Model(&Product{}).Where("status = ?", StatusPublished)
	if keyword := escapeLikePattern(strings.TrimSpace(c.Query("keyword"))); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(`title LIKE ? ESCAPE '\\' OR description LIKE ? ESCAPE '\\'`, like, like)
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
	c.JSON(http.StatusOK, gin.H{"items": h.views(c, items), "page": page, "page_size": pageSize, "total": total})
}

func (h *Handler) listMine(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	page := positiveInt(c.Query("page"), 1)
	pageSize := min(positiveInt(c.Query("page_size"), 20), 100)
	query := h.db.WithContext(c.Request.Context()).Model(&Product{}).Where("seller_id = ?", user.ID)
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
	c.JSON(http.StatusOK, gin.H{"items": h.views(c, items), "page": page, "page_size": pageSize, "total": total})
}

func (h *Handler) get(c *gin.Context) {
	var product Product
	if err := h.db.WithContext(c.Request.Context()).First(&product, "id = ?", c.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			abort(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "商品不存在")
			return
		}
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询商品失败")
		return
	}
	if product.Status != StatusPublished {
		user, ok := h.currentUser(c)
		if !ok || product.SellerID != user.ID {
			abort(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "商品不存在")
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"product": h.view(c, product)})
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
	c.JSON(http.StatusOK, gin.H{"product": h.view(c, *product)})
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
	if err := h.deleteBoundAssets(c, product); err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "删除商品资产失败")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(product).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "删除商品失败")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) publish(c *gin.Context) {
	product, ok := h.ownedProduct(c)
	if !ok {
		return
	}
	if product.Status == StatusOffShelf {
		h.transitionProduct(c, product, StatusOffShelf, StatusPublished)
		return
	}
	if count, err := h.assetCount(c, product.ID, asset.KindImage); err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询商品资产失败")
		return
	} else if count < 1 {
		abort(c, http.StatusBadRequest, "ASSET_REQUIRED", "发布商品前至少需要上传 1 张图片")
		return
	}
	h.transitionProduct(c, product, StatusDraft, StatusPublished)
}

func (h *Handler) offShelf(c *gin.Context) {
	product, ok := h.ownedProduct(c)
	if !ok {
		return
	}
	h.transitionProduct(c, product, StatusPublished, StatusOffShelf)
}

func (h *Handler) transitionProduct(c *gin.Context, product *Product, from, to string) {
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
	c.JSON(http.StatusOK, gin.H{"product": h.view(c, *product)})
}

func (h *Handler) uploadImage(c *gin.Context) {
	h.upload(c, asset.KindImage, 6, asset.MaxImageBytes)
}

func (h *Handler) uploadModel(c *gin.Context) {
	h.upload(c, asset.KindModel, 1, asset.MaxModelBytes)
}

func (h *Handler) upload(c *gin.Context, kind string, limit int, maxBytes int64) {
	product, ok := h.ownedProduct(c)
	if !ok {
		return
	}
	if product.Status == StatusOffShelf {
		abort(c, http.StatusConflict, "INVALID_PRODUCT_STATUS", "已下架商品不能上传资产")
		return
	}
	count, err := h.assetCount(c, product.ID, kind)
	if err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询商品资产失败")
		return
	}
	if kind != asset.KindModel && int(count) >= limit {
		abort(c, http.StatusConflict, "ASSET_LIMIT_EXCEEDED", "超出允许的文件数量")
		return
	}
	filename, declaredMIME, data, ok := readUpload(c, maxBytes)
	if !ok {
		return
	}
	previousModelID := ""
	if kind == asset.KindModel && product.ModelAssetID != nil {
		previousModelID = *product.ModelAssetID
	}
	stored, err := h.assets.Put(c.Request.Context(), product.SellerID, product.ID, kind, filename, declaredMIME, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		abortUploadError(c, err)
		return
	}
	if previousModelID != "" {
		if err := h.removeAsset(c, product, previousModelID); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			_ = h.assets.Delete(c.Request.Context(), stored.ID)
			abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "替换模型失败")
			return
		}
		count = 0
	}
	link := ProductAsset{ProductID: product.ID, AssetID: stored.ID, Kind: kind, SortOrder: int(count)}
	if err := h.db.WithContext(c.Request.Context()).Create(&link).Error; err != nil {
		_ = h.assets.Delete(c.Request.Context(), stored.ID)
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "保存资产记录失败")
		return
	}
	updates := map[string]any{}
	if kind == asset.KindImage && product.CoverAssetID == nil {
		updates["cover_asset_id"] = stored.ID
	}
	if kind == asset.KindModel {
		updates["model_asset_id"] = stored.ID
	}
	if len(updates) > 0 {
		h.db.WithContext(c.Request.Context()).Model(product).Updates(updates)
		h.db.WithContext(c.Request.Context()).First(product, "id = ?", product.ID)
	}
	c.JSON(http.StatusCreated, gin.H{"asset": stored.Public(product.ID), "product": h.view(c, *product)})
}

func (h *Handler) deleteAsset(c *gin.Context) {
	product, ok := h.ownedProduct(c)
	if !ok {
		return
	}
	if err := h.removeAsset(c, product, c.Param("asset_id")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			abort(c, http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在")
			return
		}
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "删除资产失败")
		return
	}
	h.db.WithContext(c.Request.Context()).First(product, "id = ?", product.ID)
	c.JSON(http.StatusOK, gin.H{"product": h.view(c, *product)})
}

func (h *Handler) getContent(c *gin.Context) {
	var product Product
	if err := h.db.WithContext(c.Request.Context()).First(&product, "id = ?", c.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			abort(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "商品不存在")
			return
		}
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询商品失败")
		return
	}
	if product.Status != StatusPublished {
		abort(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "商品不存在")
		return
	}
	var link ProductAsset
	if err := h.db.WithContext(c.Request.Context()).First(&link, "product_id = ? AND asset_id = ?", product.ID, c.Param("asset_id")).Error; err != nil {
		abort(c, http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在")
		return
	}
	body, info, stored, err := h.assets.Open(c.Request.Context(), link.AssetID)
	if err != nil {
		abort(c, http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在")
		return
	}
	defer body.Close()
	c.Header("Content-Type", info.ContentType)
	c.Header("Content-Length", strconv.FormatInt(info.Size, 10))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("Content-Disposition", "inline; filename=\""+stored.OriginalName+"\"")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, body)
}

func (h *Handler) attachGenerationModel(c *gin.Context, product *Product, jobID string) error {
	if h.jobs == nil {
		return generation.ErrJobNotFound
	}
	source, err := h.jobs.ReadyModel(c.Request.Context(), product.SellerID, jobID)
	if err != nil {
		return err
	}
	copied, err := h.assets.Copy(c.Request.Context(), source.ID, product.SellerID, product.ID)
	if err != nil {
		return err
	}
	link := ProductAsset{ProductID: product.ID, AssetID: copied.ID, Kind: asset.KindModel, SortOrder: 0}
	if err := h.db.WithContext(c.Request.Context()).Create(&link).Error; err != nil {
		_ = h.assets.Delete(c.Request.Context(), copied.ID)
		return err
	}
	if err := h.db.WithContext(c.Request.Context()).Model(product).Update("model_asset_id", copied.ID).Error; err != nil {
		_ = h.removeAsset(c, product, copied.ID)
		return err
	}
	product.ModelAssetID = &copied.ID
	return nil
}

func abortAttachError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, generation.ErrJobNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		abort(c, http.StatusNotFound, "GENERATION_JOB_NOT_FOUND", "生成任务不存在或没有可发布的模型")
	case errors.Is(err, generation.ErrOutputNotReady):
		abort(c, http.StatusConflict, "GENERATION_OUTPUT_NOT_READY", "生成任务尚未完成，不能带入发布")
	case errors.Is(err, asset.ErrInvalidFile), errors.Is(err, asset.ErrUnsupportedType), errors.Is(err, asset.ErrKindMismatch):
		abort(c, http.StatusBadRequest, "INVALID_FILE", "生成结果不是可发布的 GLB")
	case errors.Is(err, asset.ErrFileTooLarge):
		abort(c, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "文件超过大小限制")
	default:
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "带入生成模型失败")
	}
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
	return (from == StatusDraft && to == StatusPublished) ||
		(from == StatusPublished && to == StatusOffShelf) ||
		(from == StatusOffShelf && to == StatusPublished)
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func escapeLikePattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\`+`\`)
	value = strings.ReplaceAll(value, "%", `\`+"%")
	return strings.ReplaceAll(value, "_", `\`+"_")
}

func (h *Handler) currentUser(c *gin.Context) (*auth.User, bool) {
	if user, ok := auth.CurrentUser(c); ok {
		return user, true
	}
	if h.resolveUser == nil {
		return nil, false
	}
	return h.resolveUser(c)
}

func (h *Handler) view(c *gin.Context, product Product) productView {
	return h.views(c, []Product{product})[0]
}

func (h *Handler) views(c *gin.Context, products []Product) []productView {
	views := make([]productView, 0, len(products))
	if len(products) == 0 {
		return views
	}
	ids := make([]string, 0, len(products))
	for _, product := range products {
		ids = append(ids, product.ID)
	}
	var links []ProductAsset
	_ = h.db.WithContext(c.Request.Context()).Where("product_id IN ?", ids).Order("sort_order ASC, created_at ASC").Find(&links).Error
	assetIDs := make([]string, 0, len(links))
	for _, link := range links {
		assetIDs = append(assetIDs, link.AssetID)
	}
	assetsByID := map[string]asset.Asset{}
	if len(assetIDs) > 0 {
		var stored []asset.Asset
		_ = h.db.WithContext(c.Request.Context()).Where("id IN ?", assetIDs).Find(&stored).Error
		for _, item := range stored {
			assetsByID[item.ID] = item
		}
	}
	grouped := map[string][]ProductAsset{}
	for _, link := range links {
		grouped[link.ProductID] = append(grouped[link.ProductID], link)
	}
	for _, product := range products {
		view := productView{Product: product, Images: make([]asset.Public, 0)}
		for _, link := range grouped[product.ID] {
			stored, ok := assetsByID[link.AssetID]
			if !ok {
				continue
			}
			public := stored.Public(product.ID)
			if link.Kind == asset.KindModel {
				copied := public
				view.Model = &copied
				continue
			}
			view.Images = append(view.Images, public)
		}
		views = append(views, view)
	}
	return views
}

func (h *Handler) assetCount(c *gin.Context, productID, kind string) (int64, error) {
	var count int64
	err := h.db.WithContext(c.Request.Context()).Model(&ProductAsset{}).Where("product_id = ? AND kind = ?", productID, kind).Count(&count).Error
	return count, err
}

func (h *Handler) removeAsset(c *gin.Context, product *Product, assetID string) error {
	var link ProductAsset
	if err := h.db.WithContext(c.Request.Context()).First(&link, "product_id = ? AND asset_id = ?", product.ID, assetID).Error; err != nil {
		return err
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(&link).Error; err != nil {
		return err
	}
	updates := map[string]any{}
	if product.CoverAssetID != nil && *product.CoverAssetID == assetID {
		var next ProductAsset
		if err := h.db.WithContext(c.Request.Context()).Where("product_id = ? AND kind = ?", product.ID, asset.KindImage).Order("sort_order ASC, created_at ASC").First(&next).Error; err == nil {
			updates["cover_asset_id"] = next.AssetID
		} else {
			updates["cover_asset_id"] = gorm.Expr("NULL")
		}
	}
	if product.ModelAssetID != nil && *product.ModelAssetID == assetID {
		updates["model_asset_id"] = gorm.Expr("NULL")
	}
	if len(updates) > 0 {
		if err := h.db.WithContext(c.Request.Context()).Model(product).Updates(updates).Error; err != nil {
			return err
		}
	}
	return h.assets.Delete(c.Request.Context(), assetID)
}

func (h *Handler) deleteBoundAssets(c *gin.Context, product *Product) error {
	var links []ProductAsset
	if err := h.db.WithContext(c.Request.Context()).Where("product_id = ?", product.ID).Find(&links).Error; err != nil {
		return err
	}
	for _, link := range links {
		if err := h.removeAsset(c, product, link.AssetID); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	return nil
}

func readUpload(c *gin.Context, maxBytes int64) (filename, declaredMIME string, data []byte, ok bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+512)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "请上传文件")
		return "", "", nil, false
	}
	defer file.Close()
	if header.Size > maxBytes {
		abort(c, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "文件超过大小限制")
		return "", "", nil, false
	}
	limited := io.LimitReader(file, maxBytes+1)
	data, err = io.ReadAll(limited)
	if err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "读取文件失败")
		return "", "", nil, false
	}
	if int64(len(data)) > maxBytes {
		abort(c, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "文件超过大小限制")
		return "", "", nil, false
	}
	filename = filepath.Base(header.Filename)
	declaredMIME = header.Header.Get("Content-Type")
	return filename, declaredMIME, data, true
}

func abortUploadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, asset.ErrInvalidFile), errors.Is(err, asset.ErrUnsupportedType), errors.Is(err, asset.ErrKindMismatch):
		abort(c, http.StatusBadRequest, "INVALID_FILE", "文件类型、扩展名或文件头不符合要求")
	case errors.Is(err, asset.ErrFileTooLarge):
		abort(c, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "文件超过大小限制")
	default:
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "上传文件失败")
	}
}

func abort(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message, "request_id": c.GetString("request_id")}})
}
