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
// Optimized: batch writes every 4KB or 50ms to reduce flush overhead.
func handleStreaming(w http.ResponseWriter, upstreamResp *http.Response) (int, int) {
	// Set response headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(upstreamResp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("Warning: ResponseWriter does not support Flushing")
		return 0, 0
	}

	var tokensIn, tokensOut int
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

		// Parse usage from data lines
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				flushBatch()
				break
			}
			var chunk map[string]interface{}
			if json.Unmarshal([]byte(data), &chunk) == nil {
				if usage, ok := chunk["usage"].(map[string]interface{}); ok {
					if pt, ok := usage["prompt_tokens"].(float64); ok {
						tokensIn = int(pt)
					}
					if ct, ok := usage["completion_tokens"].(float64); ok {
						tokensOut = int(ct)
					}
					if pt, ok := usage["input_tokens"].(float64); ok {
						tokensIn = int(pt)
					}
					if ct, ok := usage["output_tokens"].(float64); ok {
						tokensOut = int(ct)
					}
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

	return tokensIn, tokensOut
}

// handleNonStreaming proxies non-streaming responses
func handleNonStreaming(w http.ResponseWriter, upstreamResp *http.Response) {
	// Copy response headers
	w.Header().Set("Content-Type", upstreamResp.Header.Get("Content-Type"))
	w.WriteHeader(upstreamResp.StatusCode)

	// Copy response body
	io.Copy(w, upstreamResp.Body)
}

// parseUsageJSON extracts token usage from a JSON response body (non-streaming)
func parseUsageJSON(body []byte, tokensIn, tokensOut *int) {
	var parsed map[string]interface{}
	if json.Unmarshal(body, &parsed) == nil {
		if usage, ok := parsed["usage"].(map[string]interface{}); ok {
			// OpenAI standard
			if pt, ok := usage["prompt_tokens"].(float64); ok {
				*tokensIn = int(pt)
			}
			if ct, ok := usage["completion_tokens"].(float64); ok {
				*tokensOut = int(ct)
			}
			// Some providers use input/output
			if pt, ok := usage["input_tokens"].(float64); ok {
				*tokensIn = int(pt)
			}
			if ct, ok := usage["output_tokens"].(float64); ok {
				*tokensOut = int(ct)
			}
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
				if pt, ok := usage["prompt_tokens"].(float64); ok {
					*tokensIn = int(pt)
				}
				if ct, ok := usage["completion_tokens"].(float64); ok {
					*tokensOut = int(ct)
				}
				if pt, ok := usage["input_tokens"].(float64); ok {
					*tokensIn = int(pt)
				}
				if ct, ok := usage["output_tokens"].(float64); ok {
					*tokensOut = int(ct)
				}
			}
		}
	}
}
