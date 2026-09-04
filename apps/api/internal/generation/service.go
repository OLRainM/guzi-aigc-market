package generation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"aigc-3d-platform/apps/api/internal/asset"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxActiveJobsPerUser = 5

var (
	errInvalidArgument     = errors.New("invalid generation argument")
	errIdempotencyConflict = errors.New("idempotency conflict")
	errTooManyJobs         = errors.New("too many active generation jobs")
	errJobNotFound         = errors.New("generation job not found")
	errInvalidTransition   = errors.New("invalid generation job transition")
)

type Service struct {
	db        *gorm.DB
	assets    *asset.Service
	rdb       *redis.Client
	timeout   time.Duration
	now       func() time.Time
	publisher Publisher
}

func NewService(db *gorm.DB, assets *asset.Service, rdb *redis.Client, timeout time.Duration) (*Service, error) {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	if err := db.AutoMigrate(&GenerationJob{}, &GenerationOutput{}, &GenerationOutbox{}); err != nil {
		return nil, err
	}
	service := &Service{
		db:      db,
		assets:  assets,
		rdb:     rdb,
		timeout: timeout,
		now:     time.Now,
	}
	service.publisher = NewRedisPublisher(rdb)
	return service, nil
}

func (s *Service) Create(ctx context.Context, userID, idempotencyKey, requestID string, req CreateJobRequest) (*GenerationJob, error) {
	if _, err := uuid.Parse(idempotencyKey); err != nil {
		return nil, errInvalidArgument
	}
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}
	hash, payload, err := hashRequest(req)
	if err != nil {
		return nil, err
	}
	var existing GenerationJob
	err = s.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).First(&existing).Error
	if err == nil {
		if existing.RequestHash != hash {
			return nil, errIdempotencyConflict
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var active int64
	if err := s.db.WithContext(ctx).Model(&GenerationJob{}).Where("user_id = ? AND status IN ?", userID, []Status{StatusQueued, StatusRunning}).Count(&active).Error; err != nil {
		return nil, err
	}
	if active >= maxActiveJobsPerUser {
		return nil, errTooManyJobs
	}

	now := s.now().UTC()
	job := GenerationJob{
		ID:             uuid.NewString(),
		UserID:         userID,
		IdempotencyKey: idempotencyKey,
		RequestHash:    hash,
		Status:         StatusQueued,
		Stage:          StageQueued,
		Progress:       0,
		RawPrompt:      strings.TrimSpace(req.Prompt),
		RequestPayload: payload,
		Provider:       normalizeProvider(req.Provider),
		Attempt:        1,
		MaxAttempts:    3,
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	message := StreamMessage{
		SchemaVersion: MessageVersion,
		EventType:     JobCreatedEvent,
		JobID:         job.ID,
		UserID:        userID,
		Attempt:       job.Attempt,
		RequestID:     requestID,
		CreatedAt:     now.Format(time.RFC3339),
	}
	payloadBytes, err := json.Marshal(message.Fields())
	if err != nil {
		return nil, err
	}
	outbox := GenerationOutbox{
		ID:          uuid.NewString(),
		AggregateID: job.ID,
		EventType:   JobCreatedEvent,
		Payload:     payloadBytes,
		Status:      OutboxPending,
		AvailableAt: now,
		CreatedAt:   now,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND status IN ?", userID, []Status{StatusQueued, StatusRunning}).Find(&[]GenerationJob{}).Error; err != nil {
			return err
		}
		var lockedActive int64
		if err := tx.Model(&GenerationJob{}).Where("user_id = ? AND status IN ?", userID, []Status{StatusQueued, StatusRunning}).Count(&lockedActive).Error; err != nil {
			return err
		}
		if lockedActive >= maxActiveJobsPerUser {
			return errTooManyJobs
		}
		if err := tx.Create(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return errIdempotencyConflict
			}
			return err
		}
		return tx.Create(&outbox).Error
	}); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			var conflict GenerationJob
			if loadErr := s.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).First(&conflict).Error; loadErr == nil {
				if conflict.RequestHash != hash {
					return nil, errIdempotencyConflict
				}
				return &conflict, nil
			}
		}
		return nil, err
	}
	return &job, nil
}

func (s *Service) ToResponse(job GenerationJob, outputs []GenerationOutput) JobResponse {
	if outputs == nil {
		outputs = []GenerationOutput{}
	}
	for i := range outputs {
		if outputs[i].AssetID != nil && *outputs[i].AssetID != "" {
			outputs[i].ContentURL = "/api/v1/generation-jobs/" + job.ID + "/outputs/" + *outputs[i].AssetID + "/content"
		}
	}
	var jobErr *JobError
	if job.ErrorCode != nil {
		message := ""
		if job.ErrorMessage != nil {
			message = *job.ErrorMessage
		}
		jobErr = &JobError{Code: *job.ErrorCode, Message: message, Retryable: job.Status == StatusFailed && job.Attempt < job.MaxAttempts}
	}
	return JobResponse{
		ID: job.ID, SourceJobID: job.SourceJobID, Status: job.Status, Stage: job.Stage, Progress: job.Progress,
		RawPrompt: job.RawPrompt, OptimizedPrompt: job.OptimizedPrompt, ProductType: productTypeFromPayload(job.RequestPayload),
		Provider: job.Provider, Attempt: job.Attempt, MaxAttempts: job.MaxAttempts, Outputs: outputs, Error: jobErr,
		CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt, UpdatedAt: job.UpdatedAt,
	}
}

func validateCreateRequest(req CreateJobRequest) error {
	prompt := strings.TrimSpace(req.Prompt)
	productType := strings.TrimSpace(req.ProductType)
	if prompt == "" || utf8.RuneCountInString(prompt) > 2000 {
		return errInvalidArgument
	}
	if productType == "" || utf8.RuneCountInString(productType) > 64 {
		return errInvalidArgument
	}
	if !allowedProvider(req.Provider) {
		return errInvalidArgument
	}
	if !req.CopyrightConfirmed {
		return errInvalidArgument
	}
	return nil
}

func hashRequest(req CreateJobRequest) (string, json.RawMessage, error) {
	payload := map[string]any{
		"prompt":               strings.TrimSpace(req.Prompt),
		"product_type":         strings.TrimSpace(req.ProductType),
		"provider":             req.Provider,
		"parameters":           req.Parameters,
		"copyright_confirmed":  req.CopyrightConfirmed,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), raw, nil
}

func allowedProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "mock", "http", "hy3d", "hy-3d", "tokenhub":
		return true
	default:
		return false
	}
}

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "hy3d", "hy-3d", "tokenhub":
		return "hy3d"
	case "http", "external", "manual":
		return "http"
	default:
		return "mock"
	}
}

func productTypeFromPayload(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var body struct {
		ProductType string `json:"product_type"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	return strings.TrimSpace(body.ProductType)
}
