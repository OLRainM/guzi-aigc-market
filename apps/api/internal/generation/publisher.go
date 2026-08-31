package generation

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type Publisher interface {
	Publish(ctx context.Context, message StreamMessage) error
}

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, StreamMessage) error { return nil }

type RedisPublisher struct {
	rdb *redis.Client
}

func NewRedisPublisher(rdb *redis.Client) Publisher {
	if rdb == nil {
		return noopPublisher{}
	}
	return RedisPublisher{rdb: rdb}
}

func (p RedisPublisher) Publish(ctx context.Context, message StreamMessage) error {
	id, err := p.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamName,
		Values: stringifyFields(message.Fields()),
	}).Result()
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("empty redis stream id")
	}
	return nil
}

func stringifyFields(fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		out[key] = fmt.Sprint(value)
	}
	return out
}
