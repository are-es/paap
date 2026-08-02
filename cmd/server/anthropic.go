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
	"github.com/dolvin/paap/internal/translator"
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

	// Apply headroom compression to tool results in Anthropic format
	messages = compressAnthropicToolResults(messages, modelName)
	rawBody["messages"] = messages

	// Ensure minimum max_tokens
	if mt, ok := rawBody["max_tokens"].(float64); !ok || mt < 20000 {
		rawBody["max_tokens"] = 20000
	}

	// Route by model — check groups first, then direct model
	var providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, keyAccountID string
	var err error

	// Check if model name matches a group
	var groupCount int
	db.DB.QueryRow("SELECT COUNT(*) FROM groups WHERE name=?", modelName).Scan(&groupCount)

	if strings.HasPrefix(modelName, "group:") || groupCount > 0 {
		groupName := strings.TrimPrefix(modelName, "group:")
		providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, keyAccountID, err = routeByGroup(groupName)
		if err != nil {
			writeError(w, 400, fmt.Sprintf("group routing error: %v", err))
			return
		}
	} else {
		providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, keyAccountID, err = routeByModel(modelName)
		if err != nil {
			writeError(w, 400, fmt.Sprintf("model not found: %s", modelName))
			return
		}
	}

	// Check if provider supports Anthropic format natively
	var supportsAnthropic int
	db.DB.QueryRow("SELECT COALESCE(supports_anthropic, 0) FROM providers WHERE id=?", providerID).Scan(&supportsAnthropic)

	if supportsAnthropic == 0 {
		// Provider does NOT support Anthropic — translate via OpenAI
		handleAnthropicTranslated(w, r, rawBody, providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, keyAccountID, isStream, startTime)
		return
	}

	// Provider supports Anthropic natively — forward as-is
	// But truncate tool names to 64 chars (provider limit)
	truncateAnthropicToolNames(rawBody)

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
				resp2.Body.Close()
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
// Provider-specific routing:
//   - TokenGO/Meta: {base}/v1/messages (same host, just /messages)
//   - MiMo/DeepSeek/others: {base}/anthropic/v1/messages
func resolveAnthropicUpstreamURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	lower := strings.ToLower(base)

	// Meta: /v1/messages (native Anthropic-compatible endpoint)
	if strings.Contains(lower, "meta") || strings.Contains(lower, "meta.ai") {
		if strings.HasSuffix(base, "/v1") {
			return base + "/messages"
		}
		return base + "/v1/messages"
	}

	// Hcnsec: /v1/messages (native Anthropic-compatible endpoint)
	if strings.Contains(lower, "hcnsec") {
		if strings.HasSuffix(base, "/v1") {
			return base + "/messages"
		}
		return base + "/v1/messages"
	}

	// TokenGO: same base URL, just /v1/messages
	if strings.Contains(lower, "tokengo") {
		if strings.HasSuffix(base, "/v1") {
			return base + "/messages"
		}
		return base + "/v1/messages"
	}

	// OpenRouter: /api/v1/messages
	if strings.Contains(lower, "openrouter") {
		return base + "/messages"
	}

	// MiMo/DeepSeek/others: replace /v1 with /anthropic/v1/messages
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

