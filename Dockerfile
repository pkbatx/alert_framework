# syntax=docker/dockerfile:1

FROM golang:1.22 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/alert_framework ./...

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    && rm -rf /var/lib/apt/lists/*

ENV CALLS_DIR=/data/calls \
    WORK_DIR=/data/work \
    DB_PATH=/data/work/transcriptions.db \
    HTTP_PORT=:8000

WORKDIR /app
COPY --from=builder /app/bin/alert_framework /app/alert_framework
COPY docker/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh \
    && mkdir -p /data/calls /data/work

EXPOSE 8000
ENTRYPOINT ["/entrypoint.sh"]
CMD ["/app/alert_framework"]
