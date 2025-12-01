// Build: GO111MODULE=on go mod tidy && go build -o alert_server
// Run:   ./alert_server
// Configuration via env vars:
//   PORT                 - HTTP port (default 8000)
//   GROUPME_BOT_ID       - Overrides default GroupMe bot id
//   OPENAI_API_KEY       - API key for OpenAI (required for transcription)
// Paths are configured below for callsDir and workDir.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	_ "modernc.org/sqlite"
)

const (
	callsDir   = "/home/peebs/calls"
	workDir    = "/home/peebs/ai_transcribe"
	dbPath     = workDir + "/transcriptions.db"
	groupmeURL = "https://api.groupme.com/v3/bots/post"
	defaultBot = "03926cdc985a046b27d6393ba6"
)

// transcription statuses
const (
	statusProcessing = "processing"
	statusDone       = "done"
	statusError      = "error"
)

// queue status response values
const (
	statusQueued = "queued"
)

type transcription struct {
	ID         int64     `json:"id"`
	Filename   string    `json:"filename"`
	SourcePath string    `json:"source_path"`
	Transcript *string   `json:"transcript_text"`
	Status     string    `json:"status"`
	LastError  *string   `json:"last_error"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type job struct {
	filename string
}

type server struct {
	db      *sql.DB
	jobs    chan job
	running sync.Map // filename -> struct{}
	client  *http.Client
	botID   string
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

	s := &server{
		db:     db,
		jobs:   make(chan job, 100),
		client: &http.Client{Timeout: 120 * time.Second},
		botID:  getBotID(),
	}

	go s.worker()
	go s.watch()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/transcriptions", s.handleTranscriptions)
	mux.HandleFunc("/api/transcription/", s.handleTranscription)
	mux.HandleFunc("/", s.handleRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	log.Printf("server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
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
        status TEXT NOT NULL,
        last_error TEXT NULL,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );
    CREATE TRIGGER IF NOT EXISTS transcriptions_updated_at
    AFTER UPDATE ON transcriptions
    BEGIN
        UPDATE transcriptions SET updated_at = CURRENT_TIMESTAMP WHERE id = old.id;
    END;`
	_, err := db.Exec(schema)
	return err
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
	s.queueJob(filename)
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

func (s *server) queueJob(filename string) {
	if _, exists := s.running.LoadOrStore(filename, struct{}{}); exists {
		return
	}
	select {
	case s.jobs <- job{filename: filename}:
	default:
		log.Printf("job queue full, dropping job for %s", filename)
		s.running.Delete(filename)
	}
}

func (s *server) worker() {
	for j := range s.jobs {
		if err := s.processFile(j.filename); err != nil {
			log.Printf("process %s error: %v", j.filename, err)
		}
		s.running.Delete(j.filename)
	}
}

func (s *server) processFile(filename string) error {
	sourcePath := filepath.Join(callsDir, filename)
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if existing, err := s.getTranscription(filename); err == nil {
		if existing.Status == statusDone || existing.Status == statusProcessing {
			return nil
		}
	}
	if err := s.markProcessing(filename, sourcePath); err != nil {
		return err
	}
	if err := waitForStableSize(sourcePath, info.Size(), 2*time.Second, 2); err != nil {
		s.markError(filename, err)
		return err
	}
	stagedPath := filepath.Join(workDir, filename)
	if err := copyFile(sourcePath, stagedPath); err != nil {
		s.markError(filename, err)
		return err
	}
	transcript, err := s.callOpenAI(stagedPath)
	if err != nil {
		s.markError(filename, err)
		return err
	}
	if err := s.markDone(filename, transcript); err != nil {
		return err
	}
	followup := fmt.Sprintf("%s transcript:\n%s", filename, transcript)
	if err := s.sendGroupMe(followup); err != nil {
		log.Printf("groupme follow-up failed: %v", err)
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

func (s *server) callOpenAI(path string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", errors.New("OPENAI_API_KEY not set")
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	bodyReader, bodyWriter := io.Pipe()
	writer := multipart.NewWriter(bodyWriter)

	go func() {
		defer bodyWriter.Close()
		defer writer.Close()
		fw, err := writer.CreateFormFile("file", filepath.Base(path))
		if err != nil {
			bodyWriter.CloseWithError(err)
			return
		}
		if _, err := io.Copy(fw, file); err != nil {
			bodyWriter.CloseWithError(err)
			return
		}
		writer.WriteField("model", "gpt-4o-transcribe")
	}()

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/audio/transcriptions", bodyReader)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai status %d: %s", resp.StatusCode, string(b))
	}

	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.Text == "" {
		return "", errors.New("empty transcript from openai")
	}
	return parsed.Text, nil
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
	if r.URL.Path != "/" && r.Method == http.MethodGet {
		s.handleFile(w, r)
		return
	}
	entries, err := os.ReadDir(callsDir)
	if err != nil {
		http.Error(w, "failed to list", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<html><body><h1>Calls</h1><ul>")
	for _, e := range entries {
		name := e.Name()
		escaped := url.PathEscape(name)
		fmt.Fprintf(w, "<li><a href=\"/%s\">%s</a> - <a href=\"/api/transcription/%s\">transcript</a></li>", escaped, name, escaped)
	}
	fmt.Fprintf(w, "</ul></body></html>")
}

func (s *server) handleFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" || strings.HasPrefix(path, "api/") {
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

func (s *server) handleTranscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	filename := strings.TrimPrefix(r.URL.Path, "/api/transcription/")
	filename, err := url.PathUnescape(filename)
	if err != nil || filename == "" {
		http.NotFound(w, r)
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
				"filename":        existing.Filename,
				"status":          existing.Status,
				"transcript_text": existing.Transcript,
			})
			return
		case statusProcessing:
			respondJSON(w, map[string]interface{}{
				"filename": existing.Filename,
				"status":   existing.Status,
			})
			return
		case statusError:
			s.queueJob(cleaned)
			respondJSON(w, map[string]interface{}{
				"filename": existing.Filename,
				"status":   statusQueued,
			})
			return
		}
	}

	s.queueJob(cleaned)
	respondJSON(w, map[string]interface{}{
		"filename": cleaned,
		"status":   statusQueued,
	})
}

