package admin

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
	"aigc-3d-platform/apps/api/internal/generation"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type adminAuthResponse struct {
	AccessToken string `json:"access_token"`
	User        struct {
		ID string `json:"id"`
	} `json:"user"`
}

func setupAdminServer(t *testing.T) (*gin.Engine, *gorm.DB, *auth.Handler, *generation.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	authHandler, err := auth.New(db, auth.Config{JWTSecret: "a-development-secret-with-32-characters", Issuer: "test", AccessTTL: time.Hour, RefreshTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	assets, err := asset.New(db, asset.NewMemoryStore("test-assets"), "test-assets")
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := generation.NewService(db, assets, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.New(db, assets, jobs); err != nil {
		t.Fatal(err)
	}
	handler, err := New(db, jobs)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	api := router.Group("/api/v1")
	authHandler.RegisterRoutes(api)
	handler.RegisterRoutes(api, authHandler.Authenticate())
	return router, db, authHandler, jobs
}

func registerAdminTestUser(t *testing.T, router http.Handler, db *gorm.DB, username string, adminRole bool) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload adminAuthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if adminRole {
		var user auth.User
		if err := db.Preload("Roles").First(&user, "id = ?", payload.User.ID).Error; err != nil {
			t.Fatal(err)
		}
		var role auth.Role
		if err := db.Where("code = ?", "ADMIN").First(&role).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&user).Association("Roles").Append(&role); err != nil {
			t.Fatal(err)
		}
	}
	return payload.AccessToken, payload.User.ID
}

func adminRequest(router http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAdminRoutesRequireRole(t *testing.T) {
	router, _, _, _ := setupAdminServer(t)
	userToken, _ := registerAdminTestUser(t, router, nil, "ordinary", false)
	if rec := adminRequest(router, http.MethodGet, "/api/v1/admin/users", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous admin request = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(router, http.MethodGet, "/api/v1/admin/users", userToken); rec.Code != http.StatusForbidden {
		t.Fatalf("ordinary user admin request = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminOffShelfAuditsAndLists(t *testing.T) {
	router, db, _, _ := setupAdminServer(t)
	adminToken, adminID := registerAdminTestUser(t, router, db, "admin01", true)
	sellerToken, sellerID := registerAdminTestUser(t, router, db, "seller01", false)
	product := catalog.Product{ID: uuid.NewString(), SellerID: sellerID, Title: "管理测试商品", Description: "desc", PriceCents: 1000, IPName: "原创", Category: "手办", Condition: "全新", Stock: 1, Status: catalog.StatusPublished}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	_ = sellerToken
	if rec := adminRequest(router, http.MethodGet, "/api/v1/admin/users?page=1&page_size=20", adminToken); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(adminID)) {
		t.Fatalf("admin users = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(router, http.MethodGet, "/api/v1/admin/products?status=PUBLISHED", adminToken); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(product.ID)) {
		t.Fatalf("admin products = %d body=%s", rec.Code, rec.Body.String())
	}
	rec := adminRequest(router, http.MethodPost, "/api/v1/admin/products/"+product.ID+"/off-shelf", adminToken)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"OFF_SHELF"`)) {
		t.Fatalf("off shelf = %d body=%s", rec.Code, rec.Body.String())
	}
	var stored catalog.Product
	if err := db.First(&stored, "id = ?", product.ID).Error; err != nil || stored.Status != catalog.StatusOffShelf {
		t.Fatalf("stored product = %+v err=%v", stored, err)
	}
	var audit AuditLog
	if err := db.Where("action = ? AND target_id = ?", "PRODUCT_OFF_SHELF", product.ID).First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.ActorID != adminID {
		t.Fatalf("audit actor = %s, want %s", audit.ActorID, adminID)
	}
	if rec := adminRequest(router, http.MethodGet, "/api/v1/admin/audit-logs", adminToken); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("PRODUCT_OFF_SHELF")) {
		t.Fatalf("audit logs = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminRetryCreatesOutboxAndAudit(t *testing.T) {
	router, db, _, jobs := setupAdminServer(t)
	adminToken, adminID := registerAdminTestUser(t, router, db, "admin02", true)
	job := generation.GenerationJob{ID: uuid.NewString(), UserID: adminID, IdempotencyKey: uuid.NewString(), RequestHash: "hash", Status: generation.StatusFailed, Stage: generation.StageGenerating, RawPrompt: "prompt", RequestPayload: []byte(`{"prompt":"prompt"}`), Provider: "mock", Attempt: 1, MaxAttempts: 3, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	rec := adminRequest(router, http.MethodPost, "/api/v1/admin/generation-jobs/"+job.ID+"/retry", adminToken)
	if rec.Code != http.StatusAccepted || !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"QUEUED"`)) {
		t.Fatalf("retry = %d body=%s", rec.Code, rec.Body.String())
	}
	var updated generation.GenerationJob
	if err := db.First(&updated, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Status != generation.StatusQueued || updated.Attempt != 2 {
		t.Fatalf("updated job = %+v", updated)
	}
	var outbox generation.GenerationOutbox
	if err := db.Where("aggregate_id = ?", job.ID).First(&outbox).Error; err != nil {
		t.Fatal(err)
	}
	if outbox.EventType != generation.JobCreatedEvent {
		t.Fatalf("outbox event = %s", outbox.EventType)
	}
	var audit AuditLog
	if err := db.Where("action = ? AND target_id = ?", "GENERATION_RETRY", job.ID).First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	_ = jobs
}
