BIN := bin/redis-from-scratch

.PHONY: build test lint bench clean

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

clean:
	rm -rf bin/
