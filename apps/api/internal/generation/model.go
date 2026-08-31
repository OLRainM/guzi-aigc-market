package generation

import (
	"encoding/json"
	"time"
)

type GenerationJob struct {
	ID                    string          `gorm:"type:char(36);primaryKey" json:"id"`
	UserID                string          `gorm:"type:char(36);not null;index:idx_generation_jobs_user_created,priority:1" json:"user_id"`
	SourceJobID           *string         `gorm:"type:char(36);index" json:"source_job_id,omitempty"`
	IdempotencyKey        string          `gorm:"type:char(36);not null;uniqueIndex:uq_generation_jobs_user_idempotency,priority:2" json:"-"`
	RequestHash           string          `gorm:"type:char(64);not null" json:"-"`
	Status                Status          `gorm:"type:varchar(16);not null;index" json:"status"`
	Stage                 Stage           `gorm:"type:varchar(32);not null" json:"stage"`
	Progress              int             `gorm:"type:tinyint unsigned;not null;default:0" json:"progress"`
	RawPrompt             string          `gorm:"type:text;not null" json:"raw_prompt"`
	OptimizedPrompt       *string         `gorm:"type:text" json:"optimized_prompt,omitempty"`
	StructuredPrompt      json.RawMessage `gorm:"type:json" json:"structured_prompt,omitempty"`
	RequestPayload        json.RawMessage `gorm:"type:json;not null" json:"-"`
	RAGContext            json.RawMessage `gorm:"column:rag_context;type:json" json:"rag_context,omitempty"`
	RAGVersion            *string         `gorm:"column:rag_version;size:64" json:"rag_version,omitempty"`
	PromptTemplateVersion *string         `gorm:"size:64" json:"prompt_template_version,omitempty"`
	Provider              string          `gorm:"size:32;not null;default:mock" json:"provider"`
	ProviderModel         *string         `gorm:"size:128" json:"provider_model,omitempty"`
	ProviderJobID         *string         `gorm:"size:191;uniqueIndex" json:"-"`
	ProviderPayload       json.RawMessage `gorm:"type:json" json:"-"`
	Attempt               int             `gorm:"not null;default:0" json:"attempt"`
	MaxAttempts           int             `gorm:"not null;default:3" json:"max_attempts"`
	ErrorCode             *string         `gorm:"size:64" json:"-"`
	ErrorMessage          *string         `gorm:"size:500" json:"-"`
	CancelRequestedAt     *time.Time      `json:"cancel_requested_at,omitempty"`
	StartedAt             *time.Time      `json:"started_at,omitempty"`
	FinishedAt            *time.Time      `json:"finished_at,omitempty"`
	Version               uint64          `gorm:"not null;default:1" json:"version"`
	CreatedAt             time.Time       `gorm:"index:idx_generation_jobs_user_created,priority:2" json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

func (GenerationJob) TableName() string { return "generation_jobs" }

type GenerationOutput struct {
	ID         string          `gorm:"type:char(36);primaryKey" json:"id"`
	JobID      string          `gorm:"type:char(36);not null;index" json:"job_id"`
	AssetID    *string         `gorm:"type:char(36);index" json:"asset_id,omitempty"`
	OutputType string          `gorm:"size:32;not null;uniqueIndex:uq_generation_outputs_identity,priority:1" json:"output_type"`
	Format     string          `gorm:"size:16;not null" json:"format"`
	ObjectKey  *string         `gorm:"size:512;uniqueIndex:uq_generation_outputs_identity,priority:2" json:"-"`
	MIMEType   string          `gorm:"size:128;not null" json:"mime_type"`
	SizeBytes  int64           `gorm:"not null;default:0" json:"size_bytes"`
	SHA256     string          `gorm:"type:char(64);not null" json:"sha256"`
	Metadata   json.RawMessage `gorm:"type:json" json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

func (GenerationOutput) TableName() string { return "generation_outputs" }

type OutboxStatus string

const (
	OutboxPending   OutboxStatus = "PENDING"
	OutboxPublished OutboxStatus = "PUBLISHED"
	OutboxFailed    OutboxStatus = "FAILED"
)

type GenerationOutbox struct {
	ID          string          `gorm:"type:char(36);primaryKey" json:"id"`
	AggregateID string          `gorm:"type:char(36);not null;index" json:"aggregate_id"`
	EventType   string          `gorm:"size:64;not null" json:"event_type"`
	Payload     json.RawMessage `gorm:"type:json;not null" json:"payload"`
	Status      OutboxStatus    `gorm:"type:varchar(16);not null;index:idx_generation_outbox_dispatch,priority:1" json:"status"`
	Attempts    int             `gorm:"not null;default:0" json:"attempts"`
	AvailableAt time.Time       `gorm:"not null;index:idx_generation_outbox_dispatch,priority:2" json:"available_at"`
	PublishedAt *time.Time      `json:"published_at,omitempty"`
	LastError   *string         `gorm:"size:500" json:"last_error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

func (GenerationOutbox) TableName() string { return "generation_outbox" }
