package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"github.com/dolvin/paap/internal/db"
)

// ── Merlin Auth ─────────────────────────────────────────

const merlinCDPURL = "http://127.0.0.1:9222"

func merlinAuth(w http.ResponseWriter, r *http.Request) {
	// Open a new tab in Brave via CDP pointing to Merlin
	req, _ := http.NewRequest("PUT", merlinCDPURL+"/json/new?https://www.getmerlin.in/id/chat", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, 502, fmt.Sprintf("CDP connection failed: %v", err))
		return
	}
	defer resp.Body.Close()

	var tabInfo map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tabInfo); err != nil {
		writeError(w, 502, "failed to parse CDP response")
		return
	}

	tabID, _ := tabInfo["id"].(string)
	tabURL, _ := tabInfo["url"].(string)

	writeJSON(w, map[string]interface{}{
		"status":  "ok",
		"message": "Merlin tab opened. Login with your Google account, then click Capture.",
		"tab_id":  tabID,
		"tab_url": tabURL,
	})
}

func merlinCapture(w http.ResponseWriter, r *http.Request) {
	// Find Merlin tab
	resp, err := http.Get(merlinCDPURL + "/json")
	if err != nil {
		writeError(w, 502, fmt.Sprintf("CDP connection failed: %v", err))
		return
	}
	defer resp.Body.Close()

	var tabs []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tabs); err != nil {
		writeError(w, 502, "failed to parse CDP tabs")
		return
	}

	// Find tab with getmerlin.in
	var merlinTab map[string]interface{}
	for _, tab := range tabs {
		url, _ := tab["url"].(string)
		if strings.Contains(url, "getmerlin.in") {
			merlinTab = tab
			break
		}
	}

	if merlinTab == nil {
		writeError(w, 404, "no Merlin tab found. Open Merlin first via /api/merlin/auth")
		return
	}

	tabID, _ := merlinTab["id"].(string)
	wsURL, _ := merlinTab["webSocketDebuggerUrl"].(string)

	if wsURL == "" {
		writeError(w, 502, "no WebSocket URL for Merlin tab")
		return
	}

	// Connect via WebSocket and read localStorage
	token, err := extractFirebaseToken(wsURL)
	if err != nil {
		writeError(w, 502, fmt.Sprintf("failed to extract token: %v", err))
		return
	}

	if token == "" {
		writeError(w, 401, "no Firebase token found. Are you logged in to Merlin?")
		return
	}

	// Check if provider exists
	var providerID string
	err = db.DB.QueryRow("SELECT id FROM providers WHERE name='Merlin'").Scan(&providerID)
	if err != nil {
		// Create provider
		providerID = genID()
		_, err = db.DB.Exec("INSERT INTO providers (id, name, base_url, is_active, round_robin) VALUES (?, ?, ?, 1, 1)",
			providerID, "Merlin", "https://www.getmerlin.in")
		if err != nil {
			writeError(w, 500, fmt.Sprintf("failed to create provider: %v", err))
			return
		}
		// Register models
		models := []string{
			"gemini-2.5-flash-lite", "minimax-m2.7", "gemini-3.1-flash-lite",
			"claude-4.5-haiku", "kimi-k2.6", "deepseek-v4-pro", "glm-5.1",
			"grok-4.3", "gemini-3.5-flash", "gemini-3.1-pro", "gpt-5.4",
			"claude-4.6-sonnet", "claude-4.8-opus", "gpt-5.5",
		}
		for _, m := range models {
			mid := genID()
			db.DB.Exec("INSERT INTO models (id, provider_id, model_id, is_selected) VALUES (?, ?, ?, 1)",
				mid, providerID, m)
		}
	}

	// Check if this token already exists
	var existingID string
	err = db.DB.QueryRow("SELECT id FROM api_keys WHERE provider_id=? AND key_encrypted=?", providerID, token).Scan(&existingID)
	if err == nil {
		writeJSON(w, map[string]interface{}{
			"status":  "exists",
			"message": "This exact token is already registered",
			"key_id":  existingID,
			"tab_id":  tabID,
		})
		return
	}

	// Check if there's an existing key for this provider — update it instead of creating new
	var oldKeyID string
	err = db.DB.QueryRow("SELECT id FROM api_keys WHERE provider_id=? AND is_active=1 LIMIT 1", providerID).Scan(&oldKeyID)
	if err == nil {
		// Update existing key with new token
		_, err = db.DB.Exec("UPDATE api_keys SET key_encrypted=?, name=? WHERE id=?", token, "Merlin account", oldKeyID)
		if err != nil {
			writeError(w, 500, fmt.Sprintf("failed to update key: %v", err))
			return
		}
		writeJSON(w, map[string]interface{}{
			"status":     "updated",
			"message":    "Token refreshed successfully!",
			"key_id":     oldKeyID,
			"provider":   "Merlin",
			"tab_id":     tabID,
			"token_len":  len(token),
		})
		return
	}

	// Save new key
	keyID := genID()
	_, err = db.DB.Exec("INSERT INTO api_keys (id, provider_id, name, key_encrypted, is_active) VALUES (?, ?, ?, ?, 1)",
		keyID, providerID, "Merlin account", token)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("failed to save key: %v", err))
		return
	}

	writeJSON(w, map[string]interface{}{
		"status":     "ok",
		"message":    "Merlin account added successfully!",
		"key_id":     keyID,
		"provider":   "Merlin",
		"tab_id":     tabID,
		"token_len":  len(token),
	})
}