func (s *server) handleTranscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rows, err := s.db.Query(`SELECT id, filename, source_path, transcript_text, status, last_error, created_at, updated_at FROM transcriptions ORDER BY created_at DESC`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []transcription
	for rows.Next() {
		var t transcription
		if err := rows.Scan(&t.ID, &t.Filename, &t.SourcePath, &t.Transcript, &t.Status, &t.LastError, &t.CreatedAt, &t.UpdatedAt); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		result = append(result, t)
	}
	respondJSON(w, result)
}

func respondJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *server) getTranscription(filename string) (*transcription, error) {
	row := s.db.QueryRow(`SELECT id, filename, source_path, transcript_text, status, last_error, created_at, updated_at FROM transcriptions WHERE filename = ?`, filename)
	var t transcription
	if err := row.Scan(&t.ID, &t.Filename, &t.SourcePath, &t.Transcript, &t.Status, &t.LastError, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *server) markProcessing(filename, sourcePath string) error {
	_, err := s.db.Exec(`INSERT INTO transcriptions (filename, source_path, status) VALUES (?, ?, ?)
        ON CONFLICT(filename) DO UPDATE SET status=excluded.status, source_path=excluded.source_path`, filename, sourcePath, statusProcessing)
	return err
}

func (s *server) markDone(filename, transcript string) error {
	_, err := s.db.Exec(`UPDATE transcriptions SET status=?, transcript_text=?, last_error=NULL WHERE filename=?`, statusDone, transcript, filename)
	return err
}

func (s *server) markError(filename string, cause error) {
	msg := cause.Error()
	if _, err := s.db.Exec(`UPDATE transcriptions SET status=?, last_error=? WHERE filename=?`, statusError, msg, filename); err != nil {
		log.Printf("failed to mark error: %v", err)
	}
}
