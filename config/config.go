package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// Config holds service configuration derived from environment variables.
type Config struct {
	HTTPPort      string
	CallsDir      string
	BackfillLimit int
	JobQueueSize  int
	WorkerCount   int
	JobTimeoutSec int
	GroupMeBotID  string
	GroupMeToken  string
	DevUI         bool
}

const (
	defaultPort          = ":8000"
	defaultCallsDir      = "/home/peebs/calls"
	defaultBackfillLimit = 15
	maxBackfillLimit     = 50
	minQueueSize         = 1
	defaultQueueSize     = 100
	maxQueueSize         = 1024
	defaultWorkerCount   = 4
	defaultJobTimeoutSec = 60
)

// Load reads configuration from environment variables and applies sane defaults.
func Load() (Config, error) {
	cfg := Config{
		HTTPPort:      getEnv("HTTP_PORT", defaultPort),
		CallsDir:      getEnv("CALLS_DIR", defaultCallsDir),
		BackfillLimit: defaultBackfillLimit,
		JobQueueSize:  defaultQueueSize,
		WorkerCount:   defaultWorkerCount,
		JobTimeoutSec: defaultJobTimeoutSec,
		GroupMeBotID:  os.Getenv("GROUPME_BOT_ID"),
		GroupMeToken:  os.Getenv("GROUPME_ACCESS_TOKEN"),
		DevUI:         parseBoolEnv("DEV_UI"),
	}

	if legacyPort := os.Getenv("PORT"); legacyPort != "" && cfg.HTTPPort == defaultPort {
		cfg.HTTPPort = legacyPort
	}
	if !strings.HasPrefix(cfg.HTTPPort, ":") {
		cfg.HTTPPort = ":" + cfg.HTTPPort
	}

	if v := os.Getenv("BACKFILL_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("invalid BACKFILL_LIMIT=%q, clamping to %d", v, maxBackfillLimit)
			n = maxBackfillLimit
		}
		if n < 0 {
			log.Printf("BACKFILL_LIMIT must be non-negative, using default %d", defaultBackfillLimit)
			n = defaultBackfillLimit
		}
		if n > maxBackfillLimit {
			log.Printf("BACKFILL_LIMIT capped at %d (was %d)", maxBackfillLimit, n)
			n = maxBackfillLimit
		}
		cfg.BackfillLimit = n
	}

	if v := os.Getenv("WORKER_COUNT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("invalid WORKER_COUNT=%q, using default %d", v, defaultWorkerCount)
			n = defaultWorkerCount
		}
		if n <= 0 {
			log.Printf("WORKER_COUNT must be positive, using default %d", defaultWorkerCount)
			n = defaultWorkerCount
		}
		cfg.WorkerCount = n
	}

	if v := os.Getenv("JOB_QUEUE_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("invalid JOB_QUEUE_SIZE=%q, using default %d", v, defaultQueueSize)
			n = defaultQueueSize
		}
		if n < minQueueSize {
			log.Printf("JOB_QUEUE_SIZE raised to minimum %d (was %d)", minQueueSize, n)
			n = minQueueSize
		}
		if n > maxQueueSize {
			log.Printf("JOB_QUEUE_SIZE capped at %d (was %d)", maxQueueSize, n)
			n = maxQueueSize
		}
		cfg.JobQueueSize = n
	}

	if cfg.JobQueueSize < cfg.WorkerCount {
		log.Printf("JOB_QUEUE_SIZE must be >= WORKER_COUNT; using default %d", defaultQueueSize)
		cfg.JobQueueSize = max(defaultQueueSize, cfg.WorkerCount)
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseBoolEnv(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "1" || v == "true" || v == "yes" || v == "on" {
		return true
	}
	return false
}
