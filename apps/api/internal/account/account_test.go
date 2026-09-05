package account

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aigc-3d-platform/apps/api/internal/asset"
	"aigc-3d-platform/apps/api/internal/auth"
	"aigc-3d-platform/apps/api/internal/catalog"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type authBody struct {
	AccessToken string `json:"access_token"`
	User        struct {
		ID string `json:"id"`
	} `json:"user"`
}

func setupAccountServer(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	authHandler, err := auth.New(db, auth.Config{
		JWTSecret: "a-development-secret-with-32-characters", Issuer: "test",
		AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, RefreshCookie: "refresh_token",
	})
	if err != nil {
		t.Fatal(err)
	}
	assets, err := asset.New(db, asset.NewMemoryStore("aigc-assets"), "aigc-assets")
	if err != nil {
		t.Fatal(err)
	}
	catalogHandler, err := catalog.New(db, assets, nil)
	if err != nil {
		t.Fatal(err)
	}
	accountHandler, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	api := router.Group("/api/v1")
	authHandler.RegisterRoutes(api)
	catalogHandler.RegisterRoutes(api, authHandler.Authenticate(), authHandler.ResolveUser)
	accountHandler.RegisterRoutes(api, authHandler.Authenticate())
	return router, db
}

func register(t *testing.T, router http.Handler, username string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload authBody
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.AccessToken
}

func doJSON(router http.Handler, method, path, token string, payload any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if payload != nil {
		body, _ := json.Marshal(payload)
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func createPublishedProduct(t *testing.T, router http.Handler, db *gorm.DB, token, title string, price int64) string {
	t.Helper()
	rec := doJSON(router, http.MethodPost, "/api/v1/products", token, map[string]any{
		"title": title, "description": "完整描述", "price_cents": price,
		"ip_name": "原创", "category": "手办", "condition": "全新", "stock": 8,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create product status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Product struct {
			ID string `json:"id"`
		} `json:"product"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&catalog.Product{}).Where("id = ?", body.Product.ID).Update("status", catalog.StatusPublished).Error; err != nil {
		t.Fatal(err)
	}
	return body.Product.ID
}

func TestFavoriteLifecycleAndStatus(t *testing.T) {
	router, db := setupAccountServer(t)
	seller := register(t, router, "seller01")
	buyer := register(t, router, "buyer01")
	productID := createPublishedProduct(t, router, db, seller, "限定手办", 12900)

	created := doJSON(router, http.MethodPost, "/api/v1/favorites", buyer, map[string]any{
		"product_id": productID, "folder": "手办", "note": "等补货",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("favorite create = %d %s", created.Code, created.Body.String())
	}
	dup := doJSON(router, http.MethodPost, "/api/v1/favorites", buyer, map[string]any{"product_id": productID})
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate favorite = %d %s", dup.Code, dup.Body.String())
	}

	listed := doJSON(router, http.MethodGet, "/api/v1/favorites?folder=手办", buyer, nil)
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"status":"ACTIVE"`)) {
		t.Fatalf("list favorites = %d %s", listed.Code, listed.Body.String())
	}

	if err := db.Model(&catalog.Product{}).Where("id = ?", productID).Updates(map[string]any{"price_cents": 15900, "title": "限定手办改版"}).Error; err != nil {
		t.Fatal(err)
	}
	updated := doJSON(router, http.MethodGet, "/api/v1/favorites?status=UPDATED", buyer, nil)
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte(`"status":"UPDATED"`)) {
		t.Fatalf("updated favorites = %d %s", updated.Code, updated.Body.String())
	}

	if err := db.Model(&catalog.Product{}).Where("id = ?", productID).Update("status", catalog.StatusOffShelf).Error; err != nil {
		t.Fatal(err)
	}
	invalid := doJSON(router, http.MethodGet, "/api/v1/favorites?status=UNAVAILABLE", buyer, nil)
	if invalid.Code != http.StatusOK || !bytes.Contains(invalid.Body.Bytes(), []byte("商品已下架")) {
		t.Fatalf("unavailable favorites = %d %s", invalid.Code, invalid.Body.String())
	}

	var createdBody struct {
		Favorite struct {
			ID string `json:"id"`
		} `json:"favorite"`
	}
	_ = json.Unmarshal(created.Body.Bytes(), &createdBody)
	removed := doJSON(router, http.MethodDelete, "/api/v1/favorites/"+createdBody.Favorite.ID, buyer, nil)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("remove favorite = %d %s", removed.Code, removed.Body.String())
	}
}

