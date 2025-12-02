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

	"alert_framework/config"
	"alert_framework/formatting"
	"alert_framework/metrics"
	"alert_framework/queue"
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
		"gpt4-trasncribe":           {"json", "text"},
	}
	allowedExtensions = map[string]struct{}{
		".mp3": {}, ".mp4": {}, ".mpeg": {}, ".mpga": {}, ".m4a": {}, ".wav": {}, ".webm": {},
	}
	sussexTowns          = []string{"Andover", "Byram", "Frankford", "Franklin", "Green", "Hamburg", "Hardyston", "Hopatcong", "Lafayette", "Montague", "Newton", "Ogdensburg", "Sandyston", "Sparta", "Stanhope", "Stillwater", "Sussex", "Vernon", "Wantage", "Fredon", "Branchville"}
	warrenTowns          = []string{"Allamuchy", "Alpha", "Belvidere", "Blairstown", "Franklin", "Frelinghuysen", "Greenwich", "Hackettstown", "Hardwick", "Harmony", "Hope", "Independence", "Knowlton", "Liberty", "Lopatcong", "Mansfield", "Oxford", "Phillipsburg", "Pohatcong", "Washington Boro", "Washington Township", "White"}
	defaultCleanupPrompt = buildCleanupPrompt()
	countyByTown         = buildCountyLookup()
	addressPattern       = regexp.MustCompile(`(?i)(\b\d{3,6}\s+[A-Za-z0-9'\.\s]+\s+(Street|St|Road|Rd|Avenue|Ave|Highway|Hwy|Route|Rt|Lane|Ln|Drive|Dr|Court|Ct|Place|Pl|Way|Pike|Circle|Cir))`)
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
	PreviewImage         string              `json:"preview_image,omitempty"`
	Tags                 []string            `json:"tags,omitempty"`
	Segments             []transcriptSegment `json:"segments,omitempty"`
	Location             *locationGuess      `json:"location,omitempty"`
}

