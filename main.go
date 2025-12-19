package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"alert_framework/backend/refine"
	"alert_framework/config"
	"alert_framework/formatting"
	"alert_framework/metrics"
	"alert_framework/queue"
	"alert_framework/rollups"
	"alert_framework/version"
	"github.com/fsnotify/fsnotify"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	_ "modernc.org/sqlite"
)

//go:embed static/*
var embeddedStatic embed.FS

const (
	groupmeURL = "https://api.groupme.com/v3/bots/post"
	defaultBot = "03926cdc985a046b27d6393ba6"

	defaultTranscriptionModel  = "gpt-4o-transcribe"
	defaultTranscriptionMode   = "transcribe"
	defaultTranscriptionFormat = "json"
)

var (
	allowedFormats = map[string][]string{
		"whisper-1":                 {"json", "text", "srt", "verbose_json", "vtt"},
		"gpt-4o-mini-transcribe":    {"json", "text"},
		"gpt-4o-transcribe":         {"json", "text"},
		"gpt-4o-transcribe-diarize": {"json", "text", "diarized_json"},
		"gpt4-transcribe":           {"json", "text"},
	}
	allowedExtensions = map[string]struct{}{
		".mp3": {}, ".mp4": {}, ".mpeg": {}, ".mpga": {}, ".m4a": {}, ".wav": {}, ".webm": {},
	}
	ffmpegBinary                 = "ffmpeg"
	audioFilterEnabled           = true
	sussexMinLat                 = 40.9
	sussexMaxLat                 = 41.4
	sussexMinLng                 = -75.2
	sussexMaxLng                 = -74.3
	sussexTowns                  = []string{"Andover", "Byram", "Frankford", "Franklin", "Green", "Hamburg", "Hardyston", "Hopatcong", "Lafayette", "Montague", "Newton", "Ogdensburg", "Sandyston", "Sparta", "Stanhope", "Stillwater", "Sussex", "Vernon", "Wantage", "Fredon", "Branchville"}
	warrenTowns                  = []string{"Allamuchy", "Alpha", "Belvidere", "Blairstown", "Franklin", "Frelinghuysen", "Greenwich", "Hackettstown", "Hardwick", "Harmony", "Hope", "Independence", "Knowlton", "Liberty", "Lopatcong", "Mansfield", "Oxford", "Phillipsburg", "Pohatcong", "Washington Boro", "Washington Township", "White"}
	defaultCleanupPrompt         = buildCleanupPrompt()
	defaultMetadataPrompt        = buildMetadataPrompt()
	countyByTown                 = buildCountyLookup()
	addressPattern               = regexp.MustCompile(`(?i)(\b\d{3,6}\s+[A-Za-z0-9'\.\s]+\s+(Street|St|Road|Rd|Avenue|Ave|Highway|Hwy|Route|Rt|Lane|Ln|Drive|Dr|Court|Ct|Place|Pl|Way|Pike|Circle|Cir))`)
	errMetadataInferenceDisabled = errors.New("metadata inference disabled")
)

func buildCleanupPrompt() string {
	townList := "Sussex County towns: " + strings.Join(sussexTowns, ", ") + "."
	warrenList := "Warren County towns: " + strings.Join(warrenTowns, ", ") + "."
	return strings.Join([]string{
		"You are cleaning emergency radio transcripts for Sussex and Warren County, NJ.",
		"Normalize spelling and fix misheard town or agency names to the closest match from the lists below.",
		townList,
		warrenList,
		"Return JSON with fields normalized_transcript and recognized_towns (array). Maintain the original meaning and avoid adding new details.",
	}, " ")
}

func buildMetadataPrompt() string {
	sussexFocus := "Prefer addresses within Sussex County, New Jersey (especially Andover Township and surrounding boroughs)."
	output := "Return JSON with address_line, municipality, county, cross_street, confidence (0-1 float), and notes explaining the decision."
	return strings.Join([]string{
		"You are an incident metadata specialist for Lakeland EMS.",
		"Given noisy dispatch transcripts and file metadata, identify the most probable street-level address referenced in the call.",
		sussexFocus,
		"Only consider Warren County if the transcript clearly references it; otherwise bias toward Sussex municipalities listed in the metadata.",
		"Favor precise house numbers when available; otherwise combine the best street and municipality combination.",
		output,
		"If you cannot locate an address, set address_line to an empty string, confidence to 0, and describe why in notes.",
	}, " ")
}

func buildCountyLookup() map[string]string {
	counties := make(map[string]string)
	for _, town := range sussexTowns {
		counties[strings.ToLower(town)] = "Sussex"
	}
	for _, town := range warrenTowns {
		counties[strings.ToLower(town)] = "Warren"
	}
	return counties
}

func isWithinSussexCounty(lat, lng float64) bool {
	if lat == 0 && lng == 0 {
		return false
	}
	return lat >= sussexMinLat && lat <= sussexMaxLat && lng >= sussexMinLng && lng <= sussexMaxLng
}

// transcription statuses
const (
	statusQueued     = "queued"
	statusProcessing = "processing"
	statusDone       = "done"
	statusError      = "error"
)

const (
	processingStaleAfter = 3 * time.Hour
)

type migration struct {
	version int
	name    string
	up      func(db *sql.DB) error
}

// DTOs

type transcription struct {
	ID                   int64      `json:"id"`
	Filename             string     `json:"filename"`
	SourcePath           string     `json:"source_path"`
	ProcessedPath        string     `json:"-"`
	Source               string     `json:"source"`
	Transcript           *string    `json:"transcript_text"`
	RawTranscript        *string    `json:"raw_transcript_text"`
	CleanTranscript      *string    `json:"clean_transcript_text"`
	Translation          *string    `json:"translation_text"`
	Status               string     `json:"status"`
	LastError            *string    `json:"last_error"`
	SizeBytes            *int64     `json:"size_bytes"`
	DurationSeconds      *float64   `json:"duration_seconds"`
	Hash                 *string    `json:"hash"`
	DuplicateOf          *string    `json:"duplicate_of"`
	RequestedModel       *string    `json:"requested_model"`
	RequestedMode        *string    `json:"requested_mode"`
	RequestedFormat      *string    `json:"requested_format"`
	ActualModel          *string    `json:"actual_openai_model_used"`
	DiarizedJSON         *string    `json:"diarized_json"`
	RecognizedTowns      *string    `json:"recognized_towns"`
	NormalizedTranscript *string    `json:"normalized_transcript"`
	CallType             *string    `json:"call_type"`
	CallTimestamp        *time.Time `json:"call_timestamp"`
	TagsJSON             *string    `json:"tags"`
	Latitude             *float64   `json:"latitude"`
	Longitude            *float64   `json:"longitude"`
	LocationLabel        *string    `json:"location_label"`
	LocationSource       *string    `json:"location_source"`
	RefinedMetadata      *string    `json:"refined_metadata"`
	AddressJSON          *string    `json:"address_json"`
	NeedsManualReview    bool       `json:"needs_manual_review"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	Similar              []similar  `json:"similar,omitempty"`
}

type similar struct {
	Filename string  `json:"filename"`
	Score    float64 `json:"score"`
}

type transcriptSegment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Speaker string  `json:"speaker,omitempty"`
}

type transcriptionResponse struct {
	ID                   int64               `json:"id"`
	Filename             string              `json:"filename"`
	SourcePath           string              `json:"source_path,omitempty"`
	Source               string              `json:"source"`
	Transcript           *string             `json:"transcript_text,omitempty"`
	RawTranscript        *string             `json:"raw_transcript_text,omitempty"`
	CleanTranscript      *string             `json:"clean_transcript_text,omitempty"`
	Translation          *string             `json:"translation_text,omitempty"`
	Status               string              `json:"status"`
	LastError            *string             `json:"last_error,omitempty"`
	SizeBytes            *int64              `json:"size_bytes,omitempty"`
	DurationSeconds      *float64            `json:"duration_seconds,omitempty"`
	Hash                 *string             `json:"hash,omitempty"`
	DuplicateOf          *string             `json:"duplicate_of,omitempty"`
	RequestedModel       *string             `json:"requested_model,omitempty"`
	RequestedMode        *string             `json:"requested_mode,omitempty"`
	RequestedFormat      *string             `json:"requested_format,omitempty"`
	ActualModel          *string             `json:"actual_model,omitempty"`
	DiarizedJSON         *string             `json:"diarized_json,omitempty"`
	RecognizedTowns      []string            `json:"recognized_towns,omitempty"`
	NormalizedTranscript *string             `json:"normalized_transcript,omitempty"`
	CallType             *string             `json:"call_type,omitempty"`
	CallTimestamp        time.Time           `json:"call_timestamp"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
	PrettyTitle          string              `json:"pretty_title,omitempty"`
	Town                 string              `json:"town,omitempty"`
	Agency               string              `json:"agency,omitempty"`
	AudioURL             string              `json:"audio_url,omitempty"`
	AudioPath            string              `json:"audio_path,omitempty"`
	AudioFilename        string              `json:"audio_filename,omitempty"`
	CallCategory         string              `json:"call_category,omitempty"`
	TimestampLocal       string              `json:"timestamp_local,omitempty"`
	AddressLine          string              `json:"address_line,omitempty"`
	CrossStreet          string              `json:"cross_street,omitempty"`
	CityOrTown           string              `json:"city_or_town,omitempty"`
	County               string              `json:"county,omitempty"`
	State                string              `json:"state,omitempty"`
	Summary              string              `json:"summary,omitempty"`
	CleanSummary         string              `json:"clean_summary,omitempty"`
	PrimaryAgency        string              `json:"primary_agency,omitempty"`
	NormalizedCallType   string              `json:"normalized_call_type,omitempty"`
	IncidentID           string              `json:"incident_id,omitempty"`
	PreviewImage         string              `json:"preview_image,omitempty"`
	Tags                 []string            `json:"tags,omitempty"`
	Segments             []transcriptSegment `json:"segments,omitempty"`
	Location             *locationGuess      `json:"location,omitempty"`
	RefinedMetadata      *string             `json:"refined_metadata,omitempty"`
	AddressJSON          *string             `json:"address_json,omitempty"`
	NeedsManualReview    bool                `json:"needs_manual_review"`
}

type locationGuess struct {
	Label     string  `json:"label,omitempty"`
	Latitude  float64 `json:"latitude,omitempty"`
	Longitude float64 `json:"longitude,omitempty"`
	Precision string  `json:"precision,omitempty"`
	Source    string  `json:"source,omitempty"`
}

type metadataInference struct {
	AddressLine  string  `json:"address_line"`
	Municipality string  `json:"municipality"`
	County       string  `json:"county"`
	CrossStreet  string  `json:"cross_street"`
	Confidence   float64 `json:"confidence"`
	Notes        string  `json:"notes"`
}

type tagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type callStats struct {
	Total           int            `json:"total"`
	StatusCounts    map[string]int `json:"status_counts"`
	CallTypeCounts  map[string]int `json:"call_type_counts"`
	TagCounts       map[string]int `json:"tag_counts"`
	AgencyCounts    map[string]int `json:"agency_counts"`
	TownCounts      map[string]int `json:"town_counts"`
	AvailableWindow string         `json:"window"`
}

type hourlyCount struct {
	Hour  string `json:"hour"`
	Count int    `json:"count"`
}

type lastSixHourStatsResponse struct {
	TotalIncidents   int                     `json:"total_incidents"`
	ByType           map[string]int          `json:"by_type"`
	ByAgency         map[string]int          `json:"by_agency"`
	ByStatus         map[string]int          `json:"by_status"`
	TopIncidentTypes []tagCount              `json:"top_incident_types"`
	TopAgencies      []tagCount              `json:"top_agencies"`
	IncidentsPerHour []hourlyCount           `json:"incidents_per_hour"`
	Calls            []transcriptionResponse `json:"calls"`
	MapboxToken      string                  `json:"mapbox_token,omitempty"`
	Window           string                  `json:"window"`
}

type callListResponse struct {
	Window      string                  `json:"window"`
	Calls       []transcriptionResponse `json:"calls"`
	Stats       callStats               `json:"stats"`
	MapboxToken string                  `json:"mapbox_token,omitempty"`
}

