# T3.05 INFO

**Item:** M11, M7 (reporting) | **Milestone:** MS3 | **Week:** 7
**Suggested owner:** Vighnesh | **Reviewer:** Rajat
**Effort:** 2 hours
**Depends on:** T0.06, T0.08
**Unblocks:** every test that polls INFO, T8.02

## Goal

`INFO` and `INFO <section>` return Redis-format sections so `redis-cli INFO` renders and tests can parse `key:value` lines.

## Context you need

Field list frozen in MASTER-PLAN Section 4.10.
Fields whose subsystem has not merged yet are still emitted with zero values, so tests can be written against a stable schema.

## Steps

1. Register `INFO` (-1, ReadOnly). Build the reply as a bulk string with `# Section` headers, `key:value` lines, `\r\n` line endings, and a blank line between sections.
2. Sections and fields: Server (`redis_version:0.1.0-rfs`, `uptime_in_seconds`, `tcp_port`, `process_id`, `goroutines`), Clients (`connected_clients`), Stats (`total_connections_received`, `total_commands_processed`, `expired_keys`, `keyspace_hits`, `keyspace_misses`, `keys_last_iter_us`, `keys_last_reply_bytes`), Persistence (`aof_fsync_policy`, `aof_current_seq`, `aof_last_truncation_offset`, `aof_truncated_records`, `bgsave_in_progress`, `last_bgsave_status`, `last_bgsave_seq`, `last_bgsave_clone_ms`), Replication (`role:master`, `connected_replicas:0`), Keyspace (`db0:keys=N,expires=M`).
3. Keyspace hits and misses are incremented in `GET` only, as Redis does for lookups that return or do not return a value. Keep it simple: `GET`, `HGET`, `LRANGE` count as lookups; other commands do not.
4. Providers: each subsystem exposes a `func() map[string]string` registered into the server so INFO does not import `aof` or `snapshot`.
5. Test: parse output, assert every field above is present.

## Acceptance

- [ ] `redis-cli INFO` prints readable sections.
- [ ] `redis-cli --stat` runs (it reads `keys`, `used_memory` may be absent, that is fine).
- [ ] `testutil.Info` parses every field.
