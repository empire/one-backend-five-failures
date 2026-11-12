#!/usr/bin/env bash
# Three ramp runs (the app is restarted between runs), then the median.
#   STEP=step-0-naive STEP_RPS=100 STEP_S=8 LEVELS=24 scripts/bench.sh
set -euo pipefail
cd "$(dirname "$0")/.."

export STEP=${STEP:-step-0-naive}
export STEP_RPS=${STEP_RPS:-100} STEP_S=${STEP_S:-8} LEVELS=${LEVELS:-24}
RUNS=${RUNS:-3}
DIR="results/$STEP"
mkdir -p "$DIR"

go build -o "$DIR/.bench" ./tools/bench
"$DIR/.bench" env -out "$DIR/env.json"

dirs=()
for i in $(seq 1 "$RUNS"); do
  echo "== $STEP: run $i of $RUNS"
  docker compose restart app >/dev/null
  sleep 8
  RUN=$i scripts/ramp.sh
  dirs+=("$DIR/run-$i")
done
"$DIR/.bench" merge -out "$DIR" "${dirs[@]}"
rm -f "$DIR/.bench"
cat "$DIR/summary.json"
