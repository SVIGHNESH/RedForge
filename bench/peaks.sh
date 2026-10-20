#!/usr/bin/env bash
# bench/peaks.sh OUTFILE
#
# Prints the peaks of a bench/sample.sh output file:
#
#   peak_cpu_pct=<highest cpu_pct sample>
#   peak_rss_kb=<VmHWM from the last sample>
#
# VmHWM is the kernel's own high-water mark of resident memory, so the last
# line already holds the peak even if the peak fell between two samples.
set -euo pipefail

if [ $# -ne 1 ]; then
	echo "usage: $0 OUTFILE" >&2
	exit 2
fi

awk -F, '
	NR == 1 && $1 == "timestamp_ms" { next }
	{ n++; if ($2 + 0 > peak) peak = $2 + 0; hwm = $4 }
	END {
		if (n == 0) { print "peaks.sh: no samples" > "/dev/stderr"; exit 1 }
		printf "peak_cpu_pct=%.1f\npeak_rss_kb=%d\n", peak, hwm
	}
' "$1"