func TestProfileAddressAndPreferences(t *testing.T) {
	router, _ := setupAccountServer(t)
	token := register(t, router, "profile01")
	profile := doJSON(router, http.MethodPut, "/api/v1/me/profile", token, map[string]any{
		"display_name": "谷子收藏家", "bio": "喜欢手办", "phone": "13800138000",
	})
	if profile.Code != http.StatusOK || !bytes.Contains(profile.Body.Bytes(), []byte("谷子收藏家")) {
		t.Fatalf("update profile = %d %s", profile.Code, profile.Body.String())
	}
	address := doJSON(router, http.MethodPost, "/api/v1/me/addresses", token, map[string]any{
		"recipient": "张三", "phone": "13800138000", "province": "上海", "city": "上海", "detail": "浦东新区 1 号", "is_default": true,
	})
	if address.Code != http.StatusCreated {
		t.Fatalf("create address = %d %s", address.Code, address.Body.String())
	}
	prefs := doJSON(router, http.MethodPut, "/api/v1/me/preferences", token, map[string]any{
		"notify_favorite_updates": false, "notify_trade_events": true, "notify_system": true,
		"default_favorite_folder": "关注", "locale": "zh-CN",
	})
	if prefs.Code != http.StatusOK || !bytes.Contains(prefs.Body.Bytes(), []byte(`"notify_favorite_updates":false`)) {
		t.Fatalf("update prefs = %d %s", prefs.Code, prefs.Body.String())
	}
	me := doJSON(router, http.MethodGet, "/api/v1/me/profile", token, nil)
	if me.Code != http.StatusOK || !bytes.Contains(me.Body.Bytes(), []byte(`"addresses":1`)) {
		t.Fatalf("get profile = %d %s", me.Code, me.Body.String())
	}
}

func TestSandboxTradeAndReset(t *testing.T) {
	router, db := setupAccountServer(t)
	seller := register(t, router, "seller02")
	buyer := register(t, router, "buyer02")
	productID := createPublishedProduct(t, router, db, seller, "可交易手办", 20000)

	selfBuy := doJSON(router, http.MethodPost, "/api/v1/sandbox/orders", seller, map[string]any{
		"product_id": productID, "side": "BUY", "quantity": 1, "risk_acknowledged": true,
	})
	if selfBuy.Code != http.StatusConflict {
		t.Fatalf("self trade = %d %s", selfBuy.Code, selfBuy.Body.String())
	}

	noRisk := doJSON(router, http.MethodPost, "/api/v1/sandbox/orders", buyer, map[string]any{
		"product_id": productID, "side": "BUY", "quantity": 1,
	})
	if noRisk.Code != http.StatusBadRequest {
		t.Fatalf("risk required = %d %s", noRisk.Code, noRisk.Body.String())
	}

	buy := doJSON(router, http.MethodPost, "/api/v1/sandbox/orders", buyer, map[string]any{
		"product_id": productID, "side": "BUY", "quantity": 2, "risk_acknowledged": true,
	})
	if buy.Code != http.StatusCreated || !bytes.Contains(buy.Body.Bytes(), []byte(`"status":"FILLED"`)) {
		t.Fatalf("buy = %d %s", buy.Code, buy.Body.String())
	}

	overSell := doJSON(router, http.MethodPost, "/api/v1/sandbox/orders", buyer, map[string]any{
		"product_id": productID, "side": "SELL", "quantity": 9, "risk_acknowledged": true,
	})
	if overSell.Code != http.StatusConflict {
		t.Fatalf("oversell = %d %s", overSell.Code, overSell.Body.String())
	}

	sell := doJSON(router, http.MethodPost, "/api/v1/sandbox/orders", buyer, map[string]any{
		"product_id": productID, "side": "SELL", "quantity": 1, "risk_acknowledged": true,
	})
	if sell.Code != http.StatusCreated {
		t.Fatalf("sell = %d %s", sell.Code, sell.Body.String())
	}

	snapshot := doJSON(router, http.MethodGet, "/api/v1/sandbox", buyer, nil)
	if snapshot.Code != http.StatusOK || !bytes.Contains(snapshot.Body.Bytes(), []byte(`"cash_cents":9980000`)) {
		t.Fatalf("sandbox snapshot = %d %s", snapshot.Code, snapshot.Body.String())
	}

	reset := doJSON(router, http.MethodPost, "/api/v1/sandbox/reset", buyer, map[string]any{"confirm": true})
	if reset.Code != http.StatusOK || !bytes.Contains(reset.Body.Bytes(), []byte(`"cash_cents":10000000`)) {
		t.Fatalf("reset = %d %s", reset.Code, reset.Body.String())
	}
	if !bytes.Contains(reset.Body.Bytes(), []byte(`"status":"FILLED"`)) {
		t.Fatalf("reset should keep trade history: %s", reset.Body.String())
	}
}

