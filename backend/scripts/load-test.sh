#!/usr/bin/env bash
# Muse — backend load & concurrency suite.
# Deliberately NOT wired into CI, and that is the decision rather than an
# omission: these runs seed up to 100,000 catalog rows and fire thousands of
# requests, and their *numbers* are meaningful only relative to the machine
# they ran on. A CI runner's figures would vary with whatever else the
# runner is doing, so a latency assertion there would either be so loose it
# proves nothing or so tight it fails at random. What CI enforces instead is
# the API contract (scripts/api-contract.sh) and Domain coverage
# (scripts/domain-coverage.sh); this script is the tool you run by hand
# before a release, after a schema change, or when a query is rewritten.
# The suite's own assertions ARE deterministic — error counts must be zero,
# concurrent adds must land on distinct slots, pool saturation must be
# visible in EmptyAcquireCount, a cursor must advance. Only the timings are
# environment-dependent, and those are logged rather than asserted.
# Usage:
# TEST_DATABASE_URL=postgres://localhost:5432/muse_dev scripts/load-test.sh
# TEST_DATABASE_URL=... scripts/load-test.sh -run TestCatalogSearchAtScale
# Scenarios (see cmd/api/load_test.go for what each proves):
# ConcurrentVisitorReads — shared-content reads at 1/8/32/64 concurrent
# LargeAccountCollectionRoomList — 25/100/400 Rooms, no pagination anywhere
# AccountLockContention — one account vs distinct accounts, 16 adds
# PoolSaturation... — pool behaviour, and the hypothesis it killed
# CatalogSearchAtScale — 5k/50k/100k Models per category, 4 query shapes
# Synthetic data is removed by the suite. It is labelled `dev-synthetic:` /
# `dev_fixture` so a leftover row could never be mistaken for catalog
# content, and production refuses to serve that classification.

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
  echo "load-test: TEST_DATABASE_URL is not set." >&2
  echo "  These are load tests against a real PostgreSQL. Point them at a" >&2
  echo "  database you do not mind seeding and truncating." >&2
  exit 2
fi

# These suites TRUNCATE accounts, museums and rooms. Pointing them at the
# development database silently deletes the DEV account/Museum/Room the
# design phase uses — it has happened once already. Refuse it here as well
# as in the Go guard, so the mistake is caught before a single test runs.
case "${TEST_DATABASE_URL}" in
  *"/muse_dev"*|*"/muse_dev?"*|*"dbname=muse_dev"*|\
  *"/muse?"*|*"/muse_production"*|*"/muse_prod"*|*"/muse_staging"*)
    echo "$(basename "$0"): TEST_DATABASE_URL points at a development or production database." >&2
    echo "  These suites truncate accounts, museums and rooms. Use a dedicated one:" >&2
    echo "    createdb muse_test" >&2
    echo "    export TEST_DATABASE_URL=postgres://localhost:5432/muse_test?sslmode=disable" >&2
    exit 2
    ;;
esac

echo "load-test: machine — $(uname -m), $(sysctl -n hw.ncpu 2>/dev/null || nproc) logical CPUs"
echo "load-test: NOTE this process is both the load generator and the server,"
echo "           and PostgreSQL is local. Numbers are local observations, never"
echo "           production capacity guarantees."
echo

START=$(date +%s)
# Package named explicitly, never ./... — a wildcard package pattern hangs on
# this toolchain (recorded at, recurred at phases 85 and 87).
if MUSE_LOAD_TEST=1 go test ./cmd/api/ -run 'TestLoad' -count=1 -v -timeout 30m "$@"; then
  STATUS=0
else
  STATUS=$?
fi
ELAPSED=$(( $(date +%s) - START ))

echo
if [[ $STATUS -eq 0 ]]; then
  echo "load-test: PASS in ${ELAPSED}s — read the MEASURED lines above; the safe"
  echo " operating ranges they support are in 's"
  echo " outcome and."
else
  echo "load-test: FAIL in ${ELAPSED}s"
fi
exit $STATUS