type hotspotSummary struct {
	Label     string     `json:"label"`
	Latitude  float64    `json:"latitude"`
	Longitude float64    `json:"longitude"`
	Count     int        `json:"count"`
	FirstSeen *time.Time `json:"first_seen,omitempty"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
}

type hotspotListResponse struct {
	Window   string           `json:"window"`
	Hotspots []hotspotSummary `json:"hotspots"`
}

type processJob struct {
	filename    string
	source      string
	sendGroupMe bool
	force       bool
	options     TranscriptionOptions
	meta        formatting.CallMetadata
	prettyTitle string
	publicURL   string
	baseURL     string
}

type TranscriptionOptions struct {
	Model         string
	Mode          string
	Format        string
	LanguageHint  string
	Prompt        string
	AutoTranslate bool
}

type AppSettings struct {
	DefaultModel      string
	DefaultMode       string
	DefaultFormat     string
	AutoTranslate     bool
	WebhookEndpoints  []string
	PreferredLanguage string
	CleanupPrompt     string
	MetadataPrompt    string
}

type server struct {
	db             *sql.DB
	queue          *queue.Queue
	metrics        *metrics.Metrics
	running        sync.Map // filename -> struct{}
	client         *http.Client
	botID          string
	shutdown       chan struct{}
	cfg            config.Config
	tz             *time.Location
	ctx            context.Context
	locationCache  sync.Map
	refiner        *refine.Service
	rollups        *rollups.Service
	rollupMu       sync.Mutex
	rollupEnqueued bool
}

// QueueDebugResponse represents the payload returned from /debug/queue.
type QueueDebugResponse struct {
	Length        int   `json:"length"`
	Capacity      int   `json:"capacity"`
	Workers       int   `json:"workers"`
	ProcessedJobs int64 `json:"processed_jobs"`
	FailedJobs    int64 `json:"failed_jobs"`
}

func (s *server) defaultOptions() (TranscriptionOptions, error) {
	settings, err := s.loadSettings()
	if err != nil {
		return TranscriptionOptions{Model: defaultTranscriptionModel, Mode: defaultTranscriptionMode, Format: defaultTranscriptionFormat}, err
	}
	return TranscriptionOptions{
		Model:         settings.DefaultModel,
		Mode:          settings.DefaultMode,
		Format:        settings.DefaultFormat,
		LanguageHint:  settings.PreferredLanguage,
		AutoTranslate: settings.AutoTranslate,
	}, nil
}

func prepareFilesystem(cfg config.Config) error {
	required := []string{
		cfg.CallsDir,
		cfg.WorkDir,
		filepath.Dir(cfg.DBPath),
	}
	if cfg.InDocker {
		required = append(required,
			"/data/last24",
			"/data/tmp",
			"/alert_framework_data/work",
		)
	}
	for _, dir := range required {
		dir = strings.TrimSpace(dir)
		if dir == "" || dir == "." || dir == "/" {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("ensure dir %s: %w", dir, err)
		}
	}
	return nil
}

func ensureWritableDir(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("empty path")
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(path, ".writetest-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Remove(name); err != nil {
		return err
	}
	return nil
}

func parseAlertMode(value string) string {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "", "all":
		return "all"
	case "api":
		return "api"
	case "worker":
		return "worker"
	default:
		log.Printf("unknown ALERT_MODE %q, defaulting to all", value)
		return "all"
	}
}

func adminEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("ENABLE_ADMIN_ACTIONS")), "true")
}

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !adminEnabled() {
		http.NotFound(w, r)
		return false
	}
	token := strings.TrimSpace(os.Getenv("ADMIN_TOKEN"))
	if token == "" {
		http.Error(w, "admin actions disabled", http.StatusForbidden)
		return false
	}
	if r.Header.Get("X-Admin-Token") != token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func main() {
	config.LoadDotEnv(".env")
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	mode := parseAlertMode(os.Getenv("ALERT_MODE"))
	enableHTTP := mode == "all" || mode == "api"
	enableWorker := mode == "all" || mode == "worker"
	switch mode {
	case "api":
		log.Printf("startup mode=api (HTTP enabled, worker disabled)")
	case "worker":
		log.Printf("startup mode=worker (worker enabled, HTTP disabled)")
	default:
		log.Printf("startup mode=all (HTTP enabled, worker enabled)")
	}

	audioFilterEnabled = cfg.AudioFilterEnabled
	ffmpegBinary = strings.TrimSpace(cfg.FFMPEGBin)
	if ffmpegBinary == "" {
		ffmpegBinary = "ffmpeg"
	}
	log.Printf("audio preprocessing using %s (enabled=%v)", ffmpegBinary, audioFilterEnabled)

	tz, err := time.LoadLocation("EST5EDT")
	if err != nil {
		log.Printf("falling back to local timezone: %v", err)
		tz = time.Local
	}

	if err := prepareFilesystem(cfg); err != nil {
		log.Fatalf("filesystem prep failed: %v", err)
	}
	if cfg.StrictConfig || cfg.InDocker {
		if err := ensureWritableDir(cfg.CallsDir); err != nil {
			log.Fatalf("CALLS_DIR not writable (%s): %v", cfg.CallsDir, err)
		}
		if err := ensureWritableDir(cfg.WorkDir); err != nil {
			log.Fatalf("WORK_DIR not writable (%s): %v", cfg.WorkDir, err)
		}
	}

	db, err := openDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}

	m := metrics.New()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	s := &server{
		db:       db,
		client:   &http.Client{Timeout: 180 * time.Second},
		botID:    getBotID(cfg),
		shutdown: make(chan struct{}),
		cfg:      cfg,
		metrics:  m,
		tz:       tz,
		ctx:      ctx,
	}

	var refiner *refine.Service
	if enableWorker {
		refiner, err = refine.NewService(s.client, cfg)
		if err != nil {
			log.Fatalf("refine init failed: %v", err)
		}
		s.refiner = refiner
		defer refiner.Close()
		s.rollups = rollups.NewService(db, s.client, cfg.Rollup)
	}

	if enableWorker {
		s.queue = queue.New(cfg.JobQueueSize, cfg.WorkerCount, time.Duration(cfg.JobTimeoutSec)*time.Second, m)
		s.queue.Start(ctx)
		qStats := s.queue.Stats()
		m.UpdateQueue(qStats.Length, qStats.Capacity, qStats.WorkerCount)
		go s.watch()
		if s.rollups != nil {
			s.startRollupScheduler(ctx)
		}
	}

	var httpServer *http.Server
	if enableHTTP {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/transcriptions", s.handleTranscriptions)
		mux.HandleFunc("/api/transcription/", s.handleTranscription)
		mux.HandleFunc("/api/transcription", s.handleTranscriptionIndex)
		mux.HandleFunc("/api/settings", s.handleSettings)
		mux.HandleFunc("/api/stats/last6h", s.handleLastSixHoursStats)
		mux.HandleFunc("/api/hotspots", s.handleHotspots)
		mux.HandleFunc("/api/rollups", s.handleRollups)
		mux.HandleFunc("/api/rollups/", s.handleRollupDetail)
		mux.HandleFunc("/api/rollups/recompute", s.handleRollupRecompute)
		mux.HandleFunc("/api/version", s.handleVersion)
		mux.HandleFunc("/preview/", s.handlePreview)
		mux.HandleFunc("/healthz", s.handleHealth)
		mux.HandleFunc("/readyz", s.handleReady)
		mux.HandleFunc("/debug/queue", s.handleDebugQueue)
		mux.HandleFunc("/", s.handleRoot)

		httpServer = &http.Server{
			Addr:    cfg.HTTPPort,
			Handler: mux,
		}
	}

	go func() {
		<-ctx.Done()
		close(s.shutdown)
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if s.queue != nil {
			s.queue.Stop(ctxTimeout)
		}
		if httpServer != nil {
			_ = httpServer.Shutdown(ctxTimeout)
		}
	}()

	if enableHTTP {
		log.Printf("server listening on %s", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
		return
	}

	<-ctx.Done()
}

func getBotID(cfg config.Config) string {
	if cfg.GroupMeBotID != "" {
		return cfg.GroupMeBotID
	}
	if v := os.Getenv("GROUPME_BOT_ID"); v != "" {
		return v
	}
	return defaultBot
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "database is locked") || strings.Contains(lower, "sqlite_busy")
}

func withRetry(op func() error) error {
	const maxAttempts = 8
	const backoff = 100 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := op(); err != nil {
			lastErr = err
			if isSQLiteBusy(err) && attempt < maxAttempts {
				time.Sleep(backoff)
				continue
			}
			return err
		}
		return nil
	}
	return lastErr
}

func execWithRetry(db *sql.DB, query string, args ...interface{}) (sql.Result, error) {
	var res sql.Result
	err := withRetry(func() error {
		var err error
		res, err = db.Exec(query, args...)
		return err
	})
	return res, err
}

func queryWithRetry(db *sql.DB, query string, args ...interface{}) (*sql.Rows, error) {
	var rows *sql.Rows
	err := withRetry(func() error {
		var err error
		rows, err = db.Query(query, args...)
		return err
	})
	return rows, err
}

func queryRowWithRetry(db *sql.DB, scan func(*sql.Row) error, query string, args ...interface{}) error {
	return withRetry(func() error {
		row := db.QueryRow(query, args...)
		return scan(row)
	})
}

func ensureDBFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("db path is empty")
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("ensure db dir: %w", err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			f, createErr := os.Create(path)
			if createErr != nil {
				return fmt.Errorf("create db file: %w", createErr)
			}
			return f.Close()
		}
		return fmt.Errorf("stat db file: %w", err)
	}
	return nil
}

func openDB(path string) (*sql.DB, error) {
	if err := ensureDBFile(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, pragma := range pragmas {
		if _, err := execWithRetry(db, pragma); err != nil {
			log.Printf("db pragma failed (%s): %v", strings.TrimSpace(pragma), err)
		}
	}

	if err := initDB(db); err != nil {
		return nil, err
	}
	return db, nil
}

func initDB(db *sql.DB) error {
	if err := ensureSchemaVersionTable(db); err != nil {
		return err
	}
	migrations := []migration{
		{version: 1, name: "baseline schema", up: migrateBaseline},
		{version: 2, name: "add ingest source", up: migrateAddIngestSource},
		{version: 3, name: "add call metadata columns", up: migrateAddCallMetadata},
		{version: 4, name: "add location columns", up: migrateAddLocationColumns},
		{version: 5, name: "add processed audio path", up: migrateAddProcessedPath},
		{version: 6, name: "normalize call timestamps to utc", up: migrateNormalizeCallTimestampUTC},
		{version: 7, name: "add rollup tables", up: migrateAddRollups},
	}
	return applyMigrations(db, migrations)
}

func ensureSchemaVersionTable(db *sql.DB) error {
	_, err := execWithRetry(db, `CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`)
	return err
}

func currentSchemaVersion(db *sql.DB) (int, error) {
	var v int
	if err := queryRowWithRetry(db, func(row *sql.Row) error {
		return row.Scan(&v)
	}, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`); err != nil {
		return 0, err
	}
	return v, nil
}

func applyMigrations(db *sql.DB, migrations []migration) error {
	current, err := currentSchemaVersion(db)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		log.Printf("applying migration %d: %s", m.version, m.name)
		if err := m.up(db); err != nil {
			return err
		}
		if _, err := execWithRetry(db, `INSERT OR REPLACE INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, m.version); err != nil {
			return err
		}
	}
	return nil
}

func migrateBaseline(db *sql.DB) error {
	schema := `CREATE TABLE IF NOT EXISTS transcriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    filename TEXT UNIQUE NOT NULL,
    source_path TEXT NOT NULL,
    processed_path TEXT NULL,
    ingest_source TEXT NULL,
    transcript_text TEXT NULL,
    raw_transcript_text TEXT NULL,
    clean_transcript_text TEXT NULL,
    translation_text TEXT NULL,
    status TEXT NOT NULL,
    last_error TEXT NULL,
    size_bytes INTEGER NULL,
    duration_seconds REAL NULL,
    hash TEXT NULL,
    duplicate_of TEXT NULL,
    embedding TEXT NULL,
    requested_model TEXT NULL,
    requested_mode TEXT NULL,
    requested_format TEXT NULL,
    actual_openai_model_used TEXT NULL,
    diarized_json TEXT NULL,
    recognized_towns TEXT NULL,
    normalized_transcript TEXT NULL,
    call_type TEXT NULL,
    refined_metadata TEXT NULL,
    address_json TEXT NULL,
    needs_manual_review INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TRIGGER IF NOT EXISTS transcriptions_updated_at
AFTER UPDATE ON transcriptions
BEGIN
    UPDATE transcriptions SET updated_at = CURRENT_TIMESTAMP WHERE id = old.id;
END;`
	if _, err := execWithRetry(db, schema); err != nil {
		return err
	}
	needed := map[string]string{
		"raw_transcript_text":      "TEXT",
		"clean_transcript_text":    "TEXT",
		"translation_text":         "TEXT",
		"size_bytes":               "INTEGER",
		"duration_seconds":         "REAL",
		"hash":                     "TEXT",
		"duplicate_of":             "TEXT",
		"embedding":                "TEXT",
		"requested_model":          "TEXT",
		"requested_mode":           "TEXT",
		"requested_format":         "TEXT",
		"actual_openai_model_used": "TEXT",
		"diarized_json":            "TEXT",
		"recognized_towns":         "TEXT",
		"normalized_transcript":    "TEXT",
		"call_type":                "TEXT",
		"latitude":                 "REAL",
		"longitude":                "REAL",
		"location_label":           "TEXT",
		"location_source":          "TEXT",
		"processed_path":           "TEXT",
		"refined_metadata":         "TEXT",
		"address_json":             "TEXT",
		"needs_manual_review":      "INTEGER DEFAULT 0",
	}
	for col, colType := range needed {
		if err := addColumnIfMissing(db, "transcriptions", col, colType); err != nil {
			return err
		}
	}
	if _, err := execWithRetry(db, `CREATE TABLE IF NOT EXISTS app_settings (
id INTEGER PRIMARY KEY CHECK (id = 1),
default_model TEXT,
    default_mode TEXT,
    default_format TEXT,
    auto_translate INTEGER DEFAULT 0,
    webhook_endpoints TEXT,
    preferred_language TEXT,
    cleanup_prompt TEXT,
    metadata_prompt TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`); err != nil {
		return err
	}
	if _, err := execWithRetry(db, `INSERT OR IGNORE INTO app_settings(id, default_model, default_mode, default_format, auto_translate, webhook_endpoints, cleanup_prompt, metadata_prompt) VALUES(1, 'gpt-4o-transcribe', 'transcribe', 'json', 0, '[]', '', '');`); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "app_settings", "cleanup_prompt", "TEXT"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "app_settings", "metadata_prompt", "TEXT"); err != nil {
		return err
	}
	if _, err := execWithRetry(db, `UPDATE app_settings SET cleanup_prompt = ? WHERE id = 1 AND (cleanup_prompt IS NULL OR cleanup_prompt = '')`, defaultCleanupPrompt); err != nil {
		return err
	}
	if _, err := execWithRetry(db, `UPDATE app_settings SET metadata_prompt = ? WHERE id = 1 AND (metadata_prompt IS NULL OR metadata_prompt = '')`, defaultMetadataPrompt); err != nil {
		return err
	}
	return nil
}

func migrateAddIngestSource(db *sql.DB) error {
	if _, err := execWithRetry(db, "ALTER TABLE transcriptions ADD COLUMN ingest_source TEXT"); err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "duplicate column name") {
			return nil
		}
		return err
	}
	return nil
}

func migrateAddCallMetadata(db *sql.DB) error {
	if err := addColumnIfMissing(db, "transcriptions", "call_timestamp", "DATETIME"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "transcriptions", "tags", "TEXT"); err != nil {
		return err
	}
	return nil
}

func migrateAddLocationColumns(db *sql.DB) error {
	columns := map[string]string{
		"latitude":        "REAL",
		"longitude":       "REAL",
		"location_label":  "TEXT",
		"location_source": "TEXT",
	}
	for col, colType := range columns {
		if err := addColumnIfMissing(db, "transcriptions", col, colType); err != nil {
			return err
		}
	}
	return nil
}

func migrateAddProcessedPath(db *sql.DB) error {
	if err := addColumnIfMissing(db, "transcriptions", "processed_path", "TEXT"); err != nil {
		return err
	}
	_, err := execWithRetry(db, `UPDATE transcriptions SET processed_path = source_path WHERE processed_path IS NULL OR processed_path = ''`)
	return err
}

func migrateNormalizeCallTimestampUTC(db *sql.DB) error {
	rows, err := queryWithRetry(db, `SELECT filename, call_timestamp FROM transcriptions WHERE call_timestamp IS NOT NULL AND TRIM(call_timestamp) != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type pendingUpdate struct {
		filename string
		ts       time.Time
	}
	var updates []pendingUpdate

	tz, err := time.LoadLocation("EST5EDT")
	if err != nil {
		tz = time.FixedZone("EST", -5*60*60)
	}

	for rows.Next() {
		var filename string
		var raw sql.NullString
		if err := rows.Scan(&filename, &raw); err != nil {
			return err
		}
		value := strings.TrimSpace(raw.String)
		if value == "" {
			continue
		}
		ts, err := parseTimestampFlexible(value, tz)
		if err != nil {
			continue
		}
		updates = append(updates, pendingUpdate{filename: filename, ts: ts.UTC()})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, upd := range updates {
		if _, err := execWithRetry(db, `UPDATE transcriptions SET call_timestamp = ? WHERE filename = ?`, upd.ts, upd.filename); err != nil {
			return err
		}
	}
	return nil
}

func migrateAddRollups(db *sql.DB) error {
	schema := `CREATE TABLE IF NOT EXISTS rollups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rollup_key TEXT NOT NULL UNIQUE,
    start_at DATETIME NOT NULL,
    end_at DATETIME NOT NULL,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    municipality TEXT NULL,
    poi TEXT NULL,
    category TEXT NOT NULL,
    priority TEXT NOT NULL,
    title TEXT NULL,
    summary TEXT NULL,
    evidence_json TEXT NULL,
    confidence TEXT NULL,
    status TEXT NOT NULL,
    merge_suggestion TEXT NULL,
    model_name TEXT NULL,
    model_base_url TEXT NULL,
    prompt_version TEXT NULL,
    call_count INTEGER DEFAULT 0,
    last_error TEXT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TRIGGER IF NOT EXISTS rollups_updated_at
AFTER UPDATE ON rollups
BEGIN
    UPDATE rollups SET updated_at = CURRENT_TIMESTAMP WHERE id = old.id;
END;
CREATE TABLE IF NOT EXISTS rollup_calls (
    rollup_id INTEGER NOT NULL,
    call_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (rollup_id, call_id),
    FOREIGN KEY (rollup_id) REFERENCES rollups(id) ON DELETE CASCADE,
    FOREIGN KEY (call_id) REFERENCES transcriptions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_rollups_updated_at ON rollups(updated_at);
CREATE INDEX IF NOT EXISTS idx_rollups_window ON rollups(start_at, end_at);
CREATE INDEX IF NOT EXISTS idx_rollup_calls_rollup ON rollup_calls(rollup_id);
CREATE INDEX IF NOT EXISTS idx_rollup_calls_call ON rollup_calls(call_id);
CREATE TABLE IF NOT EXISTS rollup_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME NULL,
    status TEXT,
    error TEXT,
    rollup_count INTEGER DEFAULT 0
);`
	_, err := execWithRetry(db, schema)
	return err
}

func parseTimestampFlexible(raw string, tz *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	layoutsWithZone := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.000000-07:00",
	}
	for _, layout := range layoutsWithZone {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts, nil
		}
	}
	layoutsLocal := []string{
		"2006-01-02 15:04:05.000000000",
		"2006-01-02 15:04:05.000000",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layoutsLocal {
		if ts, err := time.ParseInLocation(layout, raw, tz); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse timestamp %q", raw)
}

func addColumnIfMissing(db *sql.DB, table, column, colType string) error {
	rows, err := queryWithRetry(db, fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colType)
	if _, err := execWithRetry(db, stmt); err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "duplicate column name") || strings.Contains(lower, "already exists") {
			return nil
		}
		return err
	}
	return nil
}

func (s *server) watch() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("watcher: %v", err)
	}
	defer watcher.Close()

	if err := watcher.Add(s.cfg.CallsDir); err != nil {
		log.Fatalf("watch add: %v", err)
	}

	log.Printf("watching %s for new files", s.cfg.CallsDir)
	for {
		select {
		case evt, ok := <-watcher.Events:
			if !ok {
				return
			}
			if evt.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
				filename := filepath.Base(evt.Name)
				log.Printf("detected new file: %s", filename)
				s.handleNewFile(filename)
			}
		case err := <-watcher.Errors:
			log.Printf("watch error: %v", err)
		case <-s.shutdown:
			return
		}
	}
}