func doJSONHeader(router http.Handler, method, path, token, idempotencyKey string, payload any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if payload != nil {
		body, _ := json.Marshal(payload)
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func createAddress(t *testing.T, router http.Handler, token string) string {
	t.Helper()
	rec := doJSON(router, http.MethodPost, "/api/v1/me/addresses", token, map[string]any{
		"recipient": "测试收件人", "phone": "13800138000", "province": "上海", "city": "上海", "detail": "测试路 1 号", "is_default": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create address = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Address struct {
			ID string `json:"id"`
		} `json:"address"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Address.ID
}

func TestSimulatedOrderLifecycle(t *testing.T) {
	router, db := setupAccountServer(t)
	seller := register(t, router, "seller03")
	buyer := register(t, router, "buyer03")
	productID := createPublishedProduct(t, router, db, seller, "可下单手办", 25000)
	addressID := createAddress(t, router, buyer)
	key := "11111111-1111-1111-1111-111111111111"

	selfBuy := doJSONHeader(router, http.MethodPost, "/api/v1/orders", seller, key, map[string]any{
		"product_id": productID, "address_id": addressID, "quantity": 1,
	})
	if selfBuy.Code != http.StatusConflict {
		t.Fatalf("self trade order = %d %s", selfBuy.Code, selfBuy.Body.String())
	}

	created := doJSONHeader(router, http.MethodPost, "/api/v1/orders", buyer, key, map[string]any{
		"product_id": productID, "address_id": addressID, "quantity": 2,
	})
	if created.Code != http.StatusCreated || !bytes.Contains(created.Body.Bytes(), []byte(`"status":"PENDING_PAYMENT"`)) {
		t.Fatalf("create order = %d %s", created.Code, created.Body.String())
	}
	replay := doJSONHeader(router, http.MethodPost, "/api/v1/orders", buyer, key, map[string]any{
		"product_id": productID, "address_id": addressID, "quantity": 2,
	})
	if replay.Code != http.StatusOK {
		t.Fatalf("idempotent replay = %d %s", replay.Code, replay.Body.String())
	}
	conflict := doJSONHeader(router, http.MethodPost, "/api/v1/orders", buyer, key, map[string]any{
		"product_id": productID, "address_id": addressID, "quantity": 1,
	})
	if conflict.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict = %d %s", conflict.Code, conflict.Body.String())
	}

	var createdBody struct {
		Order struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"order"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	var product catalog.Product
	if err := db.First(&product, "id = ?", productID).Error; err != nil {
		t.Fatal(err)
	}
	if product.Stock != 6 {
		t.Fatalf("stock after create = %d", product.Stock)
	}

	overStock := doJSONHeader(router, http.MethodPost, "/api/v1/orders", buyer, "22222222-2222-2222-2222-222222222222", map[string]any{
		"product_id": productID, "address_id": addressID, "quantity": 9,
	})
	if overStock.Code != http.StatusConflict {
		t.Fatalf("over stock = %d %s", overStock.Code, overStock.Body.String())
	}

	pay := doJSON(router, http.MethodPost, "/api/v1/orders/"+createdBody.Order.ID+"/pay", buyer, map[string]any{})
	if pay.Code != http.StatusOK || !bytes.Contains(pay.Body.Bytes(), []byte(`"status":"PAID"`)) {
		t.Fatalf("pay = %d %s", pay.Code, pay.Body.String())
	}
	snapshot := doJSON(router, http.MethodGet, "/api/v1/sandbox", buyer, nil)
	if snapshot.Code != http.StatusOK || !bytes.Contains(snapshot.Body.Bytes(), []byte(`"cash_cents":9950000`)) {
		t.Fatalf("buyer cash after pay = %d %s", snapshot.Code, snapshot.Body.String())
	}

	buyerShip := doJSON(router, http.MethodPost, "/api/v1/orders/"+createdBody.Order.ID+"/ship", buyer, map[string]any{"tracking_no": "SF123"})
	if buyerShip.Code != http.StatusForbidden {
		t.Fatalf("buyer ship = %d %s", buyerShip.Code, buyerShip.Body.String())
	}
	ship := doJSON(router, http.MethodPost, "/api/v1/orders/"+createdBody.Order.ID+"/ship", seller, map[string]any{"tracking_no": "SF123456"})
	if ship.Code != http.StatusOK || !bytes.Contains(ship.Body.Bytes(), []byte(`"status":"SHIPPED"`)) {
		t.Fatalf("ship = %d %s", ship.Code, ship.Body.String())
	}
	confirm := doJSON(router, http.MethodPost, "/api/v1/orders/"+createdBody.Order.ID+"/confirm", buyer, map[string]any{})
	if confirm.Code != http.StatusOK || !bytes.Contains(confirm.Body.Bytes(), []byte(`"status":"COMPLETED"`)) {
		t.Fatalf("confirm = %d %s", confirm.Code, confirm.Body.String())
	}
	sellerCash := doJSON(router, http.MethodGet, "/api/v1/sandbox", seller, nil)
	if sellerCash.Code != http.StatusOK || !bytes.Contains(sellerCash.Body.Bytes(), []byte(`"cash_cents":10050000`)) {
		t.Fatalf("seller cash after confirm = %d %s", sellerCash.Code, sellerCash.Body.String())
	}
}

func TestSimulatedOrderCancelRestoresStockAndRefund(t *testing.T) {
	router, db := setupAccountServer(t)
	seller := register(t, router, "seller04")
	buyer := register(t, router, "buyer04")
	productID := createPublishedProduct(t, router, db, seller, "可取消手办", 30000)
	addressID := createAddress(t, router, buyer)

	pending := doJSONHeader(router, http.MethodPost, "/api/v1/orders", buyer, "33333333-3333-3333-3333-333333333333", map[string]any{
		"product_id": productID, "address_id": addressID, "quantity": 3,
	})
	if pending.Code != http.StatusCreated {
		t.Fatalf("pending order = %d %s", pending.Code, pending.Body.String())
	}
	var pendingBody struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(pending.Body.Bytes(), &pendingBody); err != nil {
		t.Fatal(err)
	}
	cancelPending := doJSON(router, http.MethodPost, "/api/v1/orders/"+pendingBody.Order.ID+"/cancel", buyer, map[string]any{"reason": "不想买了"})
	if cancelPending.Code != http.StatusOK || !bytes.Contains(cancelPending.Body.Bytes(), []byte(`"status":"CANCELED"`)) {
		t.Fatalf("cancel pending = %d %s", cancelPending.Code, cancelPending.Body.String())
	}
	var product catalog.Product
	if err := db.First(&product, "id = ?", productID).Error; err != nil {
		t.Fatal(err)
	}
	if product.Stock != 8 {
		t.Fatalf("stock after cancel pending = %d", product.Stock)
	}

	paid := doJSONHeader(router, http.MethodPost, "/api/v1/orders", buyer, "44444444-4444-4444-4444-444444444444", map[string]any{
		"product_id": productID, "address_id": addressID, "quantity": 1,
	})
	var paidBody struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(paid.Body.Bytes(), &paidBody); err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(router, http.MethodPost, "/api/v1/orders/"+paidBody.Order.ID+"/pay", buyer, map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("pay before seller cancel = %d %s", rec.Code, rec.Body.String())
	}
	sellerCancel := doJSON(router, http.MethodPost, "/api/v1/orders/"+paidBody.Order.ID+"/cancel", seller, map[string]any{"reason": "缺货"})
	if sellerCancel.Code != http.StatusOK || !bytes.Contains(sellerCancel.Body.Bytes(), []byte(`"status":"CANCELED"`)) {
		t.Fatalf("seller cancel paid = %d %s", sellerCancel.Code, sellerCancel.Body.String())
	}
	snapshot := doJSON(router, http.MethodGet, "/api/v1/sandbox", buyer, nil)
	if snapshot.Code != http.StatusOK || !bytes.Contains(snapshot.Body.Bytes(), []byte(`"cash_cents":10000000`)) {
		t.Fatalf("refund after seller cancel = %d %s", snapshot.Code, snapshot.Body.String())
	}
}

func TestOrderAccessAndInvalidStateContracts(t *testing.T) {
	router, db := setupAccountServer(t)
	seller := register(t, router, "seller-contract")
	buyer := register(t, router, "buyer-contract")
	thirdParty := register(t, router, "third-contract")
	productID := createPublishedProduct(t, router, db, seller, "状态契约手办", 12000)
	addressID := createAddress(t, router, buyer)
	created := doJSONHeader(router, http.MethodPost, "/api/v1/orders", buyer, "55555555-5555-5555-5555-555555555555", map[string]any{
		"product_id": productID, "address_id": addressID, "quantity": 1,
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create order = %d %s", created.Code, created.Body.String())
	}
	var orderBody struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &orderBody); err != nil {
		t.Fatal(err)
	}
	orderPath := "/api/v1/orders/" + orderBody.Order.ID

	if rec := doJSON(router, http.MethodGet, orderPath, thirdParty, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("third-party order read = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(router, http.MethodPost, orderPath+"/pay", seller, map[string]any{}); rec.Code != http.StatusForbidden {
		t.Fatalf("seller pay = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(router, http.MethodPost, orderPath+"/ship", seller, map[string]any{"tracking_no": "SF-CONTRACT"}); rec.Code != http.StatusConflict {
		t.Fatalf("ship before payment = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(router, http.MethodPost, orderPath+"/cancel", seller, map[string]any{"reason": "not allowed"}); rec.Code != http.StatusForbidden {
		t.Fatalf("seller cancel pending = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(router, http.MethodPost, orderPath+"/pay", buyer, map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("buyer pay = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(router, http.MethodPost, orderPath+"/pay", buyer, map[string]any{}); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate pay = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(router, http.MethodPost, orderPath+"/cancel", buyer, map[string]any{"reason": "not allowed"}); rec.Code != http.StatusForbidden {
		t.Fatalf("buyer cancel paid = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(router, http.MethodPost, orderPath+"/ship", seller, map[string]any{"tracking_no": "SF-CONTRACT"}); rec.Code != http.StatusOK {
		t.Fatalf("seller ship = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(router, http.MethodPost, orderPath+"/confirm", seller, map[string]any{}); rec.Code != http.StatusForbidden {
		t.Fatalf("seller confirm = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(router, http.MethodPost, orderPath+"/confirm", buyer, map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("buyer confirm = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(router, http.MethodPost, orderPath+"/confirm", buyer, map[string]any{}); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate confirm = %d body=%s", rec.Code, rec.Body.String())
	}
}
