// Package limit is a token-bucket rate limiter whose buckets live in the shared state store,
// so every app instance sees the same count for a client.
package limit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// The whole decision runs inside Redis as one script, so two apps cannot both take the last token.
// Time comes from Redis too, so the apps' clocks do not matter.
var script = redis.NewScript(`
local capacity = tonumber(ARGV[1])
local refill = tonumber(ARGV[2])            -- tokens added per second
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local d = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens, ts = tonumber(d[1]), tonumber(d[2])
if tokens == nil then tokens, ts = capacity, now end
tokens = math.min(capacity, tokens + (now - ts) / 1000 * refill)
local allowed, wait = 0, 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  wait = math.ceil((1 - tokens) / refill * 1000)   -- milliseconds until one token is back
end
redis.call('HSET', KEYS[1], 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', KEYS[1], math.ceil(capacity / refill * 1000) + 1000)   -- a full bucket is the same as no bucket
return {allowed, wait}
`)

// Bucket gives every client Capacity tokens, adds RefillPerSecond tokens each second, and takes one per request.
type Bucket struct {
	rdb             *redis.Client
	Capacity        int
	RefillPerSecond float64
}

// New returns a limiter that keeps its buckets in rdb.
func New(rdb *redis.Client, capacity int, refillPerSecond float64) *Bucket {
	return &Bucket{rdb: rdb, Capacity: capacity, RefillPerSecond: refillPerSecond}
}

// Allow takes one token from the client's bucket. When the bucket is empty it returns false and how long to wait.
func (b *Bucket) Allow(ctx context.Context, client string) (bool, time.Duration, error) {
	res, err := script.Run(ctx, b.rdb, []string{"limit:" + client}, b.Capacity, b.RefillPerSecond).Int64Slice()
	if err != nil {
		return false, 0, err
	}
	return res[0] == 1, time.Duration(res[1]) * time.Millisecond, nil
}
