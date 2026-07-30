package main

import (
	"log"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// qoderRequest handles requests to Qoder API with COSY signing
func qoderRequest(w http.ResponseWriter, r *http.Request, model string, rawBody map[string]interface{}, isStream bool) {
	// Get valid token
	token, err := getQoderToken()
	if err != nil {
		writeError(w, 401, err.Error())
		return
	}

	// Get user ID and machine ID from connection
	var userID, machineID string
	db.DB.QueryRow(`SELECT COALESCE(email,''), COALESCE(machine_id,'') 
		FROM provider_connections WHERE provider_id='builtin-qoder' AND is_active=1 
		ORDER BY created_at DESC LIMIT 1`).Scan(&userID, &machineID)

	// Build Qoder request body
	messages, _ := rawBody["messages"].([]interface{})
	var contents []map[string]interface{}
	var systemText string

	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)

		// Extract text content
		var textContent string
		switch c := msg["content"].(type) {
		case string:
			textContent = c
		case []interface{}:
			for _, part := range c {
				if p, ok := part.(map[string]interface{}); ok {
					if t, ok := p["text"].(string); ok {
						textContent += t + "\n"
					}
				}
			}
			textContent = strings.TrimSpace(textContent)
		case nil:
			continue
		default:
			continue
		}

		if role == "system" {
			systemText += textContent + "\n"
			continue
		}

		if textContent == "" {
			continue
		}

		contents = append(contents, map[string]interface{}{
			"role":    role,
			"content": textContent,
		})
	}

	// Build Qoder request format (matches 9router reference)
	qoderReq := map[string]interface{}{
		"request_id":      generateUUID(),
		"request_set_id":  generateUUID(),
		"chat_record_id":  generateUUID(),
		"session_id":      generateUUID(),
		"stream":          true,
		"chat_task":       "FREE_INPUT",
		"is_reply":        true,
		"is_retry":        false,
		"source":          1,
		"version":         "3",
		"session_type":    "qodercli",
		"agent_id":        "agent_common",
		"task_id":         "common",
		"code_language":   "",
		"chat_prompt":     "",
		"image_urls":      nil,
		"aliyun_user_type": "",
		"system":          systemText,
		"messages":        contents,
		"tools":           []interface{}{},
		"parameters":      map[string]interface{}{"max_tokens": 32768},
		"chat_context": map[string]interface{}{
			"chatPrompt": "",
			"imageUrls":  nil,
			"extra": map[string]interface{}{
				"context": []interface{}{},
				"modelConfig": map[string]interface{}{
					"key":          model,
					"is_reasoning": false,
				},
				"originalContent": func() string {
					if len(contents) > 0 {
						if c, ok := contents[len(contents)-1]["content"].(string); ok {
							return c
						}
					}
					return ""
				}(),
			},
			"features": []interface{}{},
			"text": func() string {
				if len(contents) > 0 {
					if c, ok := contents[len(contents)-1]["content"].(string); ok {
						return c
					}
				}
				return ""
			}(),
		},
		"model_config": map[string]interface{}{
			"key":          model,
			"is_reasoning": false,
		},
		"business": map[string]interface{}{
			"product":  "cli",
			"version":  "1.0.0",
			"type":     "agent",
			"stage":    "start",
			"id":       generateUUID(),
			"name":     func() string { if len(contents) > 0 { if c, ok := contents[len(contents)-1]["content"].(string); ok { if len(c) > 30 { return c[:30] } ; return c } }; return "" }(),
			"begin_at": time.Now().UnixMilli(),
		},
	}

	bodyBytes, _ := json.Marshal(qoderReq)

	// Encode body for WAF bypass (required before COSY signing)
	encodedBody := qoderEncodeBody(bodyBytes)
	encodedBodyBytes := []byte(encodedBody)

	// Build COSY headers using ENCODED body
	cosyHeaders, err := buildCosyHeaders(encodedBodyBytes, qoderChatURL, userID, token, machineID)
	if err != nil {
		writeError(w, 500, "COSY signing failed: "+err.Error())
		return
	}

	// Build URL with required query params (matches 9router)
	requestURL := qoderChatURL + "?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"

	// Make request with ENCODED body
	req, _ := http.NewRequest("POST", requestURL, bytes.NewReader(encodedBodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept-Encoding", "identity") // CRITICAL: gzip breaks signature validation
	req.Header.Set("X-Model-Key", model)
	req.Header.Set("X-Model-Source", "system")
	for k, v := range cosyHeaders {
		req.Header.Set(k, v)
	}

	client := sharedHTTPClient
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, 502, "qoder request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		writeError(w, resp.StatusCode, fmt.Sprintf("qoder error %d: %s", resp.StatusCode, string(errBody)))
		return
	}

	if isStream {
		handleQoderStreaming(w, resp)
	} else {
		handleQoderNonStreaming(w, resp)
	}

	// Log request
	var connID string
	db.DB.QueryRow("SELECT id FROM provider_connections WHERE provider_id='builtin-qoder' AND is_active=1 LIMIT 1").Scan(&connID)
	logProxyRequest("builtin-qoder", "Qoder", model, connID, "qoder-connection", "", "", 200, 0, 0, 0, "")
}

