# RedForge server image (T11.01, MASTER-PLAN MS11).
#
# Build from the repository root:
#   docker build -f deploy/Dockerfile -t redforge:latest .
#
# The builder matches go.mod (go 1.22) and CI (setup-go 1.22), so the image is
# built with the same toolchain the tests run on. The binary is static
# (CGO_ENABLED=0) and uses only the standard library, so the final stage is
# scratch: no shell, no libc, nothing but the server.

FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags '-s -w' \
        -o /out/redis-from-scratch ./cmd/redis-from-scratch
# The data directory is created here, owned by the unprivileged user, because
# scratch has no mkdir. Docker copies it, ownership included, into a new named
# volume on first mount, so the server can write there without running as root.
RUN mkdir -p /out/data && chown 65534:65534 /out/data

FROM scratch
COPY --from=build /out/redis-from-scratch /redis-from-scratch
COPY --from=build --chown=65534:65534 /out/data /data
USER 65534:65534
EXPOSE 6380
VOLUME /data
ENTRYPOINT ["/redis-from-scratch", "--dir", "/data", "--bind", "0.0.0.0"]
