# T11.01 Dockerfile and Docker Compose

**Item:** M11 | **Milestone:** MS11 | **Week:** 7 (first version), 13 (verified)
**Suggested owner:** Vivek | **Reviewer:** Rajat
**Effort:** 2 hours
**Depends on:** T0.01
**Unblocks:** T9.09, demo

## Steps

1. `deploy/Dockerfile`: multi-stage; builder `golang:1.22` with `CGO_ENABLED=0 go build -ldflags '-s -w'`; final stage `scratch` (or `gcr.io/distroless/static`) copying the binary; `EXPOSE 6380`; `VOLUME /data`; entrypoint `["/redis-from-scratch", "--dir", "/data", "--bind", "0.0.0.0"]`.
2. `deploy/docker-compose.yml`: service `server` with port `6380:6380` and a named volume; add a `cli` profile service running `redis:7` image's `redis-cli -h server -p 6380` for the demo.
3. `make docker` builds the image; `make demo` runs compose.
4. Verify: `docker compose up -d && docker compose run --rm cli PING`.

## Acceptance

- [ ] Image under 15 MB.
- [ ] Data survives `docker compose restart`.