// testQoderRequest makes a test request to Qoder API
func testQoderRequest(model, prompt, token string) (string, int64, error) {
	// Get user ID and machine ID
	var userID, machineID string
	db.DB.QueryRow(`SELECT COALESCE(email,''), COALESCE(machine_id,'') 
		FROM provider_connections WHERE provider_id='builtin-qoder' AND is_active=1 
		ORDER BY created_at DESC LIMIT 1`).Scan(&userID, &machineID)

	// Build request body
	qoderReq := map[string]interface{}{
		"chat_context": map[string]interface{}{
			"contents": []map[string]interface{}{
				{"role": "user", "content": prompt},
			},
			"model_config": map[string]interface{}{
				"model": model,
			},
		},
		"business": map[string]interface{}{
			"agent_id": "agent_common",
		},
	}

	bodyBytes, _ := json.Marshal(qoderReq)

	// Encode body for WAF bypass
	encodedBody := qoderEncodeBody(bodyBytes)
	encodedBodyBytes := []byte(encodedBody)

	// Build COSY headers using ENCODED body
	cosyHeaders, err := buildCosyHeaders(encodedBodyBytes, qoderChatURL, userID, token, machineID)
	if err != nil {
		return "", 0, fmt.Errorf("COSY signing failed: %v", err)
	}

	requestURL := qoderChatURL + "?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"
	req, _ := http.NewRequest("POST", requestURL, bytes.NewReader(encodedBodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("X-Model-Key", model)
	req.Header.Set("X-Model-Source", "system")
	for k, v := range cosyHeaders {
		req.Header.Set(k, v)
	}

	startTime := time.Now()
	client := sharedHTTPClient
	resp, err := client.Do(req)
	latency := time.Since(startTime).Milliseconds()
	if err != nil {
		return "", latency, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", latency, fmt.Errorf("qoder error %d: %s", resp.StatusCode, string(body))
	}

	// Parse SSE response
	var fullText string
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if delta, ok := event["delta"].(map[string]interface{}); ok {
			if text, ok := delta["text"].(string); ok {
				fullText += text
			}
		}
	}

	if fullText == "" {
		fullText = "Empty response from Qoder"
	}

	return fullText, latency, nil
}

// handleQoderNonStreaming handles non-streaming Qoder response
func handleQoderNonStreaming(w http.ResponseWriter, resp *http.Response) {
	body, _ := io.ReadAll(resp.Body)

	// Qoder returns SSE even for non-streaming
	var fullText string
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		// Extract text from event
		if delta, ok := event["delta"].(map[string]interface{}); ok {
			if text, ok := delta["text"].(string); ok {
				fullText += text
			}
		}
	}

	if fullText == "" {
		fullText = string(body)
		if len(fullText) > 500 {
			fullText = fullText[:500] + "..."
		}
	}

	openaiResp := map[string]interface{}{
		"id":      fmt.Sprintf("qoder-%d", time.Now().UnixMilli()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "qoder",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": fullText,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openaiResp)
}

// handleQoderStreaming handles streaming Qoder response
func handleQoderStreaming(w http.ResponseWriter, resp *http.Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		// Qoder wraps response in {"statusCodeValue":200,"body":"..."} envelope
		var envelope map[string]interface{}
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			continue
		}

		// Check status
		statusCode := 200
		if sv, ok := envelope["statusCodeValue"].(float64); ok {
			statusCode = int(sv)
		}
		if statusCode != 200 {
			errMsg := ""
			if m, ok := envelope["message"].(string); ok {
				errMsg = m
			}
			if m, ok := envelope["body"].(string); ok && m != "" {
				errMsg = m
			}
			log.Printf("[Qoder] Error %d: %s", statusCode, errMsg)
			continue
		}

		// Parse inner body
		innerBody := ""
		if b, ok := envelope["body"].(string); ok {
			innerBody = b
		}
		if innerBody == "" {
			continue
		}

		var inner map[string]interface{}
		if err := json.Unmarshal([]byte(innerBody), &inner); err != nil {
			continue
		}

		// Extract choices from inner body
		if choices, ok := inner["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if content, ok := delta["content"].(string); ok && content != "" {
						chunk := map[string]interface{}{
							"id":      fmt.Sprintf("qoder-%d", time.Now().UnixMilli()),
							"object":  "chat.completion.chunk",
							"created": time.Now().Unix(),
							"model":   "qoder",
							"choices": []map[string]interface{}{
								{
									"index":         0,
									"delta":         map[string]interface{}{"content": content},
									"finish_reason": nil,
								},
							},
						}
						chunkBytes, _ := json.Marshal(chunk)
						fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
						flusher.Flush()
					}
				}
			}
		}
	}

	// Send [DONE] if not already sent
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
