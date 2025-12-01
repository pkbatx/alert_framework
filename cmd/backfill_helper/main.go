package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"alert_framework/config"
	_ "modernc.org/sqlite"
)

const (
	statusDone       = "done"
	statusQueued     = "queued"
	statusProcessing = "processing"
	statusError      = "error"

	defaultConcurrency    = 8
	defaultRequestTimeout = 15 * time.Second
	staleProcessingAfter  = 3 * time.Hour
	helperUserAgent       = "alert-framework-backfill-helper/1.0"
)

var allowedAudioExtensions = map[string]struct{}{
	".mp3": {}, ".mp4": {}, ".mpeg": {}, ".mpga": {}, ".m4a": {}, ".wav": {}, ".webm": {},
}

type transcriptionStatus struct {
	Status    string
	UpdatedAt time.Time
}

type filterSummary struct {
	Done     int
	InFlight int
	Errors   int
	New      int
	Stale    int
}

type enqueueSummary struct {
	Queued int
	Failed int
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	defaultBaseURL := strings.TrimSuffix(os.Getenv("SERVICE_BASE_URL"), "/")
	if defaultBaseURL == "" {
		defaultBaseURL = "http://localhost" + cfg.HTTPPort
	}

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Backfill helper for alert_framework\n\n")
		flag.PrintDefaults()
	}

	callsDir := flag.String("calls-dir", cfg.CallsDir, "Directory containing audio recordings")
	dbPath := flag.String("db", cfg.DBPath, "Path to the SQLite DB used by the main service")
	serviceURL := flag.String("service", defaultBaseURL, "Base URL of the running service (e.g. http://localhost:8000)")
	concurrency := flag.Int("concurrency", defaultConcurrency, "Number of simultaneous enqueue requests")
	limit := flag.Int("limit", 0, "Maximum number of jobs to enqueue (0 = all pending)")
	dryRun := flag.Bool("dry-run", false, "Print the plan without sending requests")
	requeueAfter := flag.Duration("requeue-stale", staleProcessingAfter, "Mark in-flight jobs older than this as pending (0 = never)")
	requestTimeout := flag.Duration("request-timeout", defaultRequestTimeout, "HTTP client timeout per request")
	flag.Parse()

	baseURL := normalizeBaseURL(*serviceURL, cfg.HTTPPort)
	log.Printf("using service at %s", baseURL)

	files, err := listAudioFiles(*callsDir)
	if err != nil {
		log.Fatalf("scan calls dir: %v", err)
	}
	if len(files) == 0 {
		log.Println("no audio files found")
		return
	}

	statuses, err := loadStatuses(*dbPath)
	if err != nil {
		log.Fatalf("load statuses: %v", err)
	}

	pending, summary := filterPending(files, statuses, *requeueAfter)
	if *limit > 0 && len(pending) > *limit {
		pending = pending[:*limit]
	}

	log.Printf(
		"found %d audio files (done=%d, in-flight=%d, errors=%d, new=%d, stale=%d); %d pending",
		len(files), summary.Done, summary.InFlight, summary.Errors, summary.New, summary.Stale, len(pending),
	)
	if len(pending) == 0 {
		return
	}

	if *dryRun {
		for _, f := range pending {
			log.Printf("dry-run: would enqueue %s", f)
		}
		return
	}

	result := enqueue(context.Background(), baseURL, pending, *concurrency, *requestTimeout)
	log.Printf("enqueue complete: queued=%d failed=%d", result.Queued, result.Failed)
}

func normalizeBaseURL(raw string, fallbackPort string) string {
	base := strings.TrimSuffix(strings.TrimSpace(raw), "/")
	if base == "" {
		base = "http://localhost" + fallbackPort
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	return base
}

func listAudioFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if _, ok := allowedAudioExtensions[ext]; !ok {
			continue
		}
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out, nil
}

func loadStatuses(dbPath string) (map[string]transcriptionStatus, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT filename, status, updated_at FROM transcriptions`)
	if err != nil {
		return nil, fmt.Errorf("query transcriptions: %w", err)
	}
	defer rows.Close()

	statuses := make(map[string]transcriptionStatus)
	for rows.Next() {
		var name, status string
		var updated sql.NullTime
		if err := rows.Scan(&name, &status, &updated); err != nil {
			log.Printf("skip row: %v", err)
			continue
		}
		statuses[name] = transcriptionStatus{Status: status, UpdatedAt: updated.Time}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return statuses, nil
}

func filterPending(files []string, statuses map[string]transcriptionStatus, staleAfter time.Duration) ([]string, filterSummary) {
	now := time.Now()
	var pending []string
	var summary filterSummary
	for _, f := range files {
		st, ok := statuses[f]
		if !ok || st.Status == "" {
			summary.New++
			pending = append(pending, f)
			continue
		}
		switch st.Status {
		case statusDone:
			summary.Done++
		case statusError:
			summary.Errors++
			pending = append(pending, f)
		case statusQueued, statusProcessing:
			if staleAfter > 0 && !st.UpdatedAt.IsZero() && st.UpdatedAt.Add(staleAfter).Before(now) {
				summary.Stale++
				pending = append(pending, f)
				continue
			}
			summary.InFlight++
		default:
			summary.New++
			pending = append(pending, f)
		}
	}
	return pending, summary
}

func enqueue(ctx context.Context, baseURL string, files []string, concurrency int, timeout time.Duration) enqueueSummary {
	if concurrency <= 0 {
		concurrency = 1
	}
	client := &http.Client{Timeout: timeout}
	slots := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	summary := enqueueSummary{}

	for _, f := range files {
		wg.Add(1)
		slots <- struct{}{}
		go func(name string) {
			defer wg.Done()
			defer func() { <-slots }()

			endpoint := fmt.Sprintf("%s/api/transcription?filename=%s", baseURL, url.QueryEscape(name))
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
			if err != nil {
				log.Printf("enqueue %s: %v", name, err)
				mu.Lock()
				summary.Failed++
				mu.Unlock()
				return
			}
			req.Header.Set("User-Agent", helperUserAgent)
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("enqueue %s: %v", name, err)
				mu.Lock()
				summary.Failed++
				mu.Unlock()
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				log.Printf("enqueue %s: %s", name, resp.Status)
				mu.Lock()
				summary.Failed++
				mu.Unlock()
				return
			}
			mu.Lock()
			summary.Queued++
			mu.Unlock()
			log.Printf("queued %s", name)
		}(f)
	}

	wg.Wait()
	return summary
}
