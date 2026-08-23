package main

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// handleStreaming is the legacy call shape kept for callers that only need a
// flat in/out token pair. Returns: tokensIn, tokensOut, fullResponseBody.
func handleStreaming(w http.ResponseWriter, upstreamResp *http.Response) (int, int, []byte) {
	t, body := handleStreamingSplit(w, upstreamResp)
	return t.TotalIn(), t.TotalOut(), body
}

// handleStreamingSplit proxies SSE and extracts token usage from the final
// chunk, keeping the cached-prompt and reasoning sub-counters separate.
// Also captures full content for logging.
func handleStreamingSplit(w http.ResponseWriter, upstreamResp *http.Response) (tokenCounts, []byte) {
	// Set response headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(upstreamResp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("Warning: ResponseWriter does not support Flushing")
		return tokenCounts{}, nil
	}

	var counts tokenCounts
	var fullContent strings.Builder
	var model string

	scanner := bufio.NewScanner(upstreamResp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	// Batch buffer: accumulate until 4KB or 50ms
	var batch []byte
	batchSize := 0
	batchTimer := time.NewTimer(50 * time.Millisecond)
	defer batchTimer.Stop()

	flushBatch := func() {
		if len(batch) > 0 {
			w.Write(batch)
			batch = batch[:0]
			batchSize = 0
			flusher.Flush()
		}
		batchTimer.Reset(50 * time.Millisecond)
	}

	for scanner.Scan() {
		line := scanner.Text()

		// Add newline to line
		lineBytes := []byte(line + "\n")
		batch = append(batch, lineBytes...)
		batchSize += len(lineBytes)

		// Parse usage and content from data lines
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				flushBatch()
				break
			}
			var chunk map[string]interface{}
			if json.Unmarshal([]byte(data), &chunk) == nil {
				if usage, ok := chunk["usage"].(map[string]interface{}); ok {
					extractUsage(usage, &counts)
				}
				// Capture content from delta
				if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
					if choice, ok := choices[0].(map[string]interface{}); ok {
						if delta, ok := choice["delta"].(map[string]interface{}); ok {
							if content, ok := delta["content"].(string); ok {
								fullContent.WriteString(content)
							}
						}
					}
				}
				// Capture model name
				if m, ok := chunk["model"].(string); ok && m != "" && model == "" {
					model = m
				}
			}
		}

		// Check for stream termination
		if strings.TrimSpace(line) == "data: [DONE]" {
			flushBatch()
			break
		}

		// Flush if batch full (4KB)
		if batchSize >= 4*1024 {
			flushBatch()
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading upstream stream: %v", err)
	}

	// Final flush
	flushBatch()

	// Build virtual full response body for logging
	tokensIn := counts.TotalIn()
	tokensOut := counts.TotalOut()
	virtualResponse := map[string]interface{}{
		"id":    "",
		"model": model,
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": fullContent.String(),
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     tokensIn,
			"completion_tokens": tokensOut,
			"total_tokens":      tokensIn + tokensOut,
		},
	}
	bodyBytes, _ := json.Marshal(virtualResponse)

	return counts, bodyBytes
}

// handleNonStreaming proxies non-streaming responses
func handleNonStreaming(w http.ResponseWriter, upstreamResp *http.Response) {
	// Copy response headers
	w.Header().Set("Content-Type", upstreamResp.Header.Get("Content-Type"))
	w.WriteHeader(upstreamResp.StatusCode)

	// Copy response body
	io.Copy(w, upstreamResp.Body)
}

