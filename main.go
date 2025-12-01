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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	_ "modernc.org/sqlite"
)

//go:embed static/*
var embeddedStatic embed.FS

const (
	callsDir   = "/home/peebs/calls"
	workDir    = "/home/peebs/ai_transcribe"
	dbPath     = workDir + "/transcriptions.db"
	groupmeURL = "https://api.groupme.com/v3/bots/post"
	defaultBot = "03926cdc985a046b27d6393ba6"
)

var (
	allowedFormats = map[string][]string{
		"whisper-1":                 {"json", "text", "srt", "verbose_json", "vtt"},
		"gpt-4o-mini-transcribe":    {"json", "text"},
		"gpt-4o-transcribe":         {"json", "text"},
		"gpt-4o-transcribe-diarize": {"json", "text", "diarized_json"},
	}
	allowedExtensions = map[string]struct{}{
		".mp3": {}, ".mp4": {}, ".mpeg": {}, ".mpga": {}, ".m4a": {}, ".wav": {}, ".webm": {},
	}
	sussexTowns = []string{"Andover", "Byram", "Frankford", "Franklin", "Green", "Hamburg", "Hardyston", "Hopatcong", "Lafayette", "Montague", "Newton", "Ogdensburg", "Sandyston", "Sparta", "Stanhope", "Stillwater", "Sussex", "Vernon", "Wantage", "Fredon", "Branchville"}
)

// transcription statuses
const (
	statusQueued     = "queued"
	statusProcessing = "processing"
	statusDone       = "done"
	statusError      = "error"
)

// DTOs

