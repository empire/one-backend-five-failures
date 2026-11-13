#!/usr/bin/env bash
# A steady run at RATE requests per second, optionally killing one app part-way through.
#   STEP=step-1-stateless NAME=sessions-memory RATE=1000 DURATION_S=30 scripts/demo.sh
#   STEP=step-1-stateless NAME=kill KILL=app2 KILL_AFTER_S=15 RATE=1000 DURATION_S=45 scripts/demo.sh
#   STEP=step-3-ratelimit NAME=abuser-on SCRIPT=abuser.js RATE=550 ABUSER_RPS=20 DURATION_S=30 scripts/demo.sh
#   STEP=step-2-cache NAME=cache-kill KILL=cache KILL_AFTER_S=25 RATE=500 DURATION_S=50 scripts/demo.sh
# Output: results/$STEP/$NAME/run-1/{demo.json,series.json,kill.txt}
set -euo pipefail
cd "$(dirname "$0")/.."

STEP=${STEP:-step-1-stateless}
NAME=${NAME:-steady}
RATE=${RATE:-500}
DURATION_S=${DURATION_S:-40}
SCRIPT=${SCRIPT:-steady.js}     # steady.js, or abuser.js for the noisy-client run
KILL=${KILL:-}                  # compose service to kill, e.g. app2
KILL_AFTER_S=${KILL_AFTER_S:-15}
WINDOW_S=${WINDOW_S:-10}
SERVICES=${SERVICES:-app1,app2,lb,state,cache,db,worker}
EXPVARS=${EXPVARS:-http://localhost:8091/debug/vars,http://localhost:8092/debug/vars,http://localhost:8093/debug/vars}
DIR="results/$STEP/$NAME"
mkdir -p "$DIR"
go build -o "$DIR/.bench" ./tools/bench

one_run() {
  local out="$DIR/run-$1" killer=""
  mkdir -p "$out"
  rm -f "$out/kill.txt" "$out/kill.at"

  "$DIR/.bench" sample -out "$out/cpu.jsonl" -services "$SERVICES" -expvar "$EXPVARS" &
  local sampler=$!
  sleep 2

  if [ -n "$KILL" ]; then
    (
      sleep "$((KILL_AFTER_S + 1))"   # k6 spends about a second in setup (logging the users in)
      cid=$(docker compose ps -q "$KILL")
      date +%s%3N > "$out/kill.at"
      {
        echo "\$ docker kill shortener-$KILL-1"
        docker kill "$cid" 2>&1 | sed "s/^$cid\$/shortener-$KILL-1/"
      } > "$out/kill.txt"
    ) &
    killer=$!
  fi

  docker run --rm --network host --user "$(id -u):$(id -g)" \
    -e RATE="$RATE" -e DURATION_S="$DURATION_S" -e ABUSER_RPS="${ABUSER_RPS:-20}" -e USERS="${USERS:-50}" -e BASE="${BASE:-http://localhost:8088}" \
    -v "$PWD/loadtest:/scripts:ro" -v "$PWD/$out:/out" \
    grafana/k6 run --quiet --out json=/out/k6.json.gz /scripts/$SCRIPT > "$out/k6.log" 2>&1 || true
  if [ -n "$killer" ]; then wait "$killer"; fi

  kill "$sampler" 2>/dev/null || true
  wait "$sampler" 2>/dev/null || true

  local args=(-k6 "$out/k6.json.gz" -cpu "$out/cpu.jsonl" -out "$out" -window-s "$WINDOW_S" -slo-ms "${SLO_MS:-200}")
  if [ -n "$KILL" ]; then args+=(-kill-at-file "$out/kill.at"); fi
  "$DIR/.bench" demo "${args[@]}" > /dev/null

  # Put the killed app back, and give the load balancer time to see it, so the next run starts from two apps.
  if [ -n "$KILL" ]; then docker compose start "$KILL" > /dev/null; sleep 15; fi
}

echo "== $STEP/$NAME"
one_run 1
rm -f "$DIR/.bench"
cat "$DIR/run-1/demo.json"
