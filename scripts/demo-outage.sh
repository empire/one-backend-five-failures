#!/usr/bin/env bash
# Steady load while the analytics API fails every call for a while, then answers again.
# Click events wait in their lane, scans carry on, and the backlog drains when the API is back.
#   scripts/demo-outage.sh     output: results/step-4-queue/outage/run-1/{demo.json,series.json,fail.txt}
set -euo pipefail
cd "$(dirname "$0")/.."

STEP=${STEP:-step-4-queue}
NAME=${NAME:-outage}
RATE=${RATE:-500}
DURATION_S=${DURATION_S:-60}
FAIL_AFTER_S=${FAIL_AFTER_S:-15}
FAIL_FOR_S=${FAIL_FOR_S:-20}
DRAIN_MAX_S=${DRAIN_MAX_S:-90}
THIRD=http://localhost:9000
WORKER=http://localhost:8093
APPS="http://localhost:8091 http://localhost:8092"
SERVICES=${SERVICES:-app1,app2,lb,state,cache,db,worker}
EXPVARS=${EXPVARS:-http://localhost:8091/debug/vars,http://localhost:8092/debug/vars,$WORKER/debug/vars}
OUT="results/$STEP/$NAME/run-1"
mkdir -p "$OUT"
go build -o "$OUT/.bench" ./tools/bench

c() { curl --noproxy '*' -s "$@"; }
stats() { c "$THIRD/admin/stats"; }
depth() { c "$WORKER/debug/vars" | jq ".queue_$1"; }
published() { for u in $APPS; do c "$u/debug/vars" | jq ".events_$1"; done | paste -sd+ | bc; }

"$OUT/.bench" sample -out "$OUT/cpu.jsonl" -services "$SERVICES" -expvar "$EXPVARS" &
sampler=$!
trap 'kill $sampler 2>/dev/null || true' EXIT
sleep 2

s0=$(stats); click0=$(published click); scan0=$(published scan)
docker run --rm --network host --user "$(id -u):$(id -g)" \
  -e RATE="$RATE" -e DURATION_S="$DURATION_S" -e USERS=50 \
  -v "$PWD/loadtest:/scripts:ro" -v "$PWD/$OUT:/out" \
  grafana/k6 run --quiet --out json=/out/k6.json.gz /scripts/steady.js > "$OUT/k6.log" 2>&1 &
k6=$!

sleep "$((FAIL_AFTER_S + 1))"   # k6 spends about a second in setup (logging the users in)
{
  echo "\$ curl -i -X POST 'localhost:9000/admin/fail?api=analytics&pct=100'"
  c -i -X POST "$THIRD/admin/fail?api=analytics&pct=100" | head -1 | tr -d '\r'
} | tee "$OUT/fail.txt"
s1=$(stats)
sleep "$FAIL_FOR_S"
c -X POST "$THIRD/admin/fail?api=analytics&pct=0" > /dev/null
s2=$(stats); waiting=$(depth click)

wait "$k6" || true
for _ in $(seq "$DRAIN_MAX_S"); do
  [ "$(depth click)" = 0 ] && [ "$(depth scan)" = 0 ] && break
  sleep 1
done
sleep 2
s3=$(stats); click1=$(published click)
kill "$sampler" 2>/dev/null || true; wait "$sampler" 2>/dev/null || true

"$OUT/.bench" demo -k6 "$OUT/k6.json.gz" -cpu "$OUT/cpu.jsonl" -out "$OUT" > /dev/null
jq -n --argjson s0 "$s0" --argjson s1 "$s1" --argjson s2 "$s2" --argjson s3 "$s3" \
  --argjson click0 "$click0" --argjson click1 "$click1" --argjson waiting "$waiting" '
  ($click1 - $click0) as $queued | ($s3.analytics.delivered - $s0.analytics.delivered) as $delivered |
  {events_queued: $queued, events_delivered: $delivered, events_lost: ($queued - $delivered),
   waiting_when_api_returned: $waiting,
   scan_continued: ($s2.scan.delivered - $s1.scan.delivered),
   analytics_failed_calls: ($s3.analytics.failed - $s0.analytics.failed)}' > "$OUT/outage.json"
jq -s '.[0] * .[1]' "$OUT/demo.json" "$OUT/outage.json" > "$OUT/demo.tmp" && mv "$OUT/demo.tmp" "$OUT/demo.json"
rm -f "$OUT/.bench"
cat "$OUT/demo.json"
