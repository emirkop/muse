#!/usr/bin/env bash
# Muse — API contract suite.
# The one entry point for the backend's HTTP contract: request shape,
# response shape, status/error contract, auth and ownership behaviour,
# malformed input, and the privacy/nonexistent equivalence
# requires. It is deliberately *not* a new set of tests — it runs the
# whole-stack suites phases 21–84 already built, plus 's
# ledger and the three gaps it closed, as one named thing that CI can
# fail on.
# Two packages hold API-boundary tests and both run here:
# cmd/api — whole-stack: real router, real
# PostgreSQL, real signer (38 files)
# internal/identity/interfaces — handler-level identity/auth tests
# Everything else (Domain unit tests) is gate:
# scripts/domain-coverage.sh
# Usage:
# TEST_DATABASE_URL=postgres://localhost:5432/muse_test scripts/api-contract.sh
# scripts/api-contract.sh -run TestApiContract # extra `go test` flags pass through
# Requires a reachable PostgreSQL. The suites truncate and reseed, so
# point this at a database you do not mind losing.

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
  echo "api-contract: TEST_DATABASE_URL is not set." >&2
  echo "  These are integration tests against a real PostgreSQL; there is no" >&2
  echo "  mock fallback, because a mocked API contract proves nothing." >&2
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

# Packages are named explicitly, never as ./... — a wildcard package
# pattern has been observed to hang indefinitely on this toolchain
# (recorded at for scripts/domain-coverage.sh, same cause).
PACKAGES=(
  ./cmd/api
  ./internal/identity/interfaces
)

# -p 1 is load-bearing, not tuning: these packages share one database,
# so parallel package execution has them truncating each other's rows
# mid-test. Recorded since.
echo "api-contract: running ${#PACKAGES[@]} API-boundary packages against ${TEST_DATABASE_URL%%\?*}"
echo

START=$(date +%s)
if go test "${PACKAGES[@]}" -p 1 -count=1 -timeout 15m "$@"; then
  STATUS=0
else
  STATUS=$?
fi
ELAPSED=$(( $(date +%s) - START ))

echo
if [[ $STATUS -eq 0 ]]; then
  echo "api-contract: PASS in ${ELAPSED}s"
else
  echo "api-contract: FAIL in ${ELAPSED}s"
  echo
  echo "  If the failure is a route with no declared contract, the ledger in"
  echo "  cmd/api/api_contract_test.go is the file to edit: every route"
  echo "  must name its category and the suite that owns it. That is the point"
  echo "  of the check — a new endpoint cannot ship untested by omission."
fi
exit $STATUS
