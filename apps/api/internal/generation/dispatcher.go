package generation

import (
	"context"
	"encoding/json"
	"time"
)

const (
	outboxBatchSize     = 20
	outboxMaxAttempts   = 20
	dispatcherInterval  = time.Second
	outboxBackoffCap    = 30 * time.Second
)

func (s *Service) StartDispatcher() {
	if s.rdb != nil {
		s.publisher = NewRedisPublisher(s.rdb)
	}
	go s.dispatchLoop()
}

func (s *Service) dispatchLoop() {
	ticker := time.NewTicker(dispatcherInterval)
	defer ticker.Stop()
	for range ticker.C {
		_ = s.DispatchOnce(context.Background())
	}
}

func (s *Service) DispatchOnce(ctx context.Context) error {
	now := s.now().UTC()
	var items []GenerationOutbox
	if err := s.db.WithContext(ctx).
		Where("status = ? AND available_at <= ?", OutboxPending, now).
		Order("available_at ASC").
		Limit(outboxBatchSize).
		Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if err := s.publishOutbox(ctx, item, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) publishOutbox(ctx context.Context, item GenerationOutbox, now time.Time) error {
	var message StreamMessage
	if err := json.Unmarshal(item.Payload, &message); err != nil {
		return s.markOutboxFailure(ctx, item, now, err)
	}
	if err := s.publisher.Publish(ctx, message); err != nil {
		return s.markOutboxFailure(ctx, item, now, err)
	}
	publishedAt := now
	return s.db.WithContext(ctx).Model(&GenerationOutbox{}).Where("id = ?", item.ID).Updates(map[string]any{
		"status":       OutboxPublished,
		"published_at": publishedAt,
		"last_error":   nil,
	}).Error
}

func (s *Service) markOutboxFailure(ctx context.Context, item GenerationOutbox, now time.Time, publishErr error) error {
	attempts := item.Attempts + 1
	message := publishErr.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	updates := map[string]any{
		"attempts":     attempts,
		"last_error":   message,
		"available_at": now.Add(outboxBackoff(attempts)),
	}
	if attempts >= outboxMaxAttempts {
		updates["status"] = OutboxFailed
	}
	return s.db.WithContext(ctx).Model(&GenerationOutbox{}).Where("id = ?", item.ID).Updates(updates).Error
}

func outboxBackoff(attempts int) time.Duration {
	delay := time.Second
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= outboxBackoffCap {
			return outboxBackoffCap
		}
	}
	return delay
}
