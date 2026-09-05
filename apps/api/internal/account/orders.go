package account

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aigc-3d-platform/apps/api/internal/auth"
	"aigc-3d-platform/apps/api/internal/catalog"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	errOrderNotFound        = errors.New("order not found")
	errIdempotencyConflict  = errors.New("idempotency conflict")
	errInsufficientStock    = errors.New("insufficient stock")
	errInsufficientCash     = errors.New("insufficient cash")
	errSelfTrade            = errors.New("self trade forbidden")
	errInvalidOrderArgument = errors.New("invalid order argument")
	errInvalidOrderState    = errors.New("invalid order state")
	errForbiddenOrder       = errors.New("forbidden order action")
)

type createOrderRequest struct {
	ProductID string `json:"product_id"`
	AddressID string `json:"address_id"`
	Quantity  int    `json:"quantity"`
}

type shipOrderRequest struct {
	TrackingNo string `json:"tracking_no"`
}

type cancelOrderRequest struct {
	Reason string `json:"reason"`
}

type orderView struct {
	Order
	Events []OrderEvent `json:"events"`
}

func (h *Handler) createOrder(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "订单参数无效")
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	order, created, err := h.placeOrder(c, user.ID, key, req)
	if err != nil {
		abortOrderError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		h.recordActivity(c, user.ID, ActivityOrderCreate, "ORDER", order.ID, order.ProductTitle)
		h.notify(c, order.SellerID, NotifyOrderCreated, "新的待支付订单", order.ProductTitle+" 等待买家模拟支付", "/orders?role=SELL")
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{"order": h.orderView(c, *order)})
}

func (h *Handler) listOrders(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	role := strings.ToUpper(strings.TrimSpace(c.Query("role")))
	if role == "" {
		role = "BUY"
	}
	query := h.db.WithContext(c.Request.Context()).Model(&Order{})
	switch role {
	case "BUY":
		query = query.Where("buyer_id = ?", user.ID)
	case "SELL":
		query = query.Where("seller_id = ?", user.ID)
	default:
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "role 仅支持 BUY 或 SELL")
		return
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	page := positiveInt(c.Query("page"), 1)
	pageSize := min(positiveInt(c.Query("page_size"), 20), 100)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "加载订单失败")
		return
	}
	var items []Order
	if err := query.Order("created_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "加载订单失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": page, "page_size": pageSize, "total": total, "role": role})
}

func (h *Handler) getOrder(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	order, err := h.loadAccessibleOrder(c, user.ID, c.Param("id"))
	if err != nil {
		abortOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"order": h.orderView(c, *order)})
}

func (h *Handler) payOrder(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	order, err := h.transitionOrder(c, user.ID, c.Param("id"), OrderPaid, "BUYER", "")
	if err != nil {
		abortOrderError(c, err)
		return
	}
	h.recordActivity(c, user.ID, ActivityOrderPay, "ORDER", order.ID, order.ProductTitle)
	h.notify(c, order.SellerID, NotifyOrderPaid, "买家已模拟支付", order.ProductTitle+" 待发货", "/orders?role=SELL")
	c.JSON(http.StatusOK, gin.H{"order": h.orderView(c, *order)})
}

func (h *Handler) cancelOrder(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req cancelOrderRequest
	_ = c.ShouldBindJSON(&req)
	current, err := h.loadAccessibleOrder(c, user.ID, c.Param("id"))
	if err != nil {
		abortOrderError(c, err)
		return
	}
	role := "BUYER"
	if current.SellerID == user.ID && current.Status == OrderPaid {
		role = "SELLER"
	}
	order, err := h.transitionOrder(c, user.ID, c.Param("id"), OrderCanceled, role, strings.TrimSpace(req.Reason))
	if err != nil {
		abortOrderError(c, err)
		return
	}
	h.recordActivity(c, user.ID, ActivityOrderCancel, "ORDER", order.ID, order.ProductTitle)
	target := order.SellerID
	link := "/orders?role=SELL"
	if role == "SELLER" {
		target = order.BuyerID
		link = "/orders"
	}
	h.notify(c, target, NotifyOrderCanceled, "订单已取消", order.ProductTitle, link)
	c.JSON(http.StatusOK, gin.H{"order": h.orderView(c, *order)})
}

