package rollups

import "time"

const (
	StatusLLMOK      = "LLM_OK"
	StatusLLMFailed  = "LLM_FAILED"
	StatusLLMSkipped = "LLM_SKIPPED"
)

type CallRecord struct {
	ID              int64
	Filename        string
	Timestamp       time.Time
	CallType        string
	CleanTranscript string
	RawTranscript   string
	Normalized      string
	Latitude        float64
	Longitude       float64
	LocationLabel   string
	AddressJSON     string
	RefinedJSON     string
}

type Rollup struct {
	ID              int64
	Key             string
	StartAt         time.Time
	EndAt           time.Time
	Latitude        float64
	Longitude       float64
	Municipality    string
	POI             string
	Category        string
	Priority        string
	Title           string
	Summary         string
	Evidence        []string
	Confidence      string
	Status          string
	MergeSuggestion string
	ModelName       string
	ModelBaseURL    string
	PromptVersion   string
	CallIDs         []int64
	CallCount       int
	LastError       string
	UpdatedAt       time.Time
}

type RunResult struct {
	RollupCount int
	Status      string
	Error       string
}
