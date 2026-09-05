#!/usr/bin/env bash
# Benchmark runner for go-hyperliquid.
#
# Usage:
#   scripts/bench.sh run [output.txt]          Capture a benchmark report (default: bench.txt)
#   scripts/bench.sh compare <old.txt> <new.txt>  Statistical comparison via benchstat
#   scripts/bench.sh profile <BenchmarkName>   Capture CPU + memory profiles for one benchmark
#
# Workflow (see golang-benchmark skill methodology):
#   1. scripts/bench.sh run bench-before.txt   # baseline on unmodified code
#   2. ... apply ONE optimization ...
#   3. scripts/bench.sh run bench-after.txt
#   4. scripts/bench.sh compare bench-before.txt bench-after.txt
#   5. Paste the benchstat output into the commit body (perf(scope): ...)
#
# Notes:
#   - Run benchmarks on an idle machine; never run two benchmark runs concurrently
#     (shared CPU contaminates ns/op and defeats the statistics).
#   - bench/bench-before.txt etc. are gitignored via bench*.txt.
set -euo pipefail

cd "$(dirname "$0")/.."

PACKAGES=$(go list ./... | grep -v examples)
COUNT=${BENCH_COUNT:-10}
BENCHTIME=${BENCHTIME:-1s}

cmd_run() {
	local output="${1:-bench.txt}"
	echo "Running benchmarks (count=${COUNT}, benchtime=${BENCHTIME}) ..."
	# shellcheck disable=SC2086
	go test -run='^$' -bench=. -benchmem -count="${COUNT}" -benchtime="${BENCHTIME}" ${PACKAGES} | tee "${output}"
	echo ""
	echo "Report saved to ${output}"
	echo "Compare with: scripts/bench.sh compare <old.txt> ${output}"
}

cmd_compare() {
	local old="${1:?usage: scripts/bench.sh compare <old.txt> <new.txt>}"
	local new="${2:?usage: scripts/bench.sh compare <old.txt> <new.txt>}"
	if ! command -v benchstat >/dev/null; then
		echo "benchstat not found. Install it with:" >&2
		echo "  go install golang.org/x/perf/cmd/benchstat@latest" >&2
		exit 1
	fi
	benchstat "${old}" "${new}"
}

cmd_profile() {
	local bench="${1:?usage: scripts/bench.sh profile <BenchmarkName>}"
	local stem
	stem=$(echo "${bench}" | tr '[:upper:]' '[:lower:]')
	# shellcheck disable=SC2086
	go test -run='^$' -bench="^${bench}\$" -benchmem \
		-cpuprofile="${stem}.cpu.prof" -memprofile="${stem}.mem.prof" ${PACKAGES}
	echo "CPU profile:    go tool pprof ${stem}.cpu.prof"
	echo "Memory profile: go tool pprof -alloc_objects ${stem}.mem.prof"
}

case "${1:-run}" in
run)
	shift
	cmd_run "$@"
	;;
compare)
	shift
	cmd_compare "$@"
	;;
profile)
	shift
	cmd_profile "$@"
	;;
*)
	echo "usage: scripts/bench.sh [run [output.txt] | compare <old.txt> <new.txt> | profile <BenchmarkName>]" >&2
	exit 1
	;;
esac
