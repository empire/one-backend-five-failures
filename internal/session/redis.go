package session

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const ttl = 24 * time.Hour

// Redis keeps sessions in a store that every app instance shares.
type Redis struct {
	rdb *redis.Client
}

// NewRedis returns a store backed by the shared Redis.
func NewRedis(rdb *redis.Client) *Redis {
	return &Redis{rdb: rdb}
}

func (s *Redis) Create(ctx context.Context, user string) (string, error) {
	id := newID()
	return id, s.rdb.Set(ctx, "sess:"+id, user, ttl).Err()
}

func (s *Redis) User(ctx context.Context, id string) (string, bool, error) {
	user, err := s.rdb.Get(ctx, "sess:"+id).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	return user, err == nil, err
}
