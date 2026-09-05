package account

import "time"

const (
	FavoriteStatusActive      = "ACTIVE"
	FavoriteStatusUpdated     = "UPDATED"
	FavoriteStatusUnavailable = "UNAVAILABLE"
	FavoriteStatusInvalid     = "INVALID"
	DefaultFavoriteFolder     = "默认收藏"
	SandboxStartingCashCents  = int64(10000000)
	ActivityViewProduct       = "VIEW_PRODUCT"
	ActivityFavoriteAdd       = "FAVORITE_ADD"
	ActivityFavoriteRemove    = "FAVORITE_REMOVE"
	ActivityProfileUpdate     = "PROFILE_UPDATE"
	ActivityAddressSave       = "ADDRESS_SAVE"
	ActivitySandboxTrade      = "SANDBOX_TRADE"
	ActivitySandboxReset      = "SANDBOX_RESET"
	ActivityOrderCreate       = "ORDER_CREATE"
	ActivityOrderPay          = "ORDER_PAY"
	ActivityOrderCancel       = "ORDER_CANCEL"
	ActivityOrderShip         = "ORDER_SHIP"
	ActivityOrderConfirm      = "ORDER_CONFIRM"
	NotifyFavoriteUpdate      = "FAVORITE_UPDATE"
	NotifyFavoriteInvalid     = "FAVORITE_INVALID"
	NotifyTradeFilled         = "TRADE_FILLED"
	NotifyOrderCreated        = "ORDER_CREATED"
	NotifyOrderPaid           = "ORDER_PAID"
	NotifyOrderCanceled       = "ORDER_CANCELED"
	NotifyOrderShipped        = "ORDER_SHIPPED"
	NotifyOrderCompleted      = "ORDER_COMPLETED"
	NotifySystem              = "SYSTEM"
	OrderPendingPayment       = "PENDING_PAYMENT"
	OrderPaid                 = "PAID"
	OrderShipped              = "SHIPPED"
	OrderCompleted            = "COMPLETED"
	OrderCanceled             = "CANCELED"
	SideBuy                   = "BUY"
	SideSell                  = "SELL"
	OrderFilled               = "FILLED"
	OrderRejected             = "REJECTED"
)

type Profile struct {
	UserID      string    `gorm:"type:char(36);primaryKey" json:"user_id"`
	DisplayName string    `gorm:"size:64;not null" json:"display_name"`
	Bio         string    `gorm:"size:280" json:"bio,omitempty"`
	Phone       string    `gorm:"size:32" json:"phone,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Profile) TableName() string { return "user_profiles" }

type Address struct {
	ID         string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID     string    `gorm:"type:char(36);not null;index" json:"user_id"`
	Recipient  string    `gorm:"size:64;not null" json:"recipient"`
	Phone      string    `gorm:"size:32;not null" json:"phone"`
	Province   string    `gorm:"size:64;not null" json:"province"`
	City       string    `gorm:"size:64;not null" json:"city"`
	District   string    `gorm:"size:64" json:"district,omitempty"`
	Detail     string    `gorm:"size:200;not null" json:"detail"`
	PostalCode string    `gorm:"size:16" json:"postal_code,omitempty"`
	IsDefault  bool      `gorm:"not null;default:false;index" json:"is_default"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Address) TableName() string { return "user_addresses" }

