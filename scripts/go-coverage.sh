#!/usr/bin/env bash
# Scoped Go coverage for hand-written production code.
#
# Runs the whole Go suite with cross-package instrumentation so a statement
# counts as covered no matter which test binary reached it, merges the
# duplicate blocks the per-binary profiles emit, drops generated and test-only
# packages, and fails below GO_COVERAGE_MIN.
#
# Only the Go toolchain and POSIX shell/awk are used; there is deliberately no
# coverage dependency to keep the gate reproducible from a bare checkout.
set -euo pipefail

cd "$(dirname "$0")/.."

OUT_DIR=${GO_COVERAGE_DIR:-artifacts/coverage}
MIN=${GO_COVERAGE_MIN:-60}
PROFILE="$OUT_DIR/coverage.out"
RAW="$OUT_DIR/coverage.raw.out"
HTML="$OUT_DIR/coverage.html"
SUMMARY="$OUT_DIR/summary.txt"

# Packages excluded from the measured denominator. Generated RPC code is not
# hand-written, backendtest is a test helper, and webui is covered by the web
# suite rather than by Go tests.
EXCLUDE_RE='^inferencerig/(core/rpc/gen|backends/backendtest|webui)(/|$)'

mkdir -p "$OUT_DIR"

# The scored package set. This is both what gets instrumented (-coverpkg) and
# the denominator, so a package cannot be silently dropped from one and not the
# other.
scored=$(go list ./... | grep -Ev "$EXCLUDE_RE" | sort)
if [ -z "$scored" ]; then
	echo "go-coverage: no packages to measure" >&2
	exit 1
fi
coverpkg=$(echo "$scored" | paste -sd, -)

echo "go-coverage: measuring $(echo "$scored" | wc -l | tr -d ' ') packages"
go test -covermode=atomic -coverpkg="$coverpkg" -coverprofile="$RAW" ./... >/dev/null

# Merge: every test binary emits the full instrumented block set, so the same
# block appears once per binary. A block is covered when any binary executed it.
# Blocks belonging to an excluded package are dropped here too, because
# -coverpkg only bounds instrumentation, not what the profile reports.
awk -v exclude="$EXCLUDE_RE" '
	NR == 1 && $0 ~ /^mode:/ { next }
	{
		# <file>:<start>,<end> <numstmt> <count>
		block = $1
		file = block
		sub(/:.*/, "", file)
		if (file ~ exclude) next
		key = block " " $2
		if (!(key in stmts)) { stmts[key] = $2; order[++n] = key }
		if ($3 > 0) covered[key] = 1
	}
	END {
		print "mode: atomic"
		for (i = 1; i <= n; i++) {
			key = order[i]
			split(key, parts, " ")
			print parts[1], parts[2], (key in covered) ? 1 : 0
		}
	}
' "$RAW" >"$PROFILE"

go tool cover -html="$PROFILE" -o "$HTML"

# Per-package and total percentages, computed from the merged profile so the
# numbers on screen are the numbers the gate uses.
awk -v min="$MIN" '
	NR == 1 { next }
	{
		file = $1
		sub(/:.*/, "", file)
		pkg = file
		sub(/\/[^\/]*$/, "", pkg)
		total[pkg] += $2
		grand_total += $2
		if ($3 > 0) { hit[pkg] += $2; grand_hit += $2 }
		if (!(pkg in seen)) { seen[pkg] = 1; order[++n] = pkg }
	}
	END {
		if (grand_total == 0) { print "go-coverage: profile is empty" > "/dev/stderr"; exit 1 }
		# Sort package names for a stable, diffable report.
		for (i = 1; i <= n; i++)
			for (j = i + 1; j <= n; j++)
				if (order[j] < order[i]) { t = order[i]; order[i] = order[j]; order[j] = t }
		for (i = 1; i <= n; i++) {
			pkg = order[i]
			printf "%6.1f%%  %5d/%-5d  %s\n", 100 * hit[pkg] / total[pkg], hit[pkg], total[pkg], pkg
		}
		pct = 100 * grand_hit / grand_total
		printf "\ntotal: %.1f%% (%d/%d statements), minimum %s%%\n", pct, grand_hit, grand_total, min
		exit (pct < min) ? 2 : 0
	}
' "$PROFILE" | tee "$SUMMARY"

status=${PIPESTATUS[0]}
echo "go-coverage: wrote $PROFILE and $HTML"
if [ "$status" -ne 0 ]; then
	echo "go-coverage: below the $MIN% minimum" >&2
	exit 1
fi
