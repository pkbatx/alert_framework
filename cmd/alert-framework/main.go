package main

import (
    "context"
    "log"
    "os/signal"
    "syscall"

    "alert_framework/internal/app"
    "alert_framework/internal/config"
)

func main() {
    cfg := config.Load()
    application, err := app.New(cfg)
    if err != nil {
        log.Fatalf("init: %v", err)
    }
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
    defer stop()
    if err := application.Run(ctx); err != nil {
        log.Fatalf("run: %v", err)
    }
}