func (s *server) handleNewFile(filename string) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		log.Printf("skipping empty filename from watcher")
		return
	}
	opts, _ := s.defaultOptions()
	s.queueJob("watcher", filename, true, false, opts)
}

func (s *server) queueJob(source, filename string, sendGroupMe bool, force bool, opts TranscriptionOptions) bool {
	if s.queue == nil {
		log.Printf("queue disabled; skipping enqueue for %s", filename)
		return false
	}
	enqueued, _ := s.enqueueWithBackoff(context.Background(), source, filename, sendGroupMe, force, opts)
	return enqueued
}

func (s *server) shouldSkipEnqueue(filename string, force bool) (bool, string) {
	if force {
		return false, ""
	}

	if _, exists := s.running.Load(filename); exists {
		return true, "job already scheduled"
	}

	existing, err := s.getTranscription(filename)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("failed to check existing transcription for %s: %v", filename, err)
		return false, ""
	}
	if existing == nil {
		return false, ""
	}

	switch existing.Status {
	case statusDone:
		return true, "transcription already completed"
	case statusProcessing, statusQueued:
		return true, "transcription already in progress"
	}

	if existing.DuplicateOf != nil && *existing.DuplicateOf != "" {
		return true, "duplicate transcription detected"
	}

	return false, ""
}

func (s *server) enqueueWithBackoff(ctx context.Context, source, filename string, sendGroupMe bool, force bool, opts TranscriptionOptions) (bool, bool) {
	if skip, reason := s.shouldSkipEnqueue(filename, force); skip {
		if reason != "" {
			log.Printf("skipping enqueue for %s: %s", filename, reason)
		}
		return false, false
	}
	if _, exists := s.running.LoadOrStore(filename, struct{}{}); exists && !force {
		return false, false
	}
	meta, pretty, publicURL, baseURL := s.buildJobContext(filename)
	sourcePath := filepath.Join(s.cfg.CallsDir, filename)
	if err := s.markQueued(filename, sourcePath, source, 0, opts, meta.DateTime); err != nil {
		log.Printf("mark queued failed for %s: %v", filename, err)
	}
	jobPayload := processJob{filename: filename, source: source, sendGroupMe: sendGroupMe, force: force, options: opts, meta: meta, prettyTitle: pretty, publicURL: publicURL, baseURL: baseURL}
	job := queue.Job{
		ID:       filename,
		FileName: filename,
		Source:   source,
		Work: func(ctx context.Context) error {
			return s.processWithRetry(ctx, jobPayload, 2)
		},
		OnFinish: func(err error) {
			s.running.Delete(filename)
		},
	}
	const backoffWindow = 5 * time.Second
	const retryInterval = 200 * time.Millisecond
	enqueued, dropped := s.queue.EnqueueWithRetry(ctx, job, backoffWindow, retryInterval)
	if !enqueued {
		s.running.Delete(filename)
	}
	return enqueued, dropped
}

func (s *server) buildJobContext(filename string) (formatting.CallMetadata, string, string, string) {
	meta, err := formatting.ParseCallMetadataFromFilename(filename, s.tz)
	if err != nil {
		log.Printf("metadata parse failed for %s: %v", filename, err)
		meta = formatting.CallMetadata{RawFileName: filename, DateTime: time.Now().In(s.tz)}
	}
	pretty := formatting.FormatPrettyTitle(filename, meta.DateTime, s.tz)
	base := s.resolveBaseURL(nil)
	return meta, pretty, formatting.BuildListenURL(filename), base
}

func (s *server) resolveBaseURL(r *http.Request) string {
	if base := strings.TrimSpace(s.cfg.PublicBaseURL); base != "" {
		return strings.TrimRight(base, "/")
	}

	if r != nil {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
			scheme = forwarded
		}
		host := strings.TrimSpace(r.Host)
		if host != "" {
			return fmt.Sprintf("%s://%s", scheme, host)
		}
	}

	if strings.TrimSpace(s.cfg.HTTPPort) != "" {
		return fmt.Sprintf("http://localhost%s", s.cfg.HTTPPort)
	}

	return "https://ui.sussexcountyalerts.com"
}

func (s *server) publicURL(base, filename string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = "https://ui.sussexcountyalerts.com"
	}
	return fmt.Sprintf("%s/%s", base, url.PathEscape(filename))
}

func (s *server) previewURL(base, filename string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = "https://ui.sussexcountyalerts.com"
	}
	return fmt.Sprintf("%s/preview/%s.png", base, url.PathEscape(filename))
}

// ProcessAudioWithFFmpeg takes a raw file and returns a processed path.
func ProcessAudioWithFFmpeg(ctx context.Context, rawPath string) (string, error) {
	if !audioFilterEnabled {
		return rawPath, nil
	}

	ffmpegBin := strings.TrimSpace(ffmpegBinary)
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}

	ext := filepath.Ext(rawPath)
	base := strings.TrimSuffix(filepath.Base(rawPath), ext)
	processedName := base + "_proc" + ext
	processedPath := filepath.Join(filepath.Dir(rawPath), processedName)

	filter := "highpass=f=200,lowpass=f=3600,afftdn=nf=-25,acompressor=threshold=-20dB:ratio=3:attack=5:release=200,alimiter=limit=-2dB"
	args := []string{
		"-y",
		"-i", rawPath,
		"-ac", "1",
		"-ar", "16000",
		"-af", filter,
		processedPath,
	}
	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		log.Printf("ffmpeg processing failed for %s: %v (stderr: %s)", rawPath, err, strings.TrimSpace(stderr.String()))
		return rawPath, err
	}

	log.Printf("ffmpeg processed audio input=%s output=%s duration_ms=%d", rawPath, processedPath, time.Since(start).Milliseconds())
	return processedPath, nil
}

func (s *server) processWithRetry(ctx context.Context, job processJob, attempts int) error {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			log.Printf("retrying transcription for %s (attempt %d/%d)", job.filename, attempt+1, attempts)
		}
		if err := s.processFile(ctx, job); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if job.sendGroupMe && lastErr != nil {
		s.notifyTranscriptionFailure(job, lastErr)
	}
	return lastErr
}

func (s *server) notifyTranscriptionFailure(job processJob, cause error) {
	message := "transcription failed"
	if cause != nil {
		message = strings.TrimSpace(cause.Error())
		if len(message) > 160 {
			message = message[:157] + "…"
		}
	}
	listenURL := strings.TrimSpace(job.publicURL)
	if listenURL == "" {
		listenURL = formatting.BuildListenURL(job.filename)
	}
	callTime := job.meta.DateTime
	if callTime.IsZero() {
		callTime = time.Now().In(s.tz)
	}
	incident := s.buildIncidentDetails(job.meta, nil, nil, nil, nil, callTime, job.filename, listenURL, "")
	header := formatting.FormatIncidentHeader(incident)
	location := formatting.FormatIncidentLocation(incident)
	body := fmt.Sprintf("%s\n%s\n⚠️ Transcript unavailable (%s)\nListen: %s", strings.TrimSpace(header), strings.TrimSpace(location), message, listenURL)
	if err := s.sendGroupMe(body); err != nil {
		log.Printf("groupme failure alert failed: %v", err)
	}
}

func (s *server) processFile(ctx context.Context, j processJob) error {
	filename := j.filename
	sourcePath := filepath.Join(s.cfg.CallsDir, filename)
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	start := time.Now()
	decodeStart := start
	var decodeDur, transcribeDur, notifyDur time.Duration
	status := "success"
	defer func() {
		log.Printf("job_source=%s file=%s call_type=%s total=%.2fs decode=%.2fs transcribe=%.2fs notify=%.2fs status=%s", j.source, filename, j.meta.CallType, time.Since(start).Seconds(), decodeDur.Seconds(), transcribeDur.Seconds(), notifyDur.Seconds(), status)
	}()

	var existingEntry *transcription
	if existing, err := s.getTranscription(filename); err == nil {
		existingEntry = existing
		if !j.force {
			if existing.Status == statusDone || existing.Status == statusProcessing {
				return nil
			}
		}
	}
	if err := s.markProcessing(filename, sourcePath, j.source, info.Size(), j.options, j.meta.DateTime); err != nil {
		status = err.Error()
		return err
	}
	if err := waitForStableSize(ctx, sourcePath, info.Size(), 2*time.Second, 2); err != nil {
		s.markError(filename, err)
		status = err.Error()
		decodeDur = time.Since(decodeStart)
		return err
	}

	processedPath, procErr := ProcessAudioWithFFmpeg(ctx, sourcePath)
	if procErr != nil {
		log.Printf("audio preprocessing skipped for %s: %v", filename, procErr)
		processedPath = sourcePath
	}
	if err := s.updateProcessedPath(filename, processedPath); err != nil {
		log.Printf("failed to record processed path for %s: %v", filename, err)
	}

	duration := probeDuration(sourcePath)
	hashValue, err := hashFile(sourcePath)
	if err != nil {
		s.markError(filename, err)
		status = err.Error()
		decodeDur = time.Since(decodeStart)
		return err
	}

	if err := s.updateMetadata(filename, info.Size(), duration, hashValue); err != nil {
		log.Printf("metadata update failed: %v", err)
	}

	if dup := s.findDuplicate(hashValue, filename); dup != "" {
		if err := s.copyFromDuplicate(filename, dup); err != nil {
			log.Printf("failed to mirror duplicate data: %v", err)
		}
		note := fmt.Sprintf("duplicate of %s", dup)
		s.markDoneWithDetails(filename, note, nil, nil, nil, &dup, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
		if j.sendGroupMe {
			followup := fmt.Sprintf("%s transcript is duplicate of %s", filename, dup)
			_ = s.sendGroupMe(followup)
		}
		return nil
	}

	stagedPath := filepath.Join(s.cfg.WorkDir, filepath.Base(processedPath))
	if err := copyFile(processedPath, stagedPath); err != nil {
		s.markError(filename, err)
		status = err.Error()
		decodeDur = time.Since(decodeStart)
		return err
	}
	decodeDur = time.Since(decodeStart)

	transcribeStart := time.Now()
	artifacts, err := s.multiPassTranscription(stagedPath, j.options, j.meta)
	if err != nil {
		s.markError(filename, err)
		status = err.Error()
		transcribeDur = time.Since(transcribeStart)
		return err
	}
	transcribeDur = time.Since(transcribeStart)
	rawTranscript := artifacts.RawTranscript
	cleanedTranscript := artifacts.CleanTranscript
	translation := artifacts.Translation
	embedding := artifacts.Embedding
	diarized := artifacts.DiarizedJSON
	towns := artifacts.RecognizedTowns
	normalized := artifacts.NormalizedText
	actualModel := artifacts.ActualModel
	callType := artifacts.CallType
	if callType == nil && existingEntry != nil && existingEntry.CallType != nil {
		callType = existingEntry.CallType
	}
	if callType == nil && j.meta.CallType != "" {
		ct := j.meta.CallType
		callType = &ct
	}

	if normalized == nil || strings.TrimSpace(*normalized) == "" {
		fallback := formatting.NormalizeTranscript(cleanedTranscript)
		normalized = &fallback
	}

	recognized := parseRecognizedTowns(towns)
	tagsList := s.buildTags(j.meta, recognized, callType)
	var tagsJSON *string
	if data, err := json.Marshal(tagsList); err == nil {
		str := string(data)
		tagsJSON = &str
	}

	var latPtr, lonPtr *float64
	var locationLabel *string
	var locationSource *string
	var resolvedLocation *locationGuess
	candidateRecord := transcription{
		Filename:             filename,
		NormalizedTranscript: normalized,
		CleanTranscript:      &cleanedTranscript,
		RawTranscript:        &rawTranscript,
		RecognizedTowns:      towns,
		CallType:             callType,
		TagsJSON:             tagsJSON,
	}
	applyLocationGuess := func(guess *locationGuess) {
		if guess == nil {
			return
		}
		resolvedLocation = guess
		if guess.Label != "" {
			label := guess.Label
			locationLabel = &label
		}
		if guess.Source != "" {
			source := guess.Source
			locationSource = &source
		}
		if guess.Latitude != 0 || guess.Longitude != 0 {
			lat := guess.Latitude
			lon := guess.Longitude
			latPtr = &lat
			lonPtr = &lon
		}
		s.locationCache.Store(filename, guess)
	}
	if normalized != nil {
		locCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		resolved := s.parseAndGeocodeLocation(locCtx, *normalized, j.meta)
		cancel()
		applyLocationGuess(resolved)
	}
	if resolvedLocation == nil {
		applyLocationGuess(s.deriveLocation(candidateRecord, j.meta))
	}
	if resolvedLocation == nil && normalized != nil {
		metaCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		inference, err := s.inferMetadataAddress(metaCtx, *normalized, j.meta, recognized)
		cancel()
		if err != nil && !errors.Is(err, errMetadataInferenceDisabled) {
			log.Printf("metadata inference failed for %s: %v", filename, err)
		}
		if err == nil && inference != nil {
			geoCtx, geoCancel := context.WithTimeout(context.Background(), 4*time.Second)
			applyLocationGuess(s.metadataLocationGuess(geoCtx, inference, j.meta))
			geoCancel()
		}
	}
	if resolvedLocation == nil {
		applyLocationGuess(s.historicalHotspot(j.meta, recognized))
	}

	if err := s.markDoneWithDetails(filename, "", &rawTranscript, &cleanedTranscript, translation, nil, diarized, towns, normalized, actualModel, callType, tagsJSON, latPtr, lonPtr, locationLabel, locationSource, artifacts.MetadataJSON, artifacts.AddressJSON, artifacts.NeedsManualReview); err != nil {
		status = err.Error()
		return err
	}
	notifyStart := time.Now()
	if len(embedding) > 0 {
		if err := s.storeEmbedding(filename, embedding); err != nil {
			log.Printf("store embedding: %v", err)
		}
	}
	if j.sendGroupMe {
		if err := s.fireWebhooks(j); err != nil {
			log.Printf("webhook error: %v", err)
		}
		audioName := s.audioFilename(transcription{ProcessedPath: processedPath, SourcePath: sourcePath, Filename: filename})
		callTime := j.meta.DateTime
		if callTime.IsZero() {
			callTime = time.Now().In(s.tz)
		}
		incident := s.buildIncidentDetails(j.meta, callType, tagsList, resolvedLocation, recognized, callTime, audioName, formatting.BuildListenURL(audioName), cleanedTranscript)
		alertBody := formatting.BuildIncidentAlert(incident)
		if err := s.sendGroupMe(alertBody); err != nil {
			log.Printf("groupme follow-up failed: %v", err)
		}
	}
	notifyDur = time.Since(notifyStart)
	return nil
}

func waitForStableSize(ctx context.Context, path string, initial int64, interval time.Duration, required int) error {
	if initial <= 0 {
		time.Sleep(interval)
	}
	var last int64 = -1
	stable := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat: %w", err)
		}
		size := info.Size()
		if size > 0 && size == last {
			stable++
			if stable >= required {
				return nil
			}
		} else {
			stable = 0
		}
		last = size
		time.Sleep(interval)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

type transcriptionArtifacts struct {
	RawTranscript     string
	CleanTranscript   string
	Translation       *string
	Embedding         []float64
	DiarizedJSON      *string
	RecognizedTowns   *string
	NormalizedText    *string
	ActualModel       *string
	CallType          *string
	MetadataJSON      *string
	AddressJSON       *string
	NeedsManualReview bool
}

func (s *server) multiPassTranscription(path string, opts TranscriptionOptions, meta formatting.CallMetadata) (transcriptionArtifacts, error) {
	result := transcriptionArtifacts{}
	raw, diarized, actualModel, err := s.callOpenAIWithRetries(path, opts)
	if err != nil {
		return result, err
	}
	result.RawTranscript = raw
	result.DiarizedJSON = diarized
	result.ActualModel = actualModel
	cleaned := raw

	var normalized *string
	var towns *string
	var metadataJSON *string
	var addressJSON *string
	var callType *string
	var manualReview bool

	if s.refiner != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		refined, refineErr := s.refiner.Refine(ctx, refine.Request{
			Transcript:      raw,
			Metadata:        meta,
			RecognizedTowns: []string{meta.TownDisplay},
		})
		cancel()
		if refineErr != nil {
			log.Printf("refine pipeline failed: %v", refineErr)
		} else {
			if strings.TrimSpace(refined.CleanTranscript) != "" {
				cleaned = refined.CleanTranscript
			}
			if len(refined.RecognizedTowns) > 0 {
				data, _ := json.Marshal(refined.RecognizedTowns)
				townsStr := string(data)
				towns = &townsStr
			}
			callType = optionalString(strings.TrimSpace(refined.Metadata.IncidentType))
			if data, err := json.Marshal(refined.Metadata); err == nil {
				metaStr := string(data)
				metadataJSON = &metaStr
			}
			if data, err := json.Marshal(refined.Address); err == nil {
				addrStr := string(data)
				addressJSON = &addrStr
			}
			manualReview = refined.NeedsManualReview
		}
	}

	normalizationSource := cleaned
	if normalized != nil && strings.TrimSpace(*normalized) != "" {
		normalizationSource = *normalized
	}
	if normalizedText := formatting.NormalizeTranscript(normalizationSource); normalizedText != "" {
		normalized = &normalizedText
	}
	if normalized == nil {
		norm := formatting.NormalizeTranscript(cleaned)
		normalized = &norm
	}

	if towns == nil {
		if c, n, t, err := s.domainCleanup(raw); err == nil {
			if c != "" && strings.TrimSpace(cleaned) == "" {
				cleaned = c
			}
			if n != "" {
				normalized = &n
			}
			if len(t) > 0 {
				data, _ := json.Marshal(t)
				townsStr := string(data)
				towns = &townsStr
			}
		}
	}

	var translation *string
	if opts.Mode == "translate" || opts.AutoTranslate {
		if t, err := s.translateTranscript(cleaned); err == nil && t != "" {
			translation = &t
		}
	}

	emb, _ := s.embedTranscript(cleaned)
	if callType == nil {
		callType, _ = s.classifyCallType(cleaned)
	}

	result.CleanTranscript = cleaned
	result.Translation = translation
	result.Embedding = emb
	result.RecognizedTowns = towns
	result.NormalizedText = normalized
	result.CallType = callType
	result.MetadataJSON = metadataJSON
	result.AddressJSON = addressJSON
	result.NeedsManualReview = manualReview
	return result, nil
}

