# Runs once, when the primary's data directory is made (the postgres image runs everything in docker-entrypoint-initdb.d).
# The replica logs in as `replicator` to copy the data and then to follow every change.
psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "CREATE ROLE replicator WITH REPLICATION LOGIN PASSWORD 'replicator'"
echo "host replication replicator all scram-sha-256" >> "$PGDATA/pg_hba.conf"