func (h *Handler) shipOrder(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	var req shipOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.TrackingNo) == "" {
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "请填写模拟运单号")
		return
	}
	order, err := h.transitionOrder(c, user.ID, c.Param("id"), OrderShipped, "SELLER", strings.TrimSpace(req.TrackingNo))
	if err != nil {
		abortOrderError(c, err)
		return
	}
	h.recordActivity(c, user.ID, ActivityOrderShip, "ORDER", order.ID, req.TrackingNo)
	h.notify(c, order.BuyerID, NotifyOrderShipped, "卖家已模拟发货", "运单号 "+order.TrackingNo, "/orders")
	c.JSON(http.StatusOK, gin.H{"order": h.orderView(c, *order)})
}

func (h *Handler) confirmOrder(c *gin.Context) {
	user, _ := auth.CurrentUser(c)
	order, err := h.transitionOrder(c, user.ID, c.Param("id"), OrderCompleted, "BUYER", "")
	if err != nil {
		abortOrderError(c, err)
		return
	}
	h.recordActivity(c, user.ID, ActivityOrderConfirm, "ORDER", order.ID, order.ProductTitle)
	h.notify(c, order.SellerID, NotifyOrderCompleted, "买家已确认收货", order.ProductTitle+" 模拟交易完成", "/orders?role=SELL")
	c.JSON(http.StatusOK, gin.H{"order": h.orderView(c, *order)})
}

func (h *Handler) placeOrder(c *gin.Context, buyerID, idempotencyKey string, req createOrderRequest) (*Order, bool, error) {
	if _, err := uuid.Parse(idempotencyKey); err != nil {
		return nil, false, errInvalidOrderArgument
	}
	req.ProductID = strings.TrimSpace(req.ProductID)
	req.AddressID = strings.TrimSpace(req.AddressID)
	if req.Quantity < 1 || req.Quantity > 99 {
		return nil, false, errInvalidOrderArgument
	}
	hash := orderRequestHash(req)
	var existing Order
	err := h.db.WithContext(c.Request.Context()).Where("buyer_id = ? AND idempotency_key = ?", buyerID, idempotencyKey).First(&existing).Error
	if err == nil {
		if existing.RequestHash != hash {
			return nil, false, errIdempotencyConflict
		}
		return &existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	var created Order
	err = h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var product catalog.Product
		if err := lockFirst(tx, &product, "id = ?", req.ProductID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errOrderNotFound
			}
			return err
		}
		if product.Status != catalog.StatusPublished {
			return errInvalidOrderState
		}
		if product.SellerID == buyerID {
			return errSelfTrade
		}
		if product.Stock < req.Quantity {
			return errInsufficientStock
		}
		var address Address
		if err := tx.First(&address, "id = ? AND user_id = ?", req.AddressID, buyerID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errInvalidOrderArgument
			}
			return err
		}
		now := time.Now()
		if err := tx.Model(&product).Update("stock", gorm.Expr("stock - ?", req.Quantity)).Error; err != nil {
			return err
		}
		created = Order{
			ID:             uuid.NewString(),
			BuyerID:        buyerID,
			SellerID:       product.SellerID,
			ProductID:      product.ID,
			AddressID:      address.ID,
			IdempotencyKey: idempotencyKey,
			RequestHash:    hash,
			Quantity:       req.Quantity,
			UnitPriceCents: product.PriceCents,
			AmountCents:    product.PriceCents * int64(req.Quantity),
			Status:         OrderPendingPayment,
			ProductTitle:   product.Title,
			CoverURL:       coverURL(product),
			Recipient:      address.Recipient,
			Phone:          address.Phone,
			AddressText:    formatAddress(address),
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := tx.Create(&created).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return errIdempotencyConflict
			}
			return err
		}
		return appendOrderEvent(tx, created.ID, "", OrderPendingPayment, buyerID, "BUYER", "创建待支付订单")
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || errors.Is(err, errIdempotencyConflict) {
			var conflict Order
			if loadErr := h.db.WithContext(c.Request.Context()).Where("buyer_id = ? AND idempotency_key = ?", buyerID, idempotencyKey).First(&conflict).Error; loadErr == nil {
				if conflict.RequestHash != hash {
					return nil, false, errIdempotencyConflict
				}
				return &conflict, false, nil
			}
		}
		return nil, false, err
	}
	return &created, true, nil
}

