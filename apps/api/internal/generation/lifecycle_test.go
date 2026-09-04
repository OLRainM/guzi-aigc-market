package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
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

func setupWorkerRouter(t *testing.T) (*Service, *gin.Engine, string) {
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
	service, err := NewService(db, assets, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{service: service, workerToken: "worker-token"}
	router := gin.New()
	api := router.Group("/api/v1")
	authHandler.RegisterRoutes(api)
	handler.RegisterRoutes(api, authHandler.Authenticate())
	return service, router, "worker-token"
}

func jobJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode: %v body=%s", err, rec.Body.String())
	}
	job, _ := body["job"].(map[string]any)
	if job == nil {
		t.Fatalf("missing job: %s", rec.Body.String())
	}
	return job
}

func createJobWithToken(t *testing.T, router http.Handler, token string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/generation-jobs", bytes.NewReader(createJobRequest("a collectible figure")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	return jobJSON(t, rec)["id"].(string)
}

func workerJSON(method, path, token string, payload any) *http.Request {
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Worker-Token", token)
	return req
}

func authorized(method, path, token string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func minimalGLB() []byte {
	jsonChunk := []byte(`{"asset":{"version":"2.0"},"scene":0,"scenes":[{"nodes":[0]}],"nodes":[{"mesh":0}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3","max":[1.0,1.0,0.0],"min":[0.0,0.0,0.0]}],"bufferViews":[{"buffer":0,"byteLength":36}],"buffers":[{"byteLength":36}]}`)
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	binChunk := make([]byte, 36)
	jsonPart := append(append(u32(uint32(len(jsonChunk))), []byte("JSON")...), jsonChunk...)
	binPart := append(append(u32(uint32(len(binChunk))), []byte("BIN\x00")...), binChunk...)
	body := append(jsonPart, binPart...)
	header := append(append([]byte("glTF"), u32(2)...), u32(uint32(12+len(body)))...)
	return append(header, body...)
}

func u32(value uint32) []byte {
	return []byte{byte(value), byte(value >> 8), byte(value >> 16), byte(value >> 24)}
}

func TestGetGenerationJob(t *testing.T) {
	_, router, _ := setupWorkerRouter(t)
	token := registerUser(t, router, "creator")
	jobID := createJobWithToken(t, router, token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authorized(http.MethodGet, "/api/v1/generation-jobs/"+jobID, token))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	job := jobJSON(t, rec)
	if job["id"] != jobID || job["status"] != string(StatusQueued) {
		t.Fatalf("unexpected job: %#v", job)
	}
}

func TestWorkerProgressStoresOptimizedPrompt(t *testing.T) {
	_, router, workerToken := setupWorkerRouter(t)
	token := registerUser(t, router, "creator")
	jobID := createJobWithToken(t, router, token)
	claim := httptest.NewRecorder()
	router.ServeHTTP(claim, workerJSON(http.MethodPost, "/api/v1/internal/generation-jobs/"+jobID+"/claim", workerToken, map[string]any{"attempt": 1}))
	if claim.Code != http.StatusOK {
		t.Fatalf("claim status = %d body = %s", claim.Code, claim.Body.String())
	}
	progress := httptest.NewRecorder()
	router.ServeHTTP(progress, workerJSON(http.MethodPost, "/api/v1/internal/generation-jobs/"+jobID+"/progress", workerToken, map[string]any{
		"attempt": 1, "stage": string(StageOptimizingPrompt), "progress": 25,
		"optimized_prompt": "棉花娃，软填充布料分片，干净拓扑，适合导出 GLB。",
		"rag_version":      "1.0.0",
		"template_version": "text-to-3d-template.zh-CN@1.0.0",
		"rag_context":      map[string]any{"mode": "rag_fallback", "terms": []string{"棉花娃"}},
	}))
	if progress.Code != http.StatusOK {
		t.Fatalf("progress status = %d body = %s", progress.Code, progress.Body.String())
	}
	job := jobJSON(t, progress)
	if job["optimized_prompt"] != "棉花娃，软填充布料分片，干净拓扑，适合导出 GLB。" {
		t.Fatalf("optimized_prompt = %#v", job["optimized_prompt"])
	}
	if job["stage"] != string(StageOptimizingPrompt) {
		t.Fatalf("stage = %#v", job["stage"])
	}
}

func TestGetGenerationJobHidesOtherUsers(t *testing.T) {
	_, router, _ := setupWorkerRouter(t)
	owner := registerUser(t, router, "owner")
	jobID := createJobWithToken(t, router, owner)
	other := registerUser(t, router, "other")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authorized(http.MethodGet, "/api/v1/generation-jobs/"+jobID, other))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestWorkerCompletesJobWithGLB(t *testing.T) {
	_, router, workerToken := setupWorkerRouter(t)
	token := registerUser(t, router, "creator")
	jobID := createJobWithToken(t, router, token)
	claim := httptest.NewRecorder()
	router.ServeHTTP(claim, workerJSON(http.MethodPost, "/api/v1/internal/generation-jobs/"+jobID+"/claim", workerToken, map[string]any{"attempt": 1}))
	if claim.Code != http.StatusOK {
		t.Fatalf("claim status = %d body = %s", claim.Code, claim.Body.String())
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("attempt", "1")
	_ = writer.WriteField("provider_job_id", "mock-1")
	part, err := writer.CreateFormFile("file", "model.glb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(minimalGLB()); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	complete := httptest.NewRequest(http.MethodPost, "/api/v1/internal/generation-jobs/"+jobID+"/complete", body)
	complete.Header.Set("Content-Type", writer.FormDataContentType())
	complete.Header.Set("X-Worker-Token", workerToken)
	completeRec := httptest.NewRecorder()
	router.ServeHTTP(completeRec, complete)
	if completeRec.Code != http.StatusOK {
		t.Fatalf("complete status = %d body = %s", completeRec.Code, completeRec.Body.String())
	}
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, authorized(http.MethodGet, "/api/v1/generation-jobs/"+jobID, token))
	job := jobJSON(t, getRec)
	if job["status"] != string(StatusSucceeded) || job["progress"] != float64(100) {
		t.Fatalf("unexpected completed job: %#v", job)
	}
	outputs, _ := job["outputs"].([]any)
	if len(outputs) != 1 {
		t.Fatalf("outputs = %#v", job["outputs"])
	}
	contentURL, _ := outputs[0].(map[string]any)["content_url"].(string)
	contentRec := httptest.NewRecorder()
	router.ServeHTTP(contentRec, authorized(http.MethodGet, contentURL, token))
	if contentRec.Code != http.StatusOK {
		t.Fatalf("content status = %d body = %s", contentRec.Code, contentRec.Body.String())
	}
	got, _ := io.ReadAll(contentRec.Body)
	if !bytes.HasPrefix(got, []byte("glTF")) {
		t.Fatalf("content is not glb")
	}
}

func TestCancelQueuedJob(t *testing.T) {
	_, router, _ := setupWorkerRouter(t)
	token := registerUser(t, router, "creator")
	jobID := createJobWithToken(t, router, token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authorized(http.MethodPost, "/api/v1/generation-jobs/"+jobID+"/cancel", token))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if jobJSON(t, rec)["status"] != string(StatusCanceled) {
		t.Fatalf("expected canceled, got %#v", jobJSON(t, rec))
	}
}

func TestRetryFailedJob(t *testing.T) {
	_, router, workerToken := setupWorkerRouter(t)
	token := registerUser(t, router, "creator")
	jobID := createJobWithToken(t, router, token)
	fail := httptest.NewRecorder()
	router.ServeHTTP(fail, workerJSON(http.MethodPost, "/api/v1/internal/generation-jobs/"+jobID+"/fail", workerToken, map[string]any{
		"attempt": 1, "error_code": "PROVIDER_TIMEOUT", "error_message": "timeout", "retryable": true,
	}))
	if fail.Code != http.StatusOK {
		t.Fatalf("fail status = %d body = %s", fail.Code, fail.Body.String())
	}
	retry := httptest.NewRecorder()
	router.ServeHTTP(retry, authorized(http.MethodPost, "/api/v1/generation-jobs/"+jobID+"/retry", token))
	if retry.Code != http.StatusAccepted {
		t.Fatalf("retry status = %d body = %s", retry.Code, retry.Body.String())
	}
	job := jobJSON(t, retry)
	if job["status"] != string(StatusQueued) || job["attempt"] != float64(2) {
		t.Fatalf("unexpected retry job: %#v", job)
	}
}

func TestFailTimedOutJobs(t *testing.T) {
	service, router, _ := setupWorkerRouter(t)
	token := registerUser(t, router, "creator")
	jobID := createJobWithToken(t, router, token)
	service.now = func() time.Time { return time.Now().UTC().Add(3 * time.Minute) }
	count, err := service.FailTimedOut(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("timed out count = %d", count)
	}
	var job GenerationJob
	if err := service.db.First(&job, "id = ?", jobID).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusFailed || job.ErrorCode == nil || *job.ErrorCode != "GENERATION_TIMEOUT" {
		t.Fatalf("unexpected timed out job: %+v", job)
	}
}
