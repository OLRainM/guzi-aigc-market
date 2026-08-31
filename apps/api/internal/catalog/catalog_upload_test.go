package catalog

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aigc-3d-platform/apps/api/internal/asset"
	"aigc-3d-platform/apps/api/internal/auth"
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

type productBody struct {
	Product struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Images []struct {
			ID         string `json:"id"`
			ContentURL string `json:"content_url"`
		} `json:"images"`
		Model *struct {
			ID string `json:"id"`
		} `json:"model"`
	} `json:"product"`
}

func setupCatalogServer(t *testing.T) (*gin.Engine, *auth.Handler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
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
	catalogHandler, err := New(db, assets)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	api := router.Group("/api/v1")
	authHandler.RegisterRoutes(api)
	catalogHandler.RegisterRoutes(api, authHandler.Authenticate(), authHandler.ResolveUser)
	return router, authHandler
}

func registerUser(t *testing.T, router http.Handler, username string) string {
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

func createDraft(t *testing.T, router http.Handler, token string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"title": "测试手办", "description": "完整描述", "price_cents": 12900,
		"ip_name": "原创", "category": "手办", "condition": "全新", "stock": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload productBody
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Product.ID
}

func uploadFile(router http.Handler, token, productID, path, filename string, data []byte) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", filename)
	_, _ = part.Write(data)
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+productID+path, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func jpegBytes() []byte {
	return []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00}
}

func glbBytes() []byte {
	return append([]byte("glTF"), 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
}

func TestProductAssetUploadAndPublish(t *testing.T) {
	router, _ := setupCatalogServer(t)
	token := registerUser(t, router, "seller01")
	productID := createDraft(t, router, token)

	publish := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+productID+"/publish", nil)
	publish.Header.Set("Authorization", "Bearer "+token)
	publishRec := httptest.NewRecorder()
	router.ServeHTTP(publishRec, publish)
	if publishRec.Code != http.StatusBadRequest || !strings.Contains(publishRec.Body.String(), "ASSET_REQUIRED") {
		t.Fatalf("publish without image = %d %s", publishRec.Code, publishRec.Body.String())
	}

	rejected := uploadFile(router, token, productID, "/images", "cover.jpg", []byte("not-an-image"))
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("invalid image status = %d body = %s", rejected.Code, rejected.Body.String())
	}

	uploaded := uploadFile(router, token, productID, "/images", "cover.jpg", jpegBytes())
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("upload image status = %d body = %s", uploaded.Code, uploaded.Body.String())
	}
	var imagePayload productBody
	if err := json.Unmarshal(uploaded.Body.Bytes(), &imagePayload); err != nil {
		t.Fatal(err)
	}
	if len(imagePayload.Product.Images) != 1 {
		t.Fatalf("expected 1 image, got %#v", imagePayload.Product.Images)
	}

	model := uploadFile(router, token, productID, "/model", "model.glb", glbBytes())
	if model.Code != http.StatusCreated {
		t.Fatalf("upload model status = %d body = %s", model.Code, model.Body.String())
	}

	publish = httptest.NewRequest(http.MethodPost, "/api/v1/products/"+productID+"/publish", nil)
	publish.Header.Set("Authorization", "Bearer "+token)
	publishRec = httptest.NewRecorder()
	router.ServeHTTP(publishRec, publish)
	if publishRec.Code != http.StatusOK {
		t.Fatalf("publish status = %d body = %s", publishRec.Code, publishRec.Body.String())
	}

	content := httptest.NewRequest(http.MethodGet, imagePayload.Product.Images[0].ContentURL, nil)
	contentRec := httptest.NewRecorder()
	router.ServeHTTP(contentRec, content)
	if contentRec.Code != http.StatusOK {
		t.Fatalf("content status = %d body = %s", contentRec.Code, contentRec.Body.String())
	}
	if contentRec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("content type = %s", contentRec.Header().Get("Content-Type"))
	}
	body, _ := io.ReadAll(contentRec.Body)
	if !bytes.Equal(body, jpegBytes()) {
		t.Fatalf("unexpected content bytes: %x", body)
	}
}

func TestProductAssetOwnership(t *testing.T) {
	router, _ := setupCatalogServer(t)
	seller := registerUser(t, router, "seller02")
	other := registerUser(t, router, "buyer02")
	productID := createDraft(t, router, seller)
	rec := uploadFile(router, other, productID, "/images", "cover.jpg", jpegBytes())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-user upload status = %d body = %s", rec.Code, rec.Body.String())
	}
}
