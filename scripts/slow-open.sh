#!/usr/bin/env bash
# One open while the analytics API takes SLOW_MS to answer: the real curl and its time.
# Run it on step 3 (the app calls analytics on the spot) and on step 4 (the app queues the click).
#   STEP=step-3-ratelimit scripts/slow-open.sh      STEP=step-4-queue scripts/slow-open.sh
set -euo pipefail
cd "$(dirname "$0")/.."

STEP=${STEP:-step-4-queue}
SLOW_MS=${SLOW_MS:-2000}
BASE=${BASE:-http://localhost:8088}
THIRD=http://localhost:9000
OUT="results/$STEP/slow-open"
mkdir -p "$OUT"
c() { curl --noproxy '*' -s "$@"; }

c -o /dev/null "$BASE/s000001"                       # warm the cache, so the time is the analytics call
c -X POST "$THIRD/admin/slow?api=analytics&ms=$SLOW_MS"
trap 'c -X POST "$THIRD/admin/slow?api=analytics&ms=20"' EXIT
{
  echo "\$ curl -s -o /dev/null -w 'open: %{time_total}s\\n' localhost:8088/s000001"
  for _ in 1 2 3; do c -o /dev/null -w 'open: %{time_total}s\n' "$BASE/s000001"; done
} | tee "$OUT/open.txt"
awk -v ms="$SLOW_MS" '/^open:/ {gsub("s","",$2); t[NR]=$2*1000} END {m=t[2]; for (i in t) if (t[i]<m) m=t[i]; printf "{\"analytics_delay_ms\": %d, \"fastest_open_ms\": %d}\n", ms, m}' "$OUT/open.txt" | tee "$OUT/open.json"
