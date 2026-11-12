# Step 0: the naive service

**The idea.** A URL shortener written the obvious way, so a load test can show where it breaks.

**What it has**

- `POST /login` starts a session (demo only: no password). Sessions live in the app's memory.
- `POST /links` creates a short link. It calls a safety-scan API on the spot and waits.
- `GET /{code}` looks the link up in Postgres, calls an analytics API on the spot and waits, then answers `302`.
- Nothing else: no cache, no limits, one app, one database.

**Run it**

```sh
make up      # app, Postgres, and a mock of the two third-party APIs; each container is capped at 1 CPU
make seed    # 100,000 links for the test to open
make ramp    # one ramp run: the load rises one level at a time, until the service breaks
```

The service is on `localhost:8088` (port 8080 is left free).
