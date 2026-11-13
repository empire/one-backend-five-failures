# Step 3: a rate limiter on creates

**The failure.** One client that creates links in a loop is not stopped. Each create costs a safety scan and a database row, and the abuser gets all of it.

**The part.** A token bucket for each client. A bucket holds `BUCKET_SIZE` (10) tokens and gets `REFILL_PER_S` (2) new tokens each second; every create takes one. A quiet client can send a burst; when the bucket is empty the service answers `429 Too Many Requests` with a `Retry-After` header. The buckets live in the shared state store (one Lua script, so two apps cannot both take the last token), and both apps see the same count. If the limiter itself fails, the request goes through.

**Where**
- `internal/limit/bucket.go`: the bucket and its Lua script
- `internal/server/server.go`: `tooMany`, called by `create` after the session is checked
- `cmd/app/main.go`: `limiter()` (`LIMITER=off` turns it off)

**Run it**

```sh
make up LIMITER=off && make seed
make demo-abuser NAME=abuser-off   # 50 normal clients (1 create a second each) and one abuser (20 creates a second)
make up
make demo-abuser NAME=abuser-on
make burst                         # a quiet client sends 10 creates at once, then 2 more
```

`results/step-3-ratelimit/<name>/run-1/demo.json` holds `abuser_accepted_pct`, `abuser_429_pct` and `normal_429_pct`.
The ramp (`make load`) logs in 50 clients; at its higher levels each one creates faster than the bucket refills, so use `make load USERS=1000` there.
