package account

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"aigc-3d-platform/apps/api/internal/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var phonePattern = regexp.MustCompile(`^[0-9+\- ]{6,32}$`)

type profileRequest struct {
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	Phone       string `json:"phone"`
}

type addressRequest struct {
	Recipient  string `json:"recipient"`
	Phone      string `json:"phone"`
	Province   string `json:"province"`
	City       string `json:"city"`
	District   string `json:"district"`
	Detail     string `json:"detail"`
	PostalCode string `json:"postal_code"`
	IsDefault  bool   `json:"is_default"`
}

type preferenceRequest struct {
	NotifyFavoriteUpdates bool   `json:"notify_favorite_updates"`
	NotifyTradeEvents     bool   `json:"notify_trade_events"`
	NotifySystem          bool   `json:"notify_system"`
	DefaultFavoriteFolder string `json:"default_favorite_folder"`
	Locale                string `json:"locale"`
}

type activityRequest struct {
	Action     string `json:"action"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Detail     string `json:"detail"`
}

func (h *Handler) getProfile(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	profile := h.ensureProfile(c, user)
	pref := h.ensurePreference(c, user.ID)
	account := h.ensureSandbox(c, user.ID)
	var favoriteCount, unreadCount, addressCount int64
	h.db.WithContext(c.Request.Context()).Model(&Favorite{}).Where("user_id = ?", user.ID).Count(&favoriteCount)
	h.db.WithContext(c.Request.Context()).Model(&Notification{}).Where("user_id = ? AND read_at IS NULL", user.ID).Count(&unreadCount)
	h.db.WithContext(c.Request.Context()).Model(&Address{}).Where("user_id = ?", user.ID).Count(&addressCount)
	c.JSON(http.StatusOK, gin.H{
		"user": user, "profile": profile, "preferences": pref,
		"sandbox": gin.H{"cash_cents": account.CashCents, "generation": account.Generation, "reset_count": account.ResetCount},
		"stats": gin.H{"favorites": favoriteCount, "unread_notifications": unreadCount, "addresses": addressCount},
	})
}

func (h *Handler) updateProfile(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	profile := h.ensureProfile(c, user)
	var req profileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "资料格式无效")
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if len([]rune(name)) < 2 || len([]rune(name)) > 64 {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "显示名称需为 2–64 个字符")
		return
	}
	phone := strings.TrimSpace(req.Phone)
	if phone != "" && !phonePattern.MatchString(phone) {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "手机号格式无效")
		return
	}
	updates := map[string]any{"display_name": name, "bio": truncate(req.Bio, 280), "phone": phone}
	if err := h.db.WithContext(c.Request.Context()).Model(&profile).Updates(updates).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "更新资料失败")
		return
	}
	h.db.WithContext(c.Request.Context()).First(&profile, "user_id = ?", user.ID)
	h.recordActivity(c, user.ID, ActivityProfileUpdate, "profile", user.ID, name)
	c.JSON(http.StatusOK, gin.H{"profile": profile})
}

func (h *Handler) listAddresses(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	items := make([]Address, 0)
	if err := h.db.WithContext(c.Request.Context()).Where("user_id = ?", user.ID).Order("is_default DESC, created_at DESC").Find(&items).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询地址失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func validateAddress(req *addressRequest) bool {
	return strings.TrimSpace(req.Recipient) != "" && phonePattern.MatchString(strings.TrimSpace(req.Phone)) &&
		strings.TrimSpace(req.Province) != "" && strings.TrimSpace(req.City) != "" && strings.TrimSpace(req.Detail) != "" &&
		len([]rune(strings.TrimSpace(req.Detail))) <= 200
}

func addressFromRequest(userID string, req addressRequest) Address {
	return Address{
		UserID: userID, Recipient: strings.TrimSpace(req.Recipient), Phone: strings.TrimSpace(req.Phone),
		Province: strings.TrimSpace(req.Province), City: strings.TrimSpace(req.City), District: strings.TrimSpace(req.District),
		Detail: strings.TrimSpace(req.Detail), PostalCode: strings.TrimSpace(req.PostalCode), IsDefault: req.IsDefault,
	}
}

func (h *Handler) createAddress(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req addressRequest
	if err := c.ShouldBindJSON(&req); err != nil || !validateAddress(&req) {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "收货地址不完整")
		return
	}
	item := addressFromRequest(user.ID, req)
	item.ID = uuid.NewString()
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if item.IsDefault {
			if err := tx.Model(&Address{}).Where("user_id = ?", user.ID).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(&item).Error
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "保存地址失败")
		return
	}
	h.recordActivity(c, user.ID, ActivityAddressSave, "address", item.ID, item.Recipient)
	c.JSON(http.StatusCreated, gin.H{"address": item})
}

func (h *Handler) ownedAddress(c *gin.Context) (*Address, bool) {
	user, _ := auth.CurrentUser(c)
	var item Address
	if err := h.db.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", c.Param("id"), user.ID).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			abort(c, http.StatusNotFound, "ADDRESS_NOT_FOUND", "地址不存在")
			return nil, false
		}
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询地址失败")
		return nil, false
	}
	return &item, true
}

func (h *Handler) updateAddress(c *gin.Context) {
	item, ok := h.ownedAddress(c)
	if !ok {
		return
	}
	var req addressRequest
	if err := c.ShouldBindJSON(&req); err != nil || !validateAddress(&req) {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "收货地址不完整")
		return
	}
	next := addressFromRequest(item.UserID, req)
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if next.IsDefault {
			if err := tx.Model(&Address{}).Where("user_id = ?", item.UserID).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Model(item).Updates(map[string]any{
			"recipient": next.Recipient, "phone": next.Phone, "province": next.Province, "city": next.City,
			"district": next.District, "detail": next.Detail, "postal_code": next.PostalCode, "is_default": next.IsDefault,
		}).Error
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "更新地址失败")
		return
	}
	h.db.WithContext(c.Request.Context()).First(item, "id = ?", item.ID)
	h.recordActivity(c, item.UserID, ActivityAddressSave, "address", item.ID, item.Recipient)
	c.JSON(http.StatusOK, gin.H{"address": item})
}

func (h *Handler) deleteAddress(c *gin.Context) {
	item, ok := h.ownedAddress(c)
	if !ok {
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(item).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "删除地址失败")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) setDefaultAddress(c *gin.Context) {
	item, ok := h.ownedAddress(c)
	if !ok {
		return
	}
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Address{}).Where("user_id = ?", item.UserID).Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(item).Update("is_default", true).Error
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "设置默认地址失败")
		return
	}
	item.IsDefault = true
	c.JSON(http.StatusOK, gin.H{"address": item})
}

func (h *Handler) getPreferences(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	c.JSON(http.StatusOK, gin.H{"preferences": h.ensurePreference(c, user.ID)})
}

func (h *Handler) updatePreferences(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	pref := h.ensurePreference(c, user.ID)
	var req preferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "偏好设置无效")
		return
	}
	folder := strings.TrimSpace(req.DefaultFavoriteFolder)
	if folder == "" {
		folder = DefaultFavoriteFolder
	}
	if len([]rune(folder)) > 40 {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "默认分类过长")
		return
	}
	locale := strings.TrimSpace(req.Locale)
	if locale == "" {
		locale = "zh-CN"
	}
	updates := map[string]any{
		"notify_favorite_updates": req.NotifyFavoriteUpdates, "notify_trade_events": req.NotifyTradeEvents,
		"notify_system": req.NotifySystem, "default_favorite_folder": folder, "locale": locale,
	}
	if err := h.db.WithContext(c.Request.Context()).Model(&pref).Updates(updates).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "保存偏好失败")
		return
	}
	h.db.WithContext(c.Request.Context()).First(&pref, "user_id = ?", user.ID)
	c.JSON(http.StatusOK, gin.H{"preferences": pref})
}

func (h *Handler) listNotifications(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	page := positiveInt(c.Query("page"), 1)
	pageSize := min(positiveInt(c.Query("page_size"), 20), 100)
	query := h.db.WithContext(c.Request.Context()).Model(&Notification{}).Where("user_id = ?", user.ID)
	if c.Query("unread") == "true" {
		query = query.Where("read_at IS NULL")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询通知失败")
		return
	}
	items := make([]Notification, 0)
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询通知失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": page, "page_size": pageSize, "total": total})
}

func (h *Handler) readNotification(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	result := h.db.WithContext(c.Request.Context()).Model(&Notification{}).Where("id = ? AND user_id = ? AND read_at IS NULL", c.Param("id"), user.ID).Update("read_at", nowPtr())
	if result.Error != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "标记通知失败")
		return
	}
	if result.RowsAffected == 0 {
		abort(c, http.StatusNotFound, "NOTIFICATION_NOT_FOUND", "通知不存在或已读")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) readAllNotifications(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	if err := h.db.WithContext(c.Request.Context()).Model(&Notification{}).Where("user_id = ? AND read_at IS NULL", user.ID).Update("read_at", nowPtr()).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "标记通知失败")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) listActivities(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	page := positiveInt(c.Query("page"), 1)
	pageSize := min(positiveInt(c.Query("page_size"), 20), 100)
	query := h.db.WithContext(c.Request.Context()).Model(&Activity{}).Where("user_id = ?", user.ID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询历史失败")
		return
	}
	items := make([]Activity, 0)
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "查询历史失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": page, "page_size": pageSize, "total": total})
}

func (h *Handler) createActivity(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req activityRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Action != ActivityViewProduct || strings.TrimSpace(req.TargetID) == "" {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "仅支持记录商品浏览")
		return
	}
	h.recordActivity(c, user.ID, ActivityViewProduct, "product", strings.TrimSpace(req.TargetID), truncate(req.Detail, 240))
	c.Status(http.StatusNoContent)
}
