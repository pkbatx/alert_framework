package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"alert_framework/config"
	_ "modernc.org/sqlite"
)

const (
	workDir = "/home/peebs/ai_transcribe"
	dbPath  = workDir + "/transcriptions.db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := openHelperDB()
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	serviceURL := strings.TrimSuffix(getEnv("SERVICE_URL", "http://localhost:8000"), "/")
	client := &http.Client{Timeout: 30 * time.Second}

	known, err := loadStatuses(db)
	if err != nil {
		log.Fatalf("load statuses: %v", err)
	}

	files, err := os.ReadDir(cfg.CallsDir)
	if err != nil {
		log.Fatalf("read calls dir: %v", err)
	}

	var candidates []string
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".mp3") {
			continue
		}
		status := strings.ToLower(known[name])
		if status != "done" {
			candidates = append(candidates, name)
		}
	}

	if len(candidates) == 0 {
		log.Println("no historical files require transcription")
		return
	}

	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	log.Printf("requesting transcripts for %d files using %d workers", len(candidates), workers)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	jobs := make(chan string)
	successes := 0
	failed := 0
	mu := sync.Mutex{}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filename := range jobs {
				if err := enqueue(ctx, client, serviceURL, filename); err != nil {
					log.Printf("enqueue %s failed: %v", filename, err)
					mu.Lock()
					failed++
					mu.Unlock()
					continue
				}
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}

	for _, name := range candidates {
		select {
		case jobs <- name:
		case <-ctx.Done():
			break
		}
	}
	close(jobs)
	wg.Wait()

	log.Printf("completed backfill helper: queued=%d failed=%d", successes, failed)
}

func enqueue(ctx context.Context, client *http.Client, serviceURL, filename string) error {
	endpoint := fmt.Sprintf("%s/api/transcription?filename=%s", serviceURL, url.QueryEscape(filename))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return nil
}

func loadStatuses(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT filename, status FROM transcriptions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var name, status string
		if err := rows.Scan(&name, &status); err != nil {
			continue
		}
		result[name] = status
	}
	return result, rows.Err()
}

func openHelperDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
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
		if _, err := db.Exec(pragma); err != nil {
			return nil, err
		}
	}
	return db, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
