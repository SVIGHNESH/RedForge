# T8.03 Machine specification capture

**Item:** M10 | **Milestone:** MS8 | **Week:** 7
**Suggested owner:** Ritk | **Reviewer:** Rajat
**Effort:** 30 minutes
**Depends on:** nothing
**Unblocks:** MS10

## Goal

Every result directory carries the machine facts the synopsis requires: CPU model and core count, RAM, OS and kernel, Go version, Redis version.

## Steps

1. `bench/spec.sh > bench/results/<date>/spec.txt` collecting: `lscpu | grep -E 'Model name|^CPU\(s\)|Thread|MHz'`, `free -h`, `cat /etc/os-release | head -2`, `uname -r`, `go version`, `redis-server --version`, `redis-benchmark --version`, `git rev-parse HEAD`, `date -Is`, and the CPU governor from `/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor`.
2. Print a warning if the governor is not `performance`, since frequency scaling adds spread.
3. `run.sh` calls it once per results directory.

## Acceptance

- [ ] `spec.txt` contains every item above.