type Preference struct {
	UserID                 string    `gorm:"type:char(36);primaryKey" json:"user_id"`
	NotifyFavoriteUpdates  bool      `gorm:"not null;default:true" json:"notify_favorite_updates"`
	NotifyTradeEvents      bool      `gorm:"not null;default:true" json:"notify_trade_events"`
	NotifySystem           bool      `gorm:"not null;default:true" json:"notify_system"`
	DefaultFavoriteFolder  string    `gorm:"size:40;not null;default:默认收藏" json:"default_favorite_folder"`
	Locale                 string    `gorm:"size:16;not null;default:zh-CN" json:"locale"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

func (Preference) TableName() string { return "user_preferences" }

type Favorite struct {
	ID                 string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID             string     `gorm:"type:char(36);not null;uniqueIndex:idx_user_product" json:"user_id"`
	ProductID          string     `gorm:"type:char(36);not null;uniqueIndex:idx_user_product;index" json:"product_id"`
	Folder             string     `gorm:"size:40;not null;index" json:"folder"`
	Note               string     `gorm:"size:200" json:"note,omitempty"`
	SnapshotTitle      string     `gorm:"size:120;not null" json:"snapshot_title"`
	SnapshotPriceCents int64      `gorm:"not null" json:"snapshot_price_cents"`
	SnapshotStatus     string     `gorm:"size:16;not null" json:"snapshot_status"`
	SnapshotCategory   string     `gorm:"size:64;not null" json:"snapshot_category"`
	SnapshotIPName     string     `gorm:"size:80;not null" json:"snapshot_ip_name"`
	ChangeNotifiedAt   *time.Time `json:"change_notified_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (Favorite) TableName() string { return "favorites" }

type Notification struct {
	ID         string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID     string     `gorm:"type:char(36);not null;index" json:"user_id"`
	Kind       string     `gorm:"size:32;not null;index" json:"kind"`
	Title      string     `gorm:"size:120;not null" json:"title"`
	Body       string     `gorm:"size:500;not null" json:"body"`
	Link       string     `gorm:"size:200" json:"link,omitempty"`
	ReadAt     *time.Time `json:"read_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (Notification) TableName() string { return "notifications" }

type Activity struct {
	ID         string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID     string    `gorm:"type:char(36);not null;index" json:"user_id"`
	Action     string    `gorm:"size:40;not null;index" json:"action"`
	TargetType string    `gorm:"size:32" json:"target_type,omitempty"`
	TargetID   string    `gorm:"size:36;index" json:"target_id,omitempty"`
	Detail     string    `gorm:"size:240" json:"detail,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (Activity) TableName() string { return "user_activities" }

type SandboxAccount struct {
	UserID      string    `gorm:"type:char(36);primaryKey" json:"user_id"`
	CashCents   int64     `gorm:"not null" json:"cash_cents"`
	Generation  int       `gorm:"not null;default:1" json:"generation"`
	ResetCount  int       `gorm:"not null;default:0" json:"reset_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (SandboxAccount) TableName() string { return "sandbox_accounts" }

type SandboxHolding struct {
	ID           string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID       string    `gorm:"type:char(36);not null;uniqueIndex:idx_holding_user_product" json:"user_id"`
	ProductID    string    `gorm:"type:char(36);not null;uniqueIndex:idx_holding_user_product" json:"product_id"`
	Quantity     int       `gorm:"not null" json:"quantity"`
	AvgCostCents int64     `gorm:"not null" json:"avg_cost_cents"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (SandboxHolding) TableName() string { return "sandbox_holdings" }

type SandboxOrder struct {
	ID               string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID           string    `gorm:"type:char(36);not null;index" json:"user_id"`
	ProductID        string    `gorm:"type:char(36);not null;index" json:"product_id"`
	ProductTitle     string    `gorm:"size:120;not null" json:"product_title"`
	Side             string    `gorm:"size:8;not null;index" json:"side"`
	Quantity         int       `gorm:"not null" json:"quantity"`
	PriceCents       int64     `gorm:"not null" json:"price_cents"`
	AmountCents      int64     `gorm:"not null" json:"amount_cents"`
	Status           string    `gorm:"size:16;not null;index" json:"status"`
	RejectReason     string    `gorm:"size:200" json:"reject_reason,omitempty"`
	Generation       int       `gorm:"not null;index" json:"generation"`
	RiskAcknowledged bool      `gorm:"not null" json:"risk_acknowledged"`
	CreatedAt        time.Time `json:"created_at"`
}

func (SandboxOrder) TableName() string { return "sandbox_orders" }

type Order struct {
	ID             string     `gorm:"type:char(36);primaryKey" json:"id"`
	BuyerID        string     `gorm:"type:char(36);not null;uniqueIndex:uq_orders_buyer_idempotency,priority:1;index" json:"buyer_id"`
	SellerID       string     `gorm:"type:char(36);not null;index" json:"seller_id"`
	ProductID      string     `gorm:"type:char(36);not null;index" json:"product_id"`
	AddressID      string     `gorm:"type:char(36);not null" json:"address_id"`
	IdempotencyKey string     `gorm:"type:char(36);not null;uniqueIndex:uq_orders_buyer_idempotency,priority:2" json:"-"`
	RequestHash    string     `gorm:"size:128;not null" json:"-"`
	Quantity       int        `gorm:"not null" json:"quantity"`
	UnitPriceCents int64      `gorm:"not null" json:"unit_price_cents"`
	AmountCents    int64      `gorm:"not null" json:"amount_cents"`
	Status         string     `gorm:"size:24;not null;index" json:"status"`
	ProductTitle   string     `gorm:"size:120;not null" json:"product_title"`
	CoverURL       string     `gorm:"size:255" json:"cover_url,omitempty"`
	Recipient      string     `gorm:"size:64;not null" json:"recipient"`
	Phone          string     `gorm:"size:32;not null" json:"phone"`
	AddressText    string     `gorm:"size:255;not null" json:"address_text"`
	TrackingNo     string     `gorm:"size:64" json:"tracking_no,omitempty"`
	CancelReason   string     `gorm:"size:120" json:"cancel_reason,omitempty"`
	PaidAt         *time.Time `json:"paid_at,omitempty"`
	CanceledAt     *time.Time `json:"canceled_at,omitempty"`
	ShippedAt      *time.Time `json:"shipped_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (Order) TableName() string { return "trade_orders" }

type OrderEvent struct {
	ID         string    `gorm:"type:char(36);primaryKey" json:"id"`
	OrderID    string    `gorm:"type:char(36);not null;index" json:"order_id"`
	FromStatus string    `gorm:"size:24" json:"from_status,omitempty"`
	ToStatus   string    `gorm:"size:24;not null" json:"to_status"`
	ActorID    string    `gorm:"type:char(36);not null" json:"actor_id"`
	ActorRole  string    `gorm:"size:16;not null" json:"actor_role"`
	Note       string    `gorm:"size:240" json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (OrderEvent) TableName() string { return "trade_order_events" }
