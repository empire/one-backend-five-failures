#!/usr/bin/env bash
# Run a short steady load, then print what the service says about itself: GET /metrics (one app) and GET /healthz.
#   scripts/demo-metrics.sh     output: results/step-5-metrics/metrics/{metrics.txt,healthz.txt}
set -euo pipefail
cd "$(dirname "$0")/.."

STEP=${STEP:-step-5-metrics}
OUT="results/$STEP/metrics"
mkdir -p "$OUT"
c() { curl --noproxy '*' -s "$@"; }

docker run --rm --network host --user "$(id -u):$(id -g)" \
  -e RATE=300 -e DURATION_S=15 -e USERS=50 \
  -v "$PWD/loadtest:/scripts:ro" grafana/k6 run --quiet /scripts/steady.js > "$OUT/k6.log" 2>&1 || true

{ echo '$ curl -s localhost:8091/metrics'; c localhost:8091/metrics; } > "$OUT/metrics.txt"
{ echo '$ curl -s localhost:8088/healthz'; c localhost:8088/healthz; } > "$OUT/healthz.txt"
cat "$OUT/healthz.txt"
rg -v '^#' "$OUT/metrics.txt" | rg 'GET /\{code\}|in_flight|^\$' | head -20
