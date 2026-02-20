# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s -X github.com/eargollo/ditto/internal/version.Version=${VERSION}" -o /ditto ./cmd/ditto

# Runtime stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates

# User ditto is created at runtime in entrypoint with PUID:PGID (default 1000:1000)
WORKDIR /app
COPY --from=builder /ditto /app/ditto
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

ENV DITTO_PORT=8080

EXPOSE 8080

# Start as root so we can create run-as user; then run app as ditto
ENTRYPOINT ["/docker-entrypoint.sh"]