func (h *Handler) transitionOrder(c *gin.Context, actorID, orderID, toStatus, role, note string) (*Order, error) {
	var updated Order
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var order Order
		if err := lockFirst(tx, &order, "id = ?", orderID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errOrderNotFound
			}
			return err
		}
		if err := authorizeOrderAction(order, actorID, role, toStatus); err != nil {
			return err
		}
		if err := ValidateOrderTransition(order.Status, toStatus); err != nil {
			return errInvalidOrderState
		}
		now := time.Now()
		from := order.Status
		switch toStatus {
		case OrderPaid:
			if err := h.debitBuyer(tx, order); err != nil {
				return err
			}
			order.PaidAt = &now
		case OrderCanceled:
			if from == OrderPaid {
				if err := creditUser(tx, order.BuyerID, order.AmountCents); err != nil {
					return err
				}
			}
			if err := restoreStock(tx, order.ProductID, order.Quantity); err != nil {
				return err
			}
			order.CanceledAt = &now
			if note != "" {
				order.CancelReason = truncate(note, 120)
			} else if from == OrderPendingPayment {
				order.CancelReason = "买家取消待支付订单"
			} else {
				order.CancelReason = "卖家取消已支付订单"
			}
		case OrderShipped:
			order.TrackingNo = truncate(note, 64)
			order.ShippedAt = &now
		case OrderCompleted:
			if err := creditUser(tx, order.SellerID, order.AmountCents); err != nil {
				return err
			}
			order.CompletedAt = &now
		}
		order.Status = toStatus
		order.UpdatedAt = now
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if err := appendOrderEvent(tx, order.ID, from, toStatus, actorID, role, eventNote(toStatus, note)); err != nil {
			return err
		}
		updated = order
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (h *Handler) debitBuyer(tx *gorm.DB, order Order) error {
	var account SandboxAccount
	if err := lockFirst(tx, &account, "user_id = ?", order.BuyerID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		account = SandboxAccount{UserID: order.BuyerID, CashCents: SandboxStartingCashCents, Generation: 1}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
	}
	if account.CashCents < order.AmountCents {
		return errInsufficientCash
	}
	return tx.Model(&SandboxAccount{}).Where("user_id = ?", order.BuyerID).Update("cash_cents", gorm.Expr("cash_cents - ?", order.AmountCents)).Error
}

func creditUser(tx *gorm.DB, userID string, amount int64) error {
	var account SandboxAccount
	if err := lockFirst(tx, &account, "user_id = ?", userID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		account = SandboxAccount{UserID: userID, CashCents: SandboxStartingCashCents, Generation: 1}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
	}
	return tx.Model(&SandboxAccount{}).Where("user_id = ?", userID).Update("cash_cents", gorm.Expr("cash_cents + ?", amount)).Error
}