func (s *server) callOpenAIWithRetries(path string, opts TranscriptionOptions) (string, *string, *string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		transcript, diarized, model, err := s.callOpenAI(path, opts)
		if err == nil {
			return transcript, diarized, model, nil
		}
		lastErr = err
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}

	// chunked fallback: split into ~15MB segments
	chunks, err := chunkFile(path, 15*1024*1024, s.cfg.WorkDir)
	if err != nil {
		return "", nil, nil, lastErr
	}
	var combined []string
	for _, chunk := range chunks {
		t, _, model, err := s.callOpenAI(chunk, opts)
		if err != nil {
			return "", nil, nil, err
		}
		combined = append(combined, t)
		opts.Model = derefString(model, opts.Model)
	}
	finalModel := opts.Model
	return strings.Join(combined, " "), nil, &finalModel, nil
}

func (s *server) callOpenAI(path string, opts TranscriptionOptions) (string, *string, *string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", nil, nil, errors.New("OPENAI_API_KEY not set")
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", nil, nil, err
	}
	if info.Size() > 25*1024*1024 {
		return "", nil, nil, fmt.Errorf("file exceeds 25MB limit")
	}
	if _, ok := allowedExtensions[strings.ToLower(filepath.Ext(path))]; !ok {
		return "", nil, nil, fmt.Errorf("unsupported file type")
	}

	file, err := os.Open(path)
	if err != nil {
		return "", nil, nil, err
	}
	defer file.Close()

	bodyReader, bodyWriter := io.Pipe()
	writer := multipart.NewWriter(bodyWriter)

	go func() {
		defer bodyWriter.Close()
		defer writer.Close()
		fw, err := writer.CreateFormFile("file", filepath.Base(path))
		if err != nil {
			_ = bodyWriter.CloseWithError(err)
			return
		}
		if _, err := io.Copy(fw, file); err != nil {
			_ = bodyWriter.CloseWithError(err)
			return
		}
		writer.WriteField("model", opts.Model)
		if opts.LanguageHint != "" {
			writer.WriteField("language", opts.LanguageHint)
		}
		if opts.Prompt != "" {
			writer.WriteField("prompt", opts.Prompt)
		}
		if opts.Format != "" {
			writer.WriteField("response_format", opts.Format)
		}
	}()

	endpoint := "https://api.openai.com/v1/audio/transcriptions"
	if opts.Mode == "translate" {
		endpoint = "https://api.openai.com/v1/audio/translations"
	}
	req, err := http.NewRequest("POST", endpoint, bodyReader)
	if err != nil {
		return "", nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		return "", nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", nil, nil, fmt.Errorf("openai status %d: %s", resp.StatusCode, string(b))
	}

	format := opts.Format
	if format == "" {
		format = "json"
	}
	switch format {
	case "text":
		b, _ := io.ReadAll(resp.Body)
		txt := strings.TrimSpace(string(b))
		if txt == "" {
			return "", nil, nil, errors.New("empty transcript from openai")
		}
		return txt, nil, &opts.Model, nil
	default:
		var parsed map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return "", nil, nil, err
		}
		textVal, _ := parsed["text"].(string)
		if textVal == "" {
			return "", nil, nil, errors.New("empty transcript from openai")
		}
		var diarized *string
		if opts.Model == "gpt-4o-transcribe-diarize" && opts.Format == "diarized_json" {
			b, _ := json.Marshal(parsed)
			jsonStr := string(b)
			diarized = &jsonStr
		}
		modelUsed := opts.Model
		if m, ok := parsed["model"].(string); ok && m != "" {
			modelUsed = m
		}
		return textVal, diarized, &modelUsed, nil
	}
}

