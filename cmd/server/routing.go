package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dolvin/paap/cmd/server/compression"
	"github.com/dolvin/paap/internal/db"
)

// ── Round-robin counters for each provider ─────────────────
var providerRRCounters sync.Map

// getProviderRRCounter returns the round-robin counter for a provider
func getProviderRRCounter(providerID string) *atomic.Int64 {
	val, ok := providerRRCounters.Load(providerID)
	if ok {
		return val.(*atomic.Int64)
	}
	counter := &atomic.Int64{}
	actual, _ := providerRRCounters.LoadOrStore(providerID, counter)
	return actual.(*atomic.Int64)
}

// isProviderRoundRobin checks if a provider has round-robin enabled
// Reads round_robin_enabled (UI toggle) OR round_robin (direct DB)
// Safe default: round-robin on DB error (spread load, not concentrate)
func isProviderRoundRobin(providerID string) bool {
	var rr, rrEnabled int
	err := db.DB.QueryRow("SELECT COALESCE(round_robin,0), COALESCE(round_robin_enabled,0) FROM providers WHERE id = ?", providerID).Scan(&rr, &rrEnabled)
	if err != nil {
		log.Printf("[PAAP] isProviderRoundRobin DB error for %s: %v — defaulting to round-robin", providerID, err)
		return true // safe default: spread load
	}
	return rr == 1 || rrEnabled == 1
}

// autoDisableKey handles non-2xx responses.
// - 2xx: reset fail_count
// - 5xx: upstream error, don't disable but log (caller still fallbacks to other keys)
// - 402 / billing / quota: immediate disable (1x) — saldo habis permanent
// - Other 4xx: fallback only, NO disable, NO fail_count increment
// Returns true if key was disabled.
func autoDisableKey(keyID, keyName string, statusCode int, errBody string) bool {
	if statusCode >= 200 && statusCode < 300 {
		db.DB.Exec("UPDATE api_keys SET fail_count=0, last_error='' WHERE id=?", keyID)
		return false
	}

	lowerBody := strings.ToLower(errBody)
	if statusCode == 402 || strings.Contains(lowerBody, "quota") || strings.Contains(lowerBody, "billing") {
		db.DB.Exec("UPDATE api_keys SET fail_count=3, last_error=?, last_tested_at=strftime('%s','now'), is_active=0 WHERE id=?",
			errBody, keyID)
		log.Printf("[PAAP] Auto-disabled key %s (%s) — billing/quota exhausted (%d: %s)", keyName, keyID, statusCode, errBody)
		return true
	}

	if statusCode >= 500 {
		// Server error — fallback only, no disable
		log.Printf("[PAAP] Key %s (%s) server error %d: %s", keyName, keyID, statusCode, errBody)
		return false
	}

	// All other 4xx (401, 403, 404, 429, etc): fallback only, no disable, no fail_count change
	log.Printf("[PAAP] Key %s (%s) client error %d: %s", keyName, keyID, statusCode, errBody)
	return false
}

// Auth middleware that validates gateway API keys
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for local connections if configured
		if getSettingStrCached("skip_auth_local", "false") == "true" {
			remoteAddr := r.RemoteAddr
			if strings.HasPrefix(remoteAddr, "127.0.0.1:") || strings.HasPrefix(remoteAddr, "[::1]:") || remoteAddr == "::1" {
				next(w, r)
				return
			}
		}

		// Check if any gateway keys exist
		var count int
		err := db.DB.QueryRow("SELECT COUNT(*) FROM gateway_keys WHERE is_active=1").Scan(&count)
		if err != nil || count == 0 {
			// No gateway keys exist - require user to create one
			writeError(w, 401, "No gateway API keys configured. Create one in Settings > Gateway Keys first.")
			return
		}

		// Extract Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, 401, "missing Authorization header")
			return
		}

		// Expected format: "Bearer sk-xxx"
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(w, 401, "invalid Authorization format, expected: Bearer sk-xxx")
			return
		}

		apiKey := parts[1]

		// Validate key exists and is active
		var keyID string
		err = db.DB.QueryRow("SELECT id FROM gateway_keys WHERE key=? AND is_active=1", apiKey).Scan(&keyID)
		if err != nil {
			writeError(w, 401, "invalid or inactive API key")
			return
		}

		next(w, r)
	}
}

