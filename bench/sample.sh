#!/usr/bin/env bash
# bench/sample.sh PID OUTFILE
#
# Samples a process's CPU use and memory from /proc every 100 ms and appends
# one CSV line per sample:
#
#   timestamp_ms,cpu_pct,rss_kb,hwm_kb
#
# cpu_pct is user plus system time over the interval, as a percentage of one
# core (fields 14 and 15 of /proc/PID/stat, in clock ticks). It is divided by
# the measured interval rather than a fixed 0.1 s, so a late wakeup does not
# inflate the figure. rss_kb and hwm_kb are VmRSS and VmHWM from
# /proc/PID/status. Stops when PID exits or on SIGTERM or SIGINT.
#
# Apart from sleep, everything is a bash builtin reading /proc, so the sampler
# itself costs almost nothing next to the process it measures.
set -euo pipefail

if [ $# -ne 2 ]; then
	echo "usage: $0 PID OUTFILE" >&2
	exit 2
fi
pid=$1
out=$2
interval_us=100000

if [ ! -r "/proc/$pid/stat" ]; then
	echo "sample.sh: no such process: $pid" >&2
	exit 1
fi

clk_tck=$(getconf CLK_TCK)
stop=0
trap 'stop=1' TERM INT

# The helpers below set globals instead of printing, so no subshell is forked
# per sample. That keeps the sampler cheap and its interval close to 100 ms.

# read_now sets now_us from the bash 5 builtin EPOCHREALTIME.
read_now() {
	now_us=${EPOCHREALTIME/./}
}

# read_ticks sets ticks to utime + stime. The comm field may contain spaces or
# parentheses, so everything up to the last ')' is dropped before splitting;
# after that, utime and stime are the 12th and 13th remaining fields.
read_ticks() {
	local line rest
	read -r line <"/proc/$pid/stat" || return 1
	rest=${line##*) }
	local -a f
	read -r -a f <<<"$rest"
	ticks=$((f[11] + f[12]))
}

# read_mem sets rss and hwm in kB from /proc/PID/status.
read_mem() {
	local key val _unit
	rss=0 hwm=0
	while read -r key val _unit; do
		case $key in
		VmRSS:) rss=$val ;;
		VmHWM:) hwm=$val ;;
		esac
	done <"/proc/$pid/status" || return 1
}

if [ ! -s "$out" ]; then
	echo "timestamp_ms,cpu_pct,rss_kb,hwm_kb" >>"$out"
fi

ticks=0 now_us=0 rss=0 hwm=0 secs=0
read_ticks 2>/dev/null || exit 0
read_now
prev_ticks=$ticks
prev_us=$now_us
next_us=$((now_us + interval_us))

while [ "$stop" -eq 0 ]; do
	# Sleep until the next 100 ms deadline rather than for a fixed 100 ms,
	# so time spent sampling does not accumulate as drift.
	read_now
	wait_us=$((next_us - now_us))
	if [ "$wait_us" -gt 0 ]; then
		printf -v secs '0.%06d' "$wait_us"
		sleep "$secs" || true
	fi
	next_us=$((next_us + interval_us))
	[ "$stop" -eq 0 ] || break
	read_ticks 2>/dev/null || break
	read_mem 2>/dev/null || break
	read_now

	elapsed_us=$((now_us - prev_us))
	[ "$elapsed_us" -gt 0 ] || continue
	# Percent with one decimal place, in integer arithmetic:
	# tenths = delta_ticks / clk_tck / elapsed_s * 100 * 10.
	tenths=$(((ticks - prev_ticks) * 1000000000 / (clk_tck * elapsed_us)))
	printf '%s,%d.%d,%s,%s\n' $((now_us / 1000)) $((tenths / 10)) $((tenths % 10)) "$rss" "$hwm" >>"$out"

	prev_ticks=$ticks
	prev_us=$now_us
done