func lockFirst(tx *gorm.DB, dest any, query string, args ...any) *gorm.DB {
	db := tx
	if tx.Dialector.Name() != "sqlite" {
		db = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return db.First(dest, append([]any{query}, args...)...)
}

func restoreStock(tx *gorm.DB, productID string, quantity int) error {
	return tx.Model(&catalog.Product{}).Where("id = ?", productID).Update("stock", gorm.Expr("stock + ?", quantity)).Error
}

func appendOrderEvent(tx *gorm.DB, orderID, from, to, actorID, role, note string) error {
	return tx.Create(&OrderEvent{
		ID: uuid.NewString(), OrderID: orderID, FromStatus: from, ToStatus: to,
		ActorID: actorID, ActorRole: role, Note: truncate(note, 240),
	}).Error
}

func authorizeOrderAction(order Order, actorID, role, toStatus string) error {
	switch toStatus {
	case OrderPaid, OrderCompleted:
		if role != "BUYER" || order.BuyerID != actorID {
			return errForbiddenOrder
		}
	case OrderShipped:
		if role != "SELLER" || order.SellerID != actorID {
			return errForbiddenOrder
		}
	case OrderCanceled:
		if order.Status == OrderPendingPayment && order.BuyerID == actorID {
			return nil
		}
		if order.Status == OrderPaid && order.SellerID == actorID {
			return nil
		}
		return errForbiddenOrder
	default:
		return errInvalidOrderState
	}
	return nil
}

func (h *Handler) loadAccessibleOrder(c *gin.Context, userID, orderID string) (*Order, error) {
	var order Order
	err := h.db.WithContext(c.Request.Context()).First(&order, "id = ? AND (buyer_id = ? OR seller_id = ?)", orderID, userID, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func (h *Handler) orderView(c *gin.Context, order Order) orderView {
	var events []OrderEvent
	_ = h.db.WithContext(c.Request.Context()).Where("order_id = ?", order.ID).Order("created_at asc").Find(&events).Error
	if events == nil {
		events = []OrderEvent{}
	}
	return orderView{Order: order, Events: events}
}

func orderRequestHash(req createOrderRequest) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", req.ProductID, req.AddressID, req.Quantity)))
	return hex.EncodeToString(sum[:])
}

func formatAddress(address Address) string {
	parts := []string{address.Province, address.City, address.District, address.Detail}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			filtered = append(filtered, strings.TrimSpace(part))
		}
	}
	return strings.Join(filtered, " ")
}

func eventNote(status, note string) string {
	if strings.TrimSpace(note) != "" {
		return note
	}
	switch status {
	case OrderPaid:
		return "模拟支付成功"
	case OrderCanceled:
		return "取消订单"
	case OrderShipped:
		return "模拟发货"
	case OrderCompleted:
		return "确认收货"
	default:
		return "创建订单"
	}
}

func abortOrderError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errInvalidOrderArgument):
		abort(c, http.StatusBadRequest, "INVALID_ARGUMENT", "订单参数无效，需提供 Idempotency-Key、商品、地址和数量")
	case errors.Is(err, errIdempotencyConflict):
		abort(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "同一幂等键不能用于不同订单内容")
	case errors.Is(err, errSelfTrade):
		abort(c, http.StatusConflict, "SELF_TRADE_FORBIDDEN", "不能购买自己的商品")
	case errors.Is(err, errInsufficientStock):
		abort(c, http.StatusConflict, "INSUFFICIENT_STOCK", "库存不足")
	case errors.Is(err, errInsufficientCash):
		abort(c, http.StatusConflict, "INSUFFICIENT_FUNDS", "虚拟资金不足")
	case errors.Is(err, errInvalidOrderState):
		abort(c, http.StatusConflict, "INVALID_STATE", "当前订单状态不允许该操作")
	case errors.Is(err, errForbiddenOrder):
		abort(c, http.StatusForbidden, "FORBIDDEN", "无权操作该订单")
	case errors.Is(err, errOrderNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		abort(c, http.StatusNotFound, "NOT_FOUND", "订单或商品不存在")
	default:
		abort(c, http.StatusInternalServerError, "INTERNAL_ERROR", "处理订单失败")
	}
}
