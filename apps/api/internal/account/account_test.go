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
	catalogHandler, err := catalog.New(db, assets)
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