// Main chat completions handler
func chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}

	startTime := time.Now()
	paapOverheadStart := time.Now()

	// Parse full request body as map — forward ALL OpenAI-compatible params
	// (tools, tool_choice, response_format, reasoning_effort, etc.)
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

	// Log incoming request to file
	clientKey := ""
	if k := r.Context().Value("gateway_key_name"); k != nil {
		clientKey, _ = k.(string)
	}
	LogRequest(r.Method, r.URL.Path, clientKey, rawBody)

	// max_tokens: pass through from client, don't override

	// ── Global Prompt Injection (before compression) ───────────
	piEnabled := getSettingStrCached("prompt_injection_enabled", "false") == "true"
	if piEnabled {
		piText := getSettingStrCached("prompt_injection_text", "")
		piPosition := getSettingStrCached("prompt_injection_position", "prepend")
		if piText != "" {
			if msgs, ok := rawBody["messages"].([]interface{}); ok {
				var msgMaps []map[string]interface{}
				for _, m := range msgs {
					if mm, ok := m.(map[string]interface{}); ok {
						msgMaps = append(msgMaps, mm)
					}
				}
				log.Printf("[PAAP] Injecting prompt injection: enabled=%v position=%s text_len=%d", piEnabled, piPosition, len(piText))
				injectSystemPrompt(&msgMaps, piText, piPosition)
				rawBody["messages"] = func() []interface{} {
					out := make([]interface{}, len(msgMaps))
					for i, m := range msgMaps {
						out[i] = m
					}
					return out
				}()
			}
		}
	}

	// ── Compression Mode Injection ───────────────────────────────
	compressionMode := getSettingStrCached("compression_mode", "")
	if compressionMode != "" {
		// Support per-mode levels: "caveman:ultra,ponytail:full"
		modes := strings.Split(compressionMode, ",")
		var combinedText string
		for _, modeEntry := range modes {
			modeEntry = strings.TrimSpace(modeEntry)
			if modeEntry == "" {
				continue
			}
			parts := strings.SplitN(modeEntry, ":", 2)
			mode := strings.TrimSpace(parts[0])
			level := "full" // default
			if len(parts) > 1 {
				level = strings.TrimSpace(parts[1])
			}
			text := getCompressionInstruction(mode, level)
			if text != "" {
				// Caveman voice injection — terse style
				if mode == "caveman" && getSettingStrCached("caveman_voice", "true") == "true" {
					text += `

CAVEMAN VOICE: Drop articles (a/an/the), filler (just/really/basically/actually/simply), pleasantries, hedging. Fragments OK. Short synonyms (big not extensive, fix not implement a solution for). Code blocks, file paths, commands, errors, URLs: keep exact. No decorative emoji. No narrating tool calls. No status phrases. State the thing, the action, the reason. Then next step.`
				}
				if combinedText != "" {
					combinedText += "\n\n"
				}
				combinedText += text
			}
		}
		if combinedText != "" {
			if msgs, ok := rawBody["messages"].([]interface{}); ok {
				var msgMaps []map[string]interface{}
				for _, m := range msgs {
					if mm, ok := m.(map[string]interface{}); ok {
						msgMaps = append(msgMaps, mm)
					}
				}
				log.Printf("[PAAP] Compression mode=%s text_len=%d", compressionMode, len(combinedText))
				injectSystemPrompt(&msgMaps, combinedText, "prepend")
				rawBody["messages"] = func() []interface{} {
					out := make([]interface{}, len(msgMaps))
					for i, m := range msgMaps {
						out[i] = m
					}
					return out
				}()
			}
		}
	}

	// ── Unified Smart Compression ──────────────────────────
	var compressionTokensBefore, compressionTokensSaved int
	compressLevel := resolveCompressionLevel()
	log.Printf("[compression] resolved level=%s (off=%v)", compressLevel.String(), compressLevel == compression.LevelOff)
	if compressLevel != compression.LevelOff {
		if msgs, ok := rawBody["messages"].([]interface{}); ok {
			var msgMaps []map[string]interface{}
			for _, m := range msgs {
				if mm, ok := m.(map[string]interface{}); ok {
					msgMaps = append(msgMaps, mm)
				}
			}
			results := compression.CompressRawMessages(msgMaps, compressLevel, modelName)
			// Log compression events to DB + track totals
			var totalOrigBytes, totalSavedBytes int
			for _, r := range results {
				totalOrigBytes += r.OriginalSize
				if r.Savings > 0 {
					totalSavedBytes += r.Savings
					logCompressionEvent("tool", compressLevel.String(), r.OriginalSize, r.CompressedSize)
				}
			}
			// Store for logging later
			compressionTokensBefore = totalOrigBytes / 4
			compressionTokensSaved = totalSavedBytes / 4
			addCompressionStats(int64(compressionTokensBefore), int64(compressionTokensSaved))
			rawBody["messages"] = func() []interface{} {
				out := make([]interface{}, len(msgMaps))
				for i, m := range msgMaps {
					out[i] = m
				}
				return out
			}()
		}
	}

	// ── Tool System: auto-route based on content detection ───
	var toolUsed string
	var originalModel string
	if toolMatch := ProcessTools(rawBody); toolMatch != nil {
		// Remember original model for logging
		originalModel = modelName
		toolUsed = toolMatch.ToolName
		// Try each model in fallback chain
		var routedModel string
		for _, candidate := range toolMatch.Models {
			if _, _, _, _, _, _, _, _, routeErr := routeByModel(candidate); routeErr != nil {
				log.Printf("[PAAP] [TOOLS] Vision fallback: %s failed (%v), trying next", candidate, routeErr)
				continue
			}
			routedModel = candidate
			break
		}
		if routedModel != "" {
			rawBody["model"] = routedModel
			modelName = routedModel
			log.Printf("[PAAP] [TOOLS] Model overridden: %s → %s (tool: %s)", originalModel, routedModel, toolUsed)
		} else {
			log.Printf("[PAAP] [TOOLS] All vision models exhausted, using original: %s", modelName)
			toolUsed = ""
			originalModel = ""
		}
	}

	// ── Vision Tool (legacy): replace images with text descriptions ────
	if getSettingStrCached("vision_enabled", "false") == "true" {
		rawBody = applyVisionTool(rawBody)
	}
	// If no reasoning_effort at all and model contains meta/muse/lama → inject low (cheapest, safe)
	// We don't know final provider yet here (routeByModel), so we check model name heuristic + inject after routing

	// Determine routing: group or direct model
	// Auto-detect: if model name matches a group name → route as group
	var (
		providerID   string
		providerName string
		baseURL      string
		modelID      string
		keyID        string
		keyName      string
		keyValue     string
		keyAccountID string
		groupName    string
		err          error
	)

	// Check if model name matches a group
	var groupCount int
	db.DB.QueryRow("SELECT COUNT(*) FROM groups WHERE name=?", modelName).Scan(&groupCount)

	if strings.HasPrefix(modelName, "group:") || groupCount > 0 {
		// Group-based routing — PARALLEL RACE: fire all models, take first winner
		groupName = strings.TrimPrefix(modelName, "group:")
		handleGroupRace(w, r, modelName, groupName, rawBody)
		return
	} else {
		// Direct model routing
		providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, keyAccountID, err = routeByModel(modelName)
		if err != nil {
			writeError(w, 400, fmt.Sprintf("model routing error: %v", err))
			return
		}
	}

	// ── Meta default reasoning_effort injection (after routing, now we know provider) ──
	// Meta/Muse Spark: "none" → "low" (Meta rejects "none"), inject "low" if missing
	if strings.Contains(strings.ToLower(baseURL), "meta.ai") || strings.Contains(strings.ToLower(providerName), "meta") || strings.Contains(modelID, "muse-spark") {
		if re, ok := rawBody["reasoning_effort"].(string); ok && re == "none" {
			rawBody["reasoning_effort"] = "low"
		}
		if _, hasRE := rawBody["reasoning_effort"]; !hasRE {
			if _, hasReasoning := rawBody["reasoning"]; !hasReasoning {
				rawBody["reasoning_effort"] = "low"
			}
		}
	}

	// Normalize reasoning_effort — only none/low/medium/high are valid
	if re, ok := rawBody["reasoning_effort"].(string); ok {
		switch re {
		case "none", "low", "medium", "high":
			// valid
		default:
			rawBody["reasoning_effort"] = "high"
		}
	}
	if reasoning, ok := rawBody["reasoning"].(map[string]interface{}); ok {
		if eff, ok := reasoning["effort"].(string); ok {
			switch eff {
			case "none", "low", "medium", "high":
				// valid
			default:
				reasoning["effort"] = "high"
				rawBody["reasoning"] = reasoning
			}
		}
	}
	// Build upstream body — forward ALL client params, override only model + max_tokens
	upstreamBody := make(map[string]interface{})
	isMerlin := strings.Contains(strings.ToLower(baseURL), "getmerlin")

	if isMerlin {
		// Merlin uses completely different request format
		upstreamBody = convertToMerlinBody(rawBody, modelID)
		log.Printf("[MERLIN] Converted body: model=%s content=%s", modelID, upstreamBody["message"].(map[string]interface{})["content"].(string)[:min(len(upstreamBody["message"].(map[string]interface{})["content"].(string)), 100)])
	} else {
		for k, v := range rawBody {
			upstreamBody[k] = v
		}
		// Force usage reporting in streaming responses
		if isStream {
			upstreamBody["stream_options"] = map[string]interface{}{"include_usage": true}
		}
		upstreamBody["model"] = modelID
	}

	// Strip native web_search tools — unsupported providers reject them with 400
	if tools, ok := upstreamBody["tools"].([]interface{}); ok {
		filtered := []interface{}{}
		for _, t := range tools {
			if m, ok := t.(map[string]interface{}); ok && m["type"] == "web_search" {
				continue
			}
			filtered = append(filtered, t)
		}
		upstreamBody["tools"] = filtered
	}

	bodyBytes, err := json.Marshal(upstreamBody)
	if err != nil {
		writeError(w, 500, "failed to marshal request body")
		return
	}

	// ── Anigravity: use Google Gemini format ──
	if providerID == "builtin-anigravity" {
		anigravityRequest(w, r, modelID, rawBody, keyValue, isStream)
		return
	}

	upstreamURL := resolveUpstreamURL(baseURL, keyAccountID)

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, 500, "failed to create upstream request")
		return
	}

	// Set auth headers based on provider
	setProviderAuth(req, baseURL, keyValue)

	// Use proxy if configured
	client := sharedHTTPClient
	var proxyUsed string
	if proxyURL := getProviderProxy(providerID); proxyURL != "" {
		proxyUsed = proxyURL
		if transport, err := makeProxyTransport(proxyURL); err == nil {
			client.Transport = transport
		}
	}

	// Execute upstream request with auto-disable + fallback
	reqStartTime := time.Now()
	resp, err := client.Do(req)
	ttfbMs := time.Since(reqStartTime).Milliseconds()
	latencyMs := time.Since(startTime).Milliseconds()
	paapOverheadMs := time.Since(paapOverheadStart).Milliseconds()
	log.Printf("[PAAP] PAAP overhead: %dms (before provider request)", paapOverheadMs)
	log.Printf("[PAAP] TTFB (provider response start): %dms | total: %dms | model: %s", ttfbMs, latencyMs, modelID)

	if isMerlin {
		if err != nil {
			log.Printf("[MERLIN] Request error: %v", err)
		} else {
			log.Printf("[MERLIN] Response status: %d, latency: %dms", resp.StatusCode, latencyMs)
		}
	}

	if err != nil {
		// Timeout — try next key before giving up
		logProxyRequest(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed, 504, 0, 0, latencyMs, err.Error(), nil)
		// Try remaining keys on timeout
		tried := map[string]bool{keyID: true}
		for {
			nextKeyID, nextKeyName, nextKeyValue, _, ferr := getNextActiveKeyExcluding(providerID, tried)
			if ferr != nil {
				writeError(w, 504, fmt.Sprintf("upstream timeout: %v", err))
				return
			}
			tried[nextKeyID] = true
			req2, _ := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
			setProviderAuth(req2, baseURL, nextKeyValue)
			for k, v := range req.Header {
				req2.Header[k] = v
			}
			startTime2 := time.Now()
			resp2, err2 := client.Do(req2)
			latencyMs2 := time.Since(startTime2).Milliseconds()
			if err2 == nil && resp2.StatusCode == 200 {
				if isStream {
					tIn, tOut, streamBody := handleStreaming(w, resp2)
					resp2.Body.Close()
					logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, groupName, proxyUsed, 200, tIn, tOut, latencyMs2, "", streamBody)
				} else {
					bodyBytes3, _ := io.ReadAll(resp2.Body)
					resp2.Body.Close()
					logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, groupName, proxyUsed, 200, 0, 0, latencyMs2, "", bodyBytes3)
					w.Header().Set("Content-Type", "application/json")
					w.Write(bodyBytes3)
				}
				return
			}
			if resp2 != nil {
				resp2.Body.Close()
			}
			logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, groupName, proxyUsed, 504, 0, 0, latencyMs2, fmt.Sprintf("retry timeout: %v", err2), nil)
		}
	}
	defer resp.Body.Close()

	// On non-200: increment fail_count, auto-disable after 3 consecutive failures

	if resp.StatusCode != 200 {
		// Read error body for diagnostics before closing
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		errBodyStr := string(errBody)
		if len(errBodyStr) > 500 {
			errBodyStr = errBodyStr[:500]
		}
		autoDisableKey(keyID, keyName, resp.StatusCode, errBodyStr)

		// Track tried keys to prevent infinite loop
		tried := map[string]bool{keyID: true}
		// On non-auth error (429/500/503), break after trying all other keys
		// On auth error (401/403/402), disable and try all other keys

		// Fallback: try all remaining active keys
		for {
			nextKeyID, nextKeyName, nextKeyValue, _, ferr := getNextActiveKeyExcluding(providerID, tried)
			if ferr != nil {
				// No more active keys — return last error
				logProxyRequest(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed, resp.StatusCode, 0, 0, latencyMs, "all keys exhausted", nil)
				w.Header().Set("Content-Type", "application/json")
				writeError(w, resp.StatusCode, fmt.Sprintf("all keys exhausted for provider %s", providerName))
				return
			}
			tried[nextKeyID] = true

			log.Printf("[PAAP] Fallback to key %s", nextKeyName)
			req2, _ := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
			setProviderAuth(req2, baseURL, nextKeyValue)
			resp2, err2 := client.Do(req2)
			latencyMs = time.Since(startTime).Milliseconds()

			if err2 != nil {
				// Timeout on this key — log with 504, continue to next key
				logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, groupName, proxyUsed, 504, 0, 0, latencyMs, err2.Error(), nil)
				continue // try next key instead of giving up
			}

			if resp2.StatusCode == 200 {
				// Success!
				if isStream {
					tIn, tOut, streamBody := handleStreaming(w, resp2)
					resp2.Body.Close()
					logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, groupName, proxyUsed, 200, tIn, tOut, latencyMs, "", streamBody)
				TrafficLog(TrafficEntry{Model: modelID, Provider: providerName, StatusCode: 200, LatencyMs: latencyMs, CompressMode: compressionMode, PAAPOverheadMs: paapOverheadMs, TTFBMs: ttfbMs, IsStream: true, TokensIn: tIn, TokensOut: tOut})
				} else {
					// Parse tokens from non-streaming response
					bodyBytes2, _ := io.ReadAll(resp2.Body)
					resp2.Body.Close()
					var tokensIn, tokensOut int
					parseUsageJSON(bodyBytes2, &tokensIn, &tokensOut)
					logProxyRequest(providerID, providerName, modelID, nextKeyID, nextKeyName, groupName, proxyUsed, 200, tokensIn, tokensOut, latencyMs, "", bodyBytes2)
					TrafficLog(TrafficEntry{Model: modelID, Provider: providerName, StatusCode: 200, LatencyMs: latencyMs, CompressMode: compressionMode, PAAPOverheadMs: paapOverheadMs, TTFBMs: ttfbMs, IsStream: false, TokensIn: tokensIn, TokensOut: tokensOut})
					w.Header().Set("Content-Type", "application/json")
					w.Write(bodyBytes2)
				}
				return
			}

			// On non-200: increment fail_count, auto-disable after 3 consecutive failures
			errBody2, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()
			errBody2Str := string(errBody2)
			if len(errBody2Str) > 500 {
				errBody2Str = errBody2Str[:500]
			}
			autoDisableKey(nextKeyID, nextKeyName, resp2.StatusCode, errBody2Str)
		}
	}

	// Log the request — extract tokens from non-streaming response
	var tokensIn, tokensOut int
	if isStream {
		if isMerlin {
			handleMerlinStreaming(w, resp, modelID)
			logProxyRequestWithTool(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed, resp.StatusCode, 0, 0, latencyMs, "", nil, toolUsed, originalModel, compressionTokensBefore, compressionTokensSaved)
		} else {
			tIn, tOut, streamBody := handleStreaming(w, resp)
			logProxyRequestWithTool(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed, resp.StatusCode, tIn, tOut, latencyMs, "", streamBody, toolUsed, originalModel, compressionTokensBefore, compressionTokensSaved)
		}
	} else {
		if isMerlin {
			logProxyRequestWithTool(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed, resp.StatusCode, 0, 0, latencyMs, "", nil, toolUsed, originalModel, compressionTokensBefore, compressionTokensSaved)
			handleMerlinNonStreaming(w, resp, modelID)
		} else {
			bodyBytes2, _ := io.ReadAll(resp.Body)
			// Parse usage from response
			parseUsageJSON(bodyBytes2, &tokensIn, &tokensOut)
			logProxyRequestWithTool(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed, resp.StatusCode, tokensIn, tokensOut, latencyMs, "", bodyBytes2, toolUsed, originalModel, compressionTokensBefore, compressionTokensSaved)
			// Add tool header in response if tool was used
			if toolUsed != "" {
				w.Header().Set("X-PAAP-Tool", toolUsed)
				w.Header().Set("X-PAAP-Original-Model", originalModel)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(bodyBytes2)
		}
	}
}

