#!/usr/bin/env bash
# Wait until the replica has received every change the primary has made so far (the seed, for a run that starts right after it).
set -euo pipefail
cd "$(dirname "$0")/.."
lsn=$(docker compose exec -T db psql -qAt -U shortener -d shortener -c "SELECT pg_current_wal_lsn()")
for _ in $(seq 1 60); do
  caught=$(docker compose exec -T replica psql -qAt -U shortener -d shortener -c "SELECT pg_last_wal_replay_lsn() >= '$lsn'")
  if [ "$caught" = t ]; then echo "replica is caught up (primary at $lsn)"; exit 0; fi
  sleep 1
done
echo "replica did not catch up to $lsn" >&2
exit 1
