package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const promptPreviewTTL = 15 * time.Minute

type PromptOptimizer interface {
	Optimize(context.Context, PromptPreviewRequest) (*PromptPreviewResponse, error)
}

type HTTPPromptOptimizer struct {
	client *http.Client
	url    string
	token  string
}

func NewHTTPPromptOptimizer(baseURL, token string) *HTTPPromptOptimizer {
	return &HTTPPromptOptimizer{
		client: &http.Client{Timeout: 35 * time.Second},
		url:    strings.TrimRight(baseURL, "/") + "/internal/prompt-optimize",
		token:  token,
	}
}

func (o *HTTPPromptOptimizer) Optimize(ctx context.Context, input PromptPreviewRequest) (*PromptPreviewResponse, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Worker-Token", o.token)
	res, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prompt optimizer returned %s", res.Status)
	}
	var result PromptPreviewResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) PreviewPrompt(ctx context.Context, userID string, req PromptPreviewRequest) (*PromptPreview, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.ProductType = strings.TrimSpace(req.ProductType)
	if req.Prompt == "" || utf8.RuneCountInString(req.Prompt) > 2000 || req.ProductType == "" || utf8.RuneCountInString(req.ProductType) > 64 {
		return nil, errInvalidArgument
	}
	if s.optimizer == nil {
		return nil, errPromptOptimizerUnavailable
	}
	optimized, err := s.optimizer.Optimize(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errPromptOptimizerUnavailable, err)
	}
	optimized.OptimizedPrompt = strings.TrimSpace(optimized.OptimizedPrompt)
	if optimized.OptimizedPrompt == "" || utf8.RuneCountInString(optimized.OptimizedPrompt) > 1024 {
		return nil, errPromptOptimizerUnavailable
	}
	structured, err := json.Marshal(optimized.StructuredPrompt)
	if err != nil {
		return nil, err
	}
	ragContext, err := json.Marshal(optimized.RAGContext)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	preview := PromptPreview{
		ID: uuid.NewString(), UserID: userID, RawPrompt: req.Prompt, ProductType: req.ProductType,
		OptimizedPrompt: optimized.OptimizedPrompt, StructuredPrompt: structured, RAGContext: ragContext,
		RAGVersion: strings.TrimSpace(optimized.RAGVersion), PromptTemplateVersion: strings.TrimSpace(optimized.PromptTemplateVersion),
		Source: strings.TrimSpace(optimized.Source), ExpiresAt: now.Add(promptPreviewTTL), CreatedAt: now,
	}
	if preview.Source == "" {
		preview.Source = "unknown"
	}
	if err := s.db.WithContext(ctx).Create(&preview).Error; err != nil {
		return nil, err
	}
	return &preview, nil
}

func lockPromptPreview(tx *gorm.DB, userID, previewID string, now time.Time) (*PromptPreview, error) {
	var preview PromptPreview
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", previewID, userID).First(&preview).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errPromptPreviewInvalid
	}
	if err != nil {
		return nil, err
	}
	if preview.ConsumedAt != nil || !preview.ExpiresAt.After(now) {
		return nil, errPromptPreviewInvalid
	}
	return &preview, nil
}