type transcription struct {
	ID                   int64     `json:"id"`
	Filename             string    `json:"filename"`
	SourcePath           string    `json:"source_path"`
	Transcript           *string   `json:"transcript_text"`
	RawTranscript        *string   `json:"raw_transcript_text"`
	CleanTranscript      *string   `json:"clean_transcript_text"`
	Translation          *string   `json:"translation_text"`
	Status               string    `json:"status"`
	LastError            *string   `json:"last_error"`
	SizeBytes            *int64    `json:"size_bytes"`
	DurationSeconds      *float64  `json:"duration_seconds"`
	Hash                 *string   `json:"hash"`
	DuplicateOf          *string   `json:"duplicate_of"`
	RequestedModel       *string   `json:"requested_model"`
	RequestedMode        *string   `json:"requested_mode"`
	RequestedFormat      *string   `json:"requested_format"`
	ActualModel          *string   `json:"actual_openai_model_used"`
	DiarizedJSON         *string   `json:"diarized_json"`
	RecognizedTowns      *string   `json:"recognized_towns"`
	NormalizedTranscript *string   `json:"normalized_transcript"`
	CallType             *string   `json:"call_type"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	Similar              []similar `json:"similar,omitempty"`
}

type similar struct {
	Filename string  `json:"filename"`
	Score    float64 `json:"score"`
}

type job struct {
	filename    string
	sendGroupMe bool
	force       bool
	options     TranscriptionOptions
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
}

type server struct {
	db          *sql.DB
	jobs        chan job
	running     sync.Map // filename -> struct{}
	client      *http.Client
	botID       string
	workerCount int
	shutdown    chan struct{}
}

func (s *server) defaultOptions() (TranscriptionOptions, error) {
	settings, err := s.loadSettings()
	if err != nil {
		return TranscriptionOptions{Model: "gpt-4o-transcribe", Mode: "transcribe", Format: "json"}, err
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
	if err := os.MkdirAll(workDir, 0755); err != nil {
		log.Fatalf("failed to ensure work dir: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	if err := initDB(db); err != nil {
		log.Fatalf("init db: %v", err)
	}

	workerCount := 4
	if v := os.Getenv("WORKER_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			workerCount = n
		}
	}

	s := &server{
		db:          db,
		jobs:        make(chan job, 200),
		client:      &http.Client{Timeout: 180 * time.Second},
		botID:       getBotID(),
		workerCount: workerCount,
		shutdown:    make(chan struct{}),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	for i := 0; i < s.workerCount; i++ {
		go s.worker()
	}
	go s.watch()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/transcriptions", s.handleTranscriptions)
	mux.HandleFunc("/api/transcription/", s.handleTranscription)
	mux.HandleFunc("/api/transcription", s.handleTranscriptionIndex)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/", s.handleRoot)

	server := &http.Server{
		Addr:    ":" + getPort(),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		close(s.shutdown)
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctxTimeout)
	}()

	log.Printf("server listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

func getPort() string {
	if v := os.Getenv("PORT"); v != "" {
		return v
	}
	return "8000"
}

func getBotID() string {
	if v := os.Getenv("GROUPME_BOT_ID"); v != "" {
		return v
	}
	return defaultBot
}

func initDB(db *sql.DB) error {
	schema := `CREATE TABLE IF NOT EXISTS transcriptions (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        filename TEXT UNIQUE NOT NULL,
        source_path TEXT NOT NULL,
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
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );
    CREATE TRIGGER IF NOT EXISTS transcriptions_updated_at
    AFTER UPDATE ON transcriptions
    BEGIN
        UPDATE transcriptions SET updated_at = CURRENT_TIMESTAMP WHERE id = old.id;
    END;`
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	// legacy compatibility: ensure columns exist
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
	rows, err := db.Query("PRAGMA table_info(transcriptions);")
	if err != nil {
		return err
	}
	defer rows.Close()
	existing := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		existing[name] = struct{}{}
	}
	for col, colType := range needed {
		if _, ok := existing[col]; !ok {
			stmt := fmt.Sprintf("ALTER TABLE transcriptions ADD COLUMN %s %s", col, colType)
			if _, err := db.Exec(stmt); err != nil {
				return err
			}
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS app_settings (
        id INTEGER PRIMARY KEY CHECK (id = 1),
        default_model TEXT,
        default_mode TEXT,
        default_format TEXT,
        auto_translate INTEGER DEFAULT 0,
        webhook_endpoints TEXT,
        preferred_language TEXT,
        updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );
    INSERT OR IGNORE INTO app_settings(id, default_model, default_mode, default_format, auto_translate, webhook_endpoints) VALUES(1, 'gpt-4o-transcribe', 'transcribe', 'json', 0, '[]');
    `); err != nil {
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

	if err := watcher.Add(callsDir); err != nil {
		log.Fatalf("watch add: %v", err)
	}

	log.Printf("watching %s for new files", callsDir)
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
	pretty := s.runPretty(filename)
	publicURL := fmt.Sprintf("https://calls.sussexcountyalerts.com/%s", url.PathEscape(filename))
	text := fmt.Sprintf("%s - %s", pretty, publicURL)
	if err := s.sendGroupMe(text); err != nil {
		log.Printf("groupme send failed: %v", err)
	}
	opts, _ := s.defaultOptions()
	s.queueJob(filename, true, false, opts)
}

func (s *server) runPretty(filename string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pretty.sh", filename)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("pretty.sh failed: %v", err)
		return filename
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return filename
	}
	return trimmed
}

func (s *server) queueJob(filename string, sendGroupMe bool, force bool, opts TranscriptionOptions) {
	if _, exists := s.running.LoadOrStore(filename, struct{}{}); exists && !force {
		return
	}
	select {
	case s.jobs <- job{filename: filename, sendGroupMe: sendGroupMe, force: force, options: opts}:
	default:
		log.Printf("job queue full, dropping job for %s", filename)
		s.running.Delete(filename)
	}
}

func (s *server) worker() {
	for j := range s.jobs {
		if err := s.processFile(j); err != nil {
			log.Printf("process %s error: %v", j.filename, err)
		}
		s.running.Delete(j.filename)
	}
}

