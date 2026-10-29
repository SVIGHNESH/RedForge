#!/usr/bin/env bash
# bench/run.sh --target rfs|redis (--scenario N ... | --all) [options]
#
# Runs evaluation scenarios from bench/scenarios.conf against our server (rfs)
# or official Redis with matched settings (MASTER-PLAN MS8 P8.1).
#
# Every sub-case gets a warm-up and then three timed runs. Each of those is a
# separate server start in a fresh, empty directory, which is how the keyspace
# and the log are reset between runs (MASTER-PLAN Section 2 item 3). Per run:
#
#   1. start the server pinned to the server CPU and wait for PING
#   2. run the sub-case's preload, if any, and check it landed
#   3. start bench/sample.sh on the server PID
#   4. run redis-benchmark pinned to the client CPUs, one process per stream
#   5. stop the sampler, stop the server, delete the directory
#
# Output goes to bench/results/<date>/<target>/:
#
#   s<N>-<subcase>[-<stream>]-<run>.csv  redis-benchmark's CSV columns plus p99.9
#   s<N>-<subcase>[-<stream>]-<run>.txt  redis-benchmark's full output
#   s<N>-<subcase>-<run>.sample.csv      bench/sample.sh output for the server
#   s<N>-<subcase>-<run>.peaks           bench/peaks.sh of that sample
#   s<N>-<subcase>-<run>.server.log      server stdout and stderr
#   cmdline.txt                          versions and every exact command line
#   runs.csv                             one row per stream per run, naming
#                                        the files above
#
# <run> is "warmup", then "run1" to "run3". The report tool ignores the
# warm-up, and reads runs.csv instead of parsing file names, because names
# like s3-mix-get-run1 cannot be split into sub-case and stream reliably. A
# rerun appends new rows; the last row for a scenario, sub-case, stream and
# run is the current one.
#
# Why the CSV is written here rather than taken from --csv: redis-benchmark's
# --csv output has no p99.9, and its percentile distribution, which only the
# full output prints, is the only place p99.9 comes from. So every run is taken
# without --csv. The CSV is rebuilt from the full output's Summary block, which
# holds the same values --csv prints, and gets two extra columns: the latency
# of the first percentile step at or above 99.9, and which step that was
# (redis-benchmark halves the distance to 100 each step, so it is 99.902
# rather than exactly 99.9).
#
# redis-benchmark's latency histogram tops out at 3 s: a slower request is
# recorded as 3000.3 ms. A run whose max reached that ceiling gets
# latency_capped=1, meaning its latency columns are lower bounds and only its
# throughput is a true measurement.
set -euo pipefail

usage() {
	cat >&2 <<'EOF'
usage: bench/run.sh --target rfs|redis (--scenario N ... | --all) [options]

  --target rfs|redis   server to benchmark
  --scenario N         run every sub-case of scenario N (repeatable)
  --subcase NAME       only this sub-case of the chosen scenarios
  --all                every scenario in the config
  --runs K             timed runs per sub-case after the warm-up (default 3)
  --out DIR            results directory (default bench/results/<date>)
  --port P             port for the server under test (default 6390)
  --workdir DIR        where per-run server directories are made; must not be
                       tmpfs (default bench/results/.work)
  --bin PATH           our server binary (default bin/redis-from-scratch)
  --config FILE        scenario file (default bench/scenarios.conf)
  --server-cpu LIST    taskset CPU list for the server (default 2)
  --client-cpus LIST   taskset CPU list for redis-benchmark (default 0,1)
EOF
	exit 2
}

die() {
	echo "run.sh: $*" >&2
	exit 1
}

bench_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_dir=$(cd "$bench_dir/.." && pwd)

orig_args=("$@")
target=""
scenarios=()
subcase_filter=""
all=0
runs=3
out=""
port=6390
workdir="$repo_dir/bench/results/.work"
bin="$repo_dir/bin/redis-from-scratch"
config="$bench_dir/scenarios.conf"
server_cpu=2
client_cpus=0,1