// usageNum reads a numeric field out of a decoded JSON usage object.
// encoding/json gives float64 for numbers, but a json.Number can arrive when a
// caller decoded with UseNumber.
func usageNum(m map[string]interface{}, key string) (int, bool) {
	switch v := m[key].(type) {
	case float64:
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

// usageSubNum reads usage[parent][key], e.g. prompt_tokens_details.cached_tokens.
func usageSubNum(usage map[string]interface{}, parent, key string) (int, bool) {
	sub, ok := usage[parent].(map[string]interface{})
	if !ok {
		return 0, false
	}
	return usageNum(sub, key)
}

// firstPositive returns the first positive value among the given
// parent.key lookups. Used so the OpenAI-named details object wins over the
// Responses-API-named one when a gateway emits both.
func firstPositive(usage map[string]interface{}, lookups [][2]string) int {
	for _, l := range lookups {
		if v, ok := usageSubNum(usage, l[0], l[1]); ok && v > 0 {
			return v
		}
	}
	return 0
}

func clampNonNegative(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

// extractUsage pulls token counts out of a provider usage object.
// OpenAI names (prompt_tokens/completion_tokens) win; Anthropic-style
// input_tokens/output_tokens are only a FALLBACK.
// Some gateways (e.g. go-router) emit both, with the Anthropic pair zeroed —
// overwriting unconditionally there logged 0 output tokens.
//
// The sub-counters follow the same precedence: prompt_tokens_details.cached_tokens
// wins over input_tokens_details.cached_tokens (the Responses API name), and
// completion_tokens_details.reasoning_tokens over output_tokens_details.reasoning_tokens.
//
// OpenAI counts reasoning tokens INSIDE completion_tokens, so the split is a
// subtraction: Out = completion_tokens - reasoning. Gemini reports thinking
// tokens OUTSIDE candidatesTokenCount, so anigravity.go adds instead — see the
// note in geminiTokenCounts.
func extractUsage(usage map[string]interface{}, t *tokenCounts) {
	promptTotal, hasPrompt := 0, false
	if pt, ok := usageNum(usage, "prompt_tokens"); ok && pt > 0 {
		promptTotal, hasPrompt = pt, true
	} else if pt, ok := usageNum(usage, "input_tokens"); ok && pt > 0 {
		promptTotal, hasPrompt = pt, true
	}
	if hasPrompt {
		cached := firstPositive(usage, [][2]string{
			{"prompt_tokens_details", "cached_tokens"},
			{"input_tokens_details", "cached_tokens"},
		})
		t.CacheRead = cached
		t.InFresh = clampNonNegative(promptTotal - cached)
	}

	outTotal, hasOut := 0, false
	if ct, ok := usageNum(usage, "completion_tokens"); ok && ct > 0 {
		outTotal, hasOut = ct, true
	} else if ct, ok := usageNum(usage, "output_tokens"); ok && ct > 0 {
		outTotal, hasOut = ct, true
	}
	if hasOut {
		reasoning := firstPositive(usage, [][2]string{
			{"completion_tokens_details", "reasoning_tokens"},
			{"output_tokens_details", "reasoning_tokens"},
		})
		t.Reasoning = reasoning
		t.Out = clampNonNegative(outTotal - reasoning)
	}
}

// parseUsageJSON extracts token usage from a JSON response body (non-streaming).
// Legacy in/out pair shape; parseUsageJSONSplit keeps the breakdown.
func parseUsageJSON(body []byte, tokensIn, tokensOut *int) {
	t := tokenCounts{InFresh: *tokensIn, Out: *tokensOut}
	parseUsageJSONSplit(body, &t)
	*tokensIn = t.TotalIn()
	*tokensOut = t.TotalOut()
}

// parseUsageJSONSplit extracts the full token breakdown from a JSON response body.
func parseUsageJSONSplit(body []byte, t *tokenCounts) {
	var parsed map[string]interface{}
	if json.Unmarshal(body, &parsed) == nil {
		if usage, ok := parsed["usage"].(map[string]interface{}); ok {
			extractUsage(usage, t)
		}
	}
}

// parseUsageSSE extracts token usage from SSE stream body (multiple data: lines).
// Legacy in/out pair shape; parseUsageSSESplit keeps the breakdown.
func parseUsageSSE(body []byte, tokensIn, tokensOut *int) {
	t := tokenCounts{InFresh: *tokensIn, Out: *tokensOut}
	parseUsageSSESplit(body, &t)
	*tokensIn = t.TotalIn()
	*tokensOut = t.TotalOut()
}

// parseUsageSSESplit extracts the full token breakdown from an SSE stream body.
func parseUsageSSESplit(body []byte, t *tokenCounts) {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		var chunk map[string]interface{}
		if json.Unmarshal([]byte(data), &chunk) == nil {
			if usage, ok := chunk["usage"].(map[string]interface{}); ok {
				extractUsage(usage, t)
			}
		}
	}
}
