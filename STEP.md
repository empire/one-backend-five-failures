# Step 2: a cache for opens

**The failure.** Almost every request is an open, and the same popular links are read again and again, yet every open reads the database.

**The part.** Cache-aside: the app asks the cache first; on a miss it reads the database and saves the answer for 5 minutes. The cache is its own Redis, set to forget (`maxmemory 64mb`, least recently used first), and it holds only copies. If the cache fails, the app skips it for 2 seconds and the database answers.

**Where**
- `internal/links/cache.go`: `Cached.Lookup` (cache-aside) and `RedisCache`
- `cmd/app/main.go`: `cached()` chooses the cache with short timeouts
- `compose.yaml`: the `cache` service, separate from `state` (which holds sessions that exist nowhere else)

**Run it**

```sh
make up          # CACHE=off gives the step-1 behaviour: every open reads the database
make seed
make load        # the ramp again: the database stays low where step 0 broke
make demo-cache  # kills the cache under steady load; prints database lookups for every 100 opens before and after
```

`results/step-2-cache/cache-kill/run-1/demo.json` holds `lookups_per_100_opens_before_kill`, `lookups_per_100_opens_after_kill` and `hit_rate_pct_before_kill`.
With a hit rate `h`, losing the cache multiplies the database's reads by `1 / (1 - h)`.
