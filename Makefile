# Run tests. Use -p 1 to avoid Postgres deadlocks (all tests share one DB).
# Start Postgres first: docker compose -f docker-compose.dev.yml up -d
.PHONY: test
test:
	go test -p 1 ./...

.PHONY: build
build:
	go build -o ditto ./cmd/ditto
