package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aigc-3d-platform/apps/api/internal/asset"
	"aigc-3d-platform/apps/api/internal/auth"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type authBody struct {
	AccessToken string `json:"access_token"`
}

type testPromptOptimizer struct{}

func (testPromptOptimizer) Optimize(_ context.Context, input PromptPreviewRequest) (*PromptPreviewResponse, error) {
	return &PromptPreviewResponse{RawPrompt: input.Prompt, ProductType: input.ProductType, OptimizedPrompt: input.Prompt + " optimized", StructuredPrompt: map[string]any{"text_to_3d_prompt": input.Prompt + " optimized"}, RAGContext: map[string]any{"mode": "test"}, Source: "test"}, nil
}

func setupGenerationServer(t *testing.T) *gin.Engine {
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
	generationHandler, err := New(db, assets, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	generationHandler.Service().optimizer = testPromptOptimizer{}
	router := gin.New()
	api := router.Group("/api/v1")
	authHandler.RegisterRoutes(api)
	generationHandler.RegisterRoutes(api, authHandler.Authenticate())
	return router
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

func promptPreviewRequest(t *testing.T, router http.Handler, token, prompt string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"prompt": prompt, "product_type": "figure"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs/prompt-preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("preview status = %d body = %s", rec.Code, rec.Body.String())
	}
	var preview PromptPreviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	return preview.ID
}

func createJobRequestForTest(t *testing.T, router http.Handler, token, prompt string) []byte {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"prompt_preview_id": promptPreviewRequest(t, router, token, prompt), "final_prompt": prompt + " optimized", "provider": "mock", "copyright_confirmed": true,
	})
	return body
}

func rawCreateJobRequest(promptPreviewID, finalPrompt string) []byte {
	body, _ := json.Marshal(map[string]any{
		"prompt_preview_id": promptPreviewID, "final_prompt": finalPrompt, "provider": "mock", "copyright_confirmed": true,
	})
	return body
}

func TestCreateGenerationJob(t *testing.T) {
	router := setupGenerationServer(t)
	token := registerUser(t, router, "creator")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs", bytes.NewReader(createJobRequestForTest(t, router, token, "a collectible figure")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Job struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			Stage    string `json:"stage"`
			Progress int    `json:"progress"`
			Provider string `json:"provider"`
			Attempt  int    `json:"attempt"`
		} `json:"job"`
		StatusURL string `json:"status_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Job.ID == "" || body.Job.Status != string(StatusQueued) || body.Job.Stage != string(StageQueued) || body.Job.Progress != 0 || body.Job.Provider != "mock" || body.Job.Attempt != 1 {
		t.Fatalf("unexpected job: %+v", body.Job)
	}
	if body.StatusURL != "/api/v1/generation-jobs/"+body.Job.ID {
		t.Fatalf("status_url = %s", body.StatusURL)
	}
}

func TestCreateGenerationJobRequiresAuth(t *testing.T) {
	router := setupGenerationServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs", bytes.NewReader(rawCreateJobRequest(uuid.NewString(), "a collectible figure")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", uuid.NewString())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreateGenerationJobRejectsInvalidPrompt(t *testing.T) {
	router := setupGenerationServer(t)
	token := registerUser(t, router, "creator")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs", bytes.NewReader(rawCreateJobRequest(uuid.NewString(), "   ")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreateGenerationJobIdempotentReplay(t *testing.T) {
	router := setupGenerationServer(t)
	token := registerUser(t, router, "creator")
	previewID := promptPreviewRequest(t, router, token, "a collectible figure")
	payload := rawCreateJobRequest(previewID, "a collectible figure optimized")
	key := uuid.NewString()
	first := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs", bytes.NewReader(payload))
	first.Header.Set("Content-Type", "application/json")
	first.Header.Set("Authorization", "Bearer "+token)
	first.Header.Set("Idempotency-Key", key)
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, first)
	second := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs", bytes.NewReader(payload))
	second.Header.Set("Content-Type", "application/json")
	second.Header.Set("Authorization", "Bearer "+token)
	second.Header.Set("Idempotency-Key", key)
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, second)
	if firstRec.Code != http.StatusAccepted || secondRec.Code != http.StatusAccepted {
		t.Fatalf("status first=%d second=%d", firstRec.Code, secondRec.Code)
	}
	var firstBody, secondBody struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	_ = json.Unmarshal(firstRec.Body.Bytes(), &firstBody)
	_ = json.Unmarshal(secondRec.Body.Bytes(), &secondBody)
	if firstBody.Job.ID == "" || firstBody.Job.ID != secondBody.Job.ID {
		t.Fatalf("idempotent replay should return the same job id: %s vs %s", firstBody.Job.ID, secondBody.Job.ID)
	}
}

func TestCreateGenerationJobIdempotencyConflict(t *testing.T) {
	router := setupGenerationServer(t)
	token := registerUser(t, router, "creator")
	firstPreviewID := promptPreviewRequest(t, router, token, "first prompt")
	secondPreviewID := promptPreviewRequest(t, router, token, "second prompt")
	key := uuid.NewString()
	first := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs", bytes.NewReader(rawCreateJobRequest(firstPreviewID, "first prompt optimized")))
	first.Header.Set("Content-Type", "application/json")
	first.Header.Set("Authorization", "Bearer "+token)
	first.Header.Set("Idempotency-Key", key)
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, first)
	second := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs", bytes.NewReader(rawCreateJobRequest(secondPreviewID, "second prompt optimized")))
	second.Header.Set("Content-Type", "application/json")
	second.Header.Set("Authorization", "Bearer "+token)
	second.Header.Set("Idempotency-Key", key)
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, second)
	if firstRec.Code != http.StatusAccepted || secondRec.Code != http.StatusConflict {
		t.Fatalf("status first=%d second=%d body=%s", firstRec.Code, secondRec.Code, secondRec.Body.String())
	}
}