while [ $# -gt 0 ]; do
	case $1 in
	--target) target=${2:-}; shift 2 ;;
	--scenario) scenarios+=("${2:-}"); shift 2 ;;
	--subcase) subcase_filter=${2:-}; shift 2 ;;
	--all) all=1; shift ;;
	--runs) runs=${2:-}; shift 2 ;;
	--out) out=${2:-}; shift 2 ;;
	--port) port=${2:-}; shift 2 ;;
	--workdir) workdir=${2:-}; shift 2 ;;
	--bin) bin=${2:-}; shift 2 ;;
	--config) config=${2:-}; shift 2 ;;
	--server-cpu) server_cpu=${2:-}; shift 2 ;;
	--client-cpus) client_cpus=${2:-}; shift 2 ;;
	-h | --help) usage ;;
	*) echo "run.sh: unknown argument: $1" >&2; usage ;;
	esac
done

case $target in
rfs | redis) ;;
*) usage ;;
esac
if [ ${#scenarios[@]} -eq 0 ] && [ "$all" -eq 0 ]; then
	usage
fi
[[ $runs =~ ^[1-9][0-9]*$ ]] || die "--runs must be a positive integer"
[[ $port =~ ^[0-9]+$ ]] || die "--port must be a number"
[ -r "$config" ] || die "cannot read $config"
[ -z "$out" ] && out="$repo_dir/bench/results/$(date +%F)"

# ---------- preflight ----------

install_hint="install official Redis 7.x, for example 'apt install redis-server redis-tools' on Debian or Ubuntu, or build it from https://download.redis.io/releases/ (Arch's 'redis' package is now Valkey)"
for tool in redis-benchmark redis-cli; do
	command -v "$tool" >/dev/null 2>&1 || die "$tool not found; $install_hint"
done
if [ "$target" = redis ]; then
	command -v redis-server >/dev/null 2>&1 || die "redis-server not found; $install_hint"
	if ! redis-server --version | grep -q '^Redis server'; then
		echo "run.sh: warning: redis-server is not official Redis: $(redis-server --version)" >&2
		echo "run.sh: warning: fine for trying the harness, not for the evaluation" >&2
	fi
else
	[ -x "$bin" ] || die "$bin not found or not executable; run 'make build'"
fi
command -v taskset >/dev/null 2>&1 || die "taskset not found; install util-linux"
taskset -c "$server_cpu" true 2>/dev/null || die "cannot pin to CPU list '$server_cpu'"
taskset -c "$client_cpus" true 2>/dev/null || die "cannot pin to CPU list '$client_cpus'"

if redis-cli -p "$port" PING >/dev/null 2>&1; then
	die "something already answers PING on port $port; stop it or pass --port"
fi

mkdir -p "$workdir"
workdir=$(cd "$workdir" && pwd)
work_fs=$(stat -f -c %T "$workdir")
# fsync is a no-op on tmpfs, which would erase the cost Scenario 5 measures.
if [ "$work_fs" = tmpfs ] || [ "$work_fs" = ramfs ]; then
	die "--workdir $workdir is on $work_fs, where fsync does nothing; use a directory on a real disk"
fi

# ---------- config ----------

# Each sub-case is identified by "scenario|subcase"; its lines are kept in
# config order so the streams of one sub-case stay together.
declare -a case_keys=()
declare -A case_policy=() case_lines=()
lineno=0
while IFS= read -r line || [ -n "$line" ]; do
	lineno=$((lineno + 1))
	[[ $line =~ ^[[:space:]]*(#|$) ]] && continue
	IFS='|' read -r sc sub pol stream preload args <<<"$line"
	[[ $sc =~ ^[0-9]+$ ]] || die "$config:$lineno: bad scenario '$sc'"
	[[ $sub =~ ^[A-Za-z0-9_-]+$ ]] || die "$config:$lineno: bad subcase '$sub'"
	[[ $stream =~ ^[A-Za-z0-9_-]*$ ]] || die "$config:$lineno: bad stream '$stream'"
	case $pol in always | everysec | no) ;; *) die "$config:$lineno: bad policy '$pol'" ;; esac
	[ -n "$args" ] || die "$config:$lineno: no benchmark arguments"
	key="$sc|$sub"
	if [ -z "${case_policy[$key]+x}" ]; then
		case_keys+=("$key")
		case_policy[$key]=$pol
		case_lines[$key]=""
	elif [ "${case_policy[$key]}" != "$pol" ]; then
		die "$config:$lineno: streams of $key disagree on policy"
	fi
	case_lines[$key]+="$stream|$preload|$args"$'\n'
done <"$config"

selected=()
for key in "${case_keys[@]}"; do
	sc=${key%%|*}
	sub=${key#*|}
	if [ ${#scenarios[@]} -gt 0 ]; then
		match=0
		for want in "${scenarios[@]}"; do
			[ "$want" = "$sc" ] && match=1
		done
		[ "$match" -eq 1 ] || continue
	fi
	if [ -n "$subcase_filter" ] && [ "$subcase_filter" != "$sub" ]; then
		continue
	fi
	selected+=("$key")
done
[ ${#selected[@]} -gt 0 ] || die "no sub-case in $config matches the selection"

# ---------- cleanup ----------

server_pid=""
sampler_pid=""
bench_pids=()
run_dir=""

stop_pid() {
	local pid=$1 i
	kill -TERM "$pid" 2>/dev/null || return 0
	for ((i = 0; i < 100; i++)); do
		kill -0 "$pid" 2>/dev/null || break
		sleep 0.1
	done
	kill -KILL "$pid" 2>/dev/null || true
	wait "$pid" 2>/dev/null || true
}

cleanup() {
	local pid
	for pid in "${bench_pids[@]}"; do
		stop_pid "$pid"
	done
	bench_pids=()
	[ -n "$sampler_pid" ] && stop_pid "$sampler_pid"
	sampler_pid=""
	[ -n "$server_pid" ] && stop_pid "$server_pid"
	server_pid=""
	[ -n "$run_dir" ] && rm -rf "$run_dir"
	run_dir=""
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# ---------- helpers ----------

# to_csv RAW CSV converts one full redis-benchmark output into a CSV with
# --csv's columns plus p99.9. It fails if any field is missing, so a crashed
# or truncated run cannot produce a CSV that looks complete.
to_csv() {
	tr '\r' '\n' <"$1" | awk '
		/^====== .* ======$/ { sub(/^====== /, ""); sub(/ ======$/, ""); test = $0; tests++ }
		/^Latency by percentile distribution:/ { inpct = 1; next }
		inpct && /^[0-9.]+% <= / {
			pct = $1; sub(/%$/, "", pct)
			if (p999 == "" && pct + 0 >= 99.9) { p999 = $3; p999pct = pct }
			next
		}
		inpct { inpct = 0 }
		/throughput summary:/ { rps = $3 }
		/latency summary \(msec\):/ { getline; getline; avg = $1; min = $2; p50 = $3; p95 = $4; p99 = $5; max = $6 }
		END {
			if (tests != 1) { print "to_csv: expected one test block, found " tests > "/dev/stderr"; exit 1 }
			if (rps == "" || max == "" || p999 == "") { print "to_csv: summary or percentile distribution missing" > "/dev/stderr"; exit 1 }
			capped = (max + 0 >= 3000) ? 1 : 0
			print "\"test\",\"rps\",\"avg_latency_ms\",\"min_latency_ms\",\"p50_latency_ms\",\"p95_latency_ms\",\"p99_latency_ms\",\"max_latency_ms\",\"p99_9_latency_ms\",\"p99_9_percentile\",\"latency_capped\""
			printf "\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%d\"\n", test, rps, avg, min, p50, p95, p99, max, p999, p999pct, capped
		}
	' >"$2.tmp"
	mv "$2.tmp" "$2"
}

# check_stderr FILE fails if redis-benchmark wrote anything to stderr besides
# the CONFIG GET warning our server is known to cause (MASTER-PLAN Section 2
# item 6).
check_stderr() {
	local extra
	extra=$(grep -v -x 'WARNING: Could not fetch server CONFIG' "$1" || true)
	if [ -n "$extra" ]; then
		echo "run.sh: redis-benchmark wrote to stderr:" >&2
		echo "$extra" >&2
		return 1
	fi
}

# server_cmd DIR POLICY sets the array cmd to the exact server command line.
server_cmd() {
	if [ "$target" = rfs ]; then
		cmd=(taskset -c "$server_cpu" "$bin" --port "$port" --bind 127.0.0.1 --dir "$1" --appendfsync "$2")
	else
		cmd=(taskset -c "$server_cpu" redis-server --port "$port" --dir "$1" --save "" --appendonly yes --appendfsync "$2" --bind 127.0.0.1 --io-threads 1)
	fi
}

# quote prints its arguments as one command line that can be pasted into a
# shell, quoting only the arguments that need it.
quote() {
	local out="" a
	for a in "$@"; do
		if [[ $a =~ ^[A-Za-z0-9_./,=:+-]+$ ]]; then
			out+="$a "
		else
			out+="'${a//\'/\'\\\'\'}' "
		fi
	done
	echo "${out% }"
}

# run_one KEY RUN performs one server start and benchmark of sub-case KEY.
run_one() {
	local key=$1 run=$2
	local sc=${key%%|*} sub=${key#*|}
	local pol=${case_policy[$key]}
	local base="s$sc-$sub"
	local stream preload args i
	local -a cmd streams preloads argss

	while IFS='|' read -r stream preload args; do
		[ -n "$args" ] || continue
		streams+=("$stream")
		preloads+=("$preload")
		argss+=("$args")
	done <<<"${case_lines[$key]}"

	run_dir=$(mktemp -d -p "$workdir" "rfs-bench.XXXXXX")
	server_cmd "$run_dir" "$pol"
	echo "$base-$run server: $(quote "${cmd[@]}")" >>"$cmdline"
	"${cmd[@]}" >"$target_dir/$base-$run.server.log" 2>&1 &
	server_pid=$!

	for ((i = 0; i < 100; i++)); do
		kill -0 "$server_pid" 2>/dev/null || die "server exited during startup; see $target_dir/$base-$run.server.log"
		redis-cli -p "$port" PING >/dev/null 2>&1 && break
		sleep 0.1
	done
	[ "$i" -lt 100 ] || die "server did not answer PING within 10 s"
	# taskset execs the server, so the PID we hold is the server itself and
	# the sampler measures it rather than a wrapper.
	[ "$(cat "/proc/$server_pid/comm")" != taskset ] || die "PID $server_pid is still taskset"

	# A preload is run once per sub-case even when several streams list it.
	local done_preloads="" pre
	for pre in "${preloads[@]}"; do
		if [ "$pre" = - ] || [ -z "$pre" ]; then
			continue
		fi
		case $done_preloads in *"|$pre|"*) continue ;; esac
		done_preloads+="|$pre|"
		read -r -a pre_args <<<"$pre"
		echo "$base-$run preload: $(quote redis-benchmark -p "$port" "${pre_args[@]}" -q)" >>"$cmdline"
		redis-benchmark -p "$port" "${pre_args[@]}" -q >/dev/null 2>"$run_dir/preload.err" ||
			die "preload failed: $(cat "$run_dir/preload.err")"
		check_stderr "$run_dir/preload.err"
		verify_preload "${pre_args[@]}"
	done

	"$bench_dir/sample.sh" "$server_pid" "$target_dir/$base-$run.sample.csv" &
	sampler_pid=$!

	local name prefix
	local -a outs=() bench_cmd
	bench_pids=()
	for i in "${!streams[@]}"; do
		name=$base
		[ -n "${streams[$i]}" ] && name="$base-${streams[$i]}"
		prefix="$target_dir/$name-$run"
		read -r -a bench_cmd <<<"${argss[$i]}"
		bench_cmd=(taskset -c "$client_cpus" redis-benchmark -p "$port" "${bench_cmd[@]}")
		echo "$name-$run benchmark: $(quote "${bench_cmd[@]}")" >>"$cmdline"
		"${bench_cmd[@]}" >"$prefix.txt" 2>"$prefix.err" &
		bench_pids+=($!)
		outs+=("$prefix")
	done

	local failed=0 pid
	for i in "${!bench_pids[@]}"; do
		pid=${bench_pids[$i]}
		if ! wait "$pid"; then
			echo "run.sh: redis-benchmark failed for ${outs[$i]}.txt" >&2
			failed=1
		fi
	done
	bench_pids=()

	stop_pid "$sampler_pid"
	sampler_pid=""
	kill -0 "$server_pid" 2>/dev/null || die "server died during the run; see $target_dir/$base-$run.server.log"
	stop_pid "$server_pid"
	server_pid=""
	rm -rf "$run_dir"
	run_dir=""

	[ "$failed" -eq 0 ] || die "benchmark failed in $base-$run"
	# A run shorter than the 100 ms sample interval has no samples. That is
	# recorded rather than fatal, so the report tool shows the gap.
	if ! "$bench_dir/peaks.sh" "$target_dir/$base-$run.sample.csv" >"$target_dir/$base-$run.peaks" 2>/dev/null; then
		echo "no_samples=1" >"$target_dir/$base-$run.peaks"
		echo "run.sh: warning: $base-$run was too short for the sampler; no CPU or memory peaks" >&2
	fi
	for i in "${!outs[@]}"; do
		prefix=${outs[$i]}
		check_stderr "$prefix.err" || die "unexpected stderr in $prefix.err"
		rm -f "$prefix.err"
		to_csv "$prefix.txt" "$prefix.csv" || die "could not parse $prefix.txt"
		if grep -q ',"1"$' "$prefix.csv"; then
			echo "run.sh: warning: ${prefix##*/} hit redis-benchmark's 3 s latency ceiling; only its throughput is exact" >&2
		fi
		echo "$sc,$sub,${streams[$i]},$run,$pol,${prefix##*/}.csv,$base-$run.sample.csv,$base-$run.peaks" >>"$manifest"
	done
}

# verify_preload ARGS checks that a '-t set -d N' preload stored an N-byte
# value at the key redis-benchmark uses without -r. GET is used because
# STRLEN is not in the command matrix.
verify_preload() {
	local size="" prev=""
	for a in "$@"; do
		[ "$prev" = -d ] && size=$a
		prev=$a
	done
	[ -n "$size" ] || return 0
	local got
	got=$(redis-cli -p "$port" GET 'key:__rand_int__' | tr -d '\n' | wc -c)
	[ "$got" -eq "$size" ] || die "preload stored $got bytes, expected $size"
}

# ---------- main ----------

target_dir="$out/$target"
mkdir -p "$target_dir"
target_dir=$(cd "$target_dir" && pwd)
cmdline="$target_dir/cmdline.txt"
manifest="$target_dir/runs.csv"
if [ ! -s "$manifest" ]; then
	echo "scenario,subcase,stream,run,policy,csv,sample,peaks" >"$manifest"
fi

{
	echo "## run.sh invocation at $(date -Is)"
	echo "invocation: $(quote bench/run.sh "${orig_args[@]}")"
	echo "commit: $(git -C "$repo_dir" rev-parse HEAD 2>/dev/null || echo unknown)"
	if [ -n "$(git -C "$repo_dir" status --porcelain --untracked-files=no 2>/dev/null)" ]; then
		echo "worktree: dirty (tracked files modified)"
	else
		echo "worktree: clean"
	fi
	if [ "$target" = rfs ]; then
		echo "server: $("$bin" --version 2>&1)"
	else
		echo "server: $(redis-server --version)"
	fi
	echo "redis-benchmark: $(redis-benchmark --version)"
	echo "workdir: $workdir ($work_fs)"
	echo "pinning: server CPU $server_cpu, client CPUs $client_cpus"
	go_env=$(env | grep -E '^(GOGC|GODEBUG|GOMAXPROCS|GOMEMLIMIT)=' | tr '\n' ' ' || true)
	echo "go runtime env: ${go_env:-none}"
} >>"$cmdline"

for key in "${selected[@]}"; do
	label="s${key%%|*}-${key#*|}"
	for run in warmup $(seq -f 'run%g' 1 "$runs"); do
		start=$(date +%s)
		run_one "$key" "$run"
		echo "run.sh: $target $label $run done in $(($(date +%s) - start)) s" >&2
	done
done
echo >>"$cmdline"
echo "run.sh: results in $target_dir" >&2
