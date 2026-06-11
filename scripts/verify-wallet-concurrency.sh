#!/usr/bin/env bash
#
# verify-wallet-concurrency.sh — proves the wallet RESERVE phase's pessimistic
# lock prevents double-spend. Spins up an ephemeral Postgres and runs the Go
# integration test that fires many concurrent reserves against one balance.
# No Paystack required (it exercises only the locked DB reserve).
#
# Requires: docker (or podman) and go.
set -euo pipefail

PG_IMAGE="${PG_IMAGE:-docker.io/library/postgres:16-alpine}"
PG_NAME="bookly-wallet-pg"
PG_PORT="${PG_PORT:-55433}"
DSN="postgres://barber:barber@localhost:${PG_PORT}/barber_booking?sslmode=disable"
WORKDIR="$(cd "$(dirname "$0")/.." && pwd)"

cleanup() {
  if [ "${KEEP:-0}" != "1" ]; then
    docker rm -f "$PG_NAME" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

need() { command -v "$1" >/dev/null 2>&1 || { echo "✗ missing required tool: $1"; exit 1; }; }
need docker
need go

echo "==> Starting Postgres"
docker rm -f "$PG_NAME" >/dev/null 2>&1 || true
docker run -d --name "$PG_NAME" \
  -e POSTGRES_USER=barber -e POSTGRES_PASSWORD=barber -e POSTGRES_DB=barber_booking \
  -p "${PG_PORT}:5432" "$PG_IMAGE" >/dev/null

echo "==> Waiting for Postgres"
ok=0
for _ in $(seq 1 90); do
  if docker exec "$PG_NAME" psql -U barber -d barber_booking -tAc 'select 1' >/dev/null 2>&1; then
    ok=$((ok + 1)); [ "$ok" -ge 3 ] && { sleep 1; break; }
  else
    ok=0
  fi
  sleep 1
done

echo "==> Running concurrency test"
( cd "$WORKDIR" && TEST_DATABASE_URL="$DSN" go test -run TestWithdrawalConcurrency ./internal/modules/payouts/ -v -count=1 )

echo ""
echo "✅ Wallet concurrency verified — the pessimistic lock prevents double-spend."
