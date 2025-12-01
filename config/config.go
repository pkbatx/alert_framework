package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
)

// Config holds service configuration derived from environment variables.
type Config struct {
	HTTPPort      string
	BackfillLimit int
	JobQueueSize  int
	WorkerCount   int
	JobTimeoutSec int
}

const (
	defaultPort          = "8000"
	defaultBackfillLimit = 15
	maxBackfillLimit     = 50
	defaultQueueSize     = 100
	defaultWorkerCount   = 4
	defaultJobTimeoutSec = 60
)

// Load reads configuration from environment variables and applies sane defaults.
func Load() (Config, error) {
	cfg := Config{
		HTTPPort:      getEnv("PORT", defaultPort),
		BackfillLimit: defaultBackfillLimit,
		JobQueueSize:  defaultQueueSize,
		WorkerCount:   defaultWorkerCount,
		JobTimeoutSec: defaultJobTimeoutSec,
	}

	if v := os.Getenv("BACKFILL_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid BACKFILL_LIMIT: %w", err)
		}
		if n < 0 {
			return cfg, fmt.Errorf("BACKFILL_LIMIT must be non-negative")
		}
		if n > maxBackfillLimit {
			log.Printf("BACKFILL_LIMIT capped at %d (was %d)", maxBackfillLimit, n)
			n = maxBackfillLimit
		}
		cfg.BackfillLimit = n
	}

	if v := os.Getenv("JOB_QUEUE_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid JOB_QUEUE_SIZE: %w", err)
		}
		if n <= 0 {
			return cfg, fmt.Errorf("JOB_QUEUE_SIZE must be positive")
		}
		cfg.JobQueueSize = n
	}

	if v := os.Getenv("WORKER_COUNT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid WORKER_COUNT: %w", err)
		}
		if n <= 0 {
			return cfg, fmt.Errorf("WORKER_COUNT must be positive")
		}
		cfg.WorkerCount = n
	}

	if v := os.Getenv("JOB_TIMEOUT_SEC"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid JOB_TIMEOUT_SEC: %w", err)
		}
		if n <= 0 {
			return cfg, fmt.Errorf("JOB_TIMEOUT_SEC must be positive")
		}
		cfg.JobTimeoutSec = n
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