type locationGuess struct {
	Label     string  `json:"label,omitempty"`
	Latitude  float64 `json:"latitude,omitempty"`
	Longitude float64 `json:"longitude,omitempty"`
	Precision string  `json:"precision,omitempty"`
	Source    string  `json:"source,omitempty"`
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

type callListResponse struct {
	Window      string                  `json:"window"`
	Calls       []transcriptionResponse `json:"calls"`
	Stats       callStats               `json:"stats"`
	MapboxToken string                  `json:"mapbox_token,omitempty"`
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
}

type server struct {
	db            *sql.DB
	queue         *queue.Queue
	metrics       *metrics.Metrics
	running       sync.Map // filename -> struct{}
	client        *http.Client
	botID         string
	shutdown      chan struct{}
	cfg           config.Config
	tz            *time.Location
	ctx           context.Context
	locationCache sync.Map
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

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	tz, err := time.LoadLocation("EST5EDT")
	if err != nil {
		log.Printf("falling back to local timezone: %v", err)
		tz = time.Local
	}

	if err := os.MkdirAll(cfg.CallsDir, 0755); err != nil {
		log.Fatalf("failed to ensure calls dir: %v", err)
	}
	if err := os.MkdirAll(cfg.WorkDir, 0755); err != nil {
		log.Fatalf("failed to ensure work dir: %v", err)
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

	s.queue = queue.New(cfg.JobQueueSize, cfg.WorkerCount, time.Duration(cfg.JobTimeoutSec)*time.Second, m)
	s.queue.Start(ctx)
	qStats := s.queue.Stats()
	m.UpdateQueue(qStats.Length, qStats.Capacity, qStats.WorkerCount)

	go s.watch()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/transcriptions", s.handleTranscriptions)
	mux.HandleFunc("/api/transcription/", s.handleTranscription)
	mux.HandleFunc("/api/transcription", s.handleTranscriptionIndex)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/preview/", s.handlePreview)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleReady)
	mux.HandleFunc("/debug/queue", s.handleDebugQueue)
	mux.HandleFunc("/", s.handleRoot)

	server := &http.Server{
		Addr:    cfg.HTTPPort,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		close(s.shutdown)
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s.queue.Stop(ctxTimeout)
		_ = server.Shutdown(ctxTimeout)
	}()

	log.Printf("server listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
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

func openDB(path string) (*sql.DB, error) {
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
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`); err != nil {
		return err
	}
	if _, err := execWithRetry(db, `INSERT OR IGNORE INTO app_settings(id, default_model, default_mode, default_format, auto_translate, webhook_endpoints, cleanup_prompt) VALUES(1, 'gpt-4o-transcribe', 'transcribe', 'json', 0, '[]', '');`); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "app_settings", "cleanup_prompt", "TEXT"); err != nil {
		return err
	}
	if _, err := execWithRetry(db, `UPDATE app_settings SET cleanup_prompt = ? WHERE id = 1 AND (cleanup_prompt IS NULL OR cleanup_prompt = '')`, defaultCleanupPrompt); err != nil {
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
	meta, pretty, publicURL, _ := s.buildJobContext(filename)
	text := formatting.BuildAlertMessage(meta, pretty, publicURL)
	if err := s.sendGroupMe(text); err != nil {
		log.Printf("groupme send failed: %v", err)
	}
	opts, _ := s.defaultOptions()
	s.queueJob("watcher", filename, true, false, opts)
}

func (s *server) queueJob(source, filename string, sendGroupMe bool, force bool, opts TranscriptionOptions) bool {
	enqueued, _ := s.enqueueWithBackoff(context.Background(), source, filename, sendGroupMe, force, opts)
	return enqueued
}

func (s *server) enqueueWithBackoff(ctx context.Context, source, filename string, sendGroupMe bool, force bool, opts TranscriptionOptions) (bool, bool) {
	if _, exists := s.running.LoadOrStore(filename, struct{}{}); exists && !force {
		return false, false
	}
	meta, pretty, publicURL, baseURL := s.buildJobContext(filename)
	sourcePath := filepath.Join(s.cfg.CallsDir, filename)
	if err := s.markQueued(filename, sourcePath, source, 0, opts, meta.DateTime); err != nil {
		log.Printf("mark queued failed for %s: %v", filename, err)
	}
	job := queue.Job{
		ID:       filename,
		FileName: filename,
		Source:   source,
		Work: func(ctx context.Context) error {
			return s.processFile(ctx, processJob{filename: filename, source: source, sendGroupMe: sendGroupMe, force: force, options: opts, meta: meta, prettyTitle: pretty, publicURL: publicURL, baseURL: baseURL})
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
	return meta, pretty, s.publicURL(base, filename), base
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
		s.markDoneWithDetails(filename, note, nil, nil, nil, &dup, nil, nil, nil, nil, nil, nil)
		if j.sendGroupMe {
			followup := fmt.Sprintf("%s transcript is duplicate of %s", filename, dup)
			_ = s.sendGroupMe(followup)
		}
		return nil
	}

	stagedPath := filepath.Join(s.cfg.WorkDir, filename)
	if err := copyFile(sourcePath, stagedPath); err != nil {
		s.markError(filename, err)
		status = err.Error()
		decodeDur = time.Since(decodeStart)
		return err
	}
	decodeDur = time.Since(decodeStart)

	transcribeStart := time.Now()
	rawTranscript, cleanedTranscript, translation, embedding, diarized, towns, normalized, actualModel, callType, err := s.multiPassTranscription(stagedPath, j.options)
	if err != nil {
		s.markError(filename, err)
		status = err.Error()
		transcribeDur = time.Since(transcribeStart)
		return err
	}
	transcribeDur = time.Since(transcribeStart)
	if callType == nil && existingEntry != nil && existingEntry.CallType != nil {
		callType = existingEntry.CallType
	}
	if callType == nil && j.meta.CallType != "" {
		ct := j.meta.CallType
		callType = &ct
	}

	recognized := parseRecognizedTowns(towns)
	tagsList := s.buildTags(j.meta, recognized, callType)
	var tagsJSON *string
	if data, err := json.Marshal(tagsList); err == nil {
		str := string(data)
		tagsJSON = &str
	}

	if err := s.markDoneWithDetails(filename, "", &rawTranscript, &cleanedTranscript, translation, nil, diarized, towns, normalized, actualModel, callType, tagsJSON); err != nil {
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
		followup := fmt.Sprintf("%s transcript:\n%s", j.prettyTitle, cleanedTranscript)
		if err := s.sendGroupMe(followup); err != nil {
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

func (s *server) multiPassTranscription(path string, opts TranscriptionOptions) (string, string, *string, []float64, *string, *string, *string, *string, *string, error) {
	raw, diarized, actualModel, err := s.callOpenAIWithRetries(path, opts)
	if err != nil {
		return "", "", nil, nil, nil, nil, nil, nil, nil, err
	}
	cleaned := raw
	normalized := (*string)(nil)
	towns := (*string)(nil)
	if c, n, t, err := s.domainCleanup(raw); err == nil {
		if c != "" {
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
	if c, err := s.enhanceTranscript(cleaned); err == nil && c != "" {
		cleaned = c
	}

	var translation *string
	if opts.Mode == "translate" || opts.AutoTranslate {
		if t, err := s.translateTranscript(cleaned); err == nil && t != "" {
			translation = &t
		}
	}

	emb, _ := s.embedTranscript(cleaned)
	ct, _ := s.classifyCallType(cleaned)
	return raw, cleaned, translation, emb, diarized, towns, normalized, actualModel, ct, nil
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
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if !s.queue.Healthy() {
		http.Error(w, "queue not ready", http.StatusServiceUnavailable)
		return
	}
	if stats := s.queue.Stats(); stats.WorkerCount <= 0 {
		http.Error(w, "no workers", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

func (s *server) handleDebugQueue(w http.ResponseWriter, r *http.Request) {
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
		filename := r.URL.Query().Get("filename")
		if filename == "" {
			http.Error(w, "filename required", http.StatusBadRequest)
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
			s.queueJob("api", cleaned, false, true, opts)
			respondJSON(w, map[string]interface{}{
				"filename": existing.Filename,
				"status":   statusQueued,
			})
			return
		}
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

	windowName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("window")))
	if windowName == "" {
		windowName = "24h"
	}
	windowDuration := 24 * time.Hour
	switch windowName {
	case "7d", "7days", "week":
		windowDuration = 7 * 24 * time.Hour
		windowName = "7d"
	case "30d", "30days", "month":
		windowDuration = 30 * 24 * time.Hour
		windowName = "30d"
	default:
		windowName = "24h"
	}

	cutoff := time.Now().Add(-windowDuration)

	base := `SELECT id, filename, source_path, COALESCE(ingest_source,'') as ingest_source, transcript_text, raw_transcript_text, clean_transcript_text, translation_text, status, last_error, size_bytes, duration_seconds, hash, duplicate_of, requested_model, requested_mode, requested_format, actual_openai_model_used, diarized_json, recognized_towns, normalized_transcript, call_type, call_timestamp, tags, created_at, updated_at FROM transcriptions`
	where := []string{"COALESCE(call_timestamp, created_at) >= ?"}
	args := []interface{}{cutoff}
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
		if err := rows.Scan(&t.ID, &t.Filename, &t.SourcePath, &t.Source, &t.Transcript, &t.RawTranscript, &t.CleanTranscript, &t.Translation, &t.Status, &t.LastError, &t.SizeBytes, &t.DurationSeconds, &t.Hash, &t.DuplicateOf, &t.RequestedModel, &t.RequestedMode, &t.RequestedFormat, &t.ActualModel, &t.DiarizedJSON, &t.RecognizedTowns, &t.NormalizedTranscript, &t.CallType, &t.CallTimestamp, &t.TagsJSON, &t.CreatedAt, &t.UpdatedAt); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		calls = append(calls, s.toResponse(t, baseURL))
	}

	filtered := make([]transcriptionResponse, 0, len(calls))
	stats := callStats{StatusCounts: make(map[string]int), CallTypeCounts: make(map[string]int), TagCounts: make(map[string]int), AgencyCounts: make(map[string]int), TownCounts: make(map[string]int), AvailableWindow: windowName}

	for _, call := range calls {
		if call.CallTimestamp.Before(cutoff) {
			continue
		}
		if statusFilter != "" && call.Status != statusFilter {
			continue
		}
		if len(tagFilter) > 0 && !hasTags(call.Tags, tagFilter) {
			continue
		}
		filtered = append(filtered, call)
		stats.Total++
		stats.StatusCounts[call.Status]++
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
	pretty := formatting.FormatPrettyTitle(t.Filename, callTime, s.tz)
	tags := parseRecognizedTownList(t.TagsJSON)
	if len(tags) == 0 {
		tags = s.buildTags(meta, recognized, callType)
	}
	tags = stripVolunteerTags(tags)

	location := s.deriveLocation(t, meta)

	return transcriptionResponse{
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
		AudioURL:             s.publicURL(baseURL, t.Filename),
		PreviewImage:         s.previewURL(baseURL, t.Filename),
		Tags:                 tags,
		Segments:             s.buildSegments(t),
		Location:             location,
	}
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
	}
	if meta.TownDisplay != "" {
		add(fmt.Sprintf("%s, NJ", meta.TownDisplay))
	}
	if meta.AgencyDisplay != "" {
		add(fmt.Sprintf("%s, NJ", meta.AgencyDisplay))
	}
	add("Sussex County, NJ")

	return candidates
}

func (s *server) geocodeWithMapbox(ctx context.Context, token, query string) *locationGuess {
	encoded := url.PathEscape(query)
	endpoint := fmt.Sprintf("https://api.mapbox.com/geocoding/v5/mapbox.places/%s.json?access_token=%s&autocomplete=true&limit=1&country=US&language=en&proximity=-74.696,41.05", encoded, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		log.Printf("mapbox request build failed: %v", err)
		return nil
	}
	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("mapbox request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("mapbox response %d for %s", resp.StatusCode, query)
		return nil
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
		return nil
	}
	if len(payload.Features) == 0 {
		return nil
	}
	feature := payload.Features[0]
	if len(feature.Center) < 2 {
		return nil
	}

	precision := ""
	if len(feature.PlaceType) > 0 {
		precision = feature.PlaceType[0]
	}

	return &locationGuess{
		Label:     feature.PlaceName,
		Latitude:  feature.Center[1],
		Longitude: feature.Center[0],
		Precision: precision,
		Source:    query,
	}
}

func (s *server) loadSettings() (AppSettings, error) {
	var settings AppSettings
	var auto sql.NullInt64
	var webhooks sql.NullString
	var defaultModel, defaultMode, defaultFormat sql.NullString
	var preferredLanguage, cleanupPrompt sql.NullString
	if err := queryRowWithRetry(s.db, func(row *sql.Row) error {
		return row.Scan(&defaultModel, &defaultMode, &defaultFormat, &auto, &webhooks, &preferredLanguage, &cleanupPrompt)
	}, `SELECT default_model, default_mode, default_format, auto_translate, webhook_endpoints, preferred_language, cleanup_prompt FROM app_settings WHERE id=1`); err != nil {
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
	case "gpt4-trasncribe", "gpt4-transcribe", "gpt-4-transcribe", "gpt4o-transcribe":
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
	hooks, _ := json.Marshal(settings.WebhookEndpoints)
	auto := 0
	if settings.AutoTranslate {
		auto = 1
	}
	res, err := execWithRetry(s.db, `UPDATE app_settings SET default_model=?, default_mode=?, default_format=?, auto_translate=?, webhook_endpoints=?, preferred_language=?, cleanup_prompt=?, updated_at=CURRENT_TIMESTAMP WHERE id=1`, settings.DefaultModel, settings.DefaultMode, settings.DefaultFormat, auto, string(hooks), settings.PreferredLanguage, settings.CleanupPrompt)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		_, err = execWithRetry(s.db, `INSERT OR REPLACE INTO app_settings(id, default_model, default_mode, default_format, auto_translate, webhook_endpoints, preferred_language, cleanup_prompt, updated_at) VALUES(1, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`, settings.DefaultModel, settings.DefaultMode, settings.DefaultFormat, auto, string(hooks), settings.PreferredLanguage, settings.CleanupPrompt)
	}
	return err
}

func (s *server) ensureSettingsRow() error {
	if _, err := execWithRetry(s.db, `INSERT OR IGNORE INTO app_settings(id, default_model, default_mode, default_format, auto_translate, webhook_endpoints, preferred_language, cleanup_prompt) VALUES(1, ?, ?, ?, 0, '[]', '', '')`, defaultTranscriptionModel, defaultTranscriptionMode, defaultTranscriptionFormat); err != nil {
		return err
	}
	_, err := execWithRetry(s.db, `UPDATE app_settings SET cleanup_prompt = COALESCE(NULLIF(cleanup_prompt, ''), ?) WHERE id=1`, defaultCleanupPrompt)
	return err
}

func (s *server) getTranscription(filename string) (*transcription, error) {
	var t transcription
	if err := queryRowWithRetry(s.db, func(row *sql.Row) error {
		return row.Scan(&t.ID, &t.Filename, &t.SourcePath, &t.Source, &t.Transcript, &t.RawTranscript, &t.CleanTranscript, &t.Translation, &t.Status, &t.LastError, &t.SizeBytes, &t.DurationSeconds, &t.Hash, &t.DuplicateOf, &t.RequestedModel, &t.RequestedMode, &t.RequestedFormat, &t.ActualModel, &t.DiarizedJSON, &t.RecognizedTowns, &t.NormalizedTranscript, &t.CallType, &t.CallTimestamp, &t.TagsJSON, &t.CreatedAt, &t.UpdatedAt)
	}, `SELECT id, filename, source_path, COALESCE(ingest_source,'') as ingest_source, transcript_text, raw_transcript_text, clean_transcript_text, translation_text, status, last_error, size_bytes, duration_seconds, hash, duplicate_of, requested_model, requested_mode, requested_format, actual_openai_model_used, diarized_json, recognized_towns, normalized_transcript, call_type, call_timestamp, tags, created_at, updated_at FROM transcriptions WHERE filename = ?`, filename); err != nil {
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
	_, err := execWithRetry(s.db, `INSERT INTO transcriptions (filename, source_path, ingest_source, status, size_bytes, requested_model, requested_mode, requested_format, call_timestamp) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(filename) DO UPDATE SET status=excluded.status, source_path=excluded.source_path, ingest_source=COALESCE(excluded.ingest_source, transcriptions.ingest_source), size_bytes=COALESCE(excluded.size_bytes, transcriptions.size_bytes), requested_model=COALESCE(excluded.requested_model, transcriptions.requested_model), requested_mode=COALESCE(excluded.requested_mode, transcriptions.requested_mode), requested_format=COALESCE(excluded.requested_format, transcriptions.requested_format), call_timestamp=COALESCE(transcriptions.call_timestamp, excluded.call_timestamp)`, filename, sourcePath, source, statusQueued, sizeVal, opts.Model, opts.Mode, opts.Format, callTimestamp)
	return err
}

func (s *server) markProcessing(filename, sourcePath, source string, size int64, opts TranscriptionOptions, callTime time.Time) error {
	callTimestamp := callTime
	if callTimestamp.IsZero() {
		callTimestamp = time.Now().In(s.tz)
	}
	_, err := execWithRetry(s.db, `INSERT INTO transcriptions (filename, source_path, ingest_source, status, size_bytes, requested_model, requested_mode, requested_format, call_timestamp) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(filename) DO UPDATE SET status=excluded.status, source_path=excluded.source_path, ingest_source=COALESCE(excluded.ingest_source, transcriptions.ingest_source), size_bytes=excluded.size_bytes, requested_model=excluded.requested_model, requested_mode=excluded.requested_mode, requested_format=excluded.requested_format, call_timestamp=COALESCE(transcriptions.call_timestamp, excluded.call_timestamp)`, filename, sourcePath, source, statusProcessing, size, opts.Model, opts.Mode, opts.Format, callTimestamp)
	return err
}

func (s *server) markDoneWithDetails(filename string, note string, raw *string, clean *string, translation *string, duplicateOf *string, diarized *string, towns *string, normalized *string, actualModel *string, callType *string, tags *string) error {
	_, err := execWithRetry(s.db, `UPDATE transcriptions SET status=?, transcript_text=?, raw_transcript_text=?, clean_transcript_text=?, translation_text=?, last_error=?, duplicate_of=?, diarized_json=?, recognized_towns=?, normalized_transcript=?, actual_openai_model_used=?, call_type=?, tags=COALESCE(?, tags) WHERE filename=?`, statusDone, clean, raw, clean, translation, nullableString(note), duplicateOf, diarized, towns, normalized, actualModel, callType, tags, filename)
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
	_, err = execWithRetry(s.db, `UPDATE transcriptions SET transcript_text=?, raw_transcript_text=?, clean_transcript_text=?, translation_text=?, status=?, last_error=NULL, diarized_json=?, recognized_towns=?, normalized_transcript=?, actual_openai_model_used=?, call_type=?, tags=COALESCE(transcriptions.tags, ?), call_timestamp=COALESCE(transcriptions.call_timestamp, ?) WHERE filename=?`, src.Transcript, src.RawTranscript, src.CleanTranscript, src.Translation, statusDone, src.DiarizedJSON, src.RecognizedTowns, src.NormalizedTranscript, src.ActualModel, src.CallType, src.TagsJSON, src.CallTimestamp, filename)
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

	var town *string
	if len(recognized) > 0 {
		town = &recognized[0]
	}
	if town == nil && j.meta.TownDisplay != "" {
		summaryTown := j.meta.TownDisplay
		town = &summaryTown
	}

	alertTime := time.Now()
	if !j.meta.DateTime.IsZero() {
		alertTime = j.meta.DateTime
	}

	payload := map[string]interface{}{
		"timestamp_utc":  alertTime.UTC().Format(time.RFC3339),
		"local_datetime": alertTime.In(s.tz).Format("2006-01-02 15:04:05"),
		"filename":       t.Filename,
		"url":            j.publicURL,
		"preview_image":  s.previewURL(j.baseURL, t.Filename),
		"pretty_title":   j.prettyTitle,
		"alert_message":  formatting.BuildAlertMessage(j.meta, j.prettyTitle, j.publicURL),
		"metadata": map[string]interface{}{
			"agency":    nullableString(j.meta.AgencyDisplay),
			"town":      nullableString(j.meta.TownDisplay),
			"call_type": nullableString(j.meta.CallType),
			"captured":  alertTime.In(s.tz).Format(time.RFC3339),
		},
		"summary": map[string]interface{}{
			"agency":           nullableString(j.meta.AgencyDisplay),
			"town":             town,
			"address":          nil,
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
