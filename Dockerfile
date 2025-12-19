# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder
WORKDIR /app

RUN apk add --no-cache ca-certificates ffmpeg

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=0.0.0-dev
ARG GIT_SHA=dev
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 go build -o /out/alert_server -ldflags "-s -w -X alert_framework/version.Version=${VERSION} -X alert_framework/version.GitSHA=${GIT_SHA} -X alert_framework/version.BuildTime=${BUILD_TIME}" .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates ffmpeg

RUN addgroup -S app && adduser -S -G app app \\
    && mkdir -p /data/calls /data/work \\
    && chown -R app:app /data

WORKDIR /app
COPY --from=builder /out/alert_server /app/alert_server
COPY config /app/config

ENV CALLS_DIR=/data/calls \
    WORK_DIR=/data/work \
    DB_PATH=/data/work/transcriptions.db \
    HTTP_PORT=:8000

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:8000/healthz || exit 1

USER app

ENTRYPOINT ["/app/alert_server"]
