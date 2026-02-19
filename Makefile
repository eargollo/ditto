# Unit tests (packages without the integration build tag). Uses tx+rollback so tests can run in parallel. Start Postgres: docker compose -f docker-compose.dev.yml up -d
.PHONY: test
test:
	go test ./... -count=1

# Integration tests only (require -tags=integration; real DB, -p 1 -parallel 1 to avoid contention). Start Postgres first.
.PHONY: integration
integration:
	go test -tags=integration -p 1 -parallel 1 -count=1 ./internal/integration

# CI: run unit tests then integration tests (both need Postgres for DB tests).
.PHONY: test-ci
test-ci: test integration

# Run the app against the dev DB (ditto). Start Postgres first: docker compose -f docker-compose.dev.yml up -d
.PHONY: run
run:
	DATABASE_URL=postgres://ditto:ditto@localhost:5432/ditto?sslmode=disable go run ./cmd/ditto

# Reset the dev database (ditto): truncate all tables. Requires: docker compose -f docker-compose.dev.yml up -d
# Rails-style: clears data so you can start fresh; schema stays (migrations run on next make run).
.PHONY: db-reset
db-reset:
	docker exec ditto-postgres-dev psql -U ditto -d ditto -c "TRUNCATE duplicate_groups_hash, file_scan, files, scans, folders RESTART IDENTITY CASCADE;"

.PHONY: build
build:
	go build -o ditto ./cmd/ditto

# Cross-compile for Windows (64-bit). Output: ditto.exe
.PHONY: build-windows
build-windows:
	GOOS=windows GOARCH=amd64 go build -o ditto.exe ./cmd/ditto

# Static analysis: gosec (same as .github/workflows/security.yml).
.PHONY: gosec
gosec:
	go run github.com/securego/gosec/v2/cmd/gosec@latest -exclude=G706 ./...

# Security: govulncheck for known vulnerabilities (same as .github/workflows/security.yml).
.PHONY: govulncheck
govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Run all static and security checks (gosec + govulncheck).
.PHONY: lint
lint: gosec govulncheck
