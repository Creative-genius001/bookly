#!/usr/bin/env bash
#
# verify-geo.sh — exercises the multi-shop migration + PostGIS discovery query
# end-to-end against ephemeral PostGIS + Redis containers.
#
# It proves, against a real database:
#   1. the legacy UNIQUE index on shops.owner_id is dropped by AutoMigrate
#      (so an owner can have multiple shops), and
#   2. GET /shops returns shops sorted by PostGIS distance.
#
# Requires: docker (or a docker-compatible CLI), curl, python3, go.
# Usage:    ./scripts/verify-geo.sh
# Env:      PG_PORT, REDIS_PORT, API_PORT to override ports; KEEP=1 to skip cleanup.
set -euo pipefail

# Fully-qualified so both Docker and podman resolve them without config.
PG_IMAGE="${PG_IMAGE:-docker.io/postgis/postgis:16-3.4}"
REDIS_IMAGE="${REDIS_IMAGE:-docker.io/library/redis:7-alpine}"
PG_NAME="bookly-verify-pg"
REDIS_NAME="bookly-verify-redis"
PG_PORT="${PG_PORT:-55432}"
REDIS_PORT="${REDIS_PORT:-56379}"
API_PORT="${API_PORT:-18080}"
DB_URL="postgres://barber:barber@localhost:${PG_PORT}/barber_booking?sslmode=disable"
BASE="http://localhost:${API_PORT}"
WORKDIR="$(cd "$(dirname "$0")/.." && pwd)"
API_BIN="$(mktemp -u)"
API_LOG="$(mktemp)"
API_PID=""

cleanup() {
  [ -n "$API_PID" ] && kill "$API_PID" 2>/dev/null || true
  if [ "${KEEP:-0}" != "1" ]; then
    docker rm -f "$PG_NAME" "$REDIS_NAME" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

need() { command -v "$1" >/dev/null 2>&1 || { echo "✗ missing required tool: $1"; exit 1; }; }
need docker; need curl; need python3; need go

psql() { docker exec -i "$PG_NAME" psql -U barber -d barber_booking -tAq "$@"; }

start_api() {
  ( cd "$WORKDIR" && DATABASE_URL="$DB_URL" REDIS_ADDR="localhost:${REDIS_PORT}" \
    JWT_SECRET="verify-secret" PORT="$API_PORT" PAYSTACK_SECRET_KEY="sk_test_dummy" \
    APP_ENV="development" "$API_BIN" >>"$API_LOG" 2>&1 & echo $! > /tmp/.bookly_api_pid )
  API_PID="$(cat /tmp/.bookly_api_pid)"
}
api_alive() { [ -n "$API_PID" ] && kill -0 "$API_PID" 2>/dev/null; }
stop_api() { [ -n "$API_PID" ] && kill "$API_PID" 2>/dev/null || true; wait "$API_PID" 2>/dev/null || true; API_PID=""; }
# Start the API and wait for /health; the API exits if the DB connect fails, so
# retry a few times to ride out the PostGIS image's init-time restart.
ensure_up() {
  for attempt in 1 2 3 4 5 6; do
    start_api
    for _ in $(seq 1 30); do
      curl -fs "${BASE}/health" >/dev/null 2>&1 && return 0
      api_alive || break
      sleep 1
    done
    echo "    (api start attempt ${attempt} failed; retrying)"
    stop_api
    sleep 3
  done
  echo "✗ API did not become healthy"; tail -40 "$API_LOG"; exit 1
}
pyget() { python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }

# Wait until Postgres answers a real query 3 times in a row (rides out the
# PostGIS image's init restart).
wait_pg() {
  local ok=0
  for _ in $(seq 1 120); do
    if docker exec "$PG_NAME" psql -U barber -d barber_booking -tAc 'select 1' >/dev/null 2>&1; then
      ok=$((ok + 1)); [ "$ok" -ge 3 ] && { sleep 2; return 0; }
    else
      ok=0
    fi
    sleep 1
  done
  echo "✗ Postgres did not become ready"; exit 1
}

echo "==> Starting PostGIS + Redis"
docker rm -f "$PG_NAME" "$REDIS_NAME" >/dev/null 2>&1 || true
docker run -d --name "$PG_NAME" -e POSTGRES_USER=barber -e POSTGRES_PASSWORD=barber \
  -e POSTGRES_DB=barber_booking -p "${PG_PORT}:5432" "$PG_IMAGE" >/dev/null
docker run -d --name "$REDIS_NAME" -p "${REDIS_PORT}:6379" "$REDIS_IMAGE" >/dev/null

echo "==> Waiting for Postgres"
wait_pg

echo "==> Building API"
( cd "$WORKDIR" && go build -o "$API_BIN" ./cmd/api )

echo "==> First migrate (fresh DB)"
ensure_up; stop_api

echo "==> Simulating a legacy DB: forcing a UNIQUE index on shops.owner_id"
psql -c "DROP INDEX IF EXISTS idx_shops_owner_id;" >/dev/null
psql -c "CREATE UNIQUE INDEX idx_shops_owner_id ON shops(owner_id);" >/dev/null
UNIQUE_BEFORE="$(psql -c "SELECT indisunique FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='idx_shops_owner_id';" | tr -d '[:space:]')"
echo "    owner_id index unique before restart: ${UNIQUE_BEFORE:-<none>}"

echo "==> Restarting API (migration must drop the unique index + enable PostGIS)"
ensure_up
UNIQUE_AFTER="$(psql -c "SELECT COALESCE((SELECT indisunique FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='idx_shops_owner_id'), false);" | tr -d '[:space:]')"
echo "    owner_id index unique after restart:  ${UNIQUE_AFTER}"
HAS_POSTGIS="$(psql -c "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='postgis');" | tr -d '[:space:]')"
echo "    postgis extension present: ${HAS_POSTGIS}"
[ "$UNIQUE_AFTER" = "t" ] && { echo "✗ owner_id index is still UNIQUE — multi-shop migration failed"; exit 1; }
[ "$HAS_POSTGIS" = "t" ] || { echo "✗ postgis extension missing"; exit 1; }

