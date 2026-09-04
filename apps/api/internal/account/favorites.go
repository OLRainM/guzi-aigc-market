package account

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"aigc-3d-platform/apps/api/internal/auth"
	"aigc-3d-platform/apps/api/internal/catalog"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type favoriteRequest struct {
	ProductID string `json:"product_id"`
	Folder    string `json:"folder"`
	Note      string `json:"note"`
}

type favoritePatchRequest struct {
	Folder *string `json:"folder"`
	Note   *string `json:"note"`
}

type batchIDsRequest struct {
	IDs []string `json:"ids"`
}

type favoriteView struct {
	Favorite
	Status          string `json:"status"`
	StatusLabel     string `json:"status_label"`
	CurrentTitle    string `json:"current_title,omitempty"`
	CurrentPriceCents int64 `json:"current_price_cents,omitempty"`
	CurrentStatus   string `json:"current_status,omitempty"`
	CoverURL        string `json:"cover_url,omitempty"`
	Available       bool   `json:"available"`
}

func favoriteStatusOf(item Favorite, product *catalog.Product, found bool) (status, label string) {
	if !found {
		return FavoriteStatusInvalid, "商品已失效"
	}
	if product.Status != catalog.StatusPublished {
		return FavoriteStatusUnavailable, "商品已下架或不可购买"
	}
	if product.Title != item.SnapshotTitle || product.PriceCents != item.SnapshotPriceCents {
		return FavoriteStatusUpdated, "商品信息已更新"
	}
	return FavoriteStatusActive, "收藏有效"
}

func (h *Handler) viewsForFavorites(c *gin.Context, items []Favorite) []favoriteView {
	views := make([]favoriteView, 0, len(items))
	if len(items) == 0 {
		return views
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ProductID)
	}
	var products []catalog.Product
	_ = h.db.WithContext(c.Request.Context()).Where("id IN ?", ids).Find(&products).Error
	byID := map[string]catalog.Product{}
	for _, product := range products {
		byID[product.ID] = product
	}
	for _, item := range items {
		product, found := byID[item.ProductID]
		status, label := favoriteStatusOf(item, &product, found)
		view := favoriteView{
			Favorite: item, Status: status, StatusLabel: label, Available: found && product.Status == catalog.StatusPublished,
		}
		if found {
			view.CurrentTitle = product.Title
			view.CurrentPriceCents = product.PriceCents
			view.CurrentStatus = product.Status
			view.CoverURL = coverURL(product)
		}
		views = append(views, view)
	}
	return views
}

func (h *Handler) listFavorites(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	page := positiveInt(c.Query("page"), 1)
	pageSize := min(positiveInt(c.Query("page_size"), 20), 100)
	query := h.db.WithContext(c.Request.Context()).Model(&Favorite{}).Where("user_id = ?", user.ID)
	if folder := strings.TrimSpace(c.Query("folder")); folder != "" {
		query = query.Where("folder = ?", folder)
	}
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("snapshot_title LIKE ? OR snapshot_ip_name LIKE ? OR snapshot_category LIKE ?", like, like, like)
	}
	statusFilter := strings.ToUpper(strings.TrimSpace(c.Query("status")))
	var items []Favorite
	if err := query.Order("created_at DESC").Find(&items).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询收藏失败")
		return
	}
	views := h.viewsForFavorites(c, items)
	if statusFilter != "" {
		filtered := make([]favoriteView, 0)
		for _, view := range views {
			if view.Status == statusFilter {
				filtered = append(filtered, view)
			}
		}
		views = filtered
	}
	total := len(views)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)
	c.JSON(http.StatusOK, gin.H{"items": views[start:end], "page": page, "page_size": pageSize, "total": total})
}

func (h *Handler) listFavoriteFolders(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	type row struct {
		Folder string `json:"folder"`
		Count  int64  `json:"count"`
	}
	rows := make([]row, 0)
	if err := h.db.WithContext(c.Request.Context()).Model(&Favorite{}).Select("folder, count(*) as count").Where("user_id = ?", user.ID).Group("folder").Order("folder ASC").Scan(&rows).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询收藏分类失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}

func (h *Handler) favoriteStatus(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	productID := strings.TrimSpace(c.Query("product_id"))
	if productID == "" {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "缺少商品 ID")
		return
	}
	var item Favorite
	err := h.db.WithContext(c.Request.Context()).Where("user_id = ? AND product_id = ?", user.ID, productID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"favorited": false})
		return
	}
	if err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询收藏失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"favorited": true, "favorite": h.viewsForFavorites(c, []Favorite{item})[0]})
}

