package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// authMiddlewareAnthropic validates Anthropic-style x-api-key header
func authMiddlewareAnthropic(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var count int
		err := db.DB.QueryRow("SELECT COUNT(*) FROM gateway_keys WHERE is_active=1").Scan(&count)
		if err != nil || count == 0 {
			writeError(w, 401, "No gateway API keys configured. Create one in Settings > Gateway Keys first.")
			return
		}

		// Anthropic uses x-api-key header
		apiKey := r.Header.Get("x-api-key")
		if apiKey == "" {
			writeError(w, 401, "missing x-api-key header")
			return
		}

		var keyID string
		err = db.DB.QueryRow("SELECT id FROM gateway_keys WHERE key=? AND is_active=1", apiKey).Scan(&keyID)
		if err != nil {
			writeError(w, 401, "invalid or inactive API key")
			return
		}

		next(w, r)
	}
}

// anthropicMessagesHandler handles Anthropic /v1/messages endpoint
func anthropicMessagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}

	startTime := time.Now()

	var rawBody map[string]interface{}
	if err := parseBody(r, &rawBody); err != nil {
		writeError(w, 400, "invalid JSON request body")
		return
	}

	modelName, _ := rawBody["model"].(string)
	messages, _ := rawBody["messages"].([]interface{})
	isStream, _ := rawBody["stream"].(bool)

	if modelName == "" || len(messages) == 0 {
		writeError(w, 400, "model and messages are required")
		return
	}

	// Ensure minimum max_tokens
	if mt, ok := rawBody["max_tokens"].(float64); !ok || mt < 20000 {
		rawBody["max_tokens"] = 20000
	}

	// Route by model — check groups first, then direct model
	var providerID, providerName, baseURL, modelID, keyID, keyName, keyValue string
	var err error

	// Check if model name matches a group
	var groupCount int
	db.DB.QueryRow("SELECT COUNT(*) FROM groups WHERE name=?", modelName).Scan(&groupCount)

	if strings.HasPrefix(modelName, "group:") || groupCount > 0 {
		groupName := strings.TrimPrefix(modelName, "group:")
		providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, _, err = routeByGroup(groupName)
		if err != nil {
			writeError(w, 400, fmt.Sprintf("group routing error: %v", err))
			return
		}
	} else {
		providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, _, err = routeByModel(modelName)
		if err != nil {
			writeError(w, 400, fmt.Sprintf("model not found: %s", modelName))
			return
		}
	}

	// Build upstream body — override model ID
	upstreamBody := make(map[string]interface{})
	for k, v := range rawBody {
		upstreamBody[k] = v
	}
	upstreamBody["model"] = modelID

	bodyBytes, err := json.Marshal(upstreamBody)
	if err != nil {
		writeError(w, 500, "failed to marshal request body")
		return
	}

	// Build upstream URL — convert OpenAI base to Anthropic endpoint
	upstreamURL := resolveAnthropicUpstreamURL(baseURL)

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, 500, "failed to create upstream request")
		return
	}

	// Set Anthropic-style auth based on provider
	setAnthropicAuth(req, baseURL, keyValue)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	// Use proxy if configured
	client := sharedHTTPClient
	var proxyUsed string
	if proxyURL := getProviderProxy(providerID); proxyURL != "" {
		proxyUsed = proxyURL
		if transport, err := makeProxyTransport(proxyURL); err == nil {
			client.Transport = transport
		}
	}

	// Execute upstream request
	resp, err := client.Do(req)
	latencyMs := time.Since(startTime).Milliseconds()

	if err != nil {
		logProxyRequest(providerID, providerName, modelID, keyID, keyName, "", proxyUsed, 0, 0, 0, latencyMs, err.Error())
		writeError(w, 502, fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	// Handle auth failure — auto-disable key + fallback
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 402 {
		db.DB.Exec("UPDATE api_keys SET is_active = 0 WHERE id = ?", keyID)
		log.Printf("[PAAP] Auto-disabled key %s (%s) — status %d", keyName, keyID, resp.StatusCode)

		tried := map[string]bool{keyID: true}
		for {
			nextKeyID, nextKeyName, nextKeyValue, _, ferr := getNextActiveKeyExcluding(providerID, tried)
			if ferr != nil {
				logProxyRequest(providerID, providerName, modelID, keyID, keyName, "", proxyUsed, resp.StatusCode, 0, 0, latencyMs, "all keys exhausted")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.StatusCode)
				io.Copy(w, resp.Body)
				return
			}
			tried[nextKeyID] = true

			req2, _ := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
			setAnthropicAuth(req2, baseURL, nextKeyValue)
			req2.Header.Set("Content-Type", "application/json")
			req2.Header.Set("anthropic-version", "2023-06-01")
			resp2, err2 := client.Do(req2)
			latencyMs = time.Since(startTime).Milliseconds()

			if err2 != nil {
				logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, "", proxyUsed, 0, 0, 0, latencyMs, err2.Error())
				writeError(w, 502, fmt.Sprintf("upstream request failed: %v", err2))
				return
			}

			if resp2.StatusCode == 200 {
				var tokensIn, tokensOut int
				if isStream {
					tokensIn, tokensOut = handleAnthropicStreaming(w, resp2)
				} else {
					tokensIn, tokensOut = handleAnthropicNonStreaming(w, resp2)
				}
				logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, "", proxyUsed, 200, tokensIn, tokensOut, latencyMs, "")
				return
			}

			resp2.Body.Close()
			if resp2.StatusCode == 401 || resp2.StatusCode == 403 || resp2.StatusCode == 402 {
				db.DB.Exec("UPDATE api_keys SET is_active = 0 WHERE id = ?", nextKeyID)
			}
		}
	}

	// Non-auth error — return as-is
	if resp.StatusCode != 200 {
		logProxyRequest(providerID, providerName, modelID, keyID, keyName, "", proxyUsed, resp.StatusCode, 0, 0, latencyMs, fmt.Sprintf("upstream status %d", resp.StatusCode))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	// Success
	var tokensIn, tokensOut int
	if isStream {
		tokensIn, tokensOut = handleAnthropicStreaming(w, resp)
	} else {
		tokensIn, tokensOut = handleAnthropicNonStreaming(w, resp)
	}
	logProxyRequest(providerID, providerName, modelID, keyID, keyName, "", proxyUsed, 200, tokensIn, tokensOut, latencyMs, "")
}