// compressAnthropicToolResults compresses tool results in Anthropic messages
// using headroom. Anthropic tool results are in role:"user" messages with
// content containing tool_result blocks. We extract those, convert to OpenAI
// tool format, run headroom compression, and write back.
//
// Deferred: full Anthropic↔OpenAI message translation. Headroom's /v1/compress
// only speaks OpenAI messages[] shape, so we only compress the tool results
// (the largest part of most requests) and leave the rest untouched.
func compressAnthropicToolResults(messages []interface{}, model string) []interface{} {
	// Early return if headroom is disabled — skip the full scan
	if getSettingStrCached("headroom_enabled", "false") != "true" {
		return messages
	}

	type toolResult struct {
		msgIdx    int
		blockIdx  int
		content   string
		isError   bool
		hasStatus bool
	}

	var toolResults []toolResult

	for i, msg := range messages {
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}

		role, _ := msgMap["role"].(string)
		if role != "user" {
			continue
		}

		content := msgMap["content"]
		contentArray, ok := content.([]interface{})
		if !ok {
			continue
		}

		for j, block := range contentArray {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}

			if blockMap["type"] != "tool_result" {
				continue
			}

			// Skip error tool results — model needs them verbatim
			isError, _ := blockMap["is_error"].(bool)
			status, _ := blockMap["status"].(string)
			if isError || status == "error" {
				continue
			}

			// Extract content — can be string or array of content blocks
			var builder strings.Builder
			switch v := blockMap["content"].(type) {
			case string:
				builder.WriteString(v)
			case []interface{}:
				for _, item := range v {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if itemMap["type"] == "text" {
							if text, ok := itemMap["text"].(string); ok {
								builder.WriteString(text)
							}
						}
					}
				}
			}

			toolContent := builder.String()
			if len(toolContent) >= headroomMinCompressSize {
				toolResults = append(toolResults, toolResult{i, j, toolContent, isError, status != ""})
			}
		}
	}

	if len(toolResults) == 0 {
		return messages
	}

	// Build OpenAI-style tool messages for headroom
	openAIMessages := make([]map[string]interface{}, len(toolResults))
	for i, tr := range toolResults {
		msg := map[string]interface{}{
			"role":    "tool",
			"content": tr.content,
		}
		// Propagate is_error/status so headroom skips error traces
		if tr.isError {
			msg["is_error"] = true
		}
		if tr.hasStatus {
			msg["status"] = "error"
		}
		openAIMessages[i] = msg
	}

	compressedMessages := CompressWithHeadroom(openAIMessages, model)

	// Write compressed content back into Anthropic messages
	for i, tr := range toolResults {
		if i >= len(compressedMessages) {
			continue
		}
		compressedContent, _ := compressedMessages[i]["content"].(string)
		if compressedContent == "" || len(compressedContent) >= len(tr.content) {
			continue // phantom or no savings
		}

		msgMap := messages[tr.msgIdx].(map[string]interface{})
		contentArr := msgMap["content"].([]interface{})
		blockMap := contentArr[tr.blockIdx].(map[string]interface{})

		// Preserve original content shape — array stays array, string stays string
		switch blockMap["content"].(type) {
		case []interface{}:
			// Rebuild array with compressed text
			blockMap["content"] = []interface{}{
				map[string]interface{}{"type": "text", "text": compressedContent},
			}
		default:
			blockMap["content"] = compressedContent
		}
	}

	return messages
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

// handleAnthropicTranslated handles Anthropic requests to providers that don't support
// Anthropic format. It translates: Anthropic request → OpenAI request → provider →
// OpenAI response → Anthropic response.
func handleAnthropicTranslated(w http.ResponseWriter, r *http.Request,
	rawBody map[string]interface{},
	providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, keyAccountID string,
	isStream bool, startTime time.Time) {

	// Convert Anthropic request to OpenAI format
	openaiBody, err := translator.AnthropicToOpenAIRequest(rawBody)
	if err != nil {
		writeError(w, 400, fmt.Sprintf("translation error: %v", err))
		return
	}

	// Override model ID
	openaiBody["model"] = modelID

	// Force usage reporting in streaming
	if isStream {
		openaiBody["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	bodyBytes, err := json.Marshal(openaiBody)
	if err != nil {
		writeError(w, 500, "failed to marshal translated request")
		return
	}

	// Build OpenAI-style upstream URL
	upstreamURL := resolveUpstreamURL(baseURL, keyAccountID)
	log.Printf("[TRANSLATE] Anthropic→OpenAI: %s %s (model=%s)", providerName, upstreamURL, modelID)

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, 500, "failed to create upstream request")
		return
	}

	// Set auth (OpenAI-style Bearer token)
	setProviderAuth(req, baseURL, keyValue)

	// Use proxy if configured
	client := sharedHTTPClient
	var proxyUsed string
	if proxyURL := getProviderProxy(providerID); proxyURL != "" {
		proxyUsed = proxyURL
		if transport, terr := makeProxyTransport(proxyURL); terr == nil {
			client.Transport = transport
		}
	}

	resp, err := client.Do(req)
	latencyMs := time.Since(startTime).Milliseconds()

	if err != nil {
		logProxyRequest(providerID, providerName, modelID, keyID, keyName, "", proxyUsed, 0, 0, 0, latencyMs, err.Error())
		writeError(w, 502, fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	// Handle non-200 with fallback
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		errBodyStr := string(errBody)
		if len(errBodyStr) > 500 {
			errBodyStr = errBodyStr[:500]
		}
		autoDisableKey(keyID, keyName, resp.StatusCode, errBodyStr)

		// Try fallback keys
		tried := map[string]bool{keyID: true}
		for {
			nextKeyID, nextKeyName, nextKeyValue, _, ferr := getNextActiveKeyExcluding(providerID, tried)
			if ferr != nil {
				logProxyRequest(providerID, providerName, modelID, keyID, keyName, "", proxyUsed, resp.StatusCode, 0, 0, latencyMs, "all keys exhausted")
				w.Header().Set("Content-Type", "application/json")
				writeError(w, resp.StatusCode, fmt.Sprintf("all keys exhausted for provider %s", providerName))
				return
			}
			tried[nextKeyID] = true

			req2, _ := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
			setProviderAuth(req2, baseURL, nextKeyValue)
			resp2, err2 := client.Do(req2)
			latencyMs = time.Since(startTime).Milliseconds()

			if err2 != nil {
				logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, "", proxyUsed, 0, 0, 0, latencyMs, err2.Error())
				continue
			}

			if resp2.StatusCode == 200 {
				// Success — translate response
				if isStream {
					tIn, tOut := handleTranslatedStreaming(w, resp2, modelID)
					logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, "", proxyUsed, 200, tIn, tOut, latencyMs, "")
				} else {
					tIn, tOut := handleTranslatedNonStreaming(w, resp2)
					logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, "", proxyUsed, 200, tIn, tOut, latencyMs, "")
				}
				return
			}

			resp2.Body.Close()
			errBody2, _ := io.ReadAll(resp2.Body)
			errBody2Str := string(errBody2)
			if len(errBody2Str) > 500 {
				errBody2Str = errBody2Str[:500]
			}
			autoDisableKey(nextKeyID, nextKeyName, resp2.StatusCode, errBody2Str)
		}
	}

	// Success — translate response
	if isStream {
		tIn, tOut := handleTranslatedStreaming(w, resp, modelID)
		logProxyRequest(providerID, providerName, modelID, keyID, keyName, "", proxyUsed, 200, tIn, tOut, latencyMs, "")
	} else {
		tIn, tOut := handleTranslatedNonStreaming(w, resp)
		logProxyRequest(providerID, providerName, modelID, keyID, keyName, "", proxyUsed, 200, tIn, tOut, latencyMs, "")
	}
}