// handleGroupRace dispatches to the correct routing mode based on group settings
func handleGroupRace(w http.ResponseWriter, r *http.Request, modelName, groupName string, rawBody map[string]interface{}) {
	// Global prompt injection (before group-level)
	piEnabled := getSettingStrCached("prompt_injection_enabled", "false") == "true"
	if piEnabled {
		piText := getSettingStrCached("prompt_injection_text", "")
		piPos := getSettingStrCached("prompt_injection_position", "prepend")
		if piText != "" {
			if msgs, ok := rawBody["messages"].([]interface{}); ok {
				var msgMaps []map[string]interface{}
				for _, m := range msgs {
					if mm, ok := m.(map[string]interface{}); ok {
						msgMaps = append(msgMaps, mm)
					}
				}
				injectSystemPrompt(&msgMaps, piText, piPos)
				rawBody["messages"] = func() []interface{} {
					out := make([]interface{}, len(msgMaps))
					for i, m := range msgMaps {
						out[i] = m
					}
					return out
				}()
			}
		}
	}

	// Get group info to determine routing mode
	groupID, raceMode, selectedKeysJSON, selectedModelsJSON, raceCount, maxKeys, _, err := getGroupInfo(groupName)
	if err != nil {
		writeError(w, 400, fmt.Sprintf("group '%s' not found", groupName))
		return
	}

	// Dispatch based on race_mode
	switch raceMode {
	case "race_keys":
		handleGroupRaceKeys(w, r, modelName, groupName, rawBody, raceCount, selectedKeysJSON)
		return
	case "round_robin":
		handleGroupRoundRobinModel(w, r, groupName, rawBody, selectedModelsJSON)
		return
	case "fail_first":
		handleGroupFailFirst(w, r, groupName, rawBody, selectedModelsJSON)
		return
	case "rr_race_keys":
		handleGroupRRRaceKeys(w, r, groupName, rawBody, maxKeys)
		return
	}

	// Default: parallel race all models (original behavior)
	handleGroupRaceAll(w, r, modelName, groupName, groupID, rawBody)
}