func (s *server) enhanceTranscript(raw string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", errors.New("OPENAI_API_KEY not set")
	}
	prompt := "Clean up the following transcript. Improve punctuation, remove duplicated phrases, and keep speaker-neutral text. Return only the cleaned transcript."
	payload := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "system", "content": prompt},
			{"role": "user", "content": raw},
		},
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("chat status %d: %s", resp.StatusCode, string(b))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("empty completion")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func (s *server) translateTranscript(text string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", errors.New("OPENAI_API_KEY not set")
	}
	prompt := "Translate to English while keeping terminology intact. Return only the translated text."
	payload := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "system", "content": prompt},
			{"role": "user", "content": text},
		},
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("translate status %d: %s", resp.StatusCode, string(b))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("empty translation")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func (s *server) domainCleanup(text string) (string, string, []string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return text, "", nil, errors.New("OPENAI_API_KEY not set")
	}
	settings, err := s.loadSettings()
	if err != nil {
		return text, "", nil, err
	}
	prompt := strings.TrimSpace(settings.CleanupPrompt)
	if prompt == "" {
		prompt = defaultCleanupPrompt
	}
	payload := map[string]interface{}{
		"model":           "gpt-4.1-mini",
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": prompt},
			{"role": "user", "content": text},
		},
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return text, "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return text, "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return text, "", nil, fmt.Errorf("cleanup status %d: %s", resp.StatusCode, string(b))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return text, "", nil, err
	}
	if len(parsed.Choices) == 0 {
		return text, "", nil, errors.New("empty cleanup")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	var result struct {
		NormalizedTranscript string   `json:"normalized_transcript"`
		RecognizedTowns      []string `json:"recognized_towns"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return text, "", nil, err
	}
	cleaned := result.NormalizedTranscript
	if cleaned == "" {
		cleaned = text
	}
	normalized := result.NormalizedTranscript
	if normalized == "" {
		normalized = cleaned
	}
	return cleaned, normalized, result.RecognizedTowns, nil
}

func (s *server) classifyCallType(text string) (*string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY not set")
	}
	prompt := "Classify the emergency call type into one of: Fire; EMS/Medical; Motor Vehicle Accident; Rescue; Hazmat; Alarm; Other / Unknown. Reply with only the label."
	payload := map[string]interface{}{
		"model": "gpt-4.1-mini",
		"messages": []map[string]string{
			{"role": "system", "content": prompt},
			{"role": "user", "content": text},
		},
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("classification status %d: %s", resp.StatusCode, string(b))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("empty classification")
	}
	label := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if label == "" {
		return nil, errors.New("missing call type")
	}
	return &label, nil
}

func (s *server) embedTranscript(text string) ([]float64, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY not set")
	}
	payload := map[string]interface{}{
		"model": "text-embedding-3-small",
		"input": text,
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding status %d: %s", resp.StatusCode, string(b))
	}
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 {
		return nil, errors.New("empty embedding")
	}
	return parsed.Data[0].Embedding, nil
}

func chunkFile(path string, maxBytes int64, workDir string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() <= maxBytes {
		return []string{path}, nil
	}
	base := filepath.Base(path)
	var paths []string
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, maxBytes)
	idx := 0
	for {
		n, err := io.ReadFull(f, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		if n == 0 {
			break
		}
		chunkPath := filepath.Join(workDir, fmt.Sprintf("%s.part%d", base, idx))
		if err := os.WriteFile(chunkPath, buf[:n], 0644); err != nil {
			return nil, err
		}
		paths = append(paths, chunkPath)
		idx++
		if err == io.EOF {
			break
		}
	}
	return paths, nil
}

func (s *server) sendGroupMe(text string) error {
	payload := map[string]string{
		"bot_id": s.botID,
		"text":   text,
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", groupmeURL, strings.NewReader(string(buf)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("groupme status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, map[string]bool{"ok": true})
}

func (s *server) handleVersion(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, map[string]string{
		"version":    version.Version,
		"git_sha":    version.GitSHA,
		"build_time": version.BuildTime,
	})
}

func (s *server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if s.queue != nil {
		if !s.queue.Healthy() {
			http.Error(w, "queue not ready", http.StatusServiceUnavailable)
			return
		}
		if stats := s.queue.Stats(); stats.WorkerCount <= 0 {
			http.Error(w, "no workers", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

func (s *server) handleDebugQueue(w http.ResponseWriter, r *http.Request) {
	if s.queue == nil {
		http.Error(w, "queue disabled", http.StatusServiceUnavailable)
		return
	}
	stats := s.queue.Stats()
	s.metrics.UpdateQueue(stats.Length, stats.Capacity, stats.WorkerCount)
	snapshot := s.metrics.Snapshot()
	resp := QueueDebugResponse{
		Length:        stats.Length,
		Capacity:      stats.Capacity,
		Workers:       stats.WorkerCount,
		ProcessedJobs: snapshot.ProcessedJobs,
		FailedJobs:    snapshot.FailedJobs,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("failed to write debug queue response: %v", err)
	}
}

func (s *server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requested := strings.TrimPrefix(r.URL.Path, "/preview/")
	requested = strings.TrimSuffix(requested, ".png")
	requested = filepath.Base(requested)
	if requested == "" {
		http.NotFound(w, r)
		return
	}

	t, err := s.getTranscription(requested)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	img, err := s.renderPreviewImage(*t)
	if err != nil {
		log.Printf("preview render failed for %s: %v", requested, err)
		http.Error(w, "preview unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if err := png.Encode(w, img); err != nil {
		log.Printf("preview encode failed for %s: %v", requested, err)
	}
}

func (s *server) renderPreviewImage(t transcription) (image.Image, error) {
	const (
		width      = 1200
		height     = 630
		padding    = 48
		textWidth  = width - (padding * 2)
		lineHeight = 22
	)

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := image.NewUniform(color.RGBA{R: 11, G: 16, B: 33, A: 255})
	panel := image.NewUniform(color.RGBA{R: 18, G: 27, B: 56, A: 255})
	accent := image.NewUniform(color.RGBA{R: 126, G: 231, B: 255, A: 255})
	muted := image.NewUniform(color.RGBA{R: 165, G: 175, B: 197, A: 255})
	text := image.NewUniform(color.RGBA{R: 232, G: 238, B: 255, A: 255})
	warning := image.NewUniform(color.RGBA{R: 255, G: 209, B: 102, A: 255})

	draw.Draw(canvas, canvas.Bounds(), bg, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(padding/2, padding/2, width-padding/2, height-padding/2), panel, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(padding, padding, width-padding, padding+6), accent, image.Point{}, draw.Src)

	meta, err := formatting.ParseCallMetadataFromFilename(t.Filename, s.tz)
	if err != nil {
		meta = formatting.CallMetadata{RawFileName: t.Filename, DateTime: t.CreatedAt.In(s.tz)}
	}
	callTime := meta.DateTime
	if t.CallTimestamp != nil {
		callTime = t.CallTimestamp.In(s.tz)
	}
	if callTime.IsZero() {
		callTime = t.CreatedAt.In(s.tz)
	}
	if meta.CallType == "" && t.CallType != nil {
		meta.CallType = *t.CallType
	}
	if meta.TownDisplay == "" {
		meta.TownDisplay = meta.AgencyDisplay
	}

	title := formatting.FormatPrettyTitle(t.Filename, callTime, s.tz)
	callType := strings.ToUpper(fallbackEmpty(meta.CallType, "CALL"))
	sublineParts := []string{callTime.In(s.tz).Format("Jan 2, 2006 • 3:04 PM MST")}
	if meta.TownDisplay != "" {
		sublineParts = append(sublineParts, meta.TownDisplay)
	}
	statusLine := fmt.Sprintf("Status: %s", strings.Title(t.Status))

	snippet := "Transcript not ready yet — this preview will fill in automatically once processing finishes."
	if txt := pickTranscript(&t); txt != nil && strings.TrimSpace(*txt) != "" {
		snippet = truncateText(normalizeWhitespace(*txt), 420)
	}

	face := basicfont.Face7x13
	mutedY := drawLines(canvas, padding, padding+40, lineHeight, wrapLines("Sussex County Alerts", textWidth, face), muted, face)
	titleY := drawLines(canvas, padding, mutedY+8, lineHeight+4, wrapLines(title, textWidth, face), text, face)
	subY := drawLines(canvas, padding, titleY+6, lineHeight, wrapLines(strings.Join(sublineParts, " • "), textWidth, face), muted, face)
	drawLines(canvas, padding, subY+6, lineHeight, wrapLines(statusLine, textWidth, face), warning, face)

	captionY := subY + 40
	draw.Draw(canvas, image.Rect(padding, captionY-8, width-padding, captionY-4), accent, image.Point{}, draw.Src)
	drawLines(canvas, padding, captionY+12, lineHeight, wrapLines(callType+" preview", textWidth, face), text, face)

	drawLines(canvas, padding, captionY+34, lineHeight, wrapLines(snippet, textWidth, face), text, face)

	return canvas, nil
}

func drawLines(dst draw.Image, x, startY, lineHeight int, lines []string, colorSrc image.Image, face font.Face) int {
	d := &font.Drawer{Dst: dst, Src: colorSrc, Face: face}
	y := startY
	for _, line := range lines {
		d.Dot = fixed.P(x, y)
		d.DrawString(line)
		y += lineHeight
	}
	return y
}

func wrapLines(text string, maxWidth int, face font.Face) []string {
	var lines []string
	var current strings.Builder
	d := &font.Drawer{Face: face}

	for _, word := range strings.Fields(text) {
		candidate := word
		if current.Len() > 0 {
			candidate = current.String() + " " + word
		}
		d.Dot = fixed.Point26_6{}
		if d.MeasureString(candidate).Ceil() > maxWidth && current.Len() > 0 {
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(word)
			continue
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(word)
	}

	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.URL.Path == "/":
		data, err := embeddedStatic.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "missing UI", http.StatusInternalServerError)
			return
		}
		page := strings.ReplaceAll(string(data), "__DEV_MODE__", fmt.Sprintf("%v", s.cfg.DevUI))
		page = strings.ReplaceAll(page, "__DEFAULT_CLEANUP__", template.HTMLEscapeString(defaultCleanupPrompt))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	case strings.HasPrefix(r.URL.Path, "/static/"):
		http.FileServer(http.FS(embeddedStatic)).ServeHTTP(w, r)
	default:
		if r.Method == http.MethodGet {
			s.handleFile(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

func (s *server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.loadSettings()
		if err != nil {
			log.Printf("load settings failed: %v", err)
			http.Error(w, "settings error", http.StatusInternalServerError)
			return
		}
		respondJSON(w, settings)
	case http.MethodPost:
		if !requireAdmin(w, r) {
			return
		}
		var payload AppSettings
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if payload.DefaultModel == "" {
			payload.DefaultModel = "gpt-4o-transcribe"
		}
		if payload.DefaultMode == "" {
			payload.DefaultMode = "transcribe"
		}
		if payload.DefaultFormat == "" {
			payload.DefaultFormat = "json"
		}
		if err := s.saveSettings(payload); err != nil {
			log.Printf("save settings failed: %v", err)
			http.Error(w, "save error", http.StatusInternalServerError)
			return
		}
		respondJSON(w, map[string]string{"status": "ok"})
	default:
		http.NotFound(w, r)
	}
}

func (s *server) handleFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" || strings.HasPrefix(path, "api/") || strings.HasPrefix(path, "static/") {
		http.NotFound(w, r)
		return
	}
	cleaned := filepath.Clean(path)
	if strings.Contains(cleaned, "..") || cleaned == "." {
		http.NotFound(w, r)
		return
	}
	sourcePath := filepath.Join(s.cfg.CallsDir, cleaned)
	if _, err := os.Stat(sourcePath); err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.ServeFile(w, r, sourcePath)
}

func (s *server) handleTranscriptionIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if !requireAdmin(w, r) {
			return
		}
		filename := r.URL.Query().Get("filename")
		if filename == "" {
			http.Error(w, "filename required", http.StatusBadRequest)
			return
		}
		if s.queue == nil {
			http.Error(w, "queue disabled", http.StatusServiceUnavailable)
			return
		}
		opts, _ := s.defaultOptions()
		s.queueJob("api", filename, false, true, opts)
		respondJSON(w, map[string]string{"status": statusQueued, "filename": filename})
		return
	}
	http.NotFound(w, r)
}

func (s *server) handleTranscription(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/transcription/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	filename, err := url.PathUnescape(parts[0])
	if err != nil || filename == "" {
		http.NotFound(w, r)
		return
	}
	opts, err := s.parseOptionsFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch {
	case len(parts) == 2 && parts[1] == "similar" && r.Method == http.MethodGet:
		s.handleSimilar(w, r, filename)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cleaned := filepath.Clean(filename)
	if strings.Contains(cleaned, "..") {
		http.NotFound(w, r)
		return
	}
	sourcePath := filepath.Join(s.cfg.CallsDir, cleaned)
	if _, err := os.Stat(sourcePath); err != nil {
		http.NotFound(w, r)
		return
	}

	existing, err := s.getTranscription(cleaned)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("fetch transcription %s failed: %v", cleaned, err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	if existing != nil {
		base := s.resolveBaseURL(r)
		switch existing.Status {
		case statusDone:
			respondJSON(w, s.toResponse(*existing, base))
			return
		case statusProcessing:
			respondJSON(w, s.toResponse(*existing, base))
			return
		case statusError:
			if s.queue != nil && requireAdmin(w, r) {
				s.queueJob("api", cleaned, false, true, opts)
				respondJSON(w, map[string]interface{}{
					"filename": existing.Filename,
					"status":   statusQueued,
				})
				return
			}
			respondJSON(w, s.toResponse(*existing, base))
			return
		}
	}

	if s.queue == nil {
		http.Error(w, "queue disabled", http.StatusServiceUnavailable)
		return
	}
	if !requireAdmin(w, r) {
		http.NotFound(w, r)
		return
	}
	s.queueJob("api", cleaned, false, true, opts)
	respondJSON(w, map[string]interface{}{
		"filename": cleaned,
		"status":   statusQueued,
	})
}

func pickTranscript(t *transcription) *string {
	if t.CleanTranscript != nil {
		return t.CleanTranscript
	}
	if t.RawTranscript != nil {
		return t.RawTranscript
	}
	return t.Transcript
}

func (s *server) parseOptionsFromRequest(r *http.Request) (TranscriptionOptions, error) {
	defaults, _ := s.defaultOptions()
	opts := defaults
	q := r.URL.Query()
	if v := q.Get("model"); v != "" {
		if _, ok := allowedFormats[v]; !ok {
			return opts, fmt.Errorf("unsupported model")
		}
		opts.Model = v
	}
	if v := q.Get("mode"); v != "" {
		if v != "transcribe" && v != "translate" {
			return opts, fmt.Errorf("unsupported mode")
		}
		opts.Mode = v
	}
	if v := q.Get("format"); v != "" {
		opts.Format = v
	}
	if opts.Format == "" {
		opts.Format = defaults.Format
	}
	allowed := allowedFormats[opts.Model]
	ok := false
	for _, f := range allowed {
		if f == opts.Format {
			ok = true
			break
		}
	}
	if !ok {
		return opts, fmt.Errorf("format not supported for model")
	}
	if v := q.Get("language"); v != "" {
		opts.LanguageHint = v
	}
	if v := q.Get("prompt"); v != "" {
		opts.Prompt = v
	}
	return opts, nil
}

func (s *server) handleSimilar(w http.ResponseWriter, r *http.Request, filename string) {
	t, err := s.getTranscription(filename)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if t == nil || t.CleanTranscript == nil {
		respondJSON(w, []similar{})
		return
	}
	emb, err := s.loadEmbedding(filename)
	if err != nil || len(emb) == 0 {
		respondJSON(w, []similar{})
		return
	}

	rows, err := queryWithRetry(s.db, `SELECT filename, embedding FROM transcriptions WHERE filename != ? AND embedding IS NOT NULL`, filename)
	if err != nil {
		log.Printf("similar query failed for %s: %v", filename, err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var sims []similar
	for rows.Next() {
		var name string
		var embText sql.NullString
		if err := rows.Scan(&name, &embText); err != nil {
			continue
		}
		otherEmb, _ := parseEmbedding(embText.String)
		if len(otherEmb) == 0 {
			continue
		}
		score := cosineSimilarity(emb, otherEmb)
		sims = append(sims, similar{Filename: name, Score: score})
	}
	sort.Slice(sims, func(i, j int) bool { return sims[i].Score > sims[j].Score })
	if len(sims) > 5 {
		sims = sims[:5]
	}
	respondJSON(w, sims)
}

func (s *server) handleLastSixHoursStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawWindow := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("window")))
	windowName, windowDuration := normalizeWindowName(rawWindow, "6h")

	baseURL := s.resolveBaseURL(r)
	query := `SELECT id, filename, source_path, processed_path, COALESCE(ingest_source,'') as ingest_source, transcript_text, raw_transcript_text, clean_transcript_text, translation_text, status, last_error, size_bytes, duration_seconds, hash, duplicate_of, requested_model, requested_mode, requested_format, actual_openai_model_used, diarized_json, recognized_towns, normalized_transcript, call_type, call_timestamp, tags, latitude, longitude, location_label, location_source, refined_metadata, address_json, needs_manual_review, created_at, updated_at FROM transcriptions`
	args := []interface{}{}
	var cutoff time.Time
	if windowDuration > 0 {
		cutoff = time.Now().UTC().Add(-windowDuration)
		query += " WHERE COALESCE(call_timestamp, created_at) >= ?"
		args = append(args, cutoff)
	}
	query += " ORDER BY COALESCE(call_timestamp, created_at) DESC LIMIT 500"

	rows, err := queryWithRetry(s.db, query, args...)
	if err != nil {
		log.Printf("stats query failed: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	bucketCount := 6
	if windowDuration > 0 {
		hours := int(windowDuration.Hours())
		if hours < 1 {
			hours = 1
		}
		if hours > 72 {
			hours = 72
		}
		bucketCount = hours
	} else {
		bucketCount = 24
	}

	bucketStart := time.Now().UTC().Truncate(time.Hour).Add(time.Duration(-(bucketCount - 1)) * time.Hour)
	hourlyTemplate := make([]hourlyCount, 0, bucketCount)
	hourlyCounts := make(map[string]int, bucketCount)
	for i := 0; i < bucketCount; i++ {
		ts := bucketStart.Add(time.Duration(i) * time.Hour)
		label := ts.Format("15:04")
		hourlyTemplate = append(hourlyTemplate, hourlyCount{Hour: label})
		hourlyCounts[label] = 0
	}

	stats := lastSixHourStatsResponse{
		ByType:   make(map[string]int),
		ByAgency: make(map[string]int),
		ByStatus: make(map[string]int),
		Window:   windowName,
	}

	var calls []transcriptionResponse
	for rows.Next() {
		var t transcription
		if err := scanTranscription(rows, &t); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		call := s.toResponse(t, baseURL)
		callTime := call.CallTimestamp.UTC()
		if windowDuration > 0 && callTime.Before(cutoff) {
			continue
		}

		calls = append(calls, call)

		if call.DuplicateOf != nil && *call.DuplicateOf != "" {
			continue
		}

		stats.TotalIncidents++
		stats.ByStatus[call.Status]++
		if call.CallType != nil {
			stats.ByType[strings.ToLower(*call.CallType)]++
		}
		if call.Agency != "" {
			stats.ByAgency[strings.ToLower(call.Agency)]++
		}
		bucketKey := callTime.Truncate(time.Hour).Format("15:04")
		if _, ok := hourlyCounts[bucketKey]; ok {
			hourlyCounts[bucketKey]++
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("stats rows error: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	for i, bucket := range hourlyTemplate {
		hourlyTemplate[i].Count = hourlyCounts[bucket.Hour]
	}

	stats.TopIncidentTypes = topCounts(stats.ByType, 3)
	stats.TopAgencies = topCounts(stats.ByAgency, 3)
	stats.IncidentsPerHour = hourlyTemplate
	stats.Calls = calls
	stats.MapboxToken = s.cfg.MapboxToken

	respondJSON(w, stats)
}

func (s *server) handleHotspots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawWindow := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("window")))
	windowName, windowDuration := normalizeWindowName(rawWindow, "30d")

	limit := 15
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	clauses := []string{
		"status = 'done'",
		"location_label IS NOT NULL",
		"TRIM(location_label) != ''",
		"latitude IS NOT NULL",
		"longitude IS NOT NULL",
	}
	args := []interface{}{}
	if windowDuration > 0 {
		cutoff := time.Now().UTC().Add(-windowDuration)
		clauses = append(clauses, "COALESCE(call_timestamp, created_at) >= ?")
		args = append(args, cutoff)
	}

	query := fmt.Sprintf(`SELECT location_label, latitude, longitude, COUNT(*) AS freq,
       MIN(COALESCE(call_timestamp, created_at)) AS first_seen,
       MAX(COALESCE(call_timestamp, created_at)) AS last_seen
FROM transcriptions
WHERE %s
GROUP BY location_label, latitude, longitude
ORDER BY freq DESC, last_seen DESC
LIMIT ?`, strings.Join(clauses, " AND "))
	args = append(args, limit)

	rows, err := queryWithRetry(s.db, query, args...)
	if err != nil {
		log.Printf("hotspot query failed: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var hotspots []hotspotSummary
	for rows.Next() {
		var label string
		var lat, lon float64
		var count int
		var firstSeen, lastSeen sql.NullTime
		if err := rows.Scan(&label, &lat, &lon, &count, &firstSeen, &lastSeen); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		entry := hotspotSummary{
			Label:     label,
			Latitude:  lat,
			Longitude: lon,
			Count:     count,
		}
		if firstSeen.Valid {
			ts := firstSeen.Time
			entry.FirstSeen = &ts
		}
		if lastSeen.Valid {
			ts := lastSeen.Time
			entry.LastSeen = &ts
		}
		hotspots = append(hotspots, entry)
	}
	if err := rows.Err(); err != nil {
		log.Printf("hotspot rows error: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	respondJSON(w, hotspotListResponse{Window: windowName, Hotspots: hotspots})
}

func (s *server) handleTranscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	baseURL := s.resolveBaseURL(r)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	callTypeFilter := strings.TrimSpace(r.URL.Query().Get("call_type"))
	tagFilter := parseTagFilter(r.URL.Query().Get("tags"))

	rawWindow := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("window")))
	if rawWindow == "" {
		rawWindow = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("range")))
	}
	windowName, windowDuration := normalizeWindowName(rawWindow, "24h")

	base := `SELECT id, filename, source_path, processed_path, COALESCE(ingest_source,'') as ingest_source, transcript_text, raw_transcript_text, clean_transcript_text, translation_text, status, last_error, size_bytes, duration_seconds, hash, duplicate_of, requested_model, requested_mode, requested_format, actual_openai_model_used, diarized_json, recognized_towns, normalized_transcript, call_type, call_timestamp, tags, latitude, longitude, location_label, location_source, refined_metadata, address_json, needs_manual_review, created_at, updated_at FROM transcriptions`
	where := []string{}
	args := []interface{}{}
	var cutoff time.Time
	if windowDuration > 0 {
		cutoff = time.Now().UTC().Add(-windowDuration)
		where = append(where, "COALESCE(call_timestamp, created_at) >= ?")
		args = append(args, cutoff)
	}
	if search != "" {
		like := "%" + strings.ToLower(search) + "%"
		where = append(where, "(lower(filename) LIKE ? OR lower(coalesce(clean_transcript_text, transcript_text, '')) LIKE ? OR lower(coalesce(raw_transcript_text, '')) LIKE ? OR lower(coalesce(normalized_transcript, '')) LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if statusFilter != "" {
		where = append(where, "status = ?")
		args = append(args, statusFilter)
	}
	if callTypeFilter != "" {
		where = append(where, "lower(coalesce(call_type,'')) = ?")
		args = append(args, strings.ToLower(callTypeFilter))
	}
	if len(where) > 0 {
		base += " WHERE " + strings.Join(where, " AND ")
	}
	base += " ORDER BY COALESCE(call_timestamp, created_at) DESC LIMIT 500"

	rows, err := queryWithRetry(s.db, base, args...)
	if err != nil {
		log.Printf("transcriptions query failed: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var calls []transcriptionResponse
	for rows.Next() {
		var t transcription
		if err := scanTranscription(rows, &t); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		calls = append(calls, s.toResponse(t, baseURL))
	}

	filtered := make([]transcriptionResponse, 0, len(calls))
	stats := callStats{StatusCounts: make(map[string]int), CallTypeCounts: make(map[string]int), TagCounts: make(map[string]int), AgencyCounts: make(map[string]int), TownCounts: make(map[string]int), AvailableWindow: windowName}

	for _, call := range calls {
		if windowDuration > 0 && call.CallTimestamp.Before(cutoff) {
			continue
		}
		if statusFilter != "" && call.Status != statusFilter {
			continue
		}
		if len(tagFilter) > 0 && !hasTags(call.Tags, tagFilter) {
			continue
		}
		filtered = append(filtered, call)
		stats.StatusCounts[call.Status]++
		if call.DuplicateOf != nil && *call.DuplicateOf != "" {
			continue
		}
		stats.Total++
		if call.CallType != nil {
			stats.CallTypeCounts[strings.ToLower(*call.CallType)]++
		}
		if call.Agency != "" {
			stats.AgencyCounts[strings.ToLower(call.Agency)]++
		}
		if call.Town != "" {
			stats.TownCounts[strings.ToLower(call.Town)]++
		}
		for _, tag := range call.Tags {
			normalized := strings.ToLower(strings.TrimSpace(tag))
			if normalized == "" {
				continue
			}
			stats.TagCounts[normalized]++
		}
	}

	respondJSON(w, callListResponse{Window: windowName, Calls: filtered, Stats: stats, MapboxToken: s.cfg.MapboxToken})
}

func respondJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func normalizeWindowName(raw, defaultName string) (string, time.Duration) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(defaultName))
	}

	switch name {
	case "6h", "6hr", "6hrs", "6hours":
		return "6h", 6 * time.Hour
	case "12h", "12hr", "12hrs", "12hours", "halfday":
		return "12h", 12 * time.Hour
	case "24h", "24hr", "24hrs", "24hours", "day", "1d":
		return "24h", 24 * time.Hour
	case "72h", "72hr", "72hrs", "72hours", "3d", "3days":
		return "72h", 72 * time.Hour
	case "all", "any", "everything":
		return "all", 0
	case "7d", "7days", "week":
		return "7d", 7 * 24 * time.Hour
	case "30d", "30days", "month":
		return "30d", 30 * 24 * time.Hour
	default:
		return normalizeWindowName(defaultName, defaultName)
	}
}

func parseTagFilter(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var tags []string
	for _, part := range parts {
		tag := strings.ToLower(strings.TrimSpace(part))
		if tag != "" && tag != "volunteer" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func hasTags(values []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	lookup := make(map[string]struct{}, len(values))
	for _, v := range values {
		normalized := strings.ToLower(strings.TrimSpace(v))
		if normalized == "" {
			continue
		}
		lookup[normalized] = struct{}{}
	}
	for _, want := range required {
		if _, ok := lookup[strings.ToLower(strings.TrimSpace(want))]; !ok {
			return false
		}
	}
	return true
}

func topCounts(counts map[string]int, limit int) []tagCount {
	entries := make([]tagCount, 0, len(counts))
	for key, value := range counts {
		entries = append(entries, tagCount{Tag: key, Count: value})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Count > entries[j].Count })
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

func parseRecognizedTownList(raw *string) []string {
	if raw == nil {
		return nil
	}
	val := strings.TrimSpace(*raw)
	if val == "" {
		return nil
	}
	var towns []string
	if err := json.Unmarshal([]byte(val), &towns); err == nil {
		return towns
	}
	val = strings.Trim(val, "[]")
	val = strings.ReplaceAll(val, "\"", "")
	for _, part := range strings.Split(val, ",") {
		if t := strings.TrimSpace(part); t != "" {
			towns = append(towns, t)
		}
	}
	return towns
}

func normalizeTag(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = strings.ToUpper(p)
			continue
		}
		parts[i] = strings.ToUpper(string(p[0])) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, " ")
}

func stripVolunteerTags(tags []string) []string {
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag), "volunteer") {
			continue
		}
		filtered = append(filtered, tag)
	}
	return filtered
}

func appendIfMissing(list []string, seen map[string]struct{}, value string) []string {
	value = normalizeTag(value)
	if value == "" {
		return list
	}
	key := strings.ToLower(value)
	if _, ok := seen[key]; ok {
		return list
	}
	seen[key] = struct{}{}
	return append(list, value)
}

func (s *server) deriveCounty(meta formatting.CallMetadata, recognized []string) string {
	candidates := []string{meta.TownDisplay, meta.AgencyDisplay}
	candidates = append(candidates, recognized...)
	for _, name := range candidates {
		if county, ok := countyByTown[strings.ToLower(strings.TrimSpace(name))]; ok {
			return county
		}
	}
	return ""
}

func (s *server) buildTags(meta formatting.CallMetadata, recognized []string, callType *string) []string {
	seen := make(map[string]struct{})
	tags := make([]string, 0, 8)
	tags = appendIfMissing(tags, seen, meta.TownDisplay)
	tags = appendIfMissing(tags, seen, meta.AgencyDisplay)
	if callType != nil && *callType != "" {
		tags = appendIfMissing(tags, seen, *callType)
	} else if meta.CallType != "" {
		tags = appendIfMissing(tags, seen, meta.CallType)
	}
	for _, town := range recognized {
		tags = appendIfMissing(tags, seen, town)
	}
	if county := s.deriveCounty(meta, recognized); county != "" {
		tags = appendIfMissing(tags, seen, county+" County")
	}
	return tags
}

func coerceSpeakerLabel(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("Speaker %d", int(v)))
	case int:
		return strings.TrimSpace(fmt.Sprintf("Speaker %d", v))
	default:
		return ""
	}
}

func sanitizeSegments(segments []transcriptSegment, duration *float64) []transcriptSegment {
	if len(segments) == 0 {
		return segments
	}
	sort.SliceStable(segments, func(i, j int) bool { return segments[i].Start < segments[j].Start })
	maxDuration := -1.0
	if duration != nil {
		maxDuration = *duration
	}
	cleaned := make([]transcriptSegment, 0, len(segments))
	for _, seg := range segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		seg.Text = text
		if seg.End <= seg.Start {
			seg.End = seg.Start + 0.5
		}
		if maxDuration > 0 && seg.End > maxDuration {
			seg.End = maxDuration
		}
		cleaned = append(cleaned, seg)
	}
	return cleaned
}

func splitIntoSentences(text string) []string {
	var sentences []string
	var buf strings.Builder
	flush := func() {
		sentence := strings.TrimSpace(buf.String())
		if sentence != "" {
			sentences = append(sentences, sentence)
		}
		buf.Reset()
	}
	for _, r := range text {
		buf.WriteRune(r)
		switch r {
		case '.', '!', '?', '\n':
			flush()
		}
	}
	flush()
	return sentences
}

func fallbackSegmentsFromTranscript(text string, duration *float64) []transcriptSegment {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return nil
	}
	sentences := splitIntoSentences(cleaned)
	if len(sentences) == 0 {
		sentences = []string{cleaned}
	}
	dur := 0.0
	if duration != nil {
		dur = *duration
	}
	if dur <= 0 {
		wordCount := len(strings.Fields(cleaned))
		dur = math.Max(1, float64(wordCount)*0.5)
	}
	step := dur / float64(len(sentences))
	segments := make([]transcriptSegment, 0, len(sentences))
	current := 0.0
	for i, sentence := range sentences {
		end := current + step
		if i == len(sentences)-1 {
			end = dur
		}
		segments = append(segments, transcriptSegment{Start: current, End: end, Text: sentence})
		current = end
	}
	return sanitizeSegments(segments, duration)
}

func parseSegmentsFromDiarized(raw string) []transcriptSegment {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	type word struct {
		Start   float64     `json:"start"`
		End     float64     `json:"end"`
		Word    string      `json:"word"`
		Speaker interface{} `json:"speaker"`
		Text    string      `json:"text"`
	}
	var payload struct {
		Duration float64 `json:"duration"`
		Segments []struct {
			Start   float64     `json:"start"`
			End     float64     `json:"end"`
			Text    string      `json:"text"`
			Speaker interface{} `json:"speaker"`
			Words   []word      `json:"words"`
		} `json:"segments"`
		Words []word `json:"words"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	var segments []transcriptSegment
	for _, seg := range payload.Segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		end := seg.End
		if end <= seg.Start && len(seg.Words) > 0 {
			end = seg.Words[len(seg.Words)-1].End
		}
		segments = append(segments, transcriptSegment{Start: seg.Start, End: end, Text: text, Speaker: coerceSpeakerLabel(seg.Speaker)})
	}
	if len(segments) == 0 && len(payload.Words) > 0 {
		var current transcriptSegment
		var buffer []string
		lastEnd := 0.0
		for i, w := range payload.Words {
			text := strings.TrimSpace(w.Word)
			if text == "" {
				continue
			}
			speaker := coerceSpeakerLabel(w.Speaker)
			gap := 0.0
			if len(buffer) > 0 {
				gap = w.Start - lastEnd
			}
			if len(buffer) == 0 || speaker != current.Speaker || gap > 1.5 {
				if len(buffer) > 0 {
					current.End = lastEnd
					current.Text = strings.Join(buffer, " ")
					segments = append(segments, current)
				}
				buffer = []string{text}
				current = transcriptSegment{Start: w.Start, Speaker: speaker}
				lastEnd = w.End
				if i == len(payload.Words)-1 {
					current.End = lastEnd
					current.Text = strings.Join(buffer, " ")
					segments = append(segments, current)
				}
				continue
			}
			buffer = append(buffer, text)
			lastEnd = w.End
			if i == len(payload.Words)-1 {
				current.End = lastEnd
				current.Text = strings.Join(buffer, " ")
				segments = append(segments, current)
			}
		}
	}
	return sanitizeSegments(segments, &payload.Duration)
}

