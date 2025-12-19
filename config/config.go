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
	Rollup             RollupConfig
}

type fileConfig struct {
	CallsDir string           `json:"calls_dir" yaml:"calls_dir"`
	HTTPPort string           `json:"http_port" yaml:"http_port"`
	WorkDir  string           `json:"work_dir" yaml:"work_dir"`
	DBPath   string           `json:"db_path" yaml:"db_path"`
	NLP      NLPConfig        `json:"nlp" yaml:"nlp"`
	Rollup   rollupFileConfig `json:"rollup" yaml:"rollup"`
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

// RollupConfig captures rollup grouping and LLM summarization settings.
type RollupConfig struct {
	LookbackHours      int
	ChainWindowMin     int
	RadiusMeters       float64
	MaxCalls           int
	RefreshIntervalSec int
	LLMEnabled         bool
	PromptVersion      string
	LLMModel           string
	LLMBaseURL         string
}

type rollupFileConfig struct {
	LookbackHours      *int     `json:"lookback_hours" yaml:"lookback_hours"`
	ChainWindowMin     *int     `json:"chain_window_min" yaml:"chain_window_min"`
	RadiusMeters       *float64 `json:"radius_meters" yaml:"radius_meters"`
	MaxCalls           *int     `json:"max_calls" yaml:"max_calls"`
	RefreshIntervalSec *int     `json:"refresh_interval_sec" yaml:"refresh_interval_sec"`
	LLMEnabled         *bool    `json:"llm_enabled" yaml:"llm_enabled"`
	PromptVersion      string   `json:"prompt_version" yaml:"prompt_version"`
	LLMModel           string   `json:"llm_model" yaml:"llm_model"`
	LLMBaseURL         string   `json:"llm_base_url" yaml:"llm_base_url"`
}

func defaultRollupConfig() RollupConfig {
	return RollupConfig{
		LookbackHours:      6,
		ChainWindowMin:     30,
		RadiusMeters:       800,
		MaxCalls:           50,
		RefreshIntervalSec: 60,
		LLMEnabled:         true,
		PromptVersion:      "v1",
		LLMModel:           "gpt-4o-mini",
		LLMBaseURL:         "https://api.openai.com",
	}
}

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

	cfg.Rollup = applyRollupOverrides(defaultRollupConfig(), fileCfg.Rollup)

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

	if v, ok, err := parseIntEnv("ROLLUP_LOOKBACK_HOURS"); err != nil {
		if cfg.StrictConfig {
			return cfg, fmt.Errorf("invalid ROLLUP_LOOKBACK_HOURS: %w", err)
		}
		log.Printf("invalid ROLLUP_LOOKBACK_HOURS: %v (using default)", err)
	} else if ok && v > 0 {
		cfg.Rollup.LookbackHours = v
	}
	if v, ok, err := parseIntEnv("ROLLUP_CHAIN_WINDOW_MIN"); err != nil {
		if cfg.StrictConfig {
			return cfg, fmt.Errorf("invalid ROLLUP_CHAIN_WINDOW_MIN: %w", err)
		}
		log.Printf("invalid ROLLUP_CHAIN_WINDOW_MIN: %v (using default)", err)
	} else if ok && v > 0 {
		cfg.Rollup.ChainWindowMin = v
	}
	if v, ok, err := parseFloatEnv("ROLLUP_RADIUS_METERS"); err != nil {
		if cfg.StrictConfig {
			return cfg, fmt.Errorf("invalid ROLLUP_RADIUS_METERS: %w", err)
		}
		log.Printf("invalid ROLLUP_RADIUS_METERS: %v (using default)", err)
	} else if ok && v > 0 {
		cfg.Rollup.RadiusMeters = v
	}
	if v, ok, err := parseIntEnv("ROLLUP_MAX_CALLS"); err != nil {
		if cfg.StrictConfig {
			return cfg, fmt.Errorf("invalid ROLLUP_MAX_CALLS: %w", err)
		}
		log.Printf("invalid ROLLUP_MAX_CALLS: %v (using default)", err)
	} else if ok && v > 0 {
		cfg.Rollup.MaxCalls = v
	}
	if v, ok, err := parseIntEnv("ROLLUP_REFRESH_INTERVAL_SEC"); err != nil {
		if cfg.StrictConfig {
			return cfg, fmt.Errorf("invalid ROLLUP_REFRESH_INTERVAL_SEC: %w", err)
		}
		log.Printf("invalid ROLLUP_REFRESH_INTERVAL_SEC: %v (using default)", err)
	} else if ok && v > 0 {
		cfg.Rollup.RefreshIntervalSec = v
	}
	if v := os.Getenv("ROLLUP_LLM_ENABLED"); strings.TrimSpace(v) != "" {
		cfg.Rollup.LLMEnabled = parseBoolEnv("ROLLUP_LLM_ENABLED")
	}
	if v := strings.TrimSpace(os.Getenv("ROLLUP_PROMPT_VERSION")); v != "" {
		cfg.Rollup.PromptVersion = v
	}
	if v := strings.TrimSpace(os.Getenv("ROLLUP_LLM_MODEL")); v != "" {
		cfg.Rollup.LLMModel = v
	}
	cfg.Rollup.LLMBaseURL = firstNonEmpty(
		os.Getenv("ROLLUP_LLM_BASE_URL"),
		os.Getenv("OPENAI_BASE_URL"),
		os.Getenv("OPENAI_API_BASE"),
		cfg.Rollup.LLMBaseURL,
	)

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
	if cfg.Rollup.LookbackHours <= 0 {
		return errors.New("rollup lookback hours must be positive")
	}
	if cfg.Rollup.ChainWindowMin <= 0 {
		return errors.New("rollup chain window minutes must be positive")
	}
	if cfg.Rollup.RadiusMeters <= 0 {
		return errors.New("rollup radius meters must be positive")
	}
	if cfg.Rollup.MaxCalls <= 0 {
		return errors.New("rollup max calls must be positive")
	}
	if cfg.Rollup.RefreshIntervalSec <= 0 {
		return errors.New("rollup refresh interval must be positive")
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

func applyRollupOverrides(base RollupConfig, override rollupFileConfig) RollupConfig {
	if override.LookbackHours != nil && *override.LookbackHours > 0 {
		base.LookbackHours = *override.LookbackHours
	}
	if override.ChainWindowMin != nil && *override.ChainWindowMin > 0 {
		base.ChainWindowMin = *override.ChainWindowMin
	}
	if override.RadiusMeters != nil && *override.RadiusMeters > 0 {
		base.RadiusMeters = *override.RadiusMeters
	}
	if override.MaxCalls != nil && *override.MaxCalls > 0 {
		base.MaxCalls = *override.MaxCalls
	}
	if override.RefreshIntervalSec != nil && *override.RefreshIntervalSec > 0 {
		base.RefreshIntervalSec = *override.RefreshIntervalSec
	}
	if override.LLMEnabled != nil {
		base.LLMEnabled = *override.LLMEnabled
	}
	if strings.TrimSpace(override.PromptVersion) != "" {
		base.PromptVersion = strings.TrimSpace(override.PromptVersion)
	}
	if strings.TrimSpace(override.LLMModel) != "" {
		base.LLMModel = strings.TrimSpace(override.LLMModel)
	}
	if strings.TrimSpace(override.LLMBaseURL) != "" {
		base.LLMBaseURL = strings.TrimSpace(override.LLMBaseURL)
	}
	return base
}

func parseIntEnv(key string) (int, bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false, nil
	}
	val, err := strconv.Atoi(raw)
	return val, true, err
}

func parseFloatEnv(key string) (float64, bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false, nil
	}
	val, err := strconv.ParseFloat(raw, 64)
	return val, true, err
}
