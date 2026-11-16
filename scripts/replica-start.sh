#!/bin/bash
# The replica's start: copy the primary's data once, then run as a standby that follows the primary.
set -euo pipefail
until pg_isready -h db -U shortener -q; do sleep 1; done
if [ ! -s "$PGDATA/PG_VERSION" ]; then
  pg_basebackup -h db -U replicator -D "$PGDATA" -R -X stream   # -R: write standby.signal and the primary's address
  chmod 700 "$PGDATA"
fi
exec postgres -c hot_standby=on