// handleGroupRaceAll: original parallel race — fire all models, take first winner
func handleGroupRaceAll(w http.ResponseWriter, r *http.Request, modelName, groupName, groupID string, rawBody map[string]interface{}) {
	startTime := time.Now()
	raceID := genID()

	// Global prompt injection (before group-level)
	piEnabled := getSettingStrCached("prompt_injection_enabled", "false") == "true"
	if piEnabled {
		piText := getSettingStrCached("prompt_injection_text", "")
		piPos := getSettingStrCached("prompt_injection_position", "prepend")
		if piText != "" {
			if msgs, ok := rawBody["messages"].([]interface{}); ok {
				var msgMaps []map[string]interface{}
				for _, m := range msgs {
					if mm, ok := m.(map[string]interface{}); ok {
						msgMaps = append(msgMaps, mm)
					}
				}
				injectSystemPrompt(&msgMaps, piText, piPos)
				rawBody["messages"] = func() []interface{} {
					out := make([]interface{}, len(msgMaps))
					for i, m := range msgMaps {
						out[i] = m
					}
					return out
				}()
			}
		}
	}

	// Get inject prompt (group-level)
	injectPrompt, injectPosition := getGroupInjectPrompt(groupName)

	// Read global race settings
	raceModels := getSettingInt("race_models", 10)
	raceAPIKeys := getSettingInt("race_apikeys", 10)

	rows, err := db.DB.Query(`
		SELECT gm.provider_id, gm.model_id, p.name, p.base_url
		FROM group_models gm
		JOIN providers p ON gm.provider_id = p.id
		WHERE gm.group_id = ? AND p.is_active = 1
		ORDER BY gm.position
	`, groupID)
	if err != nil {
		writeError(w, 500, "failed to query group models")
		return
	}
	defer rows.Close()

	type raceRoute struct {
		providerID   string
		providerName string
		baseURL      string
		modelID      string
	}
	var routes []raceRoute
	for rows.Next() {
		var m raceRoute
		rows.Scan(&m.providerID, &m.modelID, &m.providerName, &m.baseURL)
		routes = append(routes, m)
	}

	if len(routes) == 0 {
		writeError(w, 400, fmt.Sprintf("group '%s' has no active models", groupName))
		return
	}

	// Apply race_models limit
	if len(routes) > raceModels {
		log.Printf("[PAAP] Group '%s' race_models=%d, limiting from %d to %d", groupName, raceModels, len(routes), raceModels)
		routes = routes[:raceModels]
	}

	// For each route, get keys limited by race_apikeys
	type raceTask struct {
		route    raceRoute
		keyID    string
		keyName  string
		keyVal   string
		keyAccID string
	}
	var tasks []raceTask
	for _, rt := range routes {
		allKeys := getAllActiveKeys(rt.providerID)
		if len(allKeys) == 0 {
			continue
		}
		if len(allKeys) > raceAPIKeys {
			allKeys = allKeys[:raceAPIKeys]
		}
		for _, k := range allKeys {
			tasks = append(tasks, raceTask{rt, k.id, k.name, k.value, k.accountID})
		}
	}

	if len(tasks) == 0 {
		writeError(w, 502, "no active keys for any model in group")
		return
	}

	totalModels := len(routes)
	totalKeysPerModel := raceAPIKeys
	totalTasks := len(tasks)
	log.Printf("[PAAP] Group '%s' racing %d models × %d keys = %d tasks", groupName, totalModels, totalKeysPerModel, totalTasks)

	// Normalize reasoning_effort for group race — only none/low/medium/high are valid
	if re, ok := rawBody["reasoning_effort"].(string); ok {
		switch re {
		case "none", "low", "medium", "high":
			// valid
		default:
			rawBody["reasoning_effort"] = "high"
		}
	}
	if reasoning, ok := rawBody["reasoning"].(map[string]interface{}); ok {
		if eff, ok := reasoning["effort"].(string); ok {
			switch eff {
			case "none", "low", "medium", "high":
				// valid
			default:
				reasoning["effort"] = "high"
				rawBody["reasoning"] = reasoning
			}
		}
	}

	// Inject system prompt if configured
	if injectPrompt != "" {
		if msgs, ok := rawBody["messages"].([]interface{}); ok {
			var msgMaps []map[string]interface{}
			for _, m := range msgs {
				if mm, ok := m.(map[string]interface{}); ok {
					msgMaps = append(msgMaps, mm)
				}
			}
			injectSystemPrompt(&msgMaps, injectPrompt, injectPosition)
			rawBody["messages"] = func() []interface{} {
				out := make([]interface{}, len(msgMaps))
				for i, m := range msgMaps {
					out[i] = m
				}
				return out
			}()
		}
	}

	// Build upstream body — forward ALL client params, override only model + max_tokens
	upstreamBody := make(map[string]interface{})
	for k, v := range rawBody {
		upstreamBody[k] = v
	}
	upstreamBody["model"] = "" // will be set per task
	upstreamBody["max_tokens"] = 20000
	// Force usage reporting in streaming responses
	if isStream, _ := rawBody["stream"].(bool); isStream {
		upstreamBody["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	// Race all tasks in parallel
	type raceResult struct {
		task       raceTask
		statusCode int
		body       []byte
		errBody    string
		latencyMs  int64
		proxyUsed  string
		err        error
	}

	resultCh := make(chan raceResult, len(tasks))

	// Check stealth mode setting
	var stealthMode int
	stealthStr := getSettingStrCached("stealth_mode", "1")
	fmt.Sscanf(stealthStr, "%d", &stealthMode)

	var ctx context.Context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, t := range tasks {
		go func(t raceTask) {
			body := make(map[string]interface{})
			for k, v := range upstreamBody {
				body[k] = v
			}
			body["model"] = t.route.modelID

			bodyBytes, _ := json.Marshal(body)
			upstreamURL := resolveUpstreamURL(t.route.baseURL, t.keyAccID)

			req, err := http.NewRequestWithContext(ctx, "POST", upstreamURL, bytes.NewReader(bodyBytes))
			if err != nil {
				resultCh <- raceResult{task: t, err: err}
				return
			}

			setProviderAuth(req, t.route.baseURL, t.keyVal)
			client := &http.Client{Timeout: sharedHTTPClient.Timeout, Transport: sharedHTTPClient.Transport}
			var raceProxyUsed string
			if proxyURL := getProviderProxy(t.route.providerID); proxyURL != "" {
				raceProxyUsed = proxyURL
				if transport, perr := makeProxyTransport(proxyURL); perr == nil {
					client.Transport = transport
				}
			}

			start := time.Now()
			resp, err := client.Do(req)
			lat := time.Since(start).Milliseconds()

			if err != nil {
				resultCh <- raceResult{task: t, err: err, latencyMs: lat, proxyUsed: raceProxyUsed}
				return
			}

			if resp.StatusCode == 200 {
				bodyBytes2, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				resultCh <- raceResult{task: t, statusCode: 200, body: bodyBytes2, latencyMs: lat, proxyUsed: raceProxyUsed}
			} else {
				errBodyBytes, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				errBodyStr := string(errBodyBytes)
				if len(errBodyStr) > 300 {
					errBodyStr = errBodyStr[:300]
				}
				autoDisableKey(t.keyID, t.keyName, resp.StatusCode, errBodyStr)
				resultCh <- raceResult{task: t, statusCode: resp.StatusCode, errBody: errBodyStr, latencyMs: lat, proxyUsed: raceProxyUsed}
			}
		}(t)
	}

	// Wait for first success or all failures
	var failures []string
	responseSent := false
	for i := 0; i < len(tasks); i++ {
		res := <-resultCh
		if res.err != nil {
			failures = append(failures, fmt.Sprintf("[%s:%s] %v", res.task.route.providerName, res.task.route.modelID, res.err))
			logRaceTask(raceID, groupName, totalModels, totalTasks, res.task.route.providerName, res.task.route.modelID, res.task.keyName, "error", 0, 0, res.latencyMs, res.proxyUsed, res.err.Error())
			continue
		}

		if res.statusCode == 200 {
			totalMs := time.Since(startTime).Milliseconds()
			var tokensIn, tokensOut int
			parseUsageJSON(res.body, &tokensIn, &tokensOut)
			if tokensIn == 0 && tokensOut == 0 {
				parseUsageSSE(res.body, &tokensIn, &tokensOut)
			}

			if !responseSent {
				responseSent = true
				logRaceTask(raceID, groupName, totalModels, totalTasks, res.task.route.providerName, res.task.route.modelID, res.task.keyName, "winner", tokensIn, tokensOut, totalMs, res.proxyUsed, "")
				log.Printf("[PAAP] Group race winner: %s/%s (%dms)", res.task.route.providerName, res.task.route.modelID, totalMs)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				w.Write(res.body)
				if cancel != nil {
					cancel()
				}
			} else {
				logRaceTask(raceID, groupName, totalModels, totalTasks, res.task.route.providerName, res.task.route.modelID, res.task.keyName, "completed", tokensIn, tokensOut, totalMs, res.proxyUsed, "")
			}
			continue
		}

		logRaceTask(raceID, groupName, totalModels, totalTasks, res.task.route.providerName, res.task.route.modelID, res.task.keyName, fmt.Sprintf("failed:%d", res.statusCode), 0, 0, res.latencyMs, res.proxyUsed, fmt.Sprintf("status %d: %s", res.statusCode, res.errBody))
		failures = append(failures, fmt.Sprintf("[%s:%s] status %d: %s", res.task.route.providerName, res.task.route.modelID, res.statusCode, res.errBody))
	}

	if responseSent {
		return
	}

	logRaceTask(raceID, groupName, totalModels, totalTasks, "", "", "", "all-failed", 0, 0, 0, "", "all tasks failed")
	writeError(w, 502, fmt.Sprintf("all group models failed: %s", strings.Join(failures, "; ")))
}

// routeByModel finds provider, key for a direct model name
func routeByModel(model string) (providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, keyAccountID string, err error) {
	// Strip "claude-" prefix added by Claude Code CLI
	// e.g. "claude-hcnsec/DeepSeek-V4-Flash" → "hcnsec/DeepSeek-V4-Flash"
	// e.g. "claude-meta-(llama)/muse-spark-1.1" → "meta-(llama)/muse-spark-1.1"
	origModel := model
	if strings.HasPrefix(model, "claude-") {
		model = strings.TrimPrefix(model, "claude-")
	}
	// Strip "[1m]" suffix (Claude Code 1M context window marker)
	model = strings.TrimSuffix(model, "[1m]")
	model = strings.TrimSuffix(model, "[1M]")

	// Handle provider/model format (e.g., "grok-cli/grok-4.5")
	if parts := strings.SplitN(model, "/", 2); len(parts) == 2 {
		provSlug := parts[0]
		actualModel := parts[1]
		var pName, pBaseURL string
		// Try by ID first, then by slugified name
		err = db.DB.QueryRow("SELECT id, name, base_url FROM providers WHERE id=? AND is_active=1", provSlug).Scan(&providerID, &pName, &pBaseURL)
		if err != nil {
			// Try builtin_id
			err = db.DB.QueryRow("SELECT id, name, base_url FROM providers WHERE builtin_id=? AND is_active=1", provSlug).Scan(&providerID, &pName, &pBaseURL)
		}
		if err != nil {
			// Search all providers for slug match
			rows, qErr := db.DB.Query("SELECT id, name, base_url FROM providers WHERE is_active=1")
			if qErr == nil {
				defer rows.Close()
				for rows.Next() {
					var pid, pname, purl string
					rows.Scan(&pid, &pname, &purl)
					slug := strings.ToLower(pname)
					slug = strings.ReplaceAll(slug, " ", "-")
					slug = strings.ReplaceAll(slug, "_", "-")
					slug = strings.ReplaceAll(slug, "(", "-")
					slug = strings.ReplaceAll(slug, ")", "")
					slug = strings.ReplaceAll(slug, "--", "-")
					slug = strings.Trim(slug, "-")
					if slug == provSlug {
						providerID = pid
						pName = pname
						pBaseURL = purl
						err = nil
						break
					}
				}
			}
		}
		if err == nil && providerID != "" {
			providerName = pName
			baseURL = pBaseURL
			modelID = actualModel

			// Get key (apikey or OAuth) via standard path
			keyID, keyName, keyValue, keyAccountID, err = getNextActiveKey(providerID)
			if err != nil {
				// Fallback: check provider_connections for OAuth tokens
				var connID, connEmail, connToken string
				connErr := db.DB.QueryRow(`SELECT id, email, access_token FROM provider_connections
					WHERE provider_id=? AND is_active=1 ORDER BY created_at DESC LIMIT 1`, providerID).Scan(&connID, &connEmail, &connToken)
				if connErr != nil {
					return "", "", "", "", "", "", "", "", fmt.Errorf("no active API keys or connections for provider '%s'", providerName)
				}
				keyID = connID
				keyName = connEmail
				keyValue = connToken
				keyAccountID = ""
				err = nil
			}

			// Auto-refresh OAuth key if expired
			var keyType string
			db.DB.QueryRow("SELECT COALESCE(key_type,'apikey') FROM api_keys WHERE id=?", keyID).Scan(&keyType)
			if keyType == "oauth" {
				var expiresAt string
				db.DB.QueryRow("SELECT COALESCE(oauth_expires_at,'') FROM api_keys WHERE id=?", keyID).Scan(&expiresAt)
				if refreshed, refreshErr := GetOAuthKeyValue(keyID, keyValue, expiresAt); refreshErr == nil {
					keyValue = refreshed
				}
			}
			return
		}
		// If provider not found by ID, fall through to normal model lookup
	}

	// Find model in models table — try model_id first, then id (for claude-* aliases)
	err = db.DB.QueryRow(`
		SELECT m.provider_id, m.model_id, p.name, p.base_url
		FROM models m
		JOIN providers p ON m.provider_id = p.id
		WHERE m.model_id = ? AND m.is_selected = 1 AND p.is_active = 1
		LIMIT 1
	`, model).Scan(&providerID, &modelID, &providerName, &baseURL)
	if err != nil {
		// Try by id column (for claude-* prefixed aliases)
		err = db.DB.QueryRow(`
			SELECT m.provider_id, m.model_id, p.name, p.base_url
			FROM models m
			JOIN providers p ON m.provider_id = p.id
			WHERE m.id = ? AND m.is_selected = 1 AND p.is_active = 1
			LIMIT 1
		`, model).Scan(&providerID, &modelID, &providerName, &baseURL)
		if err != nil {
			// Try with original claude-prefixed model name
			if origModel != model {
				err = db.DB.QueryRow(`
					SELECT m.provider_id, m.model_id, p.name, p.base_url
					FROM models m
					JOIN providers p ON m.provider_id = p.id
					WHERE m.model_id = ? AND m.is_selected = 1 AND p.is_active = 1
					LIMIT 1
				`, origModel).Scan(&providerID, &modelID, &providerName, &baseURL)
			}
			if err != nil {
				return "", "", "", "", "", "", "", "", fmt.Errorf("model '%s' not found or not selected", origModel)
			}
		}
	}

	// Get a key for this provider (round-robin) — handles both apikey and OAuth
	keyID, keyName, keyValue, keyAccountID, err = getNextActiveKey(providerID)
	if err != nil {
		// Fallback: check provider_connections for OAuth tokens (Anigravity, etc.)
		var connID, connEmail, connToken, connRefresh string
		var connExpires int64
		connErr := db.DB.QueryRow(`SELECT id, email, access_token, COALESCE(refresh_token,''), COALESCE(expires_at,0) 
			FROM provider_connections WHERE provider_id=? AND is_active=1 ORDER BY created_at DESC LIMIT 1`, providerID).Scan(
			&connID, &connEmail, &connToken, &connRefresh, &connExpires)
		if connErr != nil {
			return "", "", "", "", "", "", "", "", fmt.Errorf("no active API keys or connections for provider '%s'", providerName)
		}
		keyID = connID
		keyName = connEmail
		keyAccountID = ""
		err = nil

		// Proactive refresh: check expiry with 5 min lead, mutex per connection,
		// merge refresh_token, atomic DB update. On failure: deactivate connection
		// and surface clear "reconnect" error instead of silent 401.
		var refreshErr error
		keyValue, refreshErr = ensureAnigravityToken(connID, connRefresh)
		if refreshErr != nil {
			return "", "", "", "", "", "", "", "", fmt.Errorf("anigravity token error: %v", refreshErr)
		}
	}

	// Auto-refresh OAuth key if expired
	var keyType string
	db.DB.QueryRow("SELECT COALESCE(key_type,'apikey') FROM api_keys WHERE id=?", keyID).Scan(&keyType)
	if keyType == "oauth" {
		var expiresAt string
		db.DB.QueryRow("SELECT COALESCE(oauth_expires_at,'') FROM api_keys WHERE id=?", keyID).Scan(&expiresAt)
		refreshed, refreshErr := GetOAuthKeyValue(keyID, keyValue, expiresAt)
		if refreshErr != nil {
			return "", "", "", "", "", "", "", "", fmt.Errorf("OAuth key '%s' expired and refresh failed: %v", keyName, refreshErr)
		}
		keyValue = refreshed
	}

	return providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, keyAccountID, nil
}

// routeByGroup finds provider, key using round-robin across group models
func routeByGroup(groupName string) (providerID, providerName, baseURL, modelID, keyID, keyName, keyValue, keyAccountID string, err error) {
	// Get group info
	var groupID string
	var roundRobin int
	err = db.DB.QueryRow("SELECT id, round_robin FROM groups WHERE name = ?", groupName).Scan(&groupID, &roundRobin)
	if err != nil {
		return "", "", "", "", "", "", "", "", fmt.Errorf("group '%s' not found", groupName)
	}

	// Get models in this group
	rows, err := db.DB.Query(`
		SELECT gm.provider_id, gm.model_id, p.name, p.base_url
		FROM group_models gm
		JOIN providers p ON gm.provider_id = p.id
		WHERE gm.group_id = ? AND p.is_active = 1
		ORDER BY gm.position
	`, groupID)
	if err != nil {
		return "", "", "", "", "", "", "", "", err
	}
	defer rows.Close()

	type groupModel struct {
		providerID   string
		providerName string
		baseURL      string
		modelID      string
	}
	var models []groupModel
	for rows.Next() {
		var m groupModel
		rows.Scan(&m.providerID, &m.modelID, &m.providerName, &m.baseURL)
		models = append(models, m)
	}

	if len(models) == 0 {
		return "", "", "", "", "", "", "", "", fmt.Errorf("group '%s' has no active models", groupName)
	}

	// Select model using round-robin if enabled
	var selected groupModel
	if roundRobin == 1 {
		// Use atomic counter for round-robin
		counter := getProviderRRCounter(groupID)
		idx := int(counter.Add(1)-1) % len(models)
		selected = models[idx]
	} else {
		selected = models[0]
	}

	// Get a key for the selected provider
	keyID, keyName, keyValue, keyAccountID, err = getNextActiveKey(selected.providerID)
	if err != nil {
		// Fallback: check provider_connections for OAuth tokens
		var connID, connEmail, connToken string
		connErr := db.DB.QueryRow(`SELECT id, email, access_token FROM provider_connections 
			WHERE provider_id=? AND is_active=1 ORDER BY created_at DESC LIMIT 1`, selected.providerID).Scan(&connID, &connEmail, &connToken)
		if connErr != nil {
			return "", "", "", "", "", "", "", "", fmt.Errorf("no active API keys or connections for provider '%s'", selected.providerName)
		}
		keyID = connID
		keyName = connEmail
		keyValue = connToken
		keyAccountID = ""
		err = nil
	}

	return selected.providerID, selected.providerName, selected.baseURL, selected.modelID, keyID, keyName, keyValue, keyAccountID, nil
}

// getNextActiveKey gets the next active API key for a provider
// If round_robin is ON: rotate between all keys
// If round_robin is OFF: fill-first (use first key until exhausted)
func getNextActiveKey(providerID string) (keyID, keyName, keyValue, accountID string, err error) {
	roundRobin := isProviderRoundRobin(providerID)

	// Always order by created_at ASC for consistent ordering
	rows, err := db.DB.Query("SELECT id, name, key_encrypted, COALESCE(account_id,'') FROM api_keys WHERE provider_id = ? AND is_active = 1 ORDER BY created_at ASC", providerID)
	if err != nil {
		return "", "", "", "", err
	}
	defer rows.Close()
	type apiKey struct {
		id        string
		name      string
		value     string
		accountID string
	}
	var keys []apiKey
	for rows.Next() {
		var k apiKey
		rows.Scan(&k.id, &k.name, &k.value, &k.accountID)
		keys = append(keys, k)
	}

	if len(keys) == 0 {
		return "", "", "", "", fmt.Errorf("no active keys")
	}

	var selected apiKey
	if roundRobin {
		// Round-robin: use atomic counter
		counter := getProviderRRCounter(providerID + "_keys")
		idx := int(counter.Add(1)-1) % len(keys)
		selected = keys[idx]
	} else {
		// Fill-first: always pick the first active key
		selected = keys[0]
	}

	// Update last_used timestamp
	db.DB.Exec("UPDATE api_keys SET last_used = CURRENT_TIMESTAMP WHERE id = ?", selected.id)

	return selected.id, selected.name, selected.value, selected.accountID, nil
}

// getNextActiveKeyExcluding gets next active key, skipping already-tried IDs
func getNextActiveKeyExcluding(providerID string, exclude map[string]bool) (keyID, keyName, keyValue, accountID string, err error) {
	roundRobin := isProviderRoundRobin(providerID)

	// Always order by created_at ASC for consistent ordering
	rows, err := db.DB.Query("SELECT id, name, key_encrypted, COALESCE(account_id,'') FROM api_keys WHERE provider_id = ? AND is_active = 1 ORDER BY created_at ASC", providerID)
	if err != nil {
		return "", "", "", "", err
	}
	defer rows.Close()

	type apiKey struct {
		id        string
		name      string
		value     string
		accountID string
	}
	var keys []apiKey
	for rows.Next() {
		var k apiKey
		rows.Scan(&k.id, &k.name, &k.value, &k.accountID)
		if !exclude[k.id] {
			keys = append(keys, k)
		}
	}

	if len(keys) == 0 {
		return "", "", "", "", fmt.Errorf("no active keys")
	}

	var selected apiKey
	if roundRobin {
		// Round-robin: use atomic counter
		counter := getProviderRRCounter(providerID + "_keys")
		idx := int(counter.Add(1)-1) % len(keys)
		selected = keys[idx]
	} else {
		// Fill-first: always pick the first non-excluded key
		selected = keys[0]
	}

	// Update last_used timestamp
	db.DB.Exec("UPDATE api_keys SET last_used = CURRENT_TIMESTAMP WHERE id = ?", selected.id)

	return selected.id, selected.name, selected.value, selected.accountID, nil
}

// getAllActiveKeys returns ALL active keys for a provider
func getAllActiveKeys(providerID string) []apiKeyRow {
	rows, err := db.DB.Query("SELECT id, name, key_encrypted, COALESCE(account_id,'') FROM api_keys WHERE provider_id = ? AND is_active = 1", providerID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var keys []apiKeyRow
	for rows.Next() {
		var k apiKeyRow
		rows.Scan(&k.id, &k.name, &k.value, &k.accountID)
		keys = append(keys, k)
	}
	return keys
}

type apiKeyRow struct {
	id        string
	name      string
	value     string
	accountID string
}

// getGroupInjectPrompt retrieves the inject prompt for a group
func getGroupInjectPrompt(groupName string) (injectPrompt, injectPosition string) {
	db.DB.QueryRow("SELECT inject_prompt, inject_position FROM groups WHERE name = ?", groupName).
		Scan(&injectPrompt, &injectPosition)
	return injectPrompt, injectPosition
}

// getCompressionInstruction returns compressed instruction text for the given mode and level

// getCompressionInstruction returns compressed instruction text from config files
func getCompressionInstruction(mode, level string) string {
	return GetCompressionPrompt(mode, level)
}

// injectSystemPrompt injects a system prompt into the messages
// Wrapped with behavior rules — AI follows but never discusses
func injectSystemPrompt(messages *[]map[string]interface{}, injectPrompt, injectPosition string) {
	if injectPrompt == "" {
		return
	}

	// Minimal wrapper — most providers accept this
	wrapped := injectPrompt

	msgs := *messages

	if injectPosition == "prepend" {
		// Check if there's already a system message at position 0
		if len(msgs) > 0 && msgs[0]["role"] == "system" {
			// Append to existing system message
			existingContent, _ := msgs[0]["content"].(string)
			msgs[0]["content"] = existingContent + "\n\n" + wrapped
		} else {
			// Insert new system message at the beginning
			systemMsg := map[string]interface{}{
				"role":    "system",
				"content": wrapped,
			}
			*messages = append([]map[string]interface{}{systemMsg}, msgs...)
		}
	} else if injectPosition == "append" {
		// Append to the last user message
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i]["role"] == "user" {
				existingContent, _ := msgs[i]["content"].(string)
				msgs[i]["content"] = existingContent + "\n\n" + wrapped
				break
			}
		}
	}
}

// setProviderAuth sets the appropriate auth headers based on provider
func setProviderAuth(req *http.Request, baseURL, keyValue string) {
	lowerURL := strings.ToLower(baseURL)

	if strings.Contains(lowerURL, "cloudflare") {
		// Cloudflare Workers AI: Bearer token (API token)
		req.Header.Set("Authorization", "Bearer "+keyValue)
	} else if strings.Contains(lowerURL, "xiaomi") {
		// Xiaomi MiMo: Bearer token
		req.Header.Set("Authorization", "Bearer "+keyValue)
	} else if strings.Contains(lowerURL, "google") || strings.Contains(lowerURL, "aistudio") {
		// Google AI Studio (OpenAI-compatible endpoint): Bearer token
		req.Header.Set("Authorization", "Bearer "+keyValue)
	} else {
		// Default: Bearer token
		req.Header.Set("Authorization", "Bearer "+keyValue)
	}

	if strings.Contains(lowerURL, "kimchi") {
		req.Header.Set("User-Agent", "kimchi/0.1.50")
	}

	// grok-cli: set custom headers + OAuth Bearer token
	if strings.Contains(lowerURL, "cli-chat-proxy.grok.com") {
		req.Header.Set("User-Agent", grokUserAgent)
		req.Header.Set("x-xai-token-auth", "xai-grok-cli")
		req.Header.Set("x-grok-client-identifier", grokClientIdentifier)
		req.Header.Set("x-grok-client-version", grokClientVersion)
	}

	if strings.Contains(lowerURL, "getmerlin") {
		req.Header.Set("x-merlin-version", "web-merlin")
		req.Header.Set("x-request-timestamp", time.Now().Format("2006-01-02T15:04:05.000-07:00"))
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36")
	}

	req.Header.Set("Content-Type", "application/json")
}

// resolveUpstreamURL builds the final upstream URL based on provider type + account_id.
// Cloudflare: {base}/accounts/{accountID}/ai/v1/chat/completions
// Others: {base}/chat/completions
func resolveUpstreamURL(baseURL, accountID string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.Contains(base, "cloudflare") && accountID != "" {
		return base + "/accounts/" + accountID + "/ai/v1/chat/completions"
	}
	if strings.Contains(base, "getmerlin") {
		return base + "/arcane/api/v2/thread/unified"
	}
	return base + "/chat/completions"
}

// resolveCompressionLevel determines the compression level from settings.
// New "compress.level" takes precedence. Falls back to old settings.
func resolveCompressionLevel() compression.Level {
	// New setting
	if lvl := getSettingStrCached("compress_level", ""); lvl != "" {
		return compression.ParseLevel(lvl)
	}

	// Old settings mapping
	mode := getSettingStrCached("compression_mode", "")
	headroomOn := getSettingStrCached("headroom_enabled", "false") == "true"
	rtkOn := getSettingStrCached("rtk_enabled", "true") == "true"

	if headroomOn || rtkOn {
		return compression.LevelMedium
	}

	if strings.Contains(mode, "caveman") {
		level := getSettingStrCached("compression_level", "full")
		switch level {
		case "lite":
			return compression.LevelLite
		case "full":
			return compression.LevelMedium
		case "ultra":
			return compression.LevelHigh
		}
		return compression.LevelMedium
	}

	return compression.LevelMedium
}
