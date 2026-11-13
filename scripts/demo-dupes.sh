#!/usr/bin/env bash
# The queue can deliver an event twice. Add N events to the click lane, each one twice, and count what the analytics API receives.
#   make up          && scripts/demo-dupes.sh   the worker drops the repeats
#   make up DEDUPE=off && NAME=dupes-off scripts/demo-dupes.sh   it does not
set -euo pipefail
cd "$(dirname "$0")/.."

STEP=${STEP:-step-4-queue}
NAME=${NAME:-dupes}
N=${N:-20}
THIRD=http://localhost:9000
WORKER=http://localhost:8093
OUT="results/$STEP/$NAME/run-1"
mkdir -p "$OUT"
c() { curl --noproxy '*' -s "$@"; }

before=$(c "$THIRD/admin/stats")
dropped0=$(c "$WORKER/debug/vars" | jq .dropped_click)
tag=$(date +%s)
for i in $(seq "$N"); do
  for _ in 1 2; do
    printf 'XADD events:click * id dupe-%s-%s code s000001\n' "$tag" "$i"
  done
done | docker compose exec -T state redis-cli > /dev/null
for _ in $(seq 60); do
  [ "$(c "$WORKER/debug/vars" | jq .queue_click)" = 0 ] && break
  sleep 1
done
sleep 2
after=$(c "$THIRD/admin/stats")
dropped1=$(c "$WORKER/debug/vars" | jq .dropped_click)

jq -n --argjson b "$before" --argjson a "$after" --argjson n "$N" --argjson d "$((dropped1 - dropped0))" '
  {redeliveries_injected: $n, events_added: (2 * $n),
   events_delivered: ($a.analytics.delivered - $b.analytics.delivered),
   dupes_delivered: ($a.analytics.duplicates - $b.analytics.duplicates),
   repeats_dropped_by_worker: $d}' | tee "$OUT/demo.json"
