# Run tests. Use -p 1 to avoid Postgres deadlocks (all tests share one DB).
# Start Postgres first: docker compose -f docker-compose.dev.yml up -d
.PHONY: test
test:
	go test -p 1 ./...

# CI test (race detector, no cache). Requires DATABASE_URL (e.g. docker compose -f docker-compose.dev.yml up -d).
.PHONY: test-ci
test-ci:
	go test -v -race -count=1 -p 1 ./...

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
