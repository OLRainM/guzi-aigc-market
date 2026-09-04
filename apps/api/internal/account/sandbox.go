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

type sandboxOrderRequest struct {
	ProductID        string `json:"product_id"`
	Side             string `json:"side"`
	Quantity         int    `json:"quantity"`
	RiskAcknowledged bool   `json:"risk_acknowledged"`
}

type holdingView struct {
	SandboxHolding
	Title           string `json:"title"`
	CurrentPriceCents int64 `json:"current_price_cents"`
	MarketValueCents int64 `json:"market_value_cents"`
	UnrealizedCents int64 `json:"unrealized_cents"`
	CoverURL        string `json:"cover_url,omitempty"`
	Available       bool   `json:"available"`
}

func (h *Handler) getSandbox(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	account := h.ensureSandbox(c, user.ID)
	holdings := make([]SandboxHolding, 0)
	_ = h.db.WithContext(c.Request.Context()).Where("user_id = ? AND quantity > 0", user.ID).Order("updated_at DESC").Find(&holdings).Error
	orders := make([]SandboxOrder, 0)
	_ = h.db.WithContext(c.Request.Context()).Where("user_id = ?", user.ID).Order("created_at DESC").Limit(50).Find(&orders).Error
	c.JSON(http.StatusOK, gin.H{
		"account": account,
		"holdings": h.holdingViews(c, holdings),
		"orders": orders,
		"risk_notice": "交易沙盒使用虚拟资金，成交价取当前商品标价，不产生真实支付、发货或所有权转移。重置后资金与持仓会回到初始状态。",
		"starting_cash_cents": SandboxStartingCashCents,
	})
}

func (h *Handler) holdingViews(c *gin.Context, holdings []SandboxHolding) []holdingView {
	views := make([]holdingView, 0, len(holdings))
	if len(holdings) == 0 {
		return views
	}
	ids := make([]string, 0, len(holdings))
	for _, item := range holdings {
		ids = append(ids, item.ProductID)
	}
	var products []catalog.Product
	_ = h.db.WithContext(c.Request.Context()).Where("id IN ?", ids).Find(&products).Error
	byID := map[string]catalog.Product{}
	for _, product := range products {
		byID[product.ID] = product
	}
	for _, item := range holdings {
		view := holdingView{SandboxHolding: item}
		if product, ok := byID[item.ProductID]; ok {
			view.Title = product.Title
			view.CurrentPriceCents = product.PriceCents
			view.MarketValueCents = product.PriceCents * int64(item.Quantity)
			view.UnrealizedCents = view.MarketValueCents - item.AvgCostCents*int64(item.Quantity)
			view.CoverURL = coverURL(product)
			view.Available = product.Status == catalog.StatusPublished
		} else {
			view.Title = "已失效商品"
		}
		views = append(views, view)
	}
	return views
}

