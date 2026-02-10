# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /ditto ./cmd/ditto

# Runtime stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates

# User ditto is created at runtime in entrypoint with PUID:PGID (default 1000:1000)
WORKDIR /app
COPY --from=builder /ditto /app/ditto
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

ENV DITTO_DATA_DIR=/data
ENV DITTO_PORT=8080

# Persist data and optional scan mounts at /data and /scan
VOLUME ["/data"]
EXPOSE 8080

# Start as root so we can chown /data; then run app as ditto
ENTRYPOINT ["/docker-entrypoint.sh"]
