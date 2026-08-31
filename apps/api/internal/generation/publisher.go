package generation

import "context"

type Publisher interface {
	Publish(ctx context.Context, message StreamMessage) error
}

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, StreamMessage) error { return nil }
