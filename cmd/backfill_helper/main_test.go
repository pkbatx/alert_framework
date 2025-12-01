package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFilterPendingSkipsInflightAndDone(t *testing.T) {
	now := time.Now()
	statuses := map[string]transcriptionStatus{
		"done.mp3":       {Status: statusDone, UpdatedAt: now},
		"queued.mp3":     {Status: statusQueued, UpdatedAt: now},
		"processing.mp3": {Status: statusProcessing, UpdatedAt: now},
		"error.mp3":      {Status: statusError, UpdatedAt: now},
	}

	files := []string{"done.mp3", "queued.mp3", "processing.mp3", "error.mp3", "new.mp3"}
	pending, summary := filterPending(files, statuses, 0)

	if len(pending) != 2 {
		t.Fatalf("expected 2 pending files, got %d", len(pending))
	}
	expectedPending := []string{"error.mp3", "new.mp3"}
	if !reflect.DeepEqual(pending, expectedPending) {
		t.Fatalf("unexpected pending list: %#v", pending)
	}
	if summary.Done != 1 || summary.InFlight != 2 || summary.Errors != 1 || summary.New != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestFilterPendingRequeuesStaleInflight(t *testing.T) {
	stale := time.Now().Add(-4 * time.Hour)
	statuses := map[string]transcriptionStatus{
		"stale.mp3": {Status: statusProcessing, UpdatedAt: stale},
	}

	pending, summary := filterPending([]string{"stale.mp3"}, statuses, 3*time.Hour)
	if len(pending) != 1 || pending[0] != "stale.mp3" {
		t.Fatalf("expected stale file to be pending, got %#v", pending)
	}
	if summary.Stale != 1 {
		t.Fatalf("expected stale count to increment, got %+v", summary)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	got := normalizeBaseURL("localhost:9000/", ":8000")
	if got != "http://localhost:9000" {
		t.Fatalf("expected normalized URL, got %s", got)
	}
	got = normalizeBaseURL("", ":8000")
	if got != "http://localhost:8000" {
		t.Fatalf("expected fallback URL, got %s", got)
	}
}

func TestListAudioFilesFiltersAndSorts(t *testing.T) {
	dir := t.TempDir()
	files := []string{"z.mp3", "a.wav", "ignore.txt"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
	}

	got, err := listAudioFiles(dir)
	if err != nil {
		t.Fatalf("list audio files: %v", err)
	}

	expected := []string{"a.wav", "z.mp3"}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
}