func (s *server) buildSegments(t transcription) []transcriptSegment {
	var transcriptText string
	if txt := pickTranscript(&t); txt != nil {
		transcriptText = *txt
	}
	if t.DiarizedJSON != nil {
		if segments := parseSegmentsFromDiarized(*t.DiarizedJSON); len(segments) > 0 {
			return segments
		}
	}
	return fallbackSegmentsFromTranscript(transcriptText, t.DurationSeconds)
}

func (s *server) audioFilename(t transcription) string {
	if name := filepath.Base(strings.TrimSpace(t.ProcessedPath)); name != "" {
		return name
	}
	if name := filepath.Base(strings.TrimSpace(t.SourcePath)); name != "" {
		return name
	}
	return t.Filename
}

func sanitizeSummary(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	const maxLen = 320
	runes := []rune(trimmed)
	if len(runes) > maxLen {
		trimmed = strings.TrimSpace(string(runes[:maxLen])) + "…"
	}
	return trimmed
}

func (s *server) deriveLocationFields(meta formatting.CallMetadata, recognized []string, normalized string, loc *locationGuess) (string, string, string, string, string) {
	city := strings.TrimSpace(meta.TownDisplay)
	if city == "" && len(recognized) > 0 {
		city = strings.TrimSpace(strings.Title(strings.ToLower(recognized[0])))
	}

	var parsed *formatting.ParsedLocation
	if loc != nil && strings.TrimSpace(loc.Label) != "" {
		if p, err := formatting.ParseLocationFromTranscript(loc.Label); err == nil {
			parsed = p
		}
	}
	if parsed == nil && strings.TrimSpace(normalized) != "" {
		if p, err := formatting.ParseLocationFromTranscript(normalized); err == nil {
			parsed = p
		}
	}

	address := ""
	cross := ""
	if parsed != nil {
		if parsed.HouseNumber != "" && parsed.Street != "" {
			address = strings.TrimSpace(parsed.HouseNumber + " " + parsed.Street)
		} else if parsed.Street != "" {
			address = parsed.Street
		}
		if parsed.CrossStreet != "" {
			cross = parsed.CrossStreet
			if address == "" && parsed.Street != "" {
				address = parsed.Street
			}
		}
		if parsed.Municipality != "" {
			city = parsed.Municipality
		}
	}
	if address == "" && loc != nil && strings.TrimSpace(loc.Label) != "" {
		address = strings.TrimSpace(loc.Label)
	}

	county := ""
	if city != "" {
		county = countyByTown[strings.ToLower(city)]
	}
	state := ""
	if county != "" {
		state = "NJ"
	}
	return strings.TrimSpace(address), strings.TrimSpace(cross), strings.TrimSpace(city), county, state
}

func (s *server) buildIncidentDetails(meta formatting.CallMetadata, callType *string, tags []string, loc *locationGuess, recognized []string, callTime time.Time, audioFilename, listenURL, summary string) formatting.IncidentDetails {
	callTypeVal := derefString(callType, meta.CallType)
	address, cross, city, county, state := s.deriveLocationFields(meta, recognized, summary, loc)
	ts := callTime
	if ts.IsZero() {
		ts = time.Now().In(s.tz)
	}

	audioPath := ""
	if strings.TrimSpace(audioFilename) != "" {
		audioPath = "/" + url.PathEscape(strings.TrimLeft(audioFilename, "/"))
	}

	incidentID := meta.RawFileName
	if incidentID == "" {
		incidentID = audioFilename
	}

	return formatting.IncidentDetails{
		ID:            incidentID,
		PrettyTitle:   formatting.FormatPrettyTitle(meta.RawFileName, ts, s.tz),
		Agency:        meta.AgencyDisplay,
		CallType:      callTypeVal,
		CallCategory:  formatting.NormalizeCallCategory(callTypeVal),
		AddressLine:   address,
		CrossStreet:   cross,
		CityOrTown:    city,
		County:        county,
		State:         state,
		Summary:       sanitizeSummary(summary),
		Tags:          tags,
		Timestamp:     ts,
		ListenURL:     listenURL,
		AudioPath:     audioPath,
		AudioFilename: audioFilename,
	}
}

func (s *server) toResponse(t transcription, baseURL string) transcriptionResponse {
	meta, err := formatting.ParseCallMetadataFromFilename(t.Filename, s.tz)
	if err != nil {
		meta = formatting.CallMetadata{RawFileName: t.Filename, DateTime: t.UpdatedAt.In(s.tz)}
	}
	callTime := meta.DateTime
	if t.CallTimestamp != nil {
		callTime = t.CallTimestamp.In(s.tz)
	}
	if callTime.IsZero() {
		callTime = meta.DateTime
	}
	if callTime.IsZero() {
		callTime = t.CreatedAt.In(s.tz)
	}
	meta.DateTime = callTime
	recognized := parseRecognizedTownList(t.RecognizedTowns)
	callType := t.CallType
	if callType == nil && meta.CallType != "" {
		ct := meta.CallType
		callType = &ct
	}
	callTypeVal := strings.TrimSpace(derefString(callType, ""))
	pretty := formatting.FormatPrettyTitle(t.Filename, callTime, s.tz)
	tags := parseRecognizedTownList(t.TagsJSON)
	if len(tags) == 0 {
		tags = s.buildTags(meta, recognized, callType)
	}
	tags = stripVolunteerTags(tags)

	var location *locationGuess
	if t.Status == statusDone {
		location = s.locationFromRecord(t, meta)
		if location == nil {
			location = s.deriveLocation(t, meta)
		}
		if location == nil {
			location = s.historicalHotspot(meta, recognized)
		}
	}

	normalizedText := derefString(t.NormalizedTranscript, derefString(t.CleanTranscript, derefString(t.Transcript, "")))
	audioFilename := s.audioFilename(t)
	listenURL := s.publicURL(baseURL, audioFilename)
	incident := s.buildIncidentDetails(meta, callType, tags, location, recognized, callTime, audioFilename, listenURL, normalizedText)
	timestampLocal := incident.Timestamp.In(s.tz).Format(time.RFC3339)
	cleanSummary := sanitizeSummary(derefString(t.CleanTranscript, ""))

	return transcriptionResponse{
		ID:                   t.ID,
		Filename:             t.Filename,
		SourcePath:           t.SourcePath,
		Source:               t.Source,
		Transcript:           t.Transcript,
		RawTranscript:        t.RawTranscript,
		CleanTranscript:      t.CleanTranscript,
		Translation:          t.Translation,
		Status:               t.Status,
		LastError:            t.LastError,
		SizeBytes:            t.SizeBytes,
		DurationSeconds:      t.DurationSeconds,
		Hash:                 t.Hash,
		DuplicateOf:          t.DuplicateOf,
		RequestedModel:       t.RequestedModel,
		RequestedMode:        t.RequestedMode,
		RequestedFormat:      t.RequestedFormat,
		ActualModel:          t.ActualModel,
		DiarizedJSON:         t.DiarizedJSON,
		RecognizedTowns:      recognized,
		NormalizedTranscript: t.NormalizedTranscript,
		CallType:             callType,
		CallTimestamp:        callTime,
		CreatedAt:            t.CreatedAt,
		UpdatedAt:            t.UpdatedAt,
		PrettyTitle:          pretty,
		Town:                 meta.TownDisplay,
		Agency:               meta.AgencyDisplay,
		AudioURL:             listenURL,
		AudioPath:            incident.AudioPath,
		AudioFilename:        incident.AudioFilename,
		CallCategory:         incident.CallCategory,
		TimestampLocal:       timestampLocal,
		AddressLine:          incident.AddressLine,
		CrossStreet:          incident.CrossStreet,
		CityOrTown:           incident.CityOrTown,
		County:               incident.County,
		State:                incident.State,
		Summary:              incident.Summary,
		CleanSummary:         cleanSummary,
		PrimaryAgency:        meta.AgencyDisplay,
		NormalizedCallType:   callTypeVal,
		IncidentID:           incident.ID,
		PreviewImage:         s.previewURL(baseURL, t.Filename),
		Tags:                 tags,
		Segments:             s.buildSegments(t),
		Location:             location,
		RefinedMetadata:      t.RefinedMetadata,
		AddressJSON:          t.AddressJSON,
		NeedsManualReview:    t.NeedsManualReview,
	}
}

func (s *server) locationFromRecord(t transcription, meta formatting.CallMetadata) *locationGuess {
	if t.Latitude != nil && t.Longitude != nil {
		label := derefString(t.LocationLabel, formatting.FormatLocationLabel(&formatting.ParsedLocation{Municipality: meta.TownDisplay}))
		source := derefString(t.LocationSource, "stored")
		lat := *t.Latitude
		lng := *t.Longitude
		if !isWithinSussexCounty(lat, lng) {
			return &locationGuess{Label: label, Precision: source, Source: source}
		}
		return &locationGuess{Label: label, Latitude: lat, Longitude: lng, Precision: source, Source: source}
	}
	if t.LocationLabel != nil && strings.TrimSpace(*t.LocationLabel) != "" {
		label := strings.TrimSpace(*t.LocationLabel)
		source := derefString(t.LocationSource, "parsed")
		return &locationGuess{Label: label, Precision: source, Source: source}
	}
	return nil
}

