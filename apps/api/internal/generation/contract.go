package generation

import "time"

const (
	StreamName        = "generation_jobs"
	ConsumerGroupName = "ai-workers"
	MessageVersion    = "1"
	JobCreatedEvent   = "generation.job.created"
)

type PromptPreviewRequest struct {
	Prompt      string `json:"prompt"`
	ProductType string `json:"product_type"`
}

type PromptPreviewResponse struct {
	ID                    string         `json:"id"`
	RawPrompt             string         `json:"raw_prompt"`
	ProductType           string         `json:"product_type"`
	OptimizedPrompt       string         `json:"optimized_prompt"`
	StructuredPrompt      map[string]any `json:"structured_prompt,omitempty"`
	RAGContext            map[string]any `json:"rag_context,omitempty"`
	RAGVersion            string         `json:"rag_version,omitempty"`
	PromptTemplateVersion string         `json:"template_version,omitempty"`
	Source                string         `json:"source"`
	ExpiresAt             time.Time      `json:"expires_at"`
}

type CreateJobRequest struct {
	PromptPreviewID   string         `json:"prompt_preview_id"`
	FinalPrompt       string         `json:"final_prompt"`
	Provider          string         `json:"provider"`
	Parameters        map[string]any `json:"parameters,omitempty"`
	CopyrightConfirmed bool          `json:"copyright_confirmed"`
}

type JobError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type JobResponse struct {
	ID              string             `json:"id"`
	SourceJobID     *string            `json:"source_job_id,omitempty"`
	Status          Status             `json:"status"`
	Stage           Stage              `json:"stage"`
	Progress        int                `json:"progress"`
	RawPrompt       string             `json:"raw_prompt"`
	OptimizedPrompt *string            `json:"optimized_prompt,omitempty"`
	ProductType     string             `json:"product_type,omitempty"`
	Provider        string             `json:"provider"`
	Attempt         int                `json:"attempt"`
	MaxAttempts     int                `json:"max_attempts"`
	Outputs         []GenerationOutput `json:"outputs"`
	Error           *JobError          `json:"error"`
	CreatedAt       time.Time          `json:"created_at"`
	StartedAt       *time.Time         `json:"started_at,omitempty"`
	FinishedAt      *time.Time         `json:"finished_at,omitempty"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type StreamMessage struct {
	SchemaVersion string `json:"schema_version"`
	EventType     string `json:"event_type"`
	JobID         string `json:"job_id"`
	UserID        string `json:"user_id"`
	Attempt       int    `json:"attempt"`
	RequestID     string `json:"request_id"`
	CreatedAt     string `json:"created_at"`
}

func (m StreamMessage) Fields() map[string]any {
	return map[string]any{
		"schema_version": m.SchemaVersion,
		"event_type":     m.EventType,
		"job_id":         m.JobID,
		"user_id":        m.UserID,
		"attempt":        m.Attempt,
		"request_id":     m.RequestID,
		"created_at":     m.CreatedAt,
	}
}
