#!/usr/bin/env bash
# A quiet client sends a burst of creates at the same moment, then two more.
# The first BURST (the bucket size) pass; once the bucket is empty the service answers 429 and says when to try again.
#   scripts/burst.sh        output: results/step-3-ratelimit/burst/{burst.txt,burst.json}
set -euo pipefail
cd "$(dirname "$0")/.."

BASE=${BASE:-http://localhost:8088}
BURST=${BURST:-10}   # the bucket size
OUT=results/${STEP:-step-3-ratelimit}/burst
mkdir -p "$OUT"
c() { curl --noproxy '*' -s "$@"; }

user="quiet-$(date +%s)"
sid=$(c -D - -o /dev/null -X POST -H 'Content-Type: application/json' -d "{\"user\":\"$user\"}" "$BASE/login" |
  sed -n 's/^[Ss]et-[Cc]ookie: sid=\([^;]*\).*/\1/p')

create() { # create N: one POST /links; prints the status line for request N
  local code wait
  read -r code wait < <(c -o /dev/null -w '%{http_code} %header{retry-after}\n' -X POST \
    -H 'Content-Type: application/json' -H "Cookie: sid=$sid" \
    -d "{\"url\":\"https://example.com/burst/$1\"}" "$BASE/links")
  if [ "$code" = 201 ]; then echo "create $1: 201 Created"; else echo "create $1: $code Too Many Requests, Retry-After: $wait"; fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
{
  # The burst: BURST requests at the same moment.
  for i in $(seq 1 "$BURST"); do create "$i" > "$tmp/$i" & done
  wait
  for i in $(seq 1 "$BURST"); do cat "$tmp/$i"; done
  # Then two more straight away.
  for i in $(seq $((BURST + 1)) $((BURST + 2))); do create "$i"; done
} | tee "$OUT/burst.txt"
passed=$(grep -c ': 201' "$OUT/burst.txt" || true)
turned_away=$(grep -c ': 429' "$OUT/burst.txt" || true)
printf '{"sent": %s, "passed": %s, "turned_away": %s}\n' $((BURST + 2)) "$passed" "$turned_away" | tee "$OUT/burst.json"