// handleTranslatedStreaming converts OpenAI SSE stream to Anthropic SSE format
func handleTranslatedStreaming(w http.ResponseWriter, upstreamResp *http.Response, model string) (int, int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("Warning: ResponseWriter does not support Flushing")
		return 0, 0
	}

	st := translator.NewStreamTranslator(w, flusher.Flush, model)
	tIn, tOut := st.ProcessReader(upstreamResp.Body)
	return tIn, tOut
}

// handleTranslatedNonStreaming converts OpenAI JSON response to Anthropic JSON format
func handleTranslatedNonStreaming(w http.ResponseWriter, upstreamResp *http.Response) (int, int) {
	body, err := io.ReadAll(upstreamResp.Body)
	if err != nil {
		log.Printf("Error reading translated response: %v", err)
		w.WriteHeader(500)
		return 0, 0
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		// Can't parse — return as-is
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upstreamResp.StatusCode)
		w.Write(body)
		return 0, 0
	}

	// Convert OpenAI response to Anthropic format
	anthropicResp := translator.OpenAIToAnthropicResponse(openaiResp)

	var tokensIn, tokensOut int
	if usage, ok := anthropicResp["usage"].(map[string]interface{}); ok {
		if it, ok := usage["input_tokens"].(int); ok {
			tokensIn = it
		}
		if ot, ok := usage["output_tokens"].(int); ok {
			tokensOut = ot
		}
	}

	respBytes, _ := json.Marshal(anthropicResp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(respBytes)

	return tokensIn, tokensOut
}

// truncateAnthropicToolNames truncates tool names >64 chars in Anthropic format requests.
// Modifies rawBody in-place. Handles both top-level tools[] and tool_use in messages[].
func truncateAnthropicToolNames(rawBody map[string]interface{}) {
	// Truncate tool definitions
	if tools, ok := rawBody["tools"].([]interface{}); ok {
		for _, t := range tools {
			if tm, ok := t.(map[string]interface{}); ok {
				if name, ok := tm["name"].(string); ok && len(name) > 64 {
					tm["name"] = name[:64]
				}
			}
		}
	}

	// Truncate tool_use names in messages
	if messages, ok := rawBody["messages"].([]interface{}); ok {
		for _, msg := range messages {
			mm, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}
			content, ok := mm["content"].([]interface{})
			if !ok {
				continue
			}
			for _, block := range content {
				bm, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				if bm["type"] == "tool_use" {
					if name, ok := bm["name"].(string); ok && len(name) > 64 {
						bm["name"] = name[:64]
					}
				}
			}
		}
	}
}