func extractFirebaseToken(wsURL string) (string, error) {
	// Connect to CDP via WebSocket
	origin := "http://localhost"
	conn, err := websocket.Dial(wsURL, "", origin)
	if err != nil {
		return "", fmt.Errorf("websocket dial failed: %v", err)
	}
	defer conn.Close()

	// Send Runtime.evaluate to read localStorage
	evalMsg := map[string]interface{}{
		"id":     1,
		"method": "Runtime.evaluate",
		"params": map[string]interface{}{
			"expression": `(function() {
				try {
					var cache = JSON.parse(localStorage.getItem('reactQueryCacheWebApp') || '{}');
					var queries = (cache.clientState || {}).queries || [];
					for (var i = 0; i < queries.length; i++) {
						var q = queries[i];
						if (q && q.state && q.state.data && q.state.data.accessToken) {
							return q.state.data.accessToken;
						}
					}
					return '';
				} catch(e) {
					return 'ERROR:' + e.message;
				}
			})()`,
			"returnByValue": true,
		},
	}

	if err := websocket.JSON.Send(conn, evalMsg); err != nil {
		return "", fmt.Errorf("websocket send failed: %v", err)
	}

	var result map[string]interface{}
	if err := websocket.JSON.Receive(conn, &result); err != nil {
		return "", fmt.Errorf("websocket receive failed: %v", err)
	}

	// Parse result
	resultObj, ok := result["result"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected response format: %v", result)
	}

	innerResult, ok := resultObj["result"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected inner result format: %v", resultObj)
	}

	value, _ := innerResult["value"].(string)
	if strings.HasPrefix(value, "ERROR:") {
		return "", fmt.Errorf("%s", value)
	}

	return value, nil
}

// ── Merlin AI helpers ─────────────────────────────────────

// generateUUID generates a random UUID-like string
func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// convertToMerlinBody converts OpenAI request body to Merlin format
func convertToMerlinBody(rawBody map[string]interface{}, modelID string) map[string]interface{} {
	// Flatten messages into single content string
	messages, _ := rawBody["messages"].([]interface{})
	var parts []string
	for _, msg := range messages {
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		content := ""
		switch c := m["content"].(type) {
		case string:
			content = c
		case []interface{}:
			// Handle vision format (list of content parts)
			for _, part := range c {
				if p, ok := part.(map[string]interface{}); ok {
					if p["type"] == "text" {
						content += p["text"].(string)
					}
				}
			}
		}
		if content == "" {
			continue
		}
		switch role {
		case "system":
			parts = append(parts, "[System]\n"+content)
		case "assistant":
			parts = append(parts, "[Assistant]\n"+content)
		default:
			parts = append(parts, content)
		}
	}

	flattened := "hello"
	if len(parts) > 0 {
		flattened = strings.Join(parts, "\n\n")
	}

	return map[string]interface{}{
		"attachments": []interface{}{},
		"chatId":      generateUUID(),
		"language":    "AUTO",
		"message": map[string]interface{}{
			"childId":  generateUUID(),
			"content":  flattened,
			"context":  "",
			"id":       generateUUID(),
			"parentId": generateUUID(),
		},
		"mode":  "UNIFIED_CHAT",
		"model": modelID,
		"metadata": map[string]interface{}{
			"noTask":          true,
			"isWebpageChat":   false,
			"deepResearch":    false,
			"webAccess":       true,
			"proFinderMode":   false,
			"mcpConfig":       map[string]interface{}{"isEnabled": false},
			"merlinMagic":     false,
		},
	}
}

