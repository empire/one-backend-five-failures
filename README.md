# One Backend, Five Failures

A small URL shortener, written naively on purpose. A load test breaks it. After each failure one part is added, and the same test runs again. The code is the companion to the video "One Backend, Five Failures".

The rule: **add a part only when a failure you can see asks for it.**

This repository is about *what fails* and *which part answers it*, not about speed. Each container is limited to one CPU so that a limit shows up with a small test; the numbers a demo prints are counts (requests answered 401, events lost), not timings to compare with your own system.

## The steps

Each step is a branch. `main` is the last step plus this README and [PATTERNS.md](PATTERNS.md).

| Branch | The failure | The part | Demo |
|---|---|---|---|
| `step-0-naive` | The service breaks when the load rises: the app runs out first | (the baseline) | `make load` |
| `step-1-stateless` | A second server logs users out; one dead server is a dead service | Sessions in a shared Redis, two apps behind a load balancer (HAProxy) | `make up SESSIONS=memory && make demo-sessions NAME=sessions-memory`, then `make up && make demo-sessions` and `make demo-kill` |
| `step-2-cache` | The database reads the same popular links again and again, and the cache you add becomes a part you cannot lose | Cache-aside in a separate Redis that holds only copies | `make demo-cache` |
| `step-3-ratelimit` | One client sends far more than the others | Token bucket in the shared Redis (Lua), `429` with `Retry-After` | `make demo-abuser` (with and without `LIMITER=off`), `make burst` |
| `step-4-queue` | A slow or dead third-party API slows or breaks your users' requests | Redis Streams, one lane for each type of job, a worker, retries with backoff and jitter, dedupe on event ID | `make slow-open`, `make demo-outage`, `make demo-dupes` |
| `step-5-metrics` | You watch it only from outside | `/healthz` for the load balancer, `/metrics` with the four golden signals | `make demo-metrics` |

Every step has a short `STEP.md`: the idea, what changed, where, how to run it.

## Run it

You need Docker (with Compose). `make test` also needs Go.

```sh
git checkout step-0-naive      # or any step
make up                        # the app(s), Postgres, Redis, and a mock of the two third-party APIs
make seed                      # 100,000 links for the load test to open
make load                      # the load rises one level at a time until the service fails
make down
```

The service is on `localhost:8088` (port 8080 is left free). The mock third-party APIs are on `localhost:9000`; each has a switch for slow and failing answers (`POST /admin/slow?api=analytics&ms=2000`, `POST /admin/fail?api=analytics&pct=100`, `GET /admin/stats`).

The load test uses [k6](https://k6.io) from its Docker image, so nothing else is installed. `make load` and the demos write what they measured to `results/` (not committed).

## What is not here

- Deployment. The Docker Compose file is as far as this goes.
- Backups.
- A read replica. It would be the next step for reads; with asynchronous copying, the last writes can be lost when the leader fails.
- A dead-letter lane for events that never succeed, and a limit on the size of a lane.
- A password. `POST /login` starts a session for any name; it is a demo.
