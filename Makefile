# Test DB URL for migrate targets (dev/test only). Override with MIGRATE_DATABASE_URL for a different DB.
# Production: app runs migrations on startup; do not use these targets in production.
MIGRATE_DATABASE_URL ?= postgres://ditto:ditto@localhost:5432/ditto_test?sslmode=disable
# Use migrate CLI from PATH (faster). One-time install: make install-migrate
MIGRATE_CLI ?= migrate
MIGRATE_PATH := -path ./internal/db/migrations -database "$(MIGRATE_DATABASE_URL)"

# Install migrate CLI into GOPATH/bin (one-time, for make migrate / migrate-up / migrate-down). Matches go.mod version.
.PHONY: install-migrate
install-migrate:
	go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1

# Run all pending migrations (up). Dev/test only; run before make test or make integration. Requires Postgres: docker compose -f docker-compose.dev.yml up -d
.PHONY: migrate
migrate:
	$(MIGRATE_CLI) $(MIGRATE_PATH) up

# Alias for migrate: run all migrations up.
.PHONY: migrate-up
migrate-up: migrate

# Run migrations down. Default one step; use STEPS=N for N steps (e.g. make migrate-down STEPS=2). Dev/test only.
.PHONY: migrate-down
migrate-down:
	$(MIGRATE_CLI) $(MIGRATE_PATH) down $(or $(STEPS),1)

# Unit tests (schema must exist: run make migrate once, or CI runs migrate first). Uses test DB (DITTO_TEST_DATABASE_URL). Start Postgres: docker compose -f docker-compose.dev.yml up -d
.PHONY: test
test:
	go test ./... -count=1

# Create/update schema on the test database (ditto_test). Run once before make test or make integration. Same as make migrate. Requires Postgres: docker compose -f docker-compose.dev.yml up -d
.PHONY: test-migrate
test-migrate: migrate

# Integration tests (-p 1 -parallel 1). Schema must exist; start Postgres first.
.PHONY: integration
integration:
	go test -tags=integration -p 1 -parallel 1 -count=1 ./internal/integration

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

# Version for -ldflags. Set VERSION= or use git describe (tag or commit).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/eargollo/ditto/internal/version.Version=$(VERSION)"

.PHONY: build
build:
	go build $(LDFLAGS) -o ditto ./cmd/ditto

# Cross-compile for Windows (64-bit). Output: ditto.exe
.PHONY: build-windows
build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o ditto.exe ./cmd/ditto

# Static analysis: gosec (same as .github/workflows/security.yml).
.PHONY: gosec
gosec:
	go run github.com/securego/gosec/v2/cmd/gosec@latest -exclude=G706 ./...

# Security: govulncheck for known vulnerabilities (same as .github/workflows/security.yml).
.PHONY: govulncheck
govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Find unused code (functions, methods, types, constants). Uses staticcheck U1000.
.PHONY: unused
unused:
	go run honnef.co/go/tools/cmd/staticcheck@latest -checks=U1000 ./...

# Run all static and security checks (gosec + govulncheck).
.PHONY: lint
lint: gosec govulncheck

# Build Tailwind CSS (Option A: clean dashboard). Run before build/run to pick up UI changes.
.PHONY: css
css:
	npx tailwindcss -i ./internal/server/static/input.css -o ./internal/server/static/app.css