func (h *Handler) placeSandboxOrder(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req sandboxOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "交易请求无效")
		return
	}
	if !req.RiskAcknowledged {
		abort(c, http.StatusBadRequest, "RISK_NOT_ACKNOWLEDGED", "请先确认沙盒交易风险提示")
		return
	}
	side := strings.ToUpper(strings.TrimSpace(req.Side))
	if side != SideBuy && side != SideSell {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "只支持买入或卖出")
		return
	}
	if req.Quantity < 1 || req.Quantity > 99 {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "数量需为 1–99")
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
		abort(c, http.StatusConflict, "PRODUCT_UNAVAILABLE", "该商品当前不可交易")
		return
	}
	if product.SellerID == user.ID && side == SideBuy {
		abort(c, http.StatusConflict, "SELF_TRADE_FORBIDDEN", "不能买入自己发布的商品")
		return
	}

	var order SandboxOrder
	err = h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var account SandboxAccount
		if err := tx.First(&account, "user_id = ?", user.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				account = SandboxAccount{UserID: user.ID, CashCents: SandboxStartingCashCents, Generation: 1}
				if err := tx.Create(&account).Error; err != nil {
					return err
				}
			} else {
				return err
			}
		}
		amount := product.PriceCents * int64(req.Quantity)
		order = SandboxOrder{
			ID: uuid.NewString(), UserID: user.ID, ProductID: product.ID, ProductTitle: product.Title,
			Side: side, Quantity: req.Quantity, PriceCents: product.PriceCents, AmountCents: amount,
			Generation: account.Generation, RiskAcknowledged: true,
		}
		if side == SideBuy {
			if account.CashCents < amount {
				order.Status = OrderRejected
				order.RejectReason = "虚拟资金不足"
				return tx.Create(&order).Error
			}
			account.CashCents -= amount
			var holding SandboxHolding
			err := tx.Where("user_id = ? AND product_id = ?", user.ID, product.ID).First(&holding).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				holding = SandboxHolding{
					ID: uuid.NewString(), UserID: user.ID, ProductID: product.ID,
					Quantity: req.Quantity, AvgCostCents: product.PriceCents,
				}
				if err := tx.Create(&holding).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				totalQty := holding.Quantity + req.Quantity
				holding.AvgCostCents = (holding.AvgCostCents*int64(holding.Quantity) + amount) / int64(totalQty)
				holding.Quantity = totalQty
				if err := tx.Save(&holding).Error; err != nil {
					return err
				}
			}
		} else {
			var holding SandboxHolding
			if err := tx.Where("user_id = ? AND product_id = ?", user.ID, product.ID).First(&holding).Error; err != nil {
				order.Status = OrderRejected
				order.RejectReason = "没有可卖出的持仓"
				return tx.Create(&order).Error
			}
			if holding.Quantity < req.Quantity {
				order.Status = OrderRejected
				order.RejectReason = "可卖数量不足"
				return tx.Create(&order).Error
			}
			holding.Quantity -= req.Quantity
			account.CashCents += amount
			if holding.Quantity == 0 {
				if err := tx.Delete(&holding).Error; err != nil {
					return err
				}
			} else if err := tx.Save(&holding).Error; err != nil {
				return err
			}
		}
		order.Status = OrderFilled
		if err := tx.Save(&account).Error; err != nil {
			return err
		}
		return tx.Create(&order).Error
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "提交交易失败")
		return
	}
	if order.Status == OrderRejected {
		abort(c, http.StatusConflict, "ORDER_REJECTED", order.RejectReason)
		return
	}
	pref := h.ensurePreference(c, user.ID)
	if pref.NotifyTradeEvents {
		action := "买入"
		if side == SideSell {
			action = "卖出"
		}
		h.notify(c, user.ID, NotifyTradeFilled, "沙盒成交", action+" "+product.Title+" ×"+strconv.Itoa(req.Quantity), "/sandbox")
	}
	h.recordActivity(c, user.ID, ActivitySandboxTrade, "product", product.ID, order.Side+" "+product.Title)
	c.JSON(http.StatusCreated, gin.H{"order": order})
}

func (h *Handler) resetSandbox(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !req.Confirm {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "重置沙盒需要确认")
		return
	}
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var account SandboxAccount
		if err := tx.First(&account, "user_id = ?", user.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				account = SandboxAccount{UserID: user.ID, CashCents: SandboxStartingCashCents, Generation: 1}
				return tx.Create(&account).Error
			}
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&SandboxHolding{}).Error; err != nil {
			return err
		}
		account.CashCents = SandboxStartingCashCents
		account.Generation++
		account.ResetCount++
		return tx.Save(&account).Error
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "重置沙盒失败")
		return
	}
	h.recordActivity(c, user.ID, ActivitySandboxReset, "sandbox", user.ID, "重置虚拟资金")
	h.notify(c, user.ID, NotifySystem, "沙盒已重置", "虚拟资金已恢复为初始金额，历史成交仍可查看。", "/sandbox")
	h.getSandbox(c)
}
