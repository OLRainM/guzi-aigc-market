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
	"aigc-3d-platform/apps/api/internal/generation"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
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
	catalogHandler, err := New(db, assets, nil)
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
	unpublishedContent := httptest.NewRequest(http.MethodGet, imagePayload.Product.Images[0].ContentURL, nil)
	unpublishedContent.Header.Set("Authorization", "Bearer "+token)
	unpublishedContentRec := httptest.NewRecorder()
	router.ServeHTTP(unpublishedContentRec, unpublishedContent)
	if unpublishedContentRec.Code != http.StatusNotFound || !strings.Contains(unpublishedContentRec.Body.String(), "PRODUCT_NOT_FOUND") {
		t.Fatalf("draft content status = %d body = %s", unpublishedContentRec.Code, unpublishedContentRec.Body.String())
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

	offShelf := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+productID+"/off-shelf", nil)
	offShelf.Header.Set("Authorization", "Bearer "+token)
	offShelfRec := httptest.NewRecorder()
	router.ServeHTTP(offShelfRec, offShelf)
	if offShelfRec.Code != http.StatusOK || !strings.Contains(offShelfRec.Body.String(), `"status":"OFF_SHELF"`) {
		t.Fatalf("off shelf status = %d body = %s", offShelfRec.Code, offShelfRec.Body.String())
	}

	republish := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+productID+"/publish", nil)
	republish.Header.Set("Authorization", "Bearer "+token)
	republishRec := httptest.NewRecorder()
	router.ServeHTTP(republishRec, republish)
	if republishRec.Code != http.StatusOK || !strings.Contains(republishRec.Body.String(), `"status":"PUBLISHED"`) {
		t.Fatalf("republish status = %d body = %s", republishRec.Code, republishRec.Body.String())
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

func setupCatalogWithGeneration(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	t.Setenv("WORKER_INTERNAL_TOKEN", "worker-token")
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
	optimizerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"raw_prompt":"a collectible figure","product_type":"手办","optimized_prompt":"a collectible figure optimized","structured_prompt":{"text_to_3d_prompt":"a collectible figure optimized"},"rag_context":{"mode":"test"},"source":"test"}`)
	}))
	t.Cleanup(optimizerServer.Close)
	t.Setenv("WORKER_INTERNAL_URL", optimizerServer.URL)
	t.Setenv("WORKER_INTERNAL_TOKEN", "worker-token")
	generationHandler, err := generation.New(db, assets, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	catalogHandler, err := New(db, assets, generationHandler.Service())
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	api := router.Group("/api/v1")
	authHandler.RegisterRoutes(api)
	catalogHandler.RegisterRoutes(api, authHandler.Authenticate(), authHandler.ResolveUser)
	generationHandler.RegisterRoutes(api, authHandler.Authenticate())
	return router, "worker-token"
}

func promptPreviewRequest(t *testing.T, router http.Handler, token, prompt, productType string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"prompt": prompt, "product_type": productType})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs/prompt-preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("preview status = %d body = %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.ID == "" {
		t.Fatal("preview id is empty")
	}
	return preview.ID
}

func completeGenerationJob(t *testing.T, router http.Handler, token, workerToken string) string {
	t.Helper()
	previewID := promptPreviewRequest(t, router, token, "a collectible figure", "手办")
	body, _ := json.Marshal(map[string]any{
		"prompt_preview_id": previewID, "final_prompt": "a collectible figure optimized", "provider": "mock", "copyright_confirmed": true,
	})
	create := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs", bytes.NewReader(body))
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("Authorization", "Bearer "+token)
	create.Header.Set("Idempotency-Key", uuid.NewString())
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, create)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create job status = %d body = %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	claimBody, _ := json.Marshal(map[string]any{"attempt": 1})
	claim := httptest.NewRequest(http.MethodPost, "/api/v1/internal/generation-jobs/"+created.Job.ID+"/claim", bytes.NewReader(claimBody))
	claim.Header.Set("Content-Type", "application/json")
	claim.Header.Set("X-Worker-Token", workerToken)
	claimRec := httptest.NewRecorder()
	router.ServeHTTP(claimRec, claim)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim status = %d body = %s", claimRec.Code, claimRec.Body.String())
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("attempt", "1")
	part, _ := writer.CreateFormFile("file", "model.glb")
	_, _ = part.Write(glbBytes())
	_ = writer.Close()
	complete := httptest.NewRequest(http.MethodPost, "/api/v1/internal/generation-jobs/"+created.Job.ID+"/complete", &buf)
	complete.Header.Set("Content-Type", writer.FormDataContentType())
	complete.Header.Set("X-Worker-Token", workerToken)
	completeRec := httptest.NewRecorder()
	router.ServeHTTP(completeRec, complete)
	if completeRec.Code != http.StatusOK {
		t.Fatalf("complete status = %d body = %s", completeRec.Code, completeRec.Body.String())
	}
	return created.Job.ID
}

func TestCreateProductFromSucceededGeneration(t *testing.T) {
	router, workerToken := setupCatalogWithGeneration(t)
	token := registerUser(t, router, "seller03")
	other := registerUser(t, router, "buyer03")
	jobID := completeGenerationJob(t, router, token, workerToken)

	queuedPreviewID := promptPreviewRequest(t, router, token, "queued figure", "手办")
	queuedBody, _ := json.Marshal(map[string]any{
		"prompt_preview_id": queuedPreviewID, "final_prompt": "queued figure optimized", "provider": "mock", "copyright_confirmed": true,
	})
	queued := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs", bytes.NewReader(queuedBody))
	queued.Header.Set("Content-Type", "application/json")
	queued.Header.Set("Authorization", "Bearer "+token)
	queued.Header.Set("Idempotency-Key", uuid.NewString())
	queuedRec := httptest.NewRecorder()
	router.ServeHTTP(queuedRec, queued)
	if queuedRec.Code != http.StatusAccepted {
		t.Fatalf("queued job status = %d body = %s", queuedRec.Code, queuedRec.Body.String())
	}
	var queuedJob struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(queuedRec.Body.Bytes(), &queuedJob); err != nil {
		t.Fatal(err)
	}

	notReady, _ := json.Marshal(map[string]any{
		"title": "测试手办", "description": "完整描述", "price_cents": 12900,
		"ip_name": "原创", "category": "手办", "condition": "全新", "stock": 1,
		"generation_job_id": queuedJob.Job.ID,
	})
	notReadyReq := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewReader(notReady))
	notReadyReq.Header.Set("Content-Type", "application/json")
	notReadyReq.Header.Set("Authorization", "Bearer "+token)
	notReadyRec := httptest.NewRecorder()
	router.ServeHTTP(notReadyRec, notReadyReq)
	if notReadyRec.Code != http.StatusConflict || !strings.Contains(notReadyRec.Body.String(), "GENERATION_OUTPUT_NOT_READY") {
		t.Fatalf("not ready status = %d body = %s", notReadyRec.Code, notReadyRec.Body.String())
	}

	foreign, _ := json.Marshal(map[string]any{
		"title": "测试手办", "description": "完整描述", "price_cents": 12900,
		"ip_name": "原创", "category": "手办", "condition": "全新", "stock": 1,
		"generation_job_id": jobID,
	})
	foreignReq := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewReader(foreign))
	foreignReq.Header.Set("Content-Type", "application/json")
	foreignReq.Header.Set("Authorization", "Bearer "+other)
	foreignRec := httptest.NewRecorder()
	router.ServeHTTP(foreignRec, foreignReq)
	if foreignRec.Code != http.StatusNotFound {
		t.Fatalf("foreign job status = %d body = %s", foreignRec.Code, foreignRec.Body.String())
	}

	created, _ := json.Marshal(map[string]any{
		"title": "测试手办", "description": "完整描述", "price_cents": 12900,
		"ip_name": "原创", "category": "手办", "condition": "全新", "stock": 1,
		"generation_job_id": jobID,
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewReader(created))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create from job status = %d body = %s", createRec.Code, createRec.Body.String())
	}
	var payload productBody
	if err := json.Unmarshal(createRec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Product.Model == nil || payload.Product.Model.ID == "" {
		t.Fatalf("expected copied model, got %#v", payload.Product)
	}

	publish := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+payload.Product.ID+"/publish", nil)
	publish.Header.Set("Authorization", "Bearer "+token)
	publishRec := httptest.NewRecorder()
	router.ServeHTTP(publishRec, publish)
	if publishRec.Code != http.StatusBadRequest || !strings.Contains(publishRec.Body.String(), "ASSET_REQUIRED") {
		t.Fatalf("publish without image = %d %s", publishRec.Code, publishRec.Body.String())
	}

	uploaded := uploadFile(router, token, payload.Product.ID, "/images", "cover.jpg", jpegBytes())
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("upload image status = %d body = %s", uploaded.Code, uploaded.Body.String())
	}
	publish = httptest.NewRequest(http.MethodPost, "/api/v1/products/"+payload.Product.ID+"/publish", nil)
	publish.Header.Set("Authorization", "Bearer "+token)
	publishRec = httptest.NewRecorder()
	router.ServeHTTP(publishRec, publish)
	if publishRec.Code != http.StatusOK {
		t.Fatalf("publish status = %d body = %s", publishRec.Code, publishRec.Body.String())
	}
}