func (s *server) processFile(j job) error {
	filename := j.filename
	sourcePath := filepath.Join(callsDir, filename)
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	var existingEntry *transcription
	if existing, err := s.getTranscription(filename); err == nil {
		existingEntry = existing
		if !j.force {
			if existing.Status == statusDone || existing.Status == statusProcessing {
				return nil
			}
		}
	}
	if err := s.markProcessing(filename, sourcePath, info.Size(), j.options); err != nil {
		return err
	}
	if err := waitForStableSize(sourcePath, info.Size(), 2*time.Second, 2); err != nil {
		s.markError(filename, err)
		return err
	}

	duration := probeDuration(sourcePath)
	hashValue, err := hashFile(sourcePath)
	if err != nil {
		s.markError(filename, err)
		return err
	}

	if err := s.updateMetadata(filename, info.Size(), duration, hashValue); err != nil {
		log.Printf("metadata update failed: %v", err)
	}

	if dup := s.findDuplicate(hashValue, filename); dup != "" {
		// copy transcript data from duplicate
		if err := s.copyFromDuplicate(filename, dup); err != nil {
			log.Printf("failed to mirror duplicate data: %v", err)
		}
		note := fmt.Sprintf("duplicate of %s", dup)
		s.markDoneWithDetails(filename, note, nil, nil, nil, &dup, nil, nil, nil, nil, nil)
		if j.sendGroupMe {
			followup := fmt.Sprintf("%s transcript is duplicate of %s", filename, dup)
			_ = s.sendGroupMe(followup)
		}
		return nil
	}

	stagedPath := filepath.Join(workDir, filename)
	if err := copyFile(sourcePath, stagedPath); err != nil {
		s.markError(filename, err)
		return err
	}

	rawTranscript, cleanedTranscript, translation, embedding, diarized, towns, normalized, actualModel, callType, err := s.multiPassTranscription(stagedPath, j.options)
	if err != nil {
		s.markError(filename, err)
		return err
	}
	if callType == nil && existingEntry != nil && existingEntry.CallType != nil {
		callType = existingEntry.CallType
	}

	if err := s.markDoneWithDetails(filename, "", &rawTranscript, &cleanedTranscript, translation, nil, diarized, towns, normalized, actualModel, callType); err != nil {
		return err
	}
	if len(embedding) > 0 {
		if err := s.storeEmbedding(filename, embedding); err != nil {
			log.Printf("store embedding: %v", err)
		}
	}
	if err := s.fireWebhooks(filename); err != nil {
		log.Printf("webhook error: %v", err)
	}
	if j.sendGroupMe {
		followup := fmt.Sprintf("%s transcript:\n%s", filename, cleanedTranscript)
		if err := s.sendGroupMe(followup); err != nil {
			log.Printf("groupme follow-up failed: %v", err)
		}
	}
	return nil
}

