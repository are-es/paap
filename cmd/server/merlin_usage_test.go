package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dolvin/paap/internal/db"
)

// merlinSSEBody builds a Merlin-style SSE payload carrying text and reasoning
// deltas, optionally terminated by Merlin's DONE system event.
func merlinSSEBody(text, reasoning string, sendDone bool) string {
	var sb strings.Builder
	if reasoning != "" {
		sb.WriteString("event: message\n")
		sb.WriteString(fmt.Sprintf("data: {\"status\":\"assistant\",\"data\":{\"reasoning\":%q}}\n\n", reasoning))
	}
	sb.WriteString("event: message\n")
	sb.WriteString(fmt.Sprintf("data: {\"status\":\"assistant\",\"data\":{\"text\":%q}}\n\n", text))
	if sendDone {
		sb.WriteString("event: message\n")
		sb.WriteString("data: {\"status\":\"system\",\"data\":{\"eventType\":\"DONE\"}}\n\n")
	}
	return sb.String()
}

// TestMerlinEstimatedInput asserts the request-side estimate reads the flattened
// Merlin message content.
func TestMerlinEstimatedInput(t *testing.T) {
	body := convertToMerlinBody(map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are a helpful assistant."},
			map[string]interface{}{"role": "user", "content": strings.Repeat("explain this code ", 20)},
		},
	}, "merlin-model")

	got := merlinEstimatedInput(body)
	if got <= 0 {
		t.Fatalf("merlinEstimatedInput = %d, want > 0", got)
	}

	// A missing message object must not panic and must report nothing.
	if n := merlinEstimatedInput(map[string]interface{}{}); n != 0 {
		t.Errorf("merlinEstimatedInput(empty) = %d, want 0", n)
	}
}

// TestMerlinStreamingEstimatesTokens is the regression guard for Merlin logging
// a hardcoded 0/0 on every request, which made a paid provider indistinguishable
// from a free one.
func TestMerlinStreamingEstimatesTokens(t *testing.T) {
	setupPricingDB(t)

	const inputTokens = 250
	body := merlinSSEBody(strings.Repeat("generated output text ", 30), "thinking about it", true)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer upstream.Close()

	resp, err := http.Get(upstream.URL)
	if err != nil {
		t.Fatalf("get upstream: %v", err)
	}
	defer resp.Body.Close()

	w := httptest.NewRecorder()
	counts := handleMerlinStreaming(w, resp, "merlin-model", inputTokens)

	if !counts.Estimated {
		t.Error("Estimated = false; Merlin counts are heuristics and must be flagged")
	}
	if counts.InFresh != inputTokens {
		t.Errorf("InFresh = %d, want %d", counts.InFresh, inputTokens)
	}
	if counts.Out <= 0 {
		t.Errorf("Out = %d, want > 0 (was hardcoded 0 before the fix)", counts.Out)
	}
	if counts.Reasoning <= 0 {
		t.Errorf("Reasoning = %d, want > 0", counts.Reasoning)
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Error("response missing [DONE] sentinel")
	}
}

// TestMerlinNonStreamingEstimatesTokens covers the non-streaming path and asserts
// the client-facing usage object is no longer all zeros.
func TestMerlinNonStreamingEstimatesTokens(t *testing.T) {
	setupPricingDB(t)

	const inputTokens = 120
	body := merlinSSEBody(strings.Repeat("answer text ", 25), "", false)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer upstream.Close()

	resp, err := http.Get(upstream.URL)
	if err != nil {
		t.Fatalf("get upstream: %v", err)
	}
	defer resp.Body.Close()

	w := httptest.NewRecorder()
	counts := handleMerlinNonStreaming(w, resp, "merlin-model", inputTokens)

	if !counts.Estimated {
		t.Error("Estimated = false; Merlin counts are heuristics and must be flagged")
	}
	if counts.InFresh != inputTokens {
		t.Errorf("InFresh = %d, want %d", counts.InFresh, inputTokens)
	}
	if counts.Out <= 0 {
		t.Errorf("Out = %d, want > 0 (was hardcoded 0 before the fix)", counts.Out)
	}

	var parsed struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("parse response: %v — body: %s", err, w.Body.String())
	}
	if parsed.Usage.PromptTokens != inputTokens {
		t.Errorf("usage.prompt_tokens = %d, want %d", parsed.Usage.PromptTokens, inputTokens)
	}
	if parsed.Usage.CompletionTokens == 0 {
		t.Error("usage.completion_tokens = 0; client-facing usage must reflect the estimate")
	}
	if want := parsed.Usage.PromptTokens + parsed.Usage.CompletionTokens; parsed.Usage.TotalTokens != want {
		t.Errorf("usage.total_tokens = %d, want %d", parsed.Usage.TotalTokens, want)
	}
}

// TestMerlinLogRowMarkedEstimated asserts the estimate flag reaches the DB, so
// the dashboard can distinguish measured from guessed counts.
func TestMerlinLogRowMarkedEstimated(t *testing.T) {
	setupPricingDB(t)

	counts := tokenCounts{InFresh: 300, Out: 150, Estimated: true}
	logProxyRequestSplit("prov-justwoker", "Merlin", "claude-opus-5", "k1", "key", "", "",
		200, counts, 42, "", nil, "", "", 0, 0)

	var estimated, tokensIn, tokensOut int
	if err := db.DB.QueryRow(`SELECT tokens_estimated, tokens_in, tokens_out FROM logs ORDER BY id DESC LIMIT 1`).
		Scan(&estimated, &tokensIn, &tokensOut); err != nil {
		t.Fatalf("read back log row: %v", err)
	}
	if estimated != 1 {
		t.Errorf("tokens_estimated = %d, want 1", estimated)
	}
	if tokensIn != 300 || tokensOut != 150 {
		t.Errorf("tokens_in/out = %d/%d, want 300/150", tokensIn, tokensOut)
	}
}