// handleMerlinStreaming proxies Merlin SSE → OpenAI SSE format
func handleMerlinStreaming(w http.ResponseWriter, upstreamResp *http.Response, modelID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(upstreamResp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("Warning: ResponseWriter does not support Flushing")
		return
	}

	reader := bufio.NewReader(upstreamResp.Body)
	currentEvent := ""
	chatID := fmt.Sprintf("chatcmpl-merlin-%d", time.Now().Unix())

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("[MERLIN-SSE] read error: %v", err)
			}
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse event type
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}

		// Handle error events from Merlin
		if currentEvent == "error" {
			errMsg, _ := data["message"].(string)
			log.Printf("[MERLIN-SSE] Error event: %s", errMsg)
			errChunk := map[string]interface{}{
				"error": map[string]interface{}{
					"message": fmt.Sprintf("Merlin error: %s", errMsg),
					"type":    "upstream_error",
				},
			}
			errBytes, _ := json.Marshal(errChunk)
			fmt.Fprintf(w, "data: %s\n\n", errBytes)
			flusher.Flush()
			return
		}

		if currentEvent == "message" {
			// Check for DONE signal
			if status, ok := data["status"].(string); ok && status == "system" {
				if d, ok := data["data"].(map[string]interface{}); ok {
					if et, ok := d["eventType"].(string); ok && et == "DONE" {
						// Send OpenAI done chunk
						doneChunk := map[string]interface{}{
							"id":      chatID,
							"object":  "chat.completion.chunk",
							"created": time.Now().Unix(),
							"model":   modelID,
							"choices": []interface{}{map[string]interface{}{
								"index":         0,
								"delta":         map[string]interface{}{},
								"finish_reason": "stop",
							}},
						}
						doneBytes, _ := json.Marshal(doneChunk)
						fmt.Fprintf(w, "data: %s\n\n", doneBytes)
						fmt.Fprintf(w, "data: [DONE]\n\n")
						flusher.Flush()
						return
					}
				}
				continue
			}

			// Extract text or reasoning
			msgData, ok := data["data"].(map[string]interface{})
			if !ok {
				continue
			}

			delta := map[string]interface{}{}
			if text, ok := msgData["text"].(string); ok && text != "" {
				delta["content"] = text
			} else if reasoning, ok := msgData["reasoning"].(string); ok && reasoning != "" {
				delta["reasoning_content"] = reasoning
			} else {
				continue
			}

			chunk := map[string]interface{}{
				"id":      chatID,
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   modelID,
				"choices": []interface{}{map[string]interface{}{
					"index":         0,
					"delta":         delta,
					"finish_reason": nil,
				}},
			}
			chunkBytes, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
			flusher.Flush()
		}
	}

	// If we get here without DONE, send done anyway
	doneChunk := map[string]interface{}{
		"id":      chatID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   modelID,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{},
			"finish_reason": "stop",
		}},
	}
	doneBytes, _ := json.Marshal(doneChunk)
	fmt.Fprintf(w, "data: %s\n\n", doneBytes)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// handleMerlinNonStreaming proxies Merlin non-streaming → OpenAI format
func handleMerlinNonStreaming(w http.ResponseWriter, upstreamResp *http.Response, modelID string) {
	// Read full SSE response and extract text
	bodyBytes, _ := io.ReadAll(upstreamResp.Body)
	scanner := bufio.NewScanner(bytes.NewReader(bodyBytes))
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var textParts []string
	currentEvent := ""

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") || (currentEvent != "message" && currentEvent != "error") {
			continue
		}
		dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}

		// Handle error events
		if currentEvent == "error" {
			errMsg, _ := data["message"].(string)
			log.Printf("[MERLIN] Non-stream error: %s", errMsg)
			errorResponse := map[string]interface{}{
				"id":      fmt.Sprintf("chatcmpl-merlin-%d", time.Now().Unix()),
				"object":  "chat.completion",
				"created": time.Now().Unix(),
				"model":   modelID,
				"choices": []interface{}{map[string]interface{}{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": fmt.Sprintf("[Merlin Error: %s]", errMsg),
					},
					"finish_reason": "stop",
				}},
				"usage": map[string]interface{}{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
			}
			respBytes, _ := json.Marshal(errorResponse)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write(respBytes)
			return
		}

		if msgData, ok := data["data"].(map[string]interface{}); ok {
			if text, ok := msgData["text"].(string); ok && text != "" {
				textParts = append(textParts, text)
			}
		}
	}

	// Build OpenAI response
	response := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-merlin-%d", time.Now().Unix()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelID,
		"choices": []interface{}{map[string]interface{}{
			"index": 0,
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": strings.Join(textParts, ""),
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]interface{}{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}

	respBytes, _ := json.Marshal(response)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(respBytes)
}
