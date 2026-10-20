BIN := bin/redis-from-scratch
# Extra go test flags, e.g. make test TESTFLAGS=-v (CI uses it to prove no
# test was skipped).
TESTFLAGS ?=

DIAGRAMS := $(wildcard docs/diagrams/*.dot)
DIAGRAM_PNGS := $(patsubst docs/diagrams/%.dot,docs/diagrams/out/%.png,$(DIAGRAMS))

.PHONY: build test test-short lint bench clean diagrams rtm docker demo

build:
	go build -o $(BIN) ./cmd/redis-from-scratch

# test builds the binary first so compile errors surface before the slower
# integration run; the integration tests build their own copy through
# testutil.BuildOnce, so they never depend on bin/.
test: build
	go test -race -count=1 $(TESTFLAGS) ./...

# test-short skips the slow tests (anything that sleeps for seconds).
test-short: build
	go test -race -count=1 -short $(TESTFLAGS) ./...

lint:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi
	go vet ./...

bench:
	@echo "see bench/"

diagrams: $(DIAGRAM_PNGS)

# Graphviz warnings go to stderr; the rule fails if dot printed any, so a
# broken diagram cannot be committed silently.
docs/diagrams/out/%.png: docs/diagrams/%.dot
	@mkdir -p $(dir $@)
	@dot -Tpng -Gdpi=150 $< -o $@ 2> $@.warn; status=$$?; \
	if [ $$status -ne 0 ] || [ -s $@.warn ]; then cat $@.warn; rm -f $@.warn; exit 1; fi; \
	rm -f $@.warn; echo "rendered $@"

# Regenerate the requirements traceability matrix from the GitHub issues.
# Needs gh authenticated.
rtm:
	go run ./scripts/tasks/rtm > docs/RTM.md.tmp && mv docs/RTM.md.tmp docs/RTM.md

# Build the server image (deploy/Dockerfile, context is the repo root).
docker:
	docker build -f deploy/Dockerfile -t redforge:latest .

# Start the demo topology in the background; then
#   docker compose -f deploy/docker-compose.yml run --rm cli PING
demo:
	docker compose -f deploy/docker-compose.yml up -d --build

clean:
	rm -rf bin/
