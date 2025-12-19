package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds service configuration derived from environment variables.
type Config struct {
	HTTPPort           string
	CallsDir           string
	JobQueueSize       int
	WorkerCount        int
	JobTimeoutSec      int
	GroupMeBotID       string
	GroupMeToken       string
	WorkDir            string
	DBPath             string
	DevUI              bool
	MapboxToken        string
	PublicBaseURL      string
	AudioFilterEnabled bool
	FFMPEGBin          string
	NLP                NLPConfig
	NLPConfigPath      string
	StrictConfig       bool
	InDocker           bool
}

type fileConfig struct {
	CallsDir string    `json:"calls_dir" yaml:"calls_dir"`
	HTTPPort string    `json:"http_port" yaml:"http_port"`
	WorkDir  string    `json:"work_dir" yaml:"work_dir"`
	DBPath   string    `json:"db_path" yaml:"db_path"`
	NLP      NLPConfig `json:"nlp" yaml:"nlp"`
}

const (
	defaultPort          = ":8000"
	defaultCallsDir      = "runtime/calls"
	defaultWorkDir       = "runtime/work"
	defaultDBFile        = "transcriptions.db"
	minQueueSize         = 1
	defaultQueueSize     = 100
	maxQueueSize         = 1024
	defaultWorkerCount   = 4
	defaultJobTimeoutSec = 60
)

// Load reads configuration from environment variables and applies sane defaults.
func Load() (Config, error) {
	cfg := Config{
		JobQueueSize:       defaultQueueSize,
		WorkerCount:        defaultWorkerCount,
		JobTimeoutSec:      defaultJobTimeoutSec,
		GroupMeBotID:       os.Getenv("GROUPME_BOT_ID"),
		GroupMeToken:       os.Getenv("GROUPME_ACCESS_TOKEN"),
		DevUI:              parseBoolEnv("DEV_UI"),
		MapboxToken:        os.Getenv("MAPBOX_TOKEN"),
		PublicBaseURL:      strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"),
		AudioFilterEnabled: parseBoolEnvDefault("AUDIO_FILTER_ENABLED", true),
		FFMPEGBin:          getEnv("FFMPEG_BIN", "ffmpeg"),
		StrictConfig:       parseBoolEnv("STRICT_CONFIG"),
		InDocker:           parseBoolEnv("IN_DOCKER"),
	}

	configPath := getEnv("CONFIG_PATH", filepath.Join("config", "config.yaml"))
	nlpPath := getEnv("NLP_CONFIG_PATH", configPath)
	cfg.NLPConfigPath = nlpPath

	fileCfg, fileErr := loadFileConfig(configPath)
	if fileErr != nil {
		if cfg.StrictConfig {
			return cfg, fmt.Errorf("config load failed (%s): %w", configPath, fileErr)
		}
		log.Printf("config load failed (%s): %v (using defaults)", configPath, fileErr)
	}

	cfg.CallsDir = firstNonEmpty(os.Getenv("CALLS_DIR"), fileCfg.CallsDir, defaultCallsDir)
	cfg.WorkDir = firstNonEmpty(os.Getenv("WORK_DIR"), fileCfg.WorkDir, defaultWorkDir)
	if dbPath := os.Getenv("DB_PATH"); dbPath != "" {
		cfg.DBPath = dbPath
	} else if fileCfg.DBPath != "" {
		cfg.DBPath = fileCfg.DBPath
	} else {
		cfg.DBPath = filepath.Join(cfg.WorkDir, defaultDBFile)
	}

	cfg.HTTPPort = firstNonEmpty(os.Getenv("HTTP_PORT"), fileCfg.HTTPPort, defaultPort)
	if legacyPort := os.Getenv("PORT"); legacyPort != "" && cfg.HTTPPort == defaultPort {
		cfg.HTTPPort = legacyPort
	}
	if !strings.HasPrefix(cfg.HTTPPort, ":") {
		cfg.HTTPPort = ":" + cfg.HTTPPort
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

	nlpCfg, err := LoadNLPConfig(nlpPath)
	if err != nil {
		if cfg.StrictConfig {
			return cfg, fmt.Errorf("nlp config load failed (%s): %w", nlpPath, err)
		}
		log.Printf("nlp config load failed (%s): %v (using defaults)", nlpPath, err)
		nlpCfg = DefaultNLPConfig()
	}
	cfg.NLP = nlpCfg

	if err := validateConfig(cfg); err != nil {
		if cfg.StrictConfig {
			return cfg, err
		}
		log.Printf("config validation failed: %v (continuing)", err)
	}

	return cfg, nil
}

func loadFileConfig(path string) (fileConfig, error) {
	var cfg fileConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if len(data) == 0 {
		return cfg, errors.New("empty config file")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
	case ".yaml", ".yml", "":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
	default:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.CallsDir) == "" {
		return errors.New("CALLS_DIR is required")
	}
	if strings.TrimSpace(cfg.HTTPPort) == "" {
		return errors.New("HTTP_PORT is required")
	}
	if len(cfg.NLP.MapboxBoundingBox) != 0 && len(cfg.NLP.MapboxBoundingBox) != 4 {
		return fmt.Errorf("nlp.mapbox_bounding_box must have 4 floats (got %d)", len(cfg.NLP.MapboxBoundingBox))
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, val := range values {
		if strings.TrimSpace(val) != "" {
			return val
		}
	}
	return ""
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

func parseBoolEnvDefault(key string, defaultVal bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	return parseBoolEnv(key)
}
