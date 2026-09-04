package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"aigc-3d-platform/apps/api/internal/asset"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WorkerProgressRequest struct {
	Attempt               int             `json:"attempt"`
	Stage                 Stage           `json:"stage"`
	Progress              int             `json:"progress"`
	OptimizedPrompt       *string         `json:"optimized_prompt,omitempty"`
	ProductType           *string         `json:"product_type,omitempty"`
	RAGContext            json.RawMessage `json:"rag_context,omitempty"`
	RAGVersion            *string         `json:"rag_version,omitempty"`
	PromptTemplateVersion *string         `json:"template_version,omitempty"`
	StructuredPrompt      json.RawMessage `json:"structured_prompt,omitempty"`
}

type WorkerFailRequest struct {
	Attempt      int    `json:"attempt"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
	Retryable    bool   `json:"retryable"`
}

type WorkerCompleteRequest struct {
	Attempt       int    `json:"attempt"`
	ProviderJobID string `json:"provider_job_id"`
	Filename      string `json:"filename"`
	MIMEType      string `json:"mime_type"`
	Body          []byte `json:"-"`
}

func (s *Service) Get(ctx context.Context, userID, jobID string) (*GenerationJob, []GenerationOutput, error) {
	job, err := s.loadJob(ctx, jobID)
	if err != nil {
		return nil, nil, err
	}
	if job.UserID != userID {
		return nil, nil, errJobNotFound
	}
	outputs, err := s.listOutputs(ctx, job.ID)
	if err != nil {
		return nil, nil, err
	}
	return job, outputs, nil
}

func (s *Service) List(ctx context.Context, userID string, page, pageSize int) ([]GenerationJob, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	query := s.db.WithContext(ctx).Model(&GenerationJob{}).Where("user_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var jobs []GenerationJob
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&jobs).Error; err != nil {
		return nil, 0, err
	}
	return jobs, total, nil
}

func (s *Service) Claim(ctx context.Context, jobID string, attempt int) (*GenerationJob, error) {
	now := s.now().UTC()
	var claimed *GenerationJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := lockJob(tx, jobID)
		if err != nil {
			return err
		}
		if job.CancelRequestedAt != nil {
			if err := s.markCanceled(tx, job, now); err != nil {
				return err
			}
			claimed = job
			return nil
		}
		if job.Status == StatusRunning && job.Attempt == attempt {
			claimed = job
			return nil
		}
		if job.Status == StatusSucceeded || job.Status == StatusCanceled {
			claimed = job
			return nil
		}
		if job.Status != StatusQueued || job.Attempt != attempt {
			return errInvalidTransition
		}
		if err := ValidateTransition(job.Status, StatusRunning, false); err != nil {
			return errInvalidTransition
		}
		updates := map[string]any{
			"status":     StatusRunning,
			"stage":      StageOptimizingPrompt,
			"progress":   5,
			"started_at": now,
			"updated_at": now,
			"version":    job.Version + 1,
		}
		if err := tx.Model(job).Updates(updates).Error; err != nil {
			return err
		}
		job.Status = StatusRunning
		job.Stage = StageOptimizingPrompt
		job.Progress = 5
		job.StartedAt = &now
		job.UpdatedAt = now
		job.Version++
		claimed = job
		return nil
	})
	return claimed, err
}

func (s *Service) ReportProgress(ctx context.Context, jobID string, req WorkerProgressRequest) (*GenerationJob, error) {
	if !req.Stage.Valid() || ValidateProgress(StatusRunning, req.Progress) != nil {
		return nil, errInvalidArgument
	}
	now := s.now().UTC()
	var updated *GenerationJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := lockJob(tx, jobID)
		if err != nil {
			return err
		}
		if job.CancelRequestedAt != nil && job.Status == StatusRunning {
			if err := s.markCanceled(tx, job, now); err != nil {
				return err
			}
			updated = job
			return nil
		}
		if job.Status == StatusSucceeded || job.Status == StatusCanceled {
			updated = job
			return nil
		}
		if job.Status != StatusRunning || job.Attempt != req.Attempt {
			return errInvalidTransition
		}
		updates := map[string]any{
			"stage":      req.Stage,
			"progress":   req.Progress,
			"updated_at": now,
			"version":    job.Version + 1,
		}
		if req.OptimizedPrompt != nil {
			prompt := strings.TrimSpace(*req.OptimizedPrompt)
			if prompt != "" {
				updates["optimized_prompt"] = prompt
				job.OptimizedPrompt = &prompt
			}
		}
		if len(req.RAGContext) > 0 {
			updates["rag_context"] = req.RAGContext
			job.RAGContext = req.RAGContext
		}
		if req.RAGVersion != nil && strings.TrimSpace(*req.RAGVersion) != "" {
			version := strings.TrimSpace(*req.RAGVersion)
			updates["rag_version"] = version
			job.RAGVersion = &version
		}
		if req.PromptTemplateVersion != nil && strings.TrimSpace(*req.PromptTemplateVersion) != "" {
			version := strings.TrimSpace(*req.PromptTemplateVersion)
			updates["prompt_template_version"] = version
			job.PromptTemplateVersion = &version
		}
		if len(req.StructuredPrompt) > 0 {
			updates["structured_prompt"] = req.StructuredPrompt
			job.StructuredPrompt = req.StructuredPrompt
		}
		if err := tx.Model(job).Updates(updates).Error; err != nil {
			return err
		}
		job.Stage = req.Stage
		job.Progress = req.Progress
		job.UpdatedAt = now
		job.Version++
		updated = job
		return nil
	})
	return updated, err
}

func (s *Service) Fail(ctx context.Context, jobID string, req WorkerFailRequest) (*GenerationJob, error) {
	if strings.TrimSpace(req.ErrorCode) == "" {
		return nil, errInvalidArgument
	}
	now := s.now().UTC()
	message := strings.TrimSpace(req.ErrorMessage)
	if message == "" {
		message = "生成任务失败"
	}
	if len(message) > 500 {
		message = message[:500]
	}
	var updated *GenerationJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := lockJob(tx, jobID)
		if err != nil {
			return err
		}
		if job.CancelRequestedAt != nil {
			if err := s.markCanceled(tx, job, now); err != nil {
				return err
			}
			updated = job
			return nil
		}
		if job.Status == StatusFailed && job.Attempt == req.Attempt {
			updated = job
			return nil
		}
		if job.Status == StatusSucceeded || job.Status == StatusCanceled {
			updated = job
			return nil
		}
		if (job.Status != StatusQueued && job.Status != StatusRunning) || job.Attempt != req.Attempt {
			return errInvalidTransition
		}
		if err := ValidateTransition(job.Status, StatusFailed, false); err != nil {
			return errInvalidTransition
		}
		if err := tx.Model(job).Updates(map[string]any{
			"status":        StatusFailed,
			"stage":         job.Stage,
			"error_code":    req.ErrorCode,
			"error_message": message,
			"finished_at":   now,
			"updated_at":    now,
			"version":       job.Version + 1,
		}).Error; err != nil {
			return err
		}
		job.Status = StatusFailed
		job.ErrorCode = &req.ErrorCode
		job.ErrorMessage = &message
		job.FinishedAt = &now
		job.UpdatedAt = now
		job.Version++
		updated = job
		return nil
	})
	return updated, err
}

func (s *Service) Complete(ctx context.Context, jobID string, req WorkerCompleteRequest) (*GenerationJob, []GenerationOutput, error) {
	if len(req.Body) == 0 {
		return nil, nil, errInvalidArgument
	}
	now := s.now().UTC()
	var updated *GenerationJob
	var outputs []GenerationOutput
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := lockJob(tx, jobID)
		if err != nil {
			return err
		}
		if job.CancelRequestedAt != nil && job.Status != StatusSucceeded {
			if err := s.markCanceled(tx, job, now); err != nil {
				return err
			}
			updated = job
			return nil
		}
		if err := tx.Where("job_id = ?", job.ID).Order("created_at ASC").Find(&outputs).Error; err != nil {
			return err
		}
		if job.Status == StatusSucceeded {
			updated = job
			return nil
		}
		if job.Status != StatusRunning || job.Attempt != req.Attempt {
			return errInvalidTransition
		}
		if err := ValidateTransition(job.Status, StatusSucceeded, false); err != nil {
			return errInvalidTransition
		}
		filename := req.Filename
		if filename == "" {
			filename = "generated.glb"
		}
		stored, err := s.assets.Put(ctx, job.UserID, job.ID, asset.KindModel, filename, req.MIMEType, bytes.NewReader(req.Body), int64(len(req.Body)))
		if err != nil {
			return err
		}
		output := GenerationOutput{
			ID:         uuid.NewString(),
			JobID:      job.ID,
			AssetID:    &stored.ID,
			OutputType: "MODEL",
			Format:     "glb",
			ObjectKey:  &stored.ObjectKey,
			MIMEType:   stored.MIMEType,
			SizeBytes:  stored.SizeBytes,
			SHA256:     stored.SHA256,
			Metadata:   json.RawMessage(`{"provider":"mock"}`),
			CreatedAt:  now,
		}
		if err := tx.Create(&output).Error; err != nil {
			return err
		}
		providerJobID := strings.TrimSpace(req.ProviderJobID)
		job.Status = StatusSucceeded
		job.Stage = StageCompleted
		job.Progress = 100
		job.FinishedAt = &now
		job.UpdatedAt = now
		job.ErrorCode = nil
		job.ErrorMessage = nil
		job.Version++
		if providerJobID != "" {
			job.ProviderJobID = &providerJobID
		}
		if err := tx.Select("Status", "Stage", "Progress", "FinishedAt", "UpdatedAt", "ErrorCode", "ErrorMessage", "ProviderJobID", "Version").Updates(job).Error; err != nil {
			return err
		}
		updated = job
		outputs = []GenerationOutput{output}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return updated, outputs, nil
}

func (s *Service) Cancel(ctx context.Context, userID, jobID string) (*GenerationJob, []GenerationOutput, error) {
	now := s.now().UTC()
	var updated *GenerationJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := lockJob(tx, jobID)
		if err != nil {
			return err
		}
		if job.UserID != userID {
			return errJobNotFound
		}
		if job.Status == StatusCanceled {
			updated = job
			return nil
		}
		if job.Status == StatusSucceeded || job.Status == StatusFailed {
			return errInvalidTransition
		}
		if job.Status == StatusQueued {
			if err := s.markCanceled(tx, job, now); err != nil {
				return err
			}
			updated = job
			return nil
		}
		if err := tx.Model(job).Updates(map[string]any{
			"cancel_requested_at": now,
			"updated_at":          now,
			"version":             job.Version + 1,
		}).Error; err != nil {
			return err
		}
		job.CancelRequestedAt = &now
		job.UpdatedAt = now
		job.Version++
		updated = job
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	outputs, err := s.listOutputs(ctx, updated.ID)
	if err != nil {
		return nil, nil, err
	}
	return updated, outputs, nil
}

func (s *Service) Retry(ctx context.Context, userID, jobID, requestID string) (*GenerationJob, error) {
	now := s.now().UTC()
	var retried *GenerationJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := lockJob(tx, jobID)
		if err != nil {
			return err
		}
		if job.UserID != userID {
			return errJobNotFound
		}
		if job.Status != StatusFailed {
			return errInvalidTransition
		}
		if job.Attempt >= job.MaxAttempts {
			return errInvalidTransition
		}
		var active int64
		if err := tx.Model(&GenerationJob{}).Where("user_id = ? AND status IN ?", userID, []Status{StatusQueued, StatusRunning}).Count(&active).Error; err != nil {
			return err
		}
		if active >= maxActiveJobsPerUser {
			return errTooManyJobs
		}
		nextAttempt := job.Attempt + 1
		if err := ValidateTransition(job.Status, StatusQueued, true); err != nil {
			return errInvalidTransition
		}
		job.Status = StatusQueued
		job.Stage = StageQueued
		job.Progress = 0
		job.Attempt = nextAttempt
		job.ErrorCode = nil
		job.ErrorMessage = nil
		job.StartedAt = nil
		job.FinishedAt = nil
		job.UpdatedAt = now
		job.Version++
		if err := tx.Select("Status", "Stage", "Progress", "Attempt", "ErrorCode", "ErrorMessage", "StartedAt", "FinishedAt", "UpdatedAt", "Version").Updates(job).Error; err != nil {
			return err
		}
		message := StreamMessage{
			SchemaVersion: MessageVersion,
			EventType:     JobCreatedEvent,
			JobID:         job.ID,
			UserID:        job.UserID,
			Attempt:       nextAttempt,
			RequestID:     requestID,
			CreatedAt:     now.Format(time.RFC3339),
		}
		payloadBytes, err := json.Marshal(message.Fields())
		if err != nil {
			return err
		}
		outbox := GenerationOutbox{
			ID: uuid.NewString(), AggregateID: job.ID, EventType: JobCreatedEvent,
			Payload: payloadBytes, Status: OutboxPending, AvailableAt: now, CreatedAt: now,
		}
		if err := tx.Create(&outbox).Error; err != nil {
			return err
		}
		retried = job
		return nil
	})
	return retried, err
}

func (s *Service) FailTimedOut(ctx context.Context) (int, error) {
	now := s.now().UTC()
	cutoff := now.Add(-s.timeout)
	var jobs []GenerationJob
	if err := s.db.WithContext(ctx).Where(
		"(status = ? AND created_at <= ?) OR (status = ? AND started_at IS NOT NULL AND started_at <= ?)",
		StatusQueued, cutoff, StatusRunning, cutoff,
	).Find(&jobs).Error; err != nil {
		return 0, err
	}
	count := 0
	for _, job := range jobs {
		code := "GENERATION_TIMEOUT"
		message := "生成任务超时"
		if _, err := s.Fail(ctx, job.ID, WorkerFailRequest{Attempt: job.Attempt, ErrorCode: code, ErrorMessage: message}); err == nil {
			count++
		}
	}
	return count, nil
}

func (s *Service) StartTimeoutWatcher() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			_, _ = s.FailTimedOut(context.Background())
		}
	}()
}

func (s *Service) OpenOutput(ctx context.Context, userID, jobID, assetID string) (*GenerationJob, *asset.Asset, error) {
	job, outputs, err := s.Get(ctx, userID, jobID)
	if err != nil {
		return nil, nil, err
	}
	for _, output := range outputs {
		if output.AssetID != nil && *output.AssetID == assetID {
			stored, err := s.assets.Get(ctx, assetID)
			if err != nil {
				return nil, nil, err
			}
			return job, stored, nil
		}
	}
	return nil, nil, errJobNotFound
}

func (s *Service) loadJob(ctx context.Context, jobID string) (*GenerationJob, error) {
	var job GenerationJob
	if err := s.db.WithContext(ctx).First(&job, "id = ?", jobID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func (s *Service) listOutputs(ctx context.Context, jobID string) ([]GenerationOutput, error) {
	var outputs []GenerationOutput
	if err := s.db.WithContext(ctx).Where("job_id = ?", jobID).Order("created_at ASC").Find(&outputs).Error; err != nil {
		return nil, err
	}
	return outputs, nil
}

func lockJob(tx *gorm.DB, jobID string) (*GenerationJob, error) {
	var job GenerationJob
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "id = ?", jobID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func (s *Service) markCanceled(tx *gorm.DB, job *GenerationJob, now time.Time) error {
	if job.Status == StatusCanceled {
		return nil
	}
	if err := ValidateTransition(job.Status, StatusCanceled, false); err != nil {
		return errInvalidTransition
	}
	if err := tx.Model(job).Updates(map[string]any{
		"status":        StatusCanceled,
		"stage":         job.Stage,
		"finished_at":   now,
		"updated_at":    now,
		"error_code":    "GENERATION_CANCELED",
		"error_message": "生成任务已取消",
		"version":       job.Version + 1,
	}).Error; err != nil {
		return err
	}
	code := "GENERATION_CANCELED"
	message := "生成任务已取消"
	job.Status = StatusCanceled
	job.ErrorCode = &code
	job.ErrorMessage = &message
	job.FinishedAt = &now
	job.UpdatedAt = now
	job.Version++
	return nil
}
