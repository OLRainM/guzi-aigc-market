package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"aigc-3d-platform/apps/api/internal/asset"
	"aigc-3d-platform/apps/api/internal/auth"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type fakePublisher struct {
	mu       sync.Mutex
	messages []StreamMessage
	err      error
}

func (p *fakePublisher) Publish(_ context.Context, message StreamMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.messages = append(p.messages, message)
	return nil
}

func setupGenerationService(t *testing.T, rdb *redis.Client) (*Service, *gin.Engine) {
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
	service, err := NewService(db, assets, rdb, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	service.optimizer = testPromptOptimizer{}
	handler := &Handler{service: service}
	router := gin.New()
	api := router.Group("/api/v1")
	authHandler.RegisterRoutes(api)
	handler.RegisterRoutes(api, authHandler.Authenticate())
	return service, router
}

func createQueuedJob(t *testing.T, router http.Handler, username string) string {
	t.Helper()
	token := registerUser(t, router, username)
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
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Job.ID
}

func TestDispatchPublishesCreatedJobToRedisStream(t *testing.T) {
	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	service, router := setupGenerationService(t, rdb)
	jobID := createQueuedJob(t, router, "creator")
	if err := service.DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err := rdb.XRange(context.Background(), StreamName, "-", "+").Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("stream length = %d", len(messages))
	}
	fields := messages[0].Values
	if fields["event_type"] != JobCreatedEvent || fields["job_id"] != jobID || fields["schema_version"] != MessageVersion {
		t.Fatalf("unexpected stream fields: %#v", fields)
	}
	var outbox GenerationOutbox
	if err := service.db.Where("aggregate_id = ?", jobID).First(&outbox).Error; err != nil {
		t.Fatal(err)
	}
	if outbox.Status != OutboxPublished || outbox.PublishedAt == nil {
		t.Fatalf("outbox not published: %+v", outbox)
	}
}

func TestDispatchRetriesFailedPublish(t *testing.T) {
	service, router := setupGenerationService(t, nil)
	publisher := &fakePublisher{err: errors.New("redis unavailable")}
	service.publisher = publisher
	jobID := createQueuedJob(t, router, "creator")
	if err := service.DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var outbox GenerationOutbox
	if err := service.db.Where("aggregate_id = ?", jobID).First(&outbox).Error; err != nil {
		t.Fatal(err)
	}
	if outbox.Status != OutboxPending || outbox.Attempts != 1 || outbox.LastError == nil {
		t.Fatalf("expected pending retry, got %+v", outbox)
	}
	publisher.err = nil
	service.now = func() time.Time { return outbox.AvailableAt.Add(time.Millisecond) }
	if err := service.DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.db.Where("aggregate_id = ?", jobID).First(&outbox).Error; err != nil {
		t.Fatal(err)
	}
	if outbox.Status != OutboxPublished {
		t.Fatalf("expected published after retry, got %+v", outbox)
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if len(publisher.messages) != 1 {
		t.Fatalf("published messages = %d", len(publisher.messages))
	}
}
