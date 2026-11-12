STEP ?= step-1-stateless
NAME ?= sessions
RATE ?= 1000
RUN  ?= 1
OUT  := results/$(STEP)/run-$(RUN)

.PHONY: up down seed load test demo-sessions demo-kill

# Two apps behind a load balancer. `make up SESSIONS=memory` keeps sessions in each app's memory,
# which is the failure this step solves.
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
