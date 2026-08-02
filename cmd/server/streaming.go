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

// handleStreaming proxies SSE and extracts token usage from the final chunk.
// Also captures full content for logging.
// Returns: tokensIn, tokensOut, fullResponseBody
func handleStreaming(w http.ResponseWriter, upstreamResp *http.Response) (int, int, []byte) {
	// Set response headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(upstreamResp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("Warning: ResponseWriter does not support Flushing")
		return 0, 0, nil
	}

	var tokensIn, tokensOut int
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
					extractUsage(usage, &tokensIn, &tokensOut)
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
	virtualResponse := map[string]interface{}{
		"id":      "",
		"model":   model,
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

	return tokensIn, tokensOut, bodyBytes
}

// handleNonStreaming proxies non-streaming responses
func handleNonStreaming(w http.ResponseWriter, upstreamResp *http.Response) {
	// Copy response headers
	w.Header().Set("Content-Type", upstreamResp.Header.Get("Content-Type"))
	w.WriteHeader(upstreamResp.StatusCode)

	// Copy response body
	io.Copy(w, upstreamResp.Body)
}

// extractUsage pulls token counts out of a provider usage object.
// OpenAI names (prompt_tokens/completion_tokens) win; Anthropic-style
// input_tokens/output_tokens are only a FALLBACK.
// Some gateways (e.g. go-router) emit both, with the Anthropic pair zeroed —
// overwriting unconditionally there logged 0 output tokens.
func extractUsage(usage map[string]interface{}, tokensIn, tokensOut *int) {
	if pt, ok := usage["prompt_tokens"].(float64); ok && pt > 0 {
		*tokensIn = int(pt)
	} else if pt, ok := usage["input_tokens"].(float64); ok && pt > 0 {
		*tokensIn = int(pt)
	}
	if ct, ok := usage["completion_tokens"].(float64); ok && ct > 0 {
		*tokensOut = int(ct)
	} else if ct, ok := usage["output_tokens"].(float64); ok && ct > 0 {
		*tokensOut = int(ct)
	}
}

// parseUsageJSON extracts token usage from a JSON response body (non-streaming)
func parseUsageJSON(body []byte, tokensIn, tokensOut *int) {
	var parsed map[string]interface{}
	if json.Unmarshal(body, &parsed) == nil {
		if usage, ok := parsed["usage"].(map[string]interface{}); ok {
			extractUsage(usage, tokensIn, tokensOut)
		}
	}
}

// parseUsageSSE extracts token usage from SSE stream body (multiple data: lines)
func parseUsageSSE(body []byte, tokensIn, tokensOut *int) {
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
				extractUsage(usage, tokensIn, tokensOut)
			}
		}
	}
}