func (s *server) parseAndGeocodeLocation(ctx context.Context, normalized string, meta formatting.CallMetadata) *locationGuess {
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return nil
	}
	parsed, err := formatting.ParseLocationFromTranscript(normalized)
	if err != nil || parsed == nil {
		if meta.TownDisplay == "" {
			return nil
		}
		parsed = &formatting.ParsedLocation{Municipality: meta.TownDisplay, RawText: meta.TownDisplay}
	} else if parsed.Municipality == "" && meta.TownDisplay != "" {
		parsed.Municipality = meta.TownDisplay
	}

	label := formatting.FormatLocationLabel(parsed)
	guess := &locationGuess{Label: label, Precision: "parsed", Source: "parsed"}

	token := strings.TrimSpace(s.cfg.MapboxToken)
	if token == "" {
		guess.Source = "unconfigured"
		return guess
	}

	lat, lng, precision, err := formatting.GeocodeParsedLocation(ctx, s.client, formatting.GeocoderConfig{Token: token, BBox: []float64{sussexMinLng, sussexMinLat, sussexMaxLng, sussexMaxLat}}, parsed)
	if err != nil {
		return guess
	}
	if !isWithinSussexCounty(lat, lng) {
		guess.Source = "parsed_out_of_county"
		return guess
	}
	guess.Latitude = lat
	guess.Longitude = lng
	guess.Precision = precision
	guess.Source = "parsed_geocode"
	return guess
}

func (s *server) deriveLocation(t transcription, meta formatting.CallMetadata) *locationGuess {
	token := strings.TrimSpace(s.cfg.MapboxToken)
	if token == "" {
		return nil
	}

	if cached, ok := s.locationCache.Load(t.Filename); ok {
		if guess, ok := cached.(*locationGuess); ok {
			return guess
		}
	}

	candidates := s.buildLocationCandidates(t, meta)
	if len(candidates) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	for _, candidate := range candidates {
		if loc := s.geocodeWithMapbox(ctx, token, candidate); loc != nil {
			s.locationCache.Store(t.Filename, loc)
			return loc
		}
	}

	return nil
}

func (s *server) buildLocationCandidates(t transcription, meta formatting.CallMetadata) []string {
	var candidates []string
	seen := make(map[string]struct{})
	add := func(value string) {
		v := strings.TrimSpace(value)
		if v == "" {
			return
		}
		norm := strings.ToLower(v)
		if _, exists := seen[norm]; exists {
			return
		}
		seen[norm] = struct{}{}
		candidates = append(candidates, v)
	}

	transcripts := []string{}
	if t.NormalizedTranscript != nil {
		transcripts = append(transcripts, *t.NormalizedTranscript)
	}
	if t.CleanTranscript != nil {
		transcripts = append(transcripts, *t.CleanTranscript)
	}
	if t.RawTranscript != nil {
		transcripts = append(transcripts, *t.RawTranscript)
	}
	for _, text := range transcripts {
		if match := addressPattern.FindString(text); match != "" {
			add(match + " Sussex County NJ")
		}
	}

	recognized := parseRecognizedTownList(t.RecognizedTowns)
	for _, town := range recognized {
		add(fmt.Sprintf("%s, NJ", town))
		add(fmt.Sprintf("%s, Sussex County, NJ", town))
	}
	if meta.TownDisplay != "" {
		add(fmt.Sprintf("%s, NJ", meta.TownDisplay))
		add(fmt.Sprintf("%s, Sussex County, NJ", meta.TownDisplay))
	}
	if meta.AgencyDisplay != "" {
		add(fmt.Sprintf("%s, NJ", meta.AgencyDisplay))
		add(fmt.Sprintf("%s, Sussex County, NJ", meta.AgencyDisplay))
	}
	add("Sussex County, NJ")

	return candidates
}

func (s *server) historicalHotspot(meta formatting.CallMetadata, recognized []string) *locationGuess {
	keys := make([]string, 0, 4+len(recognized))
	seen := make(map[string]struct{})
	addKey := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		keys = append(keys, value)
	}
	addKey(meta.TownDisplay)
	addKey(meta.AgencyDisplay)
	for _, town := range recognized {
		addKey(town)
	}
	if len(keys) == 0 {
		return nil
	}

	placeholders := make([]string, len(keys))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	subquery := fmt.Sprintf(`EXISTS (
    SELECT 1 FROM json_each(COALESCE(tags, '[]'))
    WHERE lower(json_each.value) IN (%s)
)`, strings.Join(placeholders, ","))

	historyCutoff := time.Now().UTC().Add(-90 * 24 * time.Hour)
	args := make([]interface{}, 0, len(keys)+2)
	args = append(args, statusDone)
	for _, key := range keys {
		args = append(args, key)
	}
	args = append(args, historyCutoff)

	query := fmt.Sprintf(`SELECT location_label, latitude, longitude, COUNT(*) AS freq,
       MAX(COALESCE(call_timestamp, created_at)) AS last_seen
FROM transcriptions
WHERE status = ? AND location_label IS NOT NULL AND TRIM(location_label) != ''
  AND latitude IS NOT NULL AND longitude IS NOT NULL
  AND %s
  AND COALESCE(call_timestamp, created_at) >= ?
GROUP BY location_label, latitude, longitude
ORDER BY freq DESC, last_seen DESC
LIMIT 1`, subquery)

	var label string
	var lat, lon float64
	var freq int
	if err := queryRowWithRetry(s.db, func(row *sql.Row) error {
		return row.Scan(&label, &lat, &lon, &freq)
	}, query, args...); err != nil {
		return nil
	}

	label = strings.TrimSpace(label)
	if label == "" || (lat == 0 && lon == 0) {
		return nil
	}
	precision := "historical_hotspot"
	if freq > 1 {
		precision = fmt.Sprintf("historical_hotspot_%d", freq)
	}
	return &locationGuess{
		Label:     label,
		Latitude:  lat,
		Longitude: lon,
		Precision: precision,
		Source:    "historical_hotspot",
	}
}

func buildGeocodeQuery(raw string) string {
	q := strings.ReplaceAll(raw, "_", " ")
	q = strings.ReplaceAll(q, "-", " ")
	q = strings.TrimSpace(q)

	tokens := strings.Fields(q)
	filtered := tokens[:0]
	for _, t := range tokens {
		upper := strings.ToUpper(t)
		switch upper {
		case "FD", "EMS", "FIRE", "RESCUE", "SQUAD", "AMBULANCE", "DEPT", "DEPARTMENT", "FIREHOUSE":
		// skip service-type tokens
		default:
			filtered = append(filtered, t)
		}
	}
	q = strings.Join(filtered, " ")

	if q == "" {
		q = raw
	}

	return fmt.Sprintf("%s, Sussex County, New Jersey, USA", strings.TrimSpace(q))
}

func buildFallbackGeocodeQuery(raw string) string {
	normalized := strings.ReplaceAll(raw, "_", " ")
	normalized = strings.ReplaceAll(normalized, "-", " ")
	normalized = strings.TrimSpace(normalized)

	tokens := strings.Fields(normalized)
	filtered := tokens[:0]
	for _, t := range tokens {
		upper := strings.ToUpper(t)
		switch upper {
		case "FD", "EMS", "FIRE", "RESCUE", "SQUAD", "AMBULANCE", "DEPT", "DEPARTMENT", "FIREHOUSE":
		// skip service-type tokens
		default:
			filtered = append(filtered, t)
		}
	}
	normalized = strings.Join(filtered, " ")

	if normalized == "" {
		normalized = raw
	}

	return fmt.Sprintf("%s, New Jersey, USA", strings.TrimSpace(normalized))
}