func (h *Handler) addFavorite(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req favoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ProductID) == "" {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "请选择要收藏的商品")
		return
	}
	product, err := h.loadProduct(c, strings.TrimSpace(req.ProductID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			abort(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "商品不存在")
			return
		}
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询商品失败")
		return
	}
	if product.Status != catalog.StatusPublished {
		abort(c, http.StatusConflict, "PRODUCT_UNAVAILABLE", "只能收藏已发布商品")
		return
	}
	pref := h.ensurePreference(c, user.ID)
	folder := strings.TrimSpace(req.Folder)
	if folder == "" {
		folder = pref.DefaultFavoriteFolder
	}
	if len([]rune(folder)) > 40 {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "分类名称过长")
		return
	}
	item := Favorite{
		ID: uuid.NewString(), UserID: user.ID, ProductID: product.ID, Folder: folder,
		Note: truncate(req.Note, 200), SnapshotTitle: product.Title, SnapshotPriceCents: product.PriceCents,
		SnapshotStatus: product.Status, SnapshotCategory: product.Category, SnapshotIPName: product.IPName,
	}
	var existing Favorite
	if err := h.db.WithContext(c.Request.Context()).Where("user_id = ? AND product_id = ?", user.ID, product.ID).First(&existing).Error; err == nil {
		abort(c, http.StatusConflict, "ALREADY_FAVORITED", "该商品已在收藏中")
		return
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "收藏失败")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&item).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "收藏失败")
		return
	}
	h.recordActivity(c, user.ID, ActivityFavoriteAdd, "product", product.ID, product.Title)
	c.JSON(http.StatusCreated, gin.H{"favorite": h.viewsForFavorites(c, []Favorite{item})[0]})
}

func (h *Handler) ownedFavorite(c *gin.Context) (*Favorite, bool) {
	user, _ := auth.CurrentUser(c)
	var item Favorite
	if err := h.db.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", c.Param("id"), user.ID).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			abort(c, http.StatusNotFound, "FAVORITE_NOT_FOUND", "收藏不存在")
			return nil, false
		}
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询收藏失败")
		return nil, false
	}
	return &item, true
}

func (h *Handler) updateFavorite(c *gin.Context) {
	item, ok := h.ownedFavorite(c)
	if !ok {
		return
	}
	var req favoritePatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "请求格式无效")
		return
	}
	updates := map[string]any{}
	if req.Folder != nil {
		folder := strings.TrimSpace(*req.Folder)
		if folder == "" || len([]rune(folder)) > 40 {
			abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "分类名称无效")
			return
		}
		updates["folder"] = folder
	}
	if req.Note != nil {
		updates["note"] = truncate(*req.Note, 200)
	}
	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"favorite": h.viewsForFavorites(c, []Favorite{*item})[0]})
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(item).Updates(updates).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "更新收藏失败")
		return
	}
	h.db.WithContext(c.Request.Context()).First(item, "id = ?", item.ID)
	c.JSON(http.StatusOK, gin.H{"favorite": h.viewsForFavorites(c, []Favorite{*item})[0]})
}

func (h *Handler) removeFavorite(c *gin.Context) {
	item, ok := h.ownedFavorite(c)
	if !ok {
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(item).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "取消收藏失败")
		return
	}
	user, _ := auth.CurrentUser(c)
	h.recordActivity(c, user.ID, ActivityFavoriteRemove, "product", item.ProductID, item.SnapshotTitle)
	c.Status(http.StatusNoContent)
}

func (h *Handler) batchDeleteFavorites(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req batchIDsRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "请选择要取消的收藏")
		return
	}
	if len(req.IDs) > 50 {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "一次最多取消 50 条收藏")
		return
	}
	result := h.db.WithContext(c.Request.Context()).Where("user_id = ? AND id IN ?", user.ID, req.IDs).Delete(&Favorite{})
	if result.Error != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "批量取消收藏失败")
		return
	}
	h.recordActivity(c, user.ID, ActivityFavoriteRemove, "favorite", "", "批量取消 "+strconv.FormatInt(result.RowsAffected, 10)+" 条")
	c.JSON(http.StatusOK, gin.H{"deleted": result.RowsAffected})
}

func (h *Handler) ackFavorite(c *gin.Context) {
	item, ok := h.ownedFavorite(c)
	if !ok {
		return
	}
	product, err := h.loadProduct(c, item.ProductID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询商品失败")
		return
	}
	updates := map[string]any{"change_notified_at": nowPtr()}
	if product != nil {
		updates["snapshot_title"] = product.Title
		updates["snapshot_price_cents"] = product.PriceCents
		updates["snapshot_status"] = product.Status
		updates["snapshot_category"] = product.Category
		updates["snapshot_ip_name"] = product.IPName
	}
	if err := h.db.WithContext(c.Request.Context()).Model(item).Updates(updates).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "确认收藏状态失败")
		return
	}
	h.db.WithContext(c.Request.Context()).First(item, "id = ?", item.ID)
	c.JSON(http.StatusOK, gin.H{"favorite": h.viewsForFavorites(c, []Favorite{*item})[0]})
}
