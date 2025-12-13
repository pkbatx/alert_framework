package config

import (
    "log"
    "os"
    "strconv"
    "time"

    "github.com/joho/godotenv"
)

// Config holds all environment-driven settings.
type Config struct {
    CallsDir       string
    WorkDir        string
    DBPath         string
    HTTPPort       string
    GroupMeBotID   string
    GroupMeURL     string
    DashboardDir   string
    EnableDanger   bool
    Environment    string
    WorkerCount    int
    QueueSize      int
    EnableWatcher  bool
    TranscribeModel string
    OpsBaseURL     string
}

// Load reads configuration from environment and optional .env file.
func Load() Config {
    _ = godotenv.Load()

    cfg := Config{
        CallsDir:       getenv("CALLS_DIR", "./calls"),
        WorkDir:        getenv("WORK_DIR", "./work"),
        DBPath:         getenv("DB_PATH", "./alert.db"),
        HTTPPort:       getenv("PORT", "8080"),
        GroupMeBotID:   getenv("GROUPME_BOT_ID", ""),
        GroupMeURL:     getenv("GROUPME_URL", "https://api.groupme.com/v3/bots/post"),
        DashboardDir:   getenv("DASHBOARD_DIR", "./static"),
        EnableDanger:   getenvBool("ENABLE_DANGEROUS_OPS", false),
        Environment:    getenv("ENVIRONMENT", "local"),
        WorkerCount:    clampInt(getenvInt("WORKER_COUNT", 4), 1, 64),
        QueueSize:      clampInt(getenvInt("QUEUE_SIZE", 128), 8, 1024),
        EnableWatcher:  getenvBool("ENABLE_WATCHER", true),
        TranscribeModel: getenv("TRANSCRIPTION_MODEL", "gpt-4o-transcribe"),
        OpsBaseURL:     getenv("OPS_BASE_URL", "http://localhost:8080"),
    }

    log.Printf("config: calls_dir=%s work_dir=%s db=%s env=%s", cfg.CallsDir, cfg.WorkDir, cfg.DBPath, cfg.Environment)
    return cfg
}

func getenv(key, def string) string {
    v := os.Getenv(key)
    if v == "" {
        return def
    }
    return v
}

func getenvInt(key string, def int) int {
    v := os.Getenv(key)
    if v == "" {
        return def
    }
    n, err := strconv.Atoi(v)
    if err != nil {
        return def
    }
    return n
}

func getenvBool(key string, def bool) bool {
    v := os.Getenv(key)
    if v == "" {
        return def
    }
    b, err := strconv.ParseBool(v)
    if err != nil {
        return def
    }
    return b
}

func clampInt(v, min, max int) int {
    if v < min {
        return min
    }
    if v > max {
        return max
    }
    return v
}

// Now returns utc time helper for deterministic timestamps.
func Now() time.Time {
    return time.Now().UTC().Truncate(time.Second)
}