func (s *server) geocodeWithMapbox(ctx context.Context, token, query string) *locationGuess {
	queries := []string{buildGeocodeQuery(query)}
	if fallback := buildFallbackGeocodeQuery(query); fallback != "" && fallback != queries[0] {
		queries = append(queries, fallback)
	}

	for _, search := range queries {
		search = strings.TrimSpace(search)
		if search == "" {
			continue
		}
		encoded := url.PathEscape(search)
		endpoint := fmt.Sprintf(
			"https://api.mapbox.com/geocoding/v5/mapbox.places/%s.json?access_token=%s&autocomplete=true&limit=1&country=US&language=en&bbox=%f,%f,%f,%f",
			encoded, token, sussexMinLng, sussexMinLat, sussexMaxLng, sussexMaxLat,
		)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			log.Printf("mapbox request build failed: %v", err)
			continue
		}
		resp, err := s.client.Do(req)
		if err != nil {
			log.Printf("mapbox request failed: %v", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("mapbox response %d for %s", resp.StatusCode, search)
			continue
		}

		var payload struct {
			Features []struct {
				PlaceName string    `json:"place_name"`
				Center    []float64 `json:"center"`
				PlaceType []string  `json:"place_type"`
			} `json:"features"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			log.Printf("mapbox decode failed: %v", err)
			continue
		}
		if len(payload.Features) == 0 {
			continue
		}
		feature := payload.Features[0]
		if len(feature.Center) < 2 {
			continue
		}
		lat := feature.Center[1]
		lng := feature.Center[0]
		if !isWithinSussexCounty(lat, lng) {
			log.Printf("mapbox result outside Sussex County ignored for %s: %s (%f,%f)", search, feature.PlaceName, lat, lng)
			continue
		}

		precision := ""
		if len(feature.PlaceType) > 0 {
			precision = feature.PlaceType[0]
		}

		return &locationGuess{
			Label:     feature.PlaceName,
			Latitude:  lat,
			Longitude: lng,
			Precision: precision,
			Source:    search,
		}
	}

	return nil
}

func (s *server) inferMetadataAddress(ctx context.Context, transcript string, meta formatting.CallMetadata, recognized []string) (*metadataInference, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return nil, errMetadataInferenceDisabled
	}
	transcript = strings.TrimSpace(transcript)
	settings, err := s.loadSettings()
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(settings.MetadataPrompt)
	if prompt == "" {
		return nil, errMetadataInferenceDisabled
	}
	payload := map[string]interface{}{
		"model":           "gpt-4.1-mini",
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": prompt},
			{"role": "user", "content": metadataUserContent(transcript, meta, recognized)},
		},
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("metadata prompt status %d: %s", resp.StatusCode, string(b))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("metadata prompt returned no choices")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		return nil, errors.New("metadata prompt returned empty content")
	}
	var inference metadataInference
	if err := json.Unmarshal([]byte(content), &inference); err != nil {
		return nil, err
	}
	inference.AddressLine = strings.TrimSpace(inference.AddressLine)
	inference.Municipality = strings.TrimSpace(inference.Municipality)
	inference.County = strings.TrimSpace(inference.County)
	inference.CrossStreet = strings.TrimSpace(inference.CrossStreet)
	if inference.Confidence < 0 {
		inference.Confidence = 0
	}
	if inference.Confidence > 1 {
		inference.Confidence = 1
	}
	return &inference, nil
}

func metadataUserContent(transcript string, meta formatting.CallMetadata, recognized []string) string {
	ts := meta.DateTime
	if ts.IsZero() {
		ts = time.Now()
	}
	payload := map[string]interface{}{
		"transcript":        transcript,
		"filename":          meta.RawFileName,
		"agency":            meta.AgencyDisplay,
		"call_type":         meta.CallType,
		"call_timestamp":    ts.Format(time.RFC3339),
		"recognized_towns":  recognized,
		"fallback_town":     meta.TownDisplay,
		"raw_file_metadata": meta,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return transcript
	}
	return string(data)
}

func (s *server) metadataLocationGuess(ctx context.Context, inference *metadataInference, meta formatting.CallMetadata) *locationGuess {
	if inference == nil {
		return nil
	}
	address := strings.TrimSpace(inference.AddressLine)
	town := strings.TrimSpace(inference.Municipality)
	if town == "" {
		town = strings.TrimSpace(meta.TownDisplay)
	}
	if address == "" && town == "" {
		return nil
	}
	labelParts := []string{}
	if address != "" {
		labelParts = append(labelParts, address)
	}
	if town != "" {
		labelParts = append(labelParts, town)
	}
	label := strings.Join(labelParts, ", ")
	precision := "metadata_ai"
	if inference.Confidence > 0 {
		precision = fmt.Sprintf("metadata_ai_%02d", int(math.Round(inference.Confidence*100)))
	}
	guess := &locationGuess{
		Label:     label,
		Precision: precision,
		Source:    "metadata_prompt",
	}
	token := strings.TrimSpace(s.cfg.MapboxToken)
	if token == "" {
		return guess
	}
	var candidates []string
	if address != "" && town != "" {
		candidates = append(candidates, fmt.Sprintf("%s, %s, Sussex County, NJ", address, town))
	}
	if address != "" {
		candidates = append(candidates, fmt.Sprintf("%s, Sussex County, NJ", address))
	}
	if inference.CrossStreet != "" && town != "" {
		candidates = append(candidates, fmt.Sprintf("%s and %s, %s, Sussex County, NJ", address, inference.CrossStreet, town))
	}
	if town != "" {
		candidates = append(candidates, fmt.Sprintf("%s, Sussex County, NJ", town))
	}
	if inference.County != "" && !strings.Contains(strings.ToLower(inference.County), "sussex") {
		candidates = append(candidates, fmt.Sprintf("%s, %s, NJ", address, inference.County))
	}
	if len(candidates) == 0 && label != "" {
		candidates = append(candidates, label+" Sussex County NJ")
	}

	for _, candidate := range candidates {
		loc := s.geocodeWithMapbox(ctx, token, candidate)
		if loc == nil {
			continue
		}
		if loc.Label == "" {
			loc.Label = label
		}
		if inference.Confidence > 0 {
			loc.Precision = precision
		} else if loc.Precision == "" {
			loc.Precision = guess.Precision
		}
		loc.Source = "metadata_prompt"
		return loc
	}

	return guess
}

func (s *server) loadSettings() (AppSettings, error) {
	var settings AppSettings
	var auto sql.NullInt64
	var webhooks sql.NullString
	var defaultModel, defaultMode, defaultFormat sql.NullString
	var preferredLanguage, cleanupPrompt, metadataPrompt sql.NullString
	if err := queryRowWithRetry(s.db, func(row *sql.Row) error {
		return row.Scan(&defaultModel, &defaultMode, &defaultFormat, &auto, &webhooks, &preferredLanguage, &cleanupPrompt, &metadataPrompt)
	}, `SELECT default_model, default_mode, default_format, auto_translate, webhook_endpoints, preferred_language, cleanup_prompt, metadata_prompt FROM app_settings WHERE id=1`); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := s.ensureSettingsRow(); err != nil {
				return settings, err
			}
			return s.loadSettings()
		}
		return settings, err
	}
	settings.DefaultModel = normalizeModelName(fallbackEmpty(stringFromNull(defaultModel, defaultTranscriptionModel), defaultTranscriptionModel))
	settings.DefaultMode = fallbackEmpty(stringFromNull(defaultMode, defaultTranscriptionMode), defaultTranscriptionMode)
	settings.DefaultFormat = fallbackEmpty(stringFromNull(defaultFormat, defaultTranscriptionFormat), defaultTranscriptionFormat)
	settings.PreferredLanguage = stringFromNull(preferredLanguage, "")
	settings.CleanupPrompt = strings.TrimSpace(stringFromNull(cleanupPrompt, ""))
	settings.MetadataPrompt = strings.TrimSpace(stringFromNull(metadataPrompt, ""))
	settings.AutoTranslate = auto.Valid && auto.Int64 == 1
	hooksJSON := stringFromNull(webhooks, "[]")
	if strings.TrimSpace(hooksJSON) == "" {
		hooksJSON = "[]"
	}
	_ = json.Unmarshal([]byte(hooksJSON), &settings.WebhookEndpoints)
	if settings.WebhookEndpoints == nil {
		settings.WebhookEndpoints = []string{}
	}
	if strings.TrimSpace(settings.CleanupPrompt) == "" {
		settings.CleanupPrompt = defaultCleanupPrompt
	}
	if strings.TrimSpace(settings.MetadataPrompt) == "" {
		settings.MetadataPrompt = defaultMetadataPrompt
	}
	settings.DefaultModel = normalizeModelName(fallbackEmpty(settings.DefaultModel, defaultTranscriptionModel))
	settings.DefaultMode = fallbackEmpty(settings.DefaultMode, defaultTranscriptionMode)
	settings.DefaultFormat = fallbackEmpty(settings.DefaultFormat, defaultTranscriptionFormat)
	return settings, nil
}

func stringFromNull(ns sql.NullString, fallback string) string {
	if ns.Valid {
		return ns.String
	}
	return fallback
}

func fallbackEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizeModelName(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "gpt4-transcribe", "gpt-4-transcribe", "gpt4o-transcribe":
		return defaultTranscriptionModel
	}
	return model
}

func (s *server) saveSettings(settings AppSettings) error {
	if err := s.ensureSettingsRow(); err != nil {
		return err
	}
	if strings.TrimSpace(settings.DefaultModel) == "" {
		settings.DefaultModel = defaultTranscriptionModel
	}
	settings.DefaultModel = normalizeModelName(settings.DefaultModel)
	if strings.TrimSpace(settings.DefaultMode) == "" {
		settings.DefaultMode = defaultTranscriptionMode
	}
	if strings.TrimSpace(settings.DefaultFormat) == "" {
		settings.DefaultFormat = defaultTranscriptionFormat
	}
	if strings.TrimSpace(settings.CleanupPrompt) == "" {
		settings.CleanupPrompt = defaultCleanupPrompt
	}
	if strings.TrimSpace(settings.MetadataPrompt) == "" {
		settings.MetadataPrompt = defaultMetadataPrompt
	}
	hooks, _ := json.Marshal(settings.WebhookEndpoints)
	auto := 0
	if settings.AutoTranslate {
		auto = 1
	}
	res, err := execWithRetry(s.db, `UPDATE app_settings SET default_model=?, default_mode=?, default_format=?, auto_translate=?, webhook_endpoints=?, preferred_language=?, cleanup_prompt=?, metadata_prompt=?, updated_at=CURRENT_TIMESTAMP WHERE id=1`, settings.DefaultModel, settings.DefaultMode, settings.DefaultFormat, auto, string(hooks), settings.PreferredLanguage, settings.CleanupPrompt, settings.MetadataPrompt)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		_, err = execWithRetry(s.db, `INSERT OR REPLACE INTO app_settings(id, default_model, default_mode, default_format, auto_translate, webhook_endpoints, preferred_language, cleanup_prompt, metadata_prompt, updated_at) VALUES(1, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`, settings.DefaultModel, settings.DefaultMode, settings.DefaultFormat, auto, string(hooks), settings.PreferredLanguage, settings.CleanupPrompt, settings.MetadataPrompt)
	}
	return err
}

func (s *server) ensureSettingsRow() error {
	if _, err := execWithRetry(s.db, `INSERT OR IGNORE INTO app_settings(id, default_model, default_mode, default_format, auto_translate, webhook_endpoints, preferred_language, cleanup_prompt, metadata_prompt) VALUES(1, ?, ?, ?, 0, '[]', '', '', '')`, defaultTranscriptionModel, defaultTranscriptionMode, defaultTranscriptionFormat); err != nil {
		return err
	}
	if _, err := execWithRetry(s.db, `UPDATE app_settings SET cleanup_prompt = COALESCE(NULLIF(cleanup_prompt, ''), ?) WHERE id=1`, defaultCleanupPrompt); err != nil {
		return err
	}
	_, err := execWithRetry(s.db, `UPDATE app_settings SET metadata_prompt = COALESCE(NULLIF(metadata_prompt, ''), ?) WHERE id=1`, defaultMetadataPrompt)
	return err
}

func (s *server) getTranscription(filename string) (*transcription, error) {
	var t transcription
	if err := queryRowWithRetry(s.db, func(row *sql.Row) error {
		return scanTranscription(row, &t)
	}, `SELECT id, filename, source_path, processed_path, COALESCE(ingest_source,'') as ingest_source, transcript_text, raw_transcript_text, clean_transcript_text, translation_text, status, last_error, size_bytes, duration_seconds, hash, duplicate_of, requested_model, requested_mode, requested_format, actual_openai_model_used, diarized_json, recognized_towns, normalized_transcript, call_type, call_timestamp, tags, latitude, longitude, location_label, location_source, refined_metadata, address_json, needs_manual_review, created_at, updated_at FROM transcriptions WHERE filename = ?`, filename); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *server) markQueued(filename, sourcePath, source string, size int64, opts TranscriptionOptions, callTime time.Time) error {
	var sizeVal interface{}
	if size > 0 {
		sizeVal = size
	}
	callTimestamp := callTime
	if callTimestamp.IsZero() {
		callTimestamp = time.Now().In(s.tz)
	}
	callTimestamp = callTimestamp.UTC()
	_, err := execWithRetry(s.db, `INSERT INTO transcriptions (filename, source_path, processed_path, ingest_source, status, size_bytes, requested_model, requested_mode, requested_format, call_timestamp) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(filename) DO UPDATE SET status=excluded.status, source_path=excluded.source_path, processed_path=COALESCE(excluded.processed_path, transcriptions.processed_path), ingest_source=COALESCE(excluded.ingest_source, transcriptions.ingest_source), size_bytes=COALESCE(excluded.size_bytes, transcriptions.size_bytes), requested_model=COALESCE(excluded.requested_model, transcriptions.requested_model), requested_mode=COALESCE(excluded.requested_mode, transcriptions.requested_mode), requested_format=COALESCE(excluded.requested_format, transcriptions.requested_format), call_timestamp=COALESCE(transcriptions.call_timestamp, excluded.call_timestamp)`, filename, sourcePath, sourcePath, source, statusQueued, sizeVal, opts.Model, opts.Mode, opts.Format, callTimestamp)
	return err
}

func (s *server) markProcessing(filename, sourcePath, source string, size int64, opts TranscriptionOptions, callTime time.Time) error {
	callTimestamp := callTime
	if callTimestamp.IsZero() {
		callTimestamp = time.Now().In(s.tz)
	}
	callTimestamp = callTimestamp.UTC()
	_, err := execWithRetry(s.db, `INSERT INTO transcriptions (filename, source_path, processed_path, ingest_source, status, size_bytes, requested_model, requested_mode, requested_format, call_timestamp) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(filename) DO UPDATE SET status=excluded.status, source_path=excluded.source_path, processed_path=COALESCE(excluded.processed_path, transcriptions.processed_path), ingest_source=COALESCE(excluded.ingest_source, transcriptions.ingest_source), size_bytes=excluded.size_bytes, requested_model=excluded.requested_model, requested_mode=excluded.requested_mode, requested_format=excluded.requested_format, call_timestamp=COALESCE(transcriptions.call_timestamp, excluded.call_timestamp)`, filename, sourcePath, sourcePath, source, statusProcessing, size, opts.Model, opts.Mode, opts.Format, callTimestamp)
	return err
}

func (s *server) updateProcessedPath(filename, processedPath string) error {
	if strings.TrimSpace(processedPath) == "" {
		return nil
	}
	_, err := execWithRetry(s.db, `UPDATE transcriptions SET processed_path=? WHERE filename=?`, processedPath, filename)
	return err
}

func (s *server) markDoneWithDetails(filename string, note string, raw *string, clean *string, translation *string, duplicateOf *string, diarized *string, towns *string, normalized *string, actualModel *string, callType *string, tags *string, lat *float64, lon *float64, label *string, source *string, metadataJSON *string, addressJSON *string, manualReview bool) error {
	_, err := execWithRetry(s.db, `UPDATE transcriptions SET status=?, transcript_text=?, raw_transcript_text=?, clean_transcript_text=?, translation_text=?, last_error=?, duplicate_of=?, diarized_json=?, recognized_towns=?, normalized_transcript=?, actual_openai_model_used=?, call_type=?, tags=COALESCE(?, tags), latitude=?, longitude=?, location_label=COALESCE(?, location_label), location_source=COALESCE(?, location_source), refined_metadata=COALESCE(?, refined_metadata), address_json=COALESCE(?, address_json), needs_manual_review=? WHERE filename=?`, statusDone, clean, raw, clean, translation, nullableString(note), duplicateOf, diarized, towns, normalized, actualModel, callType, tags, lat, lon, label, source, metadataJSON, addressJSON, boolToInt(manualReview), filename)
	return err
}

func (s *server) markError(filename string, cause error) {
	msg := cause.Error()
	if _, err := execWithRetry(s.db, `UPDATE transcriptions SET status=?, last_error=? WHERE filename=?`, statusError, msg, filename); err != nil {
		log.Printf("failed to mark error: %v", err)
	}
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optionalString(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanTranscription(row rowScanner, t *transcription) error {
	var manual sql.NullInt64
	err := row.Scan(
		&t.ID,
		&t.Filename,
		&t.SourcePath,
		&t.ProcessedPath,
		&t.Source,
		&t.Transcript,
		&t.RawTranscript,
		&t.CleanTranscript,
		&t.Translation,
		&t.Status,
		&t.LastError,
		&t.SizeBytes,
		&t.DurationSeconds,
		&t.Hash,
		&t.DuplicateOf,
		&t.RequestedModel,
		&t.RequestedMode,
		&t.RequestedFormat,
		&t.ActualModel,
		&t.DiarizedJSON,
		&t.RecognizedTowns,
		&t.NormalizedTranscript,
		&t.CallType,
		&t.CallTimestamp,
		&t.TagsJSON,
		&t.Latitude,
		&t.Longitude,
		&t.LocationLabel,
		&t.LocationSource,
		&t.RefinedMetadata,
		&t.AddressJSON,
		&manual,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return err
	}
	t.NeedsManualReview = manual.Valid && manual.Int64 == 1
	return nil
}

func pointerString(s *string) *string {
	if s == nil {
		return nil
	}
	if strings.TrimSpace(*s) == "" {
		return nil
	}
	return s
}

func parseRecognizedTowns(raw *string) []string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(*raw), &arr); err == nil {
		return arr
	}
	parts := strings.Split(*raw, ",")
	var cleaned []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return cleaned
}

func derefString(s *string, fallback string) string {
	if s != nil && *s != "" {
		return *s
	}
	return fallback
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (s *server) updateMetadata(filename string, size int64, duration float64, hash string) error {
	_, err := execWithRetry(s.db, `UPDATE transcriptions SET size_bytes=?, duration_seconds=?, hash=? WHERE filename=?`, size, duration, hash, filename)
	return err
}

func (s *server) findDuplicate(hash string, filename string) string {
	if hash == "" {
		return ""
	}
	var dup string
	if err := queryRowWithRetry(s.db, func(row *sql.Row) error {
		return row.Scan(&dup)
	}, `SELECT filename FROM transcriptions WHERE hash = ? AND filename != ? AND status = ?`, hash, filename, statusDone); err == nil {
		return dup
	}
	return ""
}

func (s *server) copyFromDuplicate(filename, duplicate string) error {
	src, err := s.getTranscription(duplicate)
	if err != nil {
		return err
	}
	_, err = execWithRetry(s.db, `UPDATE transcriptions SET transcript_text=?, raw_transcript_text=?, clean_transcript_text=?, translation_text=?, status=?, last_error=NULL, diarized_json=?, recognized_towns=?, normalized_transcript=?, actual_openai_model_used=?, call_type=?, tags=COALESCE(transcriptions.tags, ?), call_timestamp=COALESCE(transcriptions.call_timestamp, ?), latitude=COALESCE(?, latitude), longitude=COALESCE(?, longitude), location_label=COALESCE(?, location_label), location_source=COALESCE(?, location_source), refined_metadata=COALESCE(?, refined_metadata), address_json=COALESCE(?, address_json), needs_manual_review=? WHERE filename=?`, src.Transcript, src.RawTranscript, src.CleanTranscript, src.Translation, statusDone, src.DiarizedJSON, src.RecognizedTowns, src.NormalizedTranscript, src.ActualModel, src.CallType, src.TagsJSON, src.CallTimestamp, src.Latitude, src.Longitude, src.LocationLabel, src.LocationSource, src.RefinedMetadata, src.AddressJSON, boolToInt(src.NeedsManualReview), filename)
	return err
}

func probeDuration(path string) float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return v
}

func (s *server) storeEmbedding(filename string, embedding []float64) error {
	data, err := json.Marshal(embedding)
	if err != nil {
		return err
	}
	_, err = execWithRetry(s.db, `UPDATE transcriptions SET embedding=? WHERE filename=?`, string(data), filename)
	return err
}

// fireWebhooks sends a standardized, human-readable payload for watcher-triggered completions.
// Schema (stable):
//
//	{
//	  "timestamp_utc": "2025-12-01T20:15:34Z",
//	  "local_datetime": "2025-12-01 15:15:34",
//	  "filename": "xxxxxxx.mp3",
//	  "url": "https://calls.sussexcountyalerts.com/<filename>",
//	  "summary": {"agency": "...", "town": "...", "address": "...", "call_type": "...", "units_dispatched": "..."},
//	  "transcript": {"raw": "...", "normalized": "...", "translated": "...", "recognized_towns": ["..."], "model": "gpt-4o-transcribe-diarize", "mode": "transcribe", "format": "json", "call_type": "Fire"}
//	}
func (s *server) fireWebhooks(j processJob) error {
	settings, err := s.loadSettings()
	if err != nil {
		return err
	}
	if len(settings.WebhookEndpoints) == 0 {
		return nil
	}
	t, err := s.getTranscription(j.filename)
	if err != nil {
		return err
	}

	recognized := parseRecognizedTowns(t.RecognizedTowns)
	normalized := pointerString(t.NormalizedTranscript)
	if normalized == nil {
		normalized = pointerString(t.CleanTranscript)
	}
	if normalized == nil {
		normalized = pointerString(t.Transcript)
	}
	raw := pointerString(t.RawTranscript)
	if raw == nil {
		raw = pointerString(t.Transcript)
	}
	translated := pointerString(t.Translation)

	model := derefString(t.ActualModel, derefString(t.RequestedModel, ""))
	mode := derefString(t.RequestedMode, "")
	format := derefString(t.RequestedFormat, "")
	callTypeVal := pointerString(t.CallType)

	alertTime := time.Now()
	if !j.meta.DateTime.IsZero() {
		alertTime = j.meta.DateTime
	}

	callTime := alertTime
	if t.CallTimestamp != nil {
		callTime = t.CallTimestamp.In(s.tz)
	}

	tags := parseRecognizedTownList(t.TagsJSON)
	if len(tags) == 0 {
		tags = s.buildTags(j.meta, recognized, callTypeVal)
	}
	location := s.locationFromRecord(*t, j.meta)
	if location == nil {
		location = s.deriveLocation(*t, j.meta)
	}

	audioFilename := s.audioFilename(*t)
	listenURL := formatting.BuildListenURL(audioFilename)
	incidentSummary := derefString(normalized, "")
	incident := s.buildIncidentDetails(j.meta, callTypeVal, tags, location, recognized, callTime, audioFilename, listenURL, incidentSummary)

	payload := map[string]interface{}{
		"timestamp_utc":  alertTime.UTC().Format(time.RFC3339),
		"local_datetime": alertTime.In(s.tz).Format("2006-01-02 15:04:05"),
		"filename":       t.Filename,
		"url":            listenURL,
		"preview_image":  s.previewURL(j.baseURL, t.Filename),
		"pretty_title":   j.prettyTitle,
		"alert_message":  formatting.BuildIncidentAlert(incident),
		"metadata": map[string]interface{}{
			"agency":        nullableString(j.meta.AgencyDisplay),
			"town":          nullableString(incident.CityOrTown),
			"county":        nullableString(incident.County),
			"state":         nullableString(incident.State),
			"address_line":  nullableString(incident.AddressLine),
			"cross_street":  nullableString(incident.CrossStreet),
			"call_type":     nullableString(derefString(callTypeVal, j.meta.CallType)),
			"call_category": nullableString(incident.CallCategory),
			"captured":      alertTime.In(s.tz).Format(time.RFC3339),
		},
		"summary": map[string]interface{}{
			"agency":           nullableString(j.meta.AgencyDisplay),
			"town":             nullableString(incident.CityOrTown),
			"county":           nullableString(incident.County),
			"state":            nullableString(incident.State),
			"address":          nullableString(incident.AddressLine),
			"call_type":        callTypeVal,
			"units_dispatched": nil,
		},
		"transcript": map[string]interface{}{
			"raw":              raw,
			"normalized":       normalized,
			"translated":       translated,
			"recognized_towns": recognized,
			"model":            nullableString(model),
			"mode":             nullableString(mode),
			"format":           nullableString(format),
			"call_type":        callTypeVal,
			"call_category":    nullableString(incident.CallCategory),
		},
	}
	buf, _ := json.Marshal(payload)
	for _, endpoint := range settings.WebhookEndpoints {
		req, err := http.NewRequest("POST", endpoint, bytes.NewReader(buf))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
	}
	return nil
}

func (s *server) loadEmbedding(filename string) ([]float64, error) {
	var emb sql.NullString
	if err := queryRowWithRetry(s.db, func(row *sql.Row) error {
		return row.Scan(&emb)
	}, `SELECT embedding FROM transcriptions WHERE filename = ?`, filename); err != nil {
		return nil, err
	}
	return parseEmbedding(emb.String)
}

func parseEmbedding(text string) ([]float64, error) {
	if text == "" {
		return nil, nil
	}
	var arr []float64
	if err := json.Unmarshal([]byte(text), &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Static files live in /static within the binary. See static/index.html for UI.
