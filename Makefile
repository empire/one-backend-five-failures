STEP ?= step-0-naive
RUN  ?= 1
OUT  := results/$(STEP)/run-$(RUN)

.PHONY: up down seed ramp test

up:
	docker compose up -d --build --wait

down:
	docker compose down -v

# 100,000 links for the load test to open.
seed:
	docker compose exec -T db psql -q -U shortener -d shortener < scripts/seed.sql

# One ramp run: records container CPU, runs k6 (from its Docker image), then writes series.json and summary.json.
# Tune with: make ramp STEP_RPS=250 STEP_S=8 LEVELS=16
ramp:
	STEP=$(STEP) RUN=$(RUN) scripts/ramp.sh

test:
	go vet ./... && go test ./...
