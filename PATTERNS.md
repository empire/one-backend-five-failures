# Where each pattern is

Line numbers are for `main`. Each row says which branch added the part.

| Pattern | Where | Added in |
|---|---|---|
| Stateless web tier: sessions in a shared store, not in the app's memory | `internal/session/store.go:14` (the interface), `internal/session/redis.go`, `cmd/app/main.go:54` (`sessions`) | `step-1-stateless` |
| Load balancer with a health check | `lb/haproxy.cfg` (`backend apps`) | `step-1-stateless` (the `/healthz` check: `step-5-metrics`) |
| Cache-aside, with the cache allowed to fail | `internal/links/cache.go:58` (`Cached.Lookup`), `cmd/app/main.go:76` (`cached`) | `step-2-cache` |
| A cache that holds only copies, set to forget | `compose.yaml` (service `cache`: `maxmemory`, `allkeys-lru`) | `step-2-cache` |
| Token-bucket rate limiter, buckets in the shared store | `internal/limit/bucket.go:48` (`Bucket.Allow`, one Lua script), `internal/server/server.go:133` (`tooMany`, the `429` and `Retry-After`) | `step-3-ratelimit` |
| A queue for work that can wait, one lane for each type of job | `internal/queue/queue.go:44` (`Producer.Publish`), `cmd/worker/main.go` (the `click` and `scan` lanes) | `step-4-queue` |
| Retries with exponential backoff and jitter | `internal/queue/queue.go:199` (`Backoff`), used in `Consumer.process` (`:151`) | `step-4-queue` |
| Dedupe on event ID | `internal/queue/queue.go:180` (`Consumer.seenBefore`) | `step-4-queue` |
| An event is never lost: it leaves the stream only after the call worked; a dead worker's events are taken over | `internal/queue/queue.go` (`Consumer.reclaim`, `Consumer.finish`) | `step-4-queue` |
| The four golden signals | `internal/metrics/metrics.go` (latency, traffic, errors, saturation) | `step-5-metrics` |
| Health check | `internal/server/server.go:60` (`GET /healthz`) | `step-5-metrics` |
