STEP ?= step-5-metrics
NAME ?= sessions
RATE ?= 1000
RUN  ?= 1
OUT  := results/$(STEP)/run-$(RUN)

.PHONY: up down seed load test demo-sessions demo-kill demo-cache demo-abuser burst demo-outage demo-dupes slow-open demo-metrics

# The whole service. (`LIMITER=off`, `CACHE=off` and `SESSIONS=memory` bring back the failures of steps 3, 2 and 1.)
up:
	docker compose up -d --build --wait

down:
	docker compose down -v

# 100,000 links for the load test to open.
seed:
	docker compose exec -T db psql -q -U shortener -d shortener < scripts/seed.sql

# Watch it fail: the load rises one level at a time (k6 runs from its Docker image) while container CPU is recorded.
# Writes results/<step>/run-1/series.json. Tune with: make load STEP_RPS=250 STEP_S=8 LEVELS=16
load:
	STEP=$(STEP) RUN=$(RUN) scripts/ramp.sh

test:
	go vet ./... && go test ./...

# Steady load through the balancer; prints the share of logged-in creates answered 401.
# Run it twice: `make up SESSIONS=memory` and `make up`.
demo-sessions:
	STEP=$(STEP) NAME=$(NAME) RATE=$(RATE) DURATION_S=30 scripts/demo.sh

# Steady load while app2 is killed; prints how many requests failed and how long the slow end stayed slow.
demo-kill:
	STEP=$(STEP) NAME=kill KILL=app2 RATE=$(RATE) DURATION_S=45 KILL_AFTER_S=15 scripts/demo.sh

# Steady load while the cache is killed: database lookups for every 100 opens, before and after.
demo-cache:
	STEP=$(STEP) NAME=cache-kill KILL=cache RATE=500 DURATION_S=50 KILL_AFTER_S=25 scripts/demo.sh

# Normal clients plus one that creates links in a loop; prints how much of each gets through.
# Run it twice: `make up LIMITER=off` then `make demo-abuser NAME=abuser-off`, and `make up` then `make demo-abuser NAME=abuser-on`.
demo-abuser:
	STEP=$(STEP) NAME=$(NAME) SCRIPT=abuser.js RATE=550 ABUSER_RPS=20 DURATION_S=30 scripts/demo.sh

# A quiet client sends a burst of creates: the first ones pass, then 429 with Retry-After.
burst:
	STEP=$(STEP) scripts/burst.sh

# One open while the analytics API is slow (run it on step 3 and on this step to see the difference).
slow-open:
	STEP=$(STEP) scripts/slow-open.sh

# Steady load; the analytics API fails every call for 20 seconds. Prints what was queued, delivered and lost.
demo-outage:
	STEP=$(STEP) scripts/demo-outage.sh

# 20 events added twice each: the worker drops the repeats. `make up DEDUPE=off` shows what happens without it.
demo-dupes:
	STEP=$(STEP) NAME=$(NAME) scripts/demo-dupes.sh

# A short load, then what the service says about itself: /metrics and /healthz.
demo-metrics:
	STEP=$(STEP) scripts/demo-metrics.sh
