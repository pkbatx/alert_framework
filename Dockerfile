# syntax=docker/dockerfile:1

# Stage 1 — Builder
FROM golang:1.24 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /app/bin/alert_framework .

# Stage 2 — Runtime
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        ffmpeg \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /data/calls /data/work /data/last24 /data/tmp /alert_framework_data/work

COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

COPY --from=builder /app/bin/alert_framework /app/alert_framework

ENV CALLS_DIR=/data/calls \
    WORK_DIR=/data/work \
    DB_PATH=/data/work/transcriptions.db \
    HTTP_PORT=:8000

EXPOSE 8000

ENTRYPOINT ["/entrypoint.sh"]
