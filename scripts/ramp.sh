#!/usr/bin/env bash
# One ramp run against whatever is up on localhost:8088: the load rises one level at a time until the service fails.
#   STEP=step-0-naive RUN=1 STEP_RPS=250 STEP_S=8 LEVELS=16 scripts/ramp.sh
set -euo pipefail
cd "$(dirname "$0")/.."

STEP=${STEP:-step-0-naive}
RUN=${RUN:-1}
STEP_RPS=${STEP_RPS:-250}
STEP_S=${STEP_S:-10}
LEVELS=${LEVELS:-16}
SERVICES=${SERVICES:-app1,app2,lb,state,cache,db,worker}
# Each app publishes its own /debug/vars (not through the load balancer) so the sampler can sum their counters.
EXPVARS=${EXPVARS:-http://localhost:8091/debug/vars,http://localhost:8092/debug/vars,http://localhost:8093/debug/vars}
OUT="results/$STEP/run-$RUN"
mkdir -p "$OUT"

go build -o "$OUT/.bench" ./tools/bench

"$OUT/.bench" sample -out "$OUT/cpu.jsonl" -services "$SERVICES" -expvar "$EXPVARS" &
SAMPLER=$!
trap 'kill $SAMPLER 2>/dev/null || true' EXIT
sleep 2

docker run --rm --network host --user "$(id -u):$(id -g)" \
  -e USERS="${USERS:-50}" -e STEP_RPS="$STEP_RPS" -e STEP_S="$STEP_S" -e LEVELS="$LEVELS" \
  -v "$PWD/loadtest:/scripts:ro" -v "$PWD/$OUT:/out" \
  grafana/k6 run --quiet --out json=/out/k6.json.gz /scripts/ramp.js > "$OUT/k6.log" 2>&1 || true

kill $SAMPLER 2>/dev/null || true
wait $SAMPLER 2>/dev/null || true

"$OUT/.bench" series -k6 "$OUT/k6.json.gz" -cpu "$OUT/cpu.jsonl" -out "$OUT" \
  -step-s "$STEP_S" -step-rps "$STEP_RPS" -levels "$LEVELS"
rm -f "$OUT/.bench"
