#!/usr/bin/env bash
#: the iOS Domain coverage gate.
# Runs every XCTest class under MuseAppTests/Domain with code coverage on,
# then aggregates line coverage over `MuseApp/Domain/` and fails when the
# AGGREGATE falls below the gate.
# Two things this deliberately is NOT ('s decision):
# - not per-file/per-package: `Domain/Interfaces/` is mostly protocol
# declarations, and a per-file bar would demand tests for `protocol`
# conformance rather than for rules.
# - not a delta gate: the delta against the committed baseline is REPORTED
# so a drop is visible, but never fails the run on its own.
# Why 80 and not Go's 95: Swift coverage is measured per file path, and the
# `Domain/` denominator includes protocol declarations and value-type
# initialisers that carry no rule. The two numbers are not comparable, and
# forcing them to match would mean writing meaningless tests.
# Usage: scripts/domain-coverage.sh
# GATE=75 DESTINATION='platform=iOS Simulator,name=iPhone 16' scripts/domain-coverage.sh
set -euo pipefail

cd "$(dirname "$0")/.."

GATE="${GATE:-80}"
DESTINATION="${DESTINATION:-platform=iOS Simulator,name=iPhone 17 Pro}"
BASELINE_FILE="scripts/domain-coverage.baseline"
RESULT_BUNDLE="$(mktemp -d -t muse-domain-cover.XXXXXX)/domain.xcresult"
trap 'rm -rf "$(dirname "$RESULT_BUNDLE")"' EXIT

# The Domain suites, discovered from the test tree rather than listed, so a
# new domain test file is gated the day it appears. Test classes are named
# `*Tests`; support types in the same files are not.
ONLY_TESTING="$(grep -rho 'final class [A-Za-z0-9_]*Tests' MuseAppTests/Domain \
  | sed 's/final class //' | sort -u | sed 's|^|-only-testing:MuseAppTests/|' | tr '\n' ' ')"
if [ -z "$ONLY_TESTING" ]; then
  echo "domain-coverage: no test classes found under MuseAppTests/Domain" >&2
  exit 2
fi

# shellcheck disable=SC2086
xcodebuild test -project MuseApp.xcodeproj -scheme MuseApp \
  -destination "$DESTINATION" -enableCodeCoverage YES \
  -resultBundlePath "$RESULT_BUNDLE" $ONLY_TESTING \
  | grep -E "Executed [0-9]+ tests|error:" | tail -1

# Aggregate over MuseApp/Domain/ from xccov's JSON, which is stable across
# Xcode versions in a way the text report is not.
TOTAL="$(xcrun xccov view --report --json "$RESULT_BUNDLE" | python3 -c '
import json, sys
report = json.load(sys.stdin)
target = next(t for t in report["targets"] if t["name"] == "MuseApp.app")
files = [f for f in target["files"] if "/MuseApp/Domain/" in f["path"]]
covered = sum(f["coveredLines"] for f in files)
executable = sum(f["executableLines"] for f in files)
if executable == 0:
    sys.exit("domain-coverage: MuseApp/Domain/ has no executable lines in the report")
print("%.1f" % (100.0 * covered / executable))
')"

echo "iOS MuseApp/Domain aggregate line coverage: ${TOTAL}% (gate ${GATE}%)"

if [ -f "$BASELINE_FILE" ]; then
  BASELINE="$(tr -d '[:space:]' < "$BASELINE_FILE")"
  DELTA="$(awk -v now="$TOTAL" -v base="$BASELINE" 'BEGIN { printf "%+.1f", now - base }')"
  echo "Delta vs committed baseline (${BASELINE}%): ${DELTA} points — informational, not a merge blocker"
else
  echo "No baseline file at ${BASELINE_FILE}; delta not reported"
fi

if awk -v now="$TOTAL" -v gate="$GATE" 'BEGIN { exit !(now + 0 < gate + 0) }'; then
  echo "FAIL: iOS Domain coverage ${TOTAL}% is below the ${GATE}% gate." >&2
  exit 1
fi
echo "PASS"
