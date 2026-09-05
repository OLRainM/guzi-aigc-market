package account

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"aigc-3d-platform/apps/api/internal/auth"
	"aigc-3d-platform/apps/api/internal/catalog"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Handler struct {
	db *gorm.DB
}

func New(db *gorm.DB) (*Handler, error) {
	if err := db.AutoMigrate(
		&Profile{}, &Address{}, &Preference{}, &Favorite{},
		&Notification{}, &Activity{}, &SandboxAccount{}, &SandboxHolding{}, &SandboxOrder{},
		&Order{}, &OrderEvent{},
	); err != nil {
		return nil, err
	}
	return &Handler{db: db}, nil
}

func (h *Handler) RegisterRoutes(group *gin.RouterGroup, authenticate gin.HandlerFunc) {
	protected := group.Group("", authenticate)
	protected.GET("/favorites", h.listFavorites)
	protected.GET("/favorites/folders", h.listFavoriteFolders)
	protected.GET("/favorites/status", h.favoriteStatus)
	protected.POST("/favorites", h.addFavorite)
	protected.PATCH("/favorites/:id", h.updateFavorite)
	protected.DELETE("/favorites/:id", h.removeFavorite)
	protected.POST("/favorites/batch-delete", h.batchDeleteFavorites)
	protected.POST("/favorites/:id/ack", h.ackFavorite)

	protected.GET("/me/profile", h.getProfile)
	protected.PUT("/me/profile", h.updateProfile)
	protected.GET("/me/addresses", h.listAddresses)
	protected.POST("/me/addresses", h.createAddress)
	protected.PUT("/me/addresses/:id", h.updateAddress)
	protected.DELETE("/me/addresses/:id", h.deleteAddress)
	protected.POST("/me/addresses/:id/default", h.setDefaultAddress)
	protected.GET("/me/preferences", h.getPreferences)
	protected.PUT("/me/preferences", h.updatePreferences)
	protected.GET("/me/notifications", h.listNotifications)
	protected.POST("/me/notifications/:id/read", h.readNotification)
	protected.POST("/me/notifications/read-all", h.readAllNotifications)
	protected.GET("/me/activities", h.listActivities)
	protected.POST("/me/activities", h.createActivity)

	protected.GET("/sandbox", h.getSandbox)
	protected.POST("/sandbox/orders", h.placeSandboxOrder)
	protected.POST("/sandbox/reset", h.resetSandbox)

	protected.GET("/orders", h.listOrders)
	protected.POST("/orders", h.createOrder)
	protected.GET("/orders/:id", h.getOrder)
	protected.POST("/orders/:id/pay", h.payOrder)
	protected.POST("/orders/:id/cancel", h.cancelOrder)
	protected.POST("/orders/:id/ship", h.shipOrder)
	protected.POST("/orders/:id/confirm", h.confirmOrder)
}

func abort(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message, "request_id": c.GetString("request_id")}})
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func (h *Handler) recordActivity(c *gin.Context, userID, action, targetType, targetID, detail string) {
	item := Activity{
		ID: uuid.NewString(), UserID: userID, Action: action,
		TargetType: targetType, TargetID: targetID, Detail: truncate(detail, 240),
	}
	_ = h.db.WithContext(c.Request.Context()).Create(&item).Error
}

func (h *Handler) notify(c *gin.Context, userID, kind, title, body, link string) {
	item := Notification{
		ID: uuid.NewString(), UserID: userID, Kind: kind,
		Title: title, Body: truncate(body, 500), Link: link,
	}
	_ = h.db.WithContext(c.Request.Context()).Create(&item).Error
}

func (h *Handler) ensureProfile(c *gin.Context, user *auth.User) Profile {
	var profile Profile
	err := h.db.WithContext(c.Request.Context()).First(&profile, "user_id = ?", user.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		profile = Profile{UserID: user.ID, DisplayName: user.Username}
		_ = h.db.WithContext(c.Request.Context()).Create(&profile).Error
	}
	return profile
}

func (h *Handler) ensurePreference(c *gin.Context, userID string) Preference {
	var pref Preference
	err := h.db.WithContext(c.Request.Context()).First(&pref, "user_id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		pref = Preference{
			UserID: userID, NotifyFavoriteUpdates: true, NotifyTradeEvents: true,
			NotifySystem: true, DefaultFavoriteFolder: DefaultFavoriteFolder, Locale: "zh-CN",
		}
		_ = h.db.WithContext(c.Request.Context()).Create(&pref).Error
	}
	return pref
}

func (h *Handler) ensureSandbox(c *gin.Context, userID string) SandboxAccount {
	var account SandboxAccount
	err := h.db.WithContext(c.Request.Context()).First(&account, "user_id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		account = SandboxAccount{UserID: userID, CashCents: SandboxStartingCashCents, Generation: 1}
		_ = h.db.WithContext(c.Request.Context()).Create(&account).Error
	}
	return account
}

func (h *Handler) loadProduct(c *gin.Context, productID string) (*catalog.Product, error) {
	var product catalog.Product
	err := h.db.WithContext(c.Request.Context()).First(&product, "id = ?", productID).Error
	if err != nil {
		return nil, err
	}
	return &product, nil
}

func coverURL(product catalog.Product) string {
	if product.CoverAssetID == nil || *product.CoverAssetID == "" {
		return ""
	}
	return "/api/v1/products/" + product.ID + "/assets/" + *product.CoverAssetID + "/content"
}

func truncate(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}

func nowPtr() *time.Time {
	now := time.Now()
	return &now
}