echo "==> Signing up an owner"
TOKEN="$(curl -fs -X POST "${BASE}/auth/signup" -H 'Content-Type: application/json' \
  -d '{"email":"owner@verify.test","phone":"+2348000000000","password":"Verify1234","role":"owner"}' \
  | pyget '["data"]["tokens"]["access_token"]')"

echo "==> Creating two shops for the same owner (proves multi-shop)"
A_SLUG="$(curl -fs -X POST "${BASE}/shops" -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
  -d '{"name":"Verify Near Shop","email":"a@verify.test","phone":"+2348000000001","timezone":"Africa/Lagos","latitude":6.4550,"longitude":3.3960}' \
  | pyget '["data"]["slug"]')"
B_SLUG="$(curl -fs -X POST "${BASE}/shops" -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
  -d '{"name":"Verify Far Shop","email":"b@verify.test","phone":"+2348000000002","timezone":"Africa/Lagos","latitude":6.6000,"longitude":3.5000}' \
  | pyget '["data"]["slug"]')"
echo "    created: ${A_SLUG}, ${B_SLUG}"

echo "==> GET /shops/mine must list 2 shops"
curl -fs "${BASE}/shops/mine" -H "Authorization: Bearer ${TOKEN}" \
  | python3 -c 'import sys,json;d=json.load(sys.stdin)["data"];assert len(d)==2,f"expected 2, got {len(d)}";print("    multi-shop OK:",len(d),"shops")'

echo "==> GET /shops discovery must be distance-sorted, nearest first"
curl -fs "${BASE}/shops?lat=6.4541&lng=3.3947&radius=50&page=1&page_size=10" \
  | A_SLUG="$A_SLUG" python3 -c '
import sys,json,os
shops=json.load(sys.stdin)["data"]["shops"]
assert len(shops)>=2, f"expected >=2 shops, got {len(shops)}"
dists=[s["distance_km"] for s in shops]
assert dists==sorted(dists), f"not distance-sorted: {dists}"
assert shops[0]["slug"]==os.environ["A_SLUG"], f"nearest should be {os.environ['A_SLUG']}, got {shops[0]['slug']}"
print("    discovery OK: nearest=%s (%.2f km), farthest=%.2f km" % (shops[0]["slug"], dists[0], dists[-1]))
'

echo ""
echo "✅ ALL CHECKS PASSED — multi-shop migration + PostGIS discovery verified against a real database."
