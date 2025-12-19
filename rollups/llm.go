package rollups

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type LLMOutput struct {
	Title           string   `json:"title"`
	Summary         string   `json:"summary"`
	Evidence        []string `json:"evidence"`
	MergeSuggestion string   `json:"merge_suggestion"`
	Confidence      string   `json:"confidence"`
}

func buildSystemPrompt(version string) string {
	return strings.TrimSpace(fmt.Sprintf(`You are a Sussex County NJ incident rollup summarizer.
Return STRICT JSON ONLY with keys: title, summary, evidence, merge_suggestion, confidence.
Rules:
- title max 60 chars
- summary max 600 chars
- evidence array 3-6 items, each max 60 chars
- merge_suggestion must be "merge" or "keep"
- confidence must be "low", "medium", or "high"
- no invented facts or locations; use ONLY the provided call data
Style: operational, succinct, Sussex-biased.
Prompt version: %s`, version))
}

func buildUserPrompt(rollup Rollup, calls []CallRecord) string {
	var b strings.Builder
	b.WriteString("Rollup anchor:\n")
	b.WriteString(fmt.Sprintf("- start_at: %s\n", rollup.StartAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- end_at: %s\n", rollup.EndAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- municipality: %s\n", safeString(rollup.Municipality)))
	b.WriteString(fmt.Sprintf("- poi: %s\n", safeString(rollup.POI)))
	b.WriteString(fmt.Sprintf("- category: %s\n", rollup.Category))
	b.WriteString(fmt.Sprintf("- priority: %s\n", rollup.Priority))
	b.WriteString("Calls:\n")
	for _, call := range calls {
		line := summarizeCallForLLM(call)
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func summarizeCallForLLM(call CallRecord) string {
	text := strings.TrimSpace(call.CleanTranscript)
	if text == "" {
		text = strings.TrimSpace(call.RawTranscript)
	}
	if text == "" {
		text = strings.TrimSpace(call.Normalized)
	}
	if len(text) > 240 {
		text = text[:240] + "…"
	}
	parts := []string{
		call.Timestamp.Format(time.RFC3339),
		safeString(call.CallType),
		safeString(normalizeMunicipality(call)),
		safeString(text),
	}
	return strings.Join(filterEmpty(parts), " | ")
}

func filterEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func safeString(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	return v
}

func callRollupLLM(ctx context.Context, client *http.Client, model, baseURL, apiKey, promptVersion string, rollup Rollup, calls []CallRecord) (LLMOutput, string, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
	payload := map[string]interface{}{
		"model":       model,
		"temperature": 0.2,
		"response_format": map[string]string{
			"type": "json_object",
		},
		"messages": []map[string]string{
			{"role": "system", "content": buildSystemPrompt(promptVersion)},
			{"role": "user", "content": buildUserPrompt(rollup, calls)},
		},
	}
	buf, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return LLMOutput{}, endpoint, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return LLMOutput{}, endpoint, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return LLMOutput{}, endpoint, fmt.Errorf("llm status %d: %s", resp.StatusCode, string(body))
	}
	var wrapper struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return LLMOutput{}, endpoint, err
	}
	if len(wrapper.Choices) == 0 {
		return LLMOutput{}, endpoint, errors.New("empty llm response")
	}
	content := strings.TrimSpace(wrapper.Choices[0].Message.Content)
	parsed, err := parseLLMOutput(content)
	return parsed, endpoint, err
}

func parseLLMOutput(content string) (LLMOutput, error) {
	obj := extractJSONObject(content)
	if obj == "" {
		return LLMOutput{}, errors.New("no json object found")
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(obj), &raw); err != nil {
		return LLMOutput{}, err
	}
	allowed := map[string]struct{}{
		"title": {}, "summary": {}, "evidence": {}, "merge_suggestion": {}, "confidence": {},
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return LLMOutput{}, fmt.Errorf("unexpected key %q", key)
		}
	}
	for _, key := range []string{"title", "summary", "evidence", "merge_suggestion", "confidence"} {
		if _, ok := raw[key]; !ok {
			return LLMOutput{}, fmt.Errorf("missing key %q", key)
		}
	}
	var out LLMOutput
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return LLMOutput{}, err
	}
	out.Title = strings.TrimSpace(out.Title)
	out.Summary = strings.TrimSpace(out.Summary)
	if len(out.Title) > 60 {
		return LLMOutput{}, errors.New("title too long")
	}
	if len(out.Summary) > 600 {
		return LLMOutput{}, errors.New("summary too long")
	}
	if len(out.Evidence) < 3 || len(out.Evidence) > 6 {
		return LLMOutput{}, errors.New("evidence must have 3-6 items")
	}
	for i, item := range out.Evidence {
		item = strings.TrimSpace(item)
		if item == "" {
			return LLMOutput{}, errors.New("evidence contains empty item")
		}
		if len(item) > 60 {
			return LLMOutput{}, errors.New("evidence item too long")
		}
		out.Evidence[i] = item
	}
	merge := strings.ToLower(strings.TrimSpace(out.MergeSuggestion))
	if merge != "merge" && merge != "keep" {
		return LLMOutput{}, errors.New("merge_suggestion must be merge or keep")
	}
	out.MergeSuggestion = merge
	conf := strings.ToLower(strings.TrimSpace(out.Confidence))
	if conf != "low" && conf != "medium" && conf != "high" {
		return LLMOutput{}, errors.New("confidence must be low, medium, or high")
	}
	out.Confidence = conf
	return out, nil
}

func extractJSONObject(input string) string {
	start := strings.Index(input, "{")
	if start == -1 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(input); i++ {
		ch := input[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return input[start : i+1]
			}
		}
	}
	return ""
}
