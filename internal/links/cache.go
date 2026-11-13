package links

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache is a place that can forget. Anything in it can be read again from the database.
type Cache interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
}

// Source is where links really live: the database.
type Source interface {
	Create(ctx context.Context, url string) (string, error)
	Lookup(ctx context.Context, code string) (url string, ok bool, err error)
}

// Cached is cache-aside: look in the cache first; on a miss, read the database and save the answer.
type Cached struct {
	db    Source
	cache Cache
	ttl   time.Duration

	// When the cache fails, skip it for a while instead of waiting on it for every request.
	skipUntil atomic.Int64 // unix nanoseconds
	hits      atomic.Int64
	misses    atomic.Int64
}

const skipFor = 2 * time.Second

// NewCached puts cache in front of db. Saved answers expire after ttl.
func NewCached(db Source, cache Cache, ttl time.Duration) *Cached {
	return &Cached{db: db, cache: cache, ttl: ttl}
}

// Create saves a link. The cache is not touched: a new code is not in it yet.
func (c *Cached) Create(ctx context.Context, url string) (string, error) {
	return c.db.Create(ctx, url)
}

// Lookup returns the long URL for code. A cache that fails is not an error: the database answers.
func (c *Cached) Lookup(ctx context.Context, code string) (string, bool, error) {
	usable := time.Now().UnixNano() >= c.skipUntil.Load()
	if usable {
		url, ok, err := c.cache.Get(ctx, "link:"+code)
		switch {
		case err != nil:
			c.skipUntil.Store(time.Now().Add(skipFor).UnixNano())
			usable = false
		case ok:
			c.hits.Add(1)
			return url, true, nil
		}
	}
	c.misses.Add(1)
	url, ok, err := c.db.Lookup(ctx, code)
	if err != nil || !ok {
		return url, ok, err
	}
	if usable {
		_ = c.cache.Set(ctx, "link:"+code, url, c.ttl) // best effort
	}
	return url, true, nil
}

// Stats returns how many lookups the cache answered, and how many it did not.
func (c *Cached) Stats() (hits, misses int64) { return c.hits.Load(), c.misses.Load() }

// RedisCache is a Cache in a Redis that holds nothing but copies. It is separate from the state
// store on purpose: it is set to forget (maxmemory, LRU), and losing it loses nothing.
type RedisCache struct{ rdb *redis.Client }

// NewRedisCache wraps a client. Give the client short timeouts: a cache that is slow is worse than no cache.
func NewRedisCache(rdb *redis.Client) *RedisCache { return &RedisCache{rdb: rdb} }

func (r *RedisCache) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := r.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (r *RedisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.rdb.Set(ctx, key, value, ttl).Err()
}