func waitForStableSize(path string, initial int64, interval time.Duration, required int) error {
	if initial <= 0 {
		time.Sleep(interval)
	}
	var last int64 = -1
	stable := 0
	for {
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
	chunks, err := chunkFile(path, 15*1024*1024)
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
	prompt := fmt.Sprintf("You are cleaning emergency radio transcripts for Sussex County, NJ. Normalize spelling, fix misheard Sussex County town names to the closest from this list: %s. Return JSON with fields normalized_transcript and recognized_towns (array). Maintain meaning.", strings.Join(sussexTowns, ", "))
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
	return cleaned, cleaned, result.RecognizedTowns, nil
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

func chunkFile(path string, maxBytes int64) ([]string, error) {
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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
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
	sourcePath := filepath.Join(callsDir, cleaned)
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
		s.queueJob(filename, false, true, opts)
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
	case len(parts) == 2 && parts[1] == "retranscribe" && r.Method == http.MethodPost:
		s.queueJob(filename, false, true, opts)
		respondJSON(w, map[string]string{"status": statusQueued, "filename": filename})
		return
	case len(parts) == 2 && parts[1] == "similar" && r.Method == http.MethodGet:
		s.handleSimilar(w, r, filename)
		return
	case len(parts) == 2 && parts[1] == "download" && r.Method == http.MethodGet:
		s.handleTranscriptDownload(w, r, filename)
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
	sourcePath := filepath.Join(callsDir, cleaned)
	if _, err := os.Stat(sourcePath); err != nil {
		http.NotFound(w, r)
		return
	}

	existing, err := s.getTranscription(cleaned)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	if existing != nil {
		switch existing.Status {
		case statusDone:
			respondJSON(w, map[string]interface{}{
				"filename":              existing.Filename,
				"status":                existing.Status,
				"transcript_text":       pickTranscript(existing),
				"raw_transcript_text":   existing.RawTranscript,
				"clean_transcript_text": existing.CleanTranscript,
				"translation_text":      existing.Translation,
				"requested_model":       existing.RequestedModel,
				"requested_mode":        existing.RequestedMode,
				"requested_format":      existing.RequestedFormat,
				"actual_model":          existing.ActualModel,
				"diarized_json":         existing.DiarizedJSON,
				"recognized_towns":      existing.RecognizedTowns,
				"normalized_transcript": existing.NormalizedTranscript,
				"call_type":             existing.CallType,
				"size_bytes":            existing.SizeBytes,
				"duration_seconds":      existing.DurationSeconds,
				"hash":                  existing.Hash,
				"duplicate_of":          existing.DuplicateOf,
				"last_error":            existing.LastError,
			})
			return
		case statusProcessing:
			respondJSON(w, map[string]interface{}{
				"filename": existing.Filename,
				"status":   existing.Status,
			})
			return
		case statusError:
			s.queueJob(cleaned, false, true, opts)
			respondJSON(w, map[string]interface{}{
				"filename": existing.Filename,
				"status":   statusQueued,
			})
			return
		}
	}

	s.queueJob(cleaned, false, true, opts)
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

func (s *server) handleTranscriptDownload(w http.ResponseWriter, r *http.Request, filename string) {
	t, err := s.getTranscription(filename)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	transcript := pickTranscript(t)
	if transcript == nil {
		http.Error(w, "transcript missing", http.StatusNotFound)
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "txt"
	}
	switch format {
	case "txt":
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.txt\"", t.Filename))
		_, _ = w.Write([]byte(*transcript))
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.json\"", t.Filename))
		respondJSON(w, map[string]interface{}{"filename": t.Filename, "transcript": transcript})
	case "srt":
		w.Header().Set("Content-Type", "application/x-subrip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.srt\"", t.Filename))
		srt := buildSimpleSRT(*transcript)
		_, _ = w.Write([]byte(srt))
	default:
		http.Error(w, "unsupported format", http.StatusBadRequest)
	}
}

