#!/usr/bin/env bash
#: the Go Domain coverage gate.
# Measures statement coverage over every `internal/*/domain` package — the
# pure business rules, which need no database, no network and no RealityKit
# to run — and fails when the AGGREGATE falls below the gate.
# Two things this deliberately is NOT ('s decision):
# - not per-package: `sharing/domain` is three functions, so one uncovered
# accessor there is a 33% drop. A per-package bar would be about
# arithmetic, not risk.
# - not a delta gate: the delta against the committed baseline is REPORTED
# on every run so a drop is visible, but a legitimate refactor that
# deletes tested code must not be blocked. Delta alone never fails.
# Usage: scripts/domain-coverage.sh # gate at the default
# GATE=90 scripts/domain-coverage.sh # override (CI keeps default)
set -euo pipefail

cd "$(dirname "$0")/.."

GATE="${GATE:-95}"
BASELINE_FILE="scripts/domain-coverage.baseline"
PROFILE="$(mktemp -t muse-domain-cover.XXXXXX)"
trap 'rm -f "$PROFILE"' EXIT

# Every domain package, discovered from the tree rather than listed, so a new
# bounded context's domain is gated the day it appears. Discovered with a
# glob, deliberately not `go list`: this script must never block on module
# resolution, a proxy, or a cache lock — a coverage gate that hangs is worse
# than one that fails.
PACKAGES=""
for dir in internal/*/domain; do
  [ -d "$dir" ] && PACKAGES="$PACKAGES ./$dir"
done
if [ -z "$PACKAGES" ]; then
  echo "domain-coverage: no internal/*/domain directories found" >&2
  exit 2
fi

# shellcheck disable=SC2086
go test -count=1 -coverprofile="$PROFILE" $PACKAGES >/dev/null

TOTAL="$(go tool cover -func="$PROFILE" | awk '/^total:/ { gsub(/%/, "", $NF); print $NF }')"
if [ -z "$TOTAL" ]; then
  echo "domain-coverage: could not read a total from the profile" >&2
  exit 2
fi

echo "Go Domain aggregate statement coverage: ${TOTAL}% (gate ${GATE}%)"

# Delta: reported, never enforced.
if [ -f "$BASELINE_FILE" ]; then
  BASELINE="$(tr -d '[:space:]' < "$BASELINE_FILE")"
  DELTA="$(awk -v now="$TOTAL" -v base="$BASELINE" 'BEGIN { printf "%+.1f", now - base }')"
  echo "Delta vs committed baseline (${BASELINE}%): ${DELTA} points — informational, not a merge blocker"
else
  echo "No baseline file at ${BASELINE_FILE}; delta not reported"
fi

# The gate.
if awk -v now="$TOTAL" -v gate="$GATE" 'BEGIN { exit !(now + 0 < gate + 0) }'; then
  echo "FAIL: Go Domain coverage ${TOTAL}% is below the ${GATE}% gate." >&2
  echo "      Per-package hint (mean of per-function coverage, to find where — not the gated number):" >&2
  go tool cover -func="$PROFILE" | awk -F'[\t:]+' '{
    if ($1 ~ /^total/) next
    n = $1; sub(/\/[^\/]*$/, "", n)
    pct = $NF; gsub(/%/, "", pct); sum[n] += pct; cnt[n]++
  } END { for (k in sum) printf "      %-50s %.1f%%\n", k, sum[k]/cnt[k] }' | sort >&2
  exit 1
fi
echo "PASS"
