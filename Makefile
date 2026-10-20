BIN := bin/redis-from-scratch

DIAGRAMS := $(wildcard docs/diagrams/*.dot)
DIAGRAM_PNGS := $(patsubst docs/diagrams/%.dot,docs/diagrams/out/%.png,$(DIAGRAMS))

.PHONY: build test lint bench clean diagrams

build:
	go build -o $(BIN) ./cmd/redis-from-scratch

test:
	go test ./...

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

clean:
	rm -rf bin/