func buildSimpleSRT(text string) string {
	lines := strings.Split(text, ". ")
	var sb strings.Builder
	for i, line := range lines {
		start := time.Duration(i) * 3 * time.Second
		end := start + 3*time.Second
		sb.WriteString(fmt.Sprintf("%d\n%02d:%02d:%02d,000 --> %02d:%02d:%02d,000\n%s\n\n", i+1,
			int(start.Hours()), int(start.Minutes())%60, int(start.Seconds())%60,
			int(end.Hours()), int(end.Minutes())%60, int(end.Seconds())%60,
			strings.TrimSpace(line)))
	}
	return sb.String()
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

	rows, err := s.db.Query(`SELECT filename, embedding FROM transcriptions WHERE filename != ? AND embedding IS NOT NULL`, filename)
	if err != nil {
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
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	statusFilter := r.URL.Query().Get("status")
	townFilter := strings.TrimSpace(r.URL.Query().Get("town"))
	callTypeFilter := strings.TrimSpace(r.URL.Query().Get("call_type"))
	sortBy := r.URL.Query().Get("sort")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	base := `SELECT id, filename, source_path, transcript_text, raw_transcript_text, clean_transcript_text, translation_text, status, last_error, size_bytes, duration_seconds, hash, duplicate_of, requested_model, requested_mode, requested_format, actual_openai_model_used, diarized_json, recognized_towns, normalized_transcript, call_type, created_at, updated_at FROM transcriptions`
	var where []string
	var args []interface{}
	if search != "" {
		like := "%" + strings.ToLower(search) + "%"
		where = append(where, "(lower(filename) LIKE ? OR lower(coalesce(clean_transcript_text, transcript_text, '')) LIKE ? OR lower(coalesce(raw_transcript_text, '')) LIKE ? OR lower(coalesce(normalized_transcript, '')) LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if statusFilter != "" {
		where = append(where, "status = ?")
		args = append(args, statusFilter)
	}
	if townFilter != "" {
		where = append(where, "recognized_towns LIKE ?")
		args = append(args, "%"+strings.ToLower(townFilter)+"%")
	}
	if callTypeFilter != "" {
		where = append(where, "lower(coalesce(call_type,'')) = ?")
		args = append(args, strings.ToLower(callTypeFilter))
	}
	if len(where) > 0 {
		base += " WHERE " + strings.Join(where, " AND ")
	}
	switch sortBy {
	case "size":
		base += " ORDER BY size_bytes DESC NULLS LAST"
	case "status":
		base += " ORDER BY status ASC, updated_at DESC"
	default:
		base += " ORDER BY updated_at DESC"
	}
	base += " LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset)

	rows, err := s.db.Query(base, args...)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []transcription
	for rows.Next() {
		var t transcription
		if err := rows.Scan(&t.ID, &t.Filename, &t.SourcePath, &t.Transcript, &t.RawTranscript, &t.CleanTranscript, &t.Translation, &t.Status, &t.LastError, &t.SizeBytes, &t.DurationSeconds, &t.Hash, &t.DuplicateOf, &t.RequestedModel, &t.RequestedMode, &t.RequestedFormat, &t.ActualModel, &t.DiarizedJSON, &t.RecognizedTowns, &t.NormalizedTranscript, &t.CallType, &t.CreatedAt, &t.UpdatedAt); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		result = append(result, t)
	}
	respondJSON(w, result)
}

func respondJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) loadSettings() (AppSettings, error) {
	row := s.db.QueryRow(`SELECT default_model, default_mode, default_format, auto_translate, webhook_endpoints, preferred_language FROM app_settings WHERE id=1`)
	var settings AppSettings
	var auto int
	var webhooks string
	if err := row.Scan(&settings.DefaultModel, &settings.DefaultMode, &settings.DefaultFormat, &auto, &webhooks, &settings.PreferredLanguage); err != nil {
		return settings, err
	}
	settings.AutoTranslate = auto == 1
	if webhooks != "" {
		_ = json.Unmarshal([]byte(webhooks), &settings.WebhookEndpoints)
	}
	if settings.WebhookEndpoints == nil {
		settings.WebhookEndpoints = []string{}
	}
	return settings, nil
}

func (s *server) saveSettings(settings AppSettings) error {
	hooks, _ := json.Marshal(settings.WebhookEndpoints)
	auto := 0
	if settings.AutoTranslate {
		auto = 1
	}
	_, err := s.db.Exec(`UPDATE app_settings SET default_model=?, default_mode=?, default_format=?, auto_translate=?, webhook_endpoints=?, preferred_language=?, updated_at=CURRENT_TIMESTAMP WHERE id=1`, settings.DefaultModel, settings.DefaultMode, settings.DefaultFormat, auto, string(hooks), settings.PreferredLanguage)
	return err
}

func (s *server) getTranscription(filename string) (*transcription, error) {
	row := s.db.QueryRow(`SELECT id, filename, source_path, transcript_text, raw_transcript_text, clean_transcript_text, translation_text, status, last_error, size_bytes, duration_seconds, hash, duplicate_of, requested_model, requested_mode, requested_format, actual_openai_model_used, diarized_json, recognized_towns, normalized_transcript, call_type, created_at, updated_at FROM transcriptions WHERE filename = ?`, filename)
	var t transcription
	if err := row.Scan(&t.ID, &t.Filename, &t.SourcePath, &t.Transcript, &t.RawTranscript, &t.CleanTranscript, &t.Translation, &t.Status, &t.LastError, &t.SizeBytes, &t.DurationSeconds, &t.Hash, &t.DuplicateOf, &t.RequestedModel, &t.RequestedMode, &t.RequestedFormat, &t.ActualModel, &t.DiarizedJSON, &t.RecognizedTowns, &t.NormalizedTranscript, &t.CallType, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *server) markProcessing(filename, sourcePath string, size int64, opts TranscriptionOptions) error {
	_, err := s.db.Exec(`INSERT INTO transcriptions (filename, source_path, status, size_bytes, requested_model, requested_mode, requested_format) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(filename) DO UPDATE SET status=excluded.status, source_path=excluded.source_path, size_bytes=excluded.size_bytes, requested_model=excluded.requested_model, requested_mode=excluded.requested_mode, requested_format=excluded.requested_format`, filename, sourcePath, statusProcessing, size, opts.Model, opts.Mode, opts.Format)
	return err
}

func (s *server) markDoneWithDetails(filename string, note string, raw *string, clean *string, translation *string, duplicateOf *string, diarized *string, towns *string, normalized *string, actualModel *string, callType *string) error {
	_, err := s.db.Exec(`UPDATE transcriptions SET status=?, transcript_text=?, raw_transcript_text=?, clean_transcript_text=?, translation_text=?, last_error=?, duplicate_of=?, diarized_json=?, recognized_towns=?, normalized_transcript=?, actual_openai_model_used=?, call_type=? WHERE filename=?`, statusDone, clean, raw, clean, translation, nullableString(note), duplicateOf, diarized, towns, normalized, actualModel, callType, filename)
	return err
}

func (s *server) markError(filename string, cause error) {
	msg := cause.Error()
	if _, err := s.db.Exec(`UPDATE transcriptions SET status=?, last_error=? WHERE filename=?`, statusError, msg, filename); err != nil {
		log.Printf("failed to mark error: %v", err)
	}
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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

func (s *server) updateMetadata(filename string, size int64, duration float64, hash string) error {
	_, err := s.db.Exec(`UPDATE transcriptions SET size_bytes=?, duration_seconds=?, hash=? WHERE filename=?`, size, duration, hash, filename)
	return err
}

func (s *server) findDuplicate(hash string, filename string) string {
	if hash == "" {
		return ""
	}
	row := s.db.QueryRow(`SELECT filename FROM transcriptions WHERE hash = ? AND filename != ? AND status = ?`, hash, filename, statusDone)
	var dup string
	if err := row.Scan(&dup); err == nil {
		return dup
	}
	return ""
}

func (s *server) copyFromDuplicate(filename, duplicate string) error {
	src, err := s.getTranscription(duplicate)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE transcriptions SET transcript_text=?, raw_transcript_text=?, clean_transcript_text=?, translation_text=?, status=?, last_error=NULL, diarized_json=?, recognized_towns=?, normalized_transcript=?, actual_openai_model_used=?, call_type=? WHERE filename=?`, src.Transcript, src.RawTranscript, src.CleanTranscript, src.Translation, statusDone, src.DiarizedJSON, src.RecognizedTowns, src.NormalizedTranscript, src.ActualModel, src.CallType, filename)
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
	_, err = s.db.Exec(`UPDATE transcriptions SET embedding=? WHERE filename=?`, string(data), filename)
	return err
}

func (s *server) fireWebhooks(filename string) error {
	settings, err := s.loadSettings()
	if err != nil {
		return err
	}
	if len(settings.WebhookEndpoints) == 0 {
		return nil
	}
	t, err := s.getTranscription(filename)
	if err != nil {
		return err
	}
	payload := map[string]interface{}{
		"filename":              t.Filename,
		"transcript_text":       pickTranscript(t),
		"raw_transcript_text":   t.RawTranscript,
		"clean_transcript_text": t.CleanTranscript,
		"translation_text":      t.Translation,
		"recognized_towns":      t.RecognizedTowns,
		"normalized_transcript": t.NormalizedTranscript,
		"call_type":             t.CallType,
		"requested_model":       t.RequestedModel,
		"requested_mode":        t.RequestedMode,
		"requested_format":      t.RequestedFormat,
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
	row := s.db.QueryRow(`SELECT embedding FROM transcriptions WHERE filename = ?`, filename)
	var emb sql.NullString
	if err := row.Scan(&emb); err != nil {
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
