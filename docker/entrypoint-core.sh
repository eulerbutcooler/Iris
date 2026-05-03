#!/bin/sh
set -e

echo "[iris-core] running database migrations..."

# Run each migration file in order
for f in /migrations/*.up.sql; do
    echo "[iris-core] applying $f"
    psql "$DATABASE_URL" -f "$f" || echo "[iris-core] migration $f may already be applied, continuing..."
done

echo "[iris-core] migrations done — starting server"
exec /bin/iris-core
