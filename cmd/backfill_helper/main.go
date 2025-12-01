package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"alert_framework/config"
	_ "modernc.org/sqlite"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	files := listAudioFiles(cfg.CallsDir)
	if len(files) == 0 {
		log.Println("no audio files found")
		return
	}

	statuses, err := loadStatuses(cfg.DBPath)
	if err != nil {
		log.Fatalf("load statuses: %v", err)
	}

	pending := filterMissing(files, statuses)
	log.Printf("found %d audio files, %d missing transcripts", len(files), len(pending))
	if len(pending) == 0 {
		return
	}

	baseURL := strings.TrimSuffix(os.Getenv("SERVICE_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "http://localhost" + cfg.HTTPPort
	}
	log.Printf("requesting transcripts from %s", baseURL)

	enqueue(baseURL, pending)
}

func listAudioFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("scan calls dir: %v", err)
		return nil
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".mp3" {
			continue
		}
		out = append(out, entry.Name())
	}
	return out
}

func loadStatuses(dbPath string) (map[string]string, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	statuses := make(map[string]string)
	rows, err := db.Query(`SELECT filename, status FROM transcriptions`)
	if err != nil {
		return statuses, nil
	}
	defer rows.Close()
	for rows.Next() {
		var name, status string
		if err := rows.Scan(&name, &status); err == nil {
			statuses[name] = status
		}
	}
	return statuses, nil
}

func filterMissing(files []string, statuses map[string]string) []string {
	var pending []string
	for _, f := range files {
		status := statuses[f]
		if status != "done" {
			pending = append(pending, f)
		}
	}
	return pending
}

func enqueue(baseURL string, files []string) {
	client := &http.Client{}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for _, f := range files {
		wg.Add(1)
		slots <- struct{}{}
		go func(name string) {
			defer wg.Done()
			defer func() { <-slots }()
			endpoint := fmt.Sprintf("%s/api/transcription?filename=%s", baseURL, url.QueryEscape(name))
			req, _ := http.NewRequest(http.MethodPost, endpoint, nil)
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("enqueue %s: %v", name, err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				log.Printf("enqueue %s: %s", name, resp.Status)
				return
			}
			log.Printf("queued %s", name)
		}(f)
	}
	wg.Wait()
}