// resolveAnthropicUpstreamURL converts OpenAI base URL to Anthropic endpoint.
// e.g. https://api.xiaomimimo.com/v1 -> https://api.xiaomimimo.com/anthropic/v1/messages
// e.g. https://api.deepseek.com/v1 -> https://api.deepseek.com/anthropic/v1/messages
func resolveAnthropicUpstreamURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	// TokenGO: same base URL, just /v1/messages
	if strings.Contains(base, "tokengo") {
		if strings.HasSuffix(base, "/v1") {
			return base + "/messages"
		}
		return base + "/v1/messages"
	}
	// MiMo/DeepSeek: replace /v1 with /anthropic/v1/messages
	if strings.HasSuffix(base, "/v1") {
		base = strings.TrimSuffix(base, "/v1")
	}
	return base + "/anthropic/v1/messages"
}

// setAnthropicAuth sets the correct auth header based on provider.
// MiMo uses "api-key", TokenGO/standard Anthropic uses "x-api-key".
func setAnthropicAuth(req *http.Request, baseURL, keyValue string) {
	lower := strings.ToLower(baseURL)
	if strings.Contains(lower, "xiaomi") || strings.Contains(lower, "mimo") {
		req.Header.Set("api-key", keyValue)
	} else {
		// TokenGO, DeepSeek, and others: standard Anthropic x-api-key
		req.Header.Set("x-api-key", keyValue)
	}
}

// handleAnthropicStreaming proxies Anthropic SSE and extracts token usage.
// Anthropic SSE format:
//
//	event: message_start
//	data: {"type":"message_start","message":{"usage":{"input_tokens":N,"output_tokens":0}}}
//	...
//	event: message_delta
//	data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":N}}
//	event: message_stop
//	data: {"type":"message_stop"}
func handleAnthropicStreaming(w http.ResponseWriter, upstreamResp *http.Response) (int, int) {
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
		lineBytes := []byte(line + "\n")
		batch = append(batch, lineBytes...)
		batchSize += len(lineBytes)

		// Parse Anthropic SSE data lines for token usage
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var chunk map[string]interface{}
			if json.Unmarshal([]byte(data), &chunk) == nil {
				// message_start -> input_tokens
				if chunk["type"] == "message_start" {
					if msg, ok := chunk["message"].(map[string]interface{}); ok {
						if usage, ok := msg["usage"].(map[string]interface{}); ok {
							if it, ok := usage["input_tokens"].(float64); ok {
								tokensIn = int(it)
							}
						}
					}
				}
				// message_delta -> output_tokens
				if chunk["type"] == "message_delta" {
					if usage, ok := chunk["usage"].(map[string]interface{}); ok {
						if ot, ok := usage["output_tokens"].(float64); ok {
							tokensOut = int(ot)
						}
					}
				}
				// message_stop -> end of stream
				if chunk["type"] == "message_stop" {
					flushBatch()
					break
				}
			}
		}

		if batchSize >= 4*1024 {
			flushBatch()
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading Anthropic upstream stream: %v", err)
	}

	flushBatch()
	return tokensIn, tokensOut
}

// handleAnthropicNonStreaming proxies non-streaming Anthropic responses and extracts token usage.
func handleAnthropicNonStreaming(w http.ResponseWriter, upstreamResp *http.Response) (int, int) {
	body, err := io.ReadAll(upstreamResp.Body)
	if err != nil {
		log.Printf("Error reading Anthropic response body: %v", err)
		w.WriteHeader(500)
		return 0, 0
	}

	// Extract token usage
	var tokensIn, tokensOut int
	var parsed map[string]interface{}
	if json.Unmarshal(body, &parsed) == nil {
		if usage, ok := parsed["usage"].(map[string]interface{}); ok {
			if it, ok := usage["input_tokens"].(float64); ok {
				tokensIn = int(it)
			}
			if ot, ok := usage["output_tokens"].(float64); ok {
				tokensOut = int(ot)
			}
		}
	}

	// Forward response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(upstreamResp.StatusCode)
	w.Write(body)

	return tokensIn, tokensOut
}
