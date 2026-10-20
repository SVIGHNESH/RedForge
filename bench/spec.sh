#!/usr/bin/env bash
# bench/spec.sh > bench/results/<date>/spec.txt
#
# Prints the machine facts every result directory must carry (Proposal
# Section 8, MASTER-PLAN MS8 P8.3): CPU model and core count, RAM, OS and
# kernel, Go version, Redis version, the commit under test, the time, and the
# CPU frequency governor.
#
# A missing tool is reported as "not found" rather than aborting, so the file
# always has every section and a gap is visible instead of silent.
# Warnings go to stderr so they reach the operator, not the spec file.
set -uo pipefail

section() {
	printf '\n## %s\n' "$1"
}

# run prints a command's output, or "<name>: not found" if it is missing.
run() {
	if command -v "$1" >/dev/null 2>&1; then
		"$@" 2>&1
	else
		echo "$1: not found"
	fi
}

governor_file=/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor
if [ -r "$governor_file" ]; then
	governor=$(<"$governor_file")
else
	governor="unavailable"
fi

echo "# RedForge benchmark machine specification"

section "CPU"
run lscpu | grep -E 'Model name|^CPU\(s\)|Thread|MHz'

section "Memory"
run free -h

section "OS"
head -2 /etc/os-release

section "Kernel"
run uname -r

section "Go"
run go version

section "Redis"
run redis-server --version
run redis-benchmark --version

section "Commit"
run git rev-parse HEAD

section "Captured at"
date -Is

section "CPU governor"
echo "$governor"

if [ "$governor" != "performance" ]; then
	echo "spec.sh: warning: CPU governor is '$governor', not 'performance'; frequency scaling adds spread to results" >&2
fi
