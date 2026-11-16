# Step 5: the service tells you how it is doing

**The need.** Until now everything was watched from outside, with a load test. A running service has to say how it is doing itself.

**The parts**
- `GET /healthz` answers `ok` while the app answers. The load balancer calls it every second (HAProxy `option httpchk`) and takes an app out after two failed checks. It is shallow on purpose: it does not call the database.
- `GET /metrics` prints the four golden signals of the Google SRE book, one metric each (Prometheus text format):
  - latency: `http_request_duration_seconds`
  - traffic: `http_requests_total`
  - errors: `http_errors_total`
  - saturation: `http_requests_in_flight`

  The label is the route that matched (`GET /{code}`), never the URL, so there is one series for each route and not one for each short link.

For a small site on a few servers this is good practice, not a necessity.

**Where**
- `internal/metrics/metrics.go`: the four metrics and the middleware
- `internal/server/server.go`: the `/healthz` and `/metrics` routes
- `lb/haproxy.cfg`: the health check

**Run it**

```sh
make up && make seed
make demo-metrics     # a short load, then GET /metrics and GET /healthz as printed
```

---

## A second database that keeps a copy (from step 2)

This is an option on top of the service above, off by default. It was built on the step 2 branch and rebased onto the current code.

**The need.** The database is one server. When it stops (an upgrade, a restart, a security update), the app cannot create links, and it can open only the links the cache already holds.

**The parts**
- The **primary** (`db`) is the only database that accepts writes. It sends every change to the replica.
- The **replica** (`replica`) starts from a copy of the primary (`pg_basebackup`), then follows its changes. It is read-only. Copying is asynchronous: a change made just before the primary stops may not have reached the replica.
- The apps send creates to the primary, and opens that miss the cache to the replica (`REPLICA_URL`). With `REPLICA_URL` empty, every read goes to the primary, as before.

Promoting the replica to a new primary is not built here.

**Where**
- `internal/links/store.go`: two pools; `Lookup` reads from the replica, `Create` and `Delete` write to the primary
- `compose.yaml`, `scripts/primary-init.sh`, `scripts/replica-start.sh`, `scripts/wait-replica.sh`: the primary's replication role, the replica's start, and the wait before a load
- `tools/bench`: opens and creates counted apart, by outcome (a 429 from the limiter is in neither)

**Run it**

```sh
make up REPLICA=off && make seed
make demo-db-down            # the database is stopped under steady load: creates fail, opens succeed only from the cache
make down && make up REPLICA=on && make seed REPLICA=on
make demo-replica            # the same load with the replica: nothing fails
make demo-primary-kill       # the primary is killed: opens keep working, creates fail
```

Results go to `results/step-5-metrics/<demo>/run-1/` (the default `STEP`).
