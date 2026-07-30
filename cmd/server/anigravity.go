package main

import (
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

// anigravityRequest translates OpenAI format to Google Gemini format for Anigravity
func anigravityRequest(w http.ResponseWriter, r *http.Request, model string, rawBody map[string]interface{}, accessToken string, isStream bool) {
	messages, _ := rawBody["messages"].([]interface{})
	var contents []map[string]interface{}
	var systemInstruction string

	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)

		// ── System messages ──
		if role == "system" {
			if c, ok := msg["content"].(string); ok {
				systemInstruction += c + "\n"
			}
			continue
		}

		// ── Tool result messages → functionResponse parts ──
		if role == "tool" {
			toolCallID, _ := msg["tool_call_id"].(string)
			content, _ := msg["content"].(string)
			// Find the function name from previous assistant tool_calls
			funcName := toolCallID // fallback
			for _, prev := range messages {
				pm, pok := prev.(map[string]interface{})
				if !pok || pm["role"] != "assistant" {
					continue
				}
				if tc, ok := pm["tool_calls"].([]interface{}); ok {
					for _, t := range tc {
						if tm, ok := t.(map[string]interface{}); ok {
							if tm["id"] == toolCallID {
								if fn, ok := tm["function"].(map[string]interface{}); ok {
									funcName, _ = fn["name"].(string)
								}
							}
						}
					}
				}
			}
			contents = append(contents, map[string]interface{}{
				"role": "user",
				"parts": []map[string]interface{}{
					{"functionResponse": map[string]interface{}{
						"name":     funcName,
						"response": map[string]interface{}{"result": content},
					}},
				},
			})
			continue
		}

		// ── Assistant messages with tool_calls → functionCall parts ──
		if role == "assistant" {
			var parts []map[string]interface{}

			// Add text content if present
			if c, ok := msg["content"].(string); ok && c != "" {
				parts = append(parts, map[string]interface{}{"text": c})
			}

			// Add functionCall parts
			if tc, ok := msg["tool_calls"].([]interface{}); ok {
				for _, t := range tc {
					if tm, ok := t.(map[string]interface{}); ok {
						if fn, ok := tm["function"].(map[string]interface{}); ok {
							funcName, _ := fn["name"].(string)
							var args map[string]interface{}
							if argsStr, ok := fn["arguments"].(string); ok {
								json.Unmarshal([]byte(argsStr), &args)
							}
							if args == nil {
								args = map[string]interface{}{}
							}
							parts = append(parts, map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": funcName,
									"args": args,
								},
							})
						}
					}
				}
			}

			if len(parts) > 0 {
				contents = append(contents, map[string]interface{}{
					"role":  "model",
					"parts": parts,
				})
			}
			continue
		}

		// ── Regular user messages ──
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

		if textContent == "" {
			continue
		}

		contents = append(contents, map[string]interface{}{
			"role": "user",
			"parts": []map[string]interface{}{{"text": textContent}},
		})
	}

	// Build Gemini request
	geminiReq := map[string]interface{}{
		"contents": contents,
		"generationConfig": map[string]interface{}{
			"temperature":     1.0,
			"topP":            0.95,
			"maxOutputTokens": 64000,
		},
	}

	if systemInstruction != "" {
		geminiReq["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{{"text": strings.TrimSpace(systemInstruction)}},
		}
	}

	// ── Convert OpenAI tools to Gemini format ──
	if tools, ok := rawBody["tools"].([]interface{}); ok && len(tools) > 0 {
		var declarations []map[string]interface{}
		for _, tool := range tools {
			if tm, ok := tool.(map[string]interface{}); ok {
				if fn, ok := tm["function"].(map[string]interface{}); ok {
					decl := map[string]interface{}{
						"name": fn["name"],
					}
					if desc, ok := fn["description"].(string); ok {
						decl["description"] = desc
					}
					if params, ok := fn["parameters"].(map[string]interface{}); ok {
						decl["parameters"] = cleanGeminiSchema(params)
					} else {
						decl["parameters"] = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
					}
					declarations = append(declarations, decl)
				}
			}
		}
		if len(declarations) > 0 {
			geminiReq["tools"] = []map[string]interface{}{
				{"functionDeclarations": declarations},
			}
		}
	}

	// ── Strip blacklisted fields (9router reference) ──
	blacklist := []string{"thinking", "reasoning_effort", "reasoning", "enable_thinking", "thinking_budget", "thinkingConfig", "output_config"}
	for _, key := range blacklist {
		delete(rawBody, key)
	}

	// ── Handle thinkingConfig → reasoning_effort conversion ──
	if gc, ok := geminiReq["generationConfig"].(map[string]interface{}); ok {
		if tc, ok := gc["thinkingConfig"].(map[string]interface{}); ok {
			if budget, ok := tc["thinkingBudget"].(float64); ok && budget > 0 {
				// Convert budget to effort level (OmniRoute reference)
				// Don't send thinkingConfig to API, just note effort
			}
			delete(gc, "thinkingConfig")
		}
	}

	// Build outer wrapper
	projectID := getAnigravityProjectID()
	// Generate requestId matching 9router format: agent/{uuid}/{timestamp}/{uuid}/{step}
	convUUID := generateUUID()
	trajUUID := generateUUID()
	step := len(contents)*2 - 1
	if step < 1 {
		step = 1
	}
	requestID := fmt.Sprintf("agent/%s/%d/%s/%d", convUUID, time.Now().UnixMilli(), trajUUID, step)
	sessionID := generateUUID()

	// Add sessionId inside request (not top level)
	geminiReq["sessionId"] = sessionID

	fullBody := map[string]interface{}{
		"project":     projectID,
		"model":       model,
		"userAgent":   "antigravity",
		"requestType": "chat",
		"requestId":   requestID,
		"request":     geminiReq,
	}

	bodyBytes, _ := json.Marshal(fullBody)

	// Build URL
	action := "generateContent"
	if isStream {
		action = "streamGenerateContent?alt=sse"
	}
	upstreamURL := fmt.Sprintf("https://cloudcode-pa.googleapis.com/v1internal:%s", action)

	req, _ := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "antigravity/ide/2.1.1 linux/amd64")

	client := sharedHTTPClient
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, 502, "anigravity request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		writeError(w, resp.StatusCode, fmt.Sprintf("anigravity error %d: %s", resp.StatusCode, string(errBody)))
		return
	}

	if isStream {
		handleAnigravityStreaming(w, resp)
	} else {
		handleAnigravityNonStreaming(w, resp)
	}
}

// testAnigravityRequest makes a simple test request to Anigravity and returns content + latency
func testAnigravityRequest(model, prompt, accessToken string) (string, int64, error) {
	projectID := getAnigravityProjectID()
	convUUID := generateUUID()
	trajUUID := generateUUID()
	requestID := fmt.Sprintf("agent/%s/%d/%s/1", convUUID, time.Now().UnixMilli(), trajUUID)
	sessionID := generateUUID()

	fullBody := map[string]interface{}{
		"project":     projectID,
		"model":       model,
		"userAgent":   "antigravity",
		"requestType": "chat",
		"requestId":   requestID,
		"sessionId":   sessionID,
		"request": map[string]interface{}{
			"contents": []map[string]interface{}{
				{"role": "user", "parts": []map[string]interface{}{{"text": prompt}}},
			},
			"generationConfig": map[string]interface{}{
				"temperature":     1.0,
				"topP":            0.95,
				"maxOutputTokens": 1000,
			},
		},
	}

	bodyBytes, _ := json.Marshal(fullBody)
	upstreamURL := "https://cloudcode-pa.googleapis.com/v1internal:generateContent"

	req, _ := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "antigravity/ide/2.1.1 linux/amd64")

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
		return "", latency, fmt.Errorf("anigravity error %d: %s", resp.StatusCode, string(body))
	}

	var wrapper struct {
		Response struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return "", latency, fmt.Errorf("failed to parse response: %v", err)
	}

	content := ""
	if len(wrapper.Response.Candidates) > 0 {
		for _, part := range wrapper.Response.Candidates[0].Content.Parts {
			if part.Text != "" {
				content = part.Text
			}
		}
	}

	if content == "" {
		content = "Empty response from Anigravity"
	}

	return content, latency, nil
}

// ── Response Translation ──

// Gemini response types
type geminiPart struct {
	Text             string                 `json:"text"`
	ThoughtSignature string                 `json:"thoughtSignature"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall"`
}

type geminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type geminiCandidate struct {
	Content struct {
		Parts []geminiPart `json:"parts"`
	} `json:"content"`
	FinishReason string `json:"finishReason"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
}

// handleAnigravityNonStreaming converts Gemini response to OpenAI format
func handleAnigravityNonStreaming(w http.ResponseWriter, resp *http.Response) {
	body, _ := io.ReadAll(resp.Body)

	var wrapper struct {
		Response geminiResponse `json:"response"`
	}

	if err := json.Unmarshal(body, &wrapper); err != nil {
		writeError(w, 502, "failed to parse anigravity response")
		return
	}
	geminiResp := wrapper.Response

	content := ""
	reasoningContent := ""
	var toolCalls []map[string]interface{}
	toolCallIdx := 0

	if len(geminiResp.Candidates) > 0 {
		for _, part := range geminiResp.Candidates[0].Content.Parts {
			if part.Text != "" {
				content = part.Text
			}
			if part.ThoughtSignature != "" {
				reasoningContent = part.ThoughtSignature
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   fmt.Sprintf("call_%d_%d", time.Now().UnixMilli(), toolCallIdx),
					"type": "function",
					"function": map[string]interface{}{
						"name":      part.FunctionCall.Name,
						"arguments": string(argsJSON),
					},
				})
				toolCallIdx++
			}
		}
	}

	// Build OpenAI response
	openaiResp := map[string]interface{}{
		"id":      fmt.Sprintf("anigravity-%d", time.Now().UnixMilli()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "anigravity",
	}

	msg := map[string]interface{}{
		"role":    "assistant",
		"content": content,
	}
	if reasoningContent != "" {
		msg["reasoning_content"] = reasoningContent
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		msg["content"] = "" // OpenAI format: content is null when tool_calls present
	}

	finishReason := "stop"
	if len(geminiResp.Candidates) > 0 {
		finishReason = mapGeminiFinishReason(geminiResp.Candidates[0].FinishReason)
	}

	openaiResp["choices"] = []map[string]interface{}{
		{
			"index":         0,
			"message":       msg,
			"finish_reason": finishReason,
		},
	}

	if geminiResp.UsageMetadata != nil {
		openaiResp["usage"] = map[string]interface{}{
			"prompt_tokens":     geminiResp.UsageMetadata.PromptTokenCount,
			"completion_tokens": geminiResp.UsageMetadata.CandidatesTokenCount,
			"total_tokens":      geminiResp.UsageMetadata.TotalTokenCount,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openaiResp)

	// Log the request
	var logConnID string
	db.DB.QueryRow("SELECT id FROM provider_connections WHERE provider_id='builtin-anigravity' AND is_active=1 LIMIT 1").Scan(&logConnID)
	promptTokens, completionTokens := 0, 0
	if geminiResp.UsageMetadata != nil {
		promptTokens = geminiResp.UsageMetadata.PromptTokenCount
		completionTokens = geminiResp.UsageMetadata.CandidatesTokenCount
	}
	logProxyRequest("builtin-anigravity", "Anigravity CLI", "anigravity", logConnID, "anigravity-connection", "", "",
		200, promptTokens, completionTokens, 0, "")
}

// handleAnigravityStreaming converts Gemini SSE to OpenAI SSE format
func handleAnigravityStreaming(w http.ResponseWriter, resp *http.Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var lastUsage *struct {
		PromptTokenCount     int
		CandidatesTokenCount int
		TotalTokenCount      int
	}
	var lastFinishReason string
	toolCallIdx := 0

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var wrapper struct {
			Response geminiResponse `json:"response"`
		}

		if err := json.Unmarshal([]byte(data), &wrapper); err != nil {
			continue
		}
		geminiChunk := wrapper.Response

		if len(geminiChunk.Candidates) > 0 {
			if geminiChunk.Candidates[0].FinishReason != "" {
				lastFinishReason = geminiChunk.Candidates[0].FinishReason
			}
		}

		if geminiChunk.UsageMetadata != nil {
			lastUsage = &struct {
				PromptTokenCount     int
				CandidatesTokenCount int
				TotalTokenCount      int
			}{
				PromptTokenCount:     geminiChunk.UsageMetadata.PromptTokenCount,
				CandidatesTokenCount: geminiChunk.UsageMetadata.CandidatesTokenCount,
				TotalTokenCount:      geminiChunk.UsageMetadata.TotalTokenCount,
			}
		}

		// Convert finish_reason
		var openaiFinish *string
		if lastFinishReason != "" {
			mapped := mapGeminiFinishReason(lastFinishReason)
			openaiFinish = &mapped
		}

		// Process parts - text and function calls
		if len(geminiChunk.Candidates) > 0 {
			for _, part := range geminiChunk.Candidates[0].Content.Parts {
				// Text chunk
				if part.Text != "" {
					chunk := map[string]interface{}{
						"id":      fmt.Sprintf("anigravity-%d", time.Now().UnixMilli()),
						"object":  "chat.completion.chunk",
						"created": time.Now().Unix(),
						"model":   "anigravity",
						"choices": []map[string]interface{}{
							{
								"index":         0,
								"delta":         map[string]interface{}{"content": part.Text},
								"finish_reason": openaiFinish,
							},
						},
					}
					if lastUsage != nil {
						chunk["usage"] = map[string]interface{}{
							"prompt_tokens":     lastUsage.PromptTokenCount,
							"completion_tokens": lastUsage.CandidatesTokenCount,
							"total_tokens":      lastUsage.TotalTokenCount,
						}
					}
					chunkBytes, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
					flusher.Flush()
				}

				// Function call chunk
				if part.FunctionCall != nil {
					argsJSON, _ := json.Marshal(part.FunctionCall.Args)
					chunk := map[string]interface{}{
						"id":      fmt.Sprintf("anigravity-%d", time.Now().UnixMilli()),
						"object":  "chat.completion.chunk",
						"created": time.Now().Unix(),
						"model":   "anigravity",
						"choices": []map[string]interface{}{
							{
								"index": 0,
								"delta": map[string]interface{}{
									"tool_calls": []map[string]interface{}{
										{
											"index": toolCallIdx,
											"id":    fmt.Sprintf("call_%d_%d", time.Now().UnixMilli(), toolCallIdx),
											"type":  "function",
											"function": map[string]interface{}{
												"name":      part.FunctionCall.Name,
												"arguments": string(argsJSON),
											},
										},
									},
								},
								"finish_reason": openaiFinish,
							},
						},
					}
					if lastUsage != nil {
						chunk["usage"] = map[string]interface{}{
							"prompt_tokens":     lastUsage.PromptTokenCount,
							"completion_tokens": lastUsage.CandidatesTokenCount,
							"total_tokens":      lastUsage.TotalTokenCount,
						}
					}
					chunkBytes, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
					flusher.Flush()
					toolCallIdx++
				}
			}
		}

		if lastFinishReason != "" {
			break
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// mapGeminiFinishReason maps Gemini finish reasons to OpenAI format
func mapGeminiFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	default:
		return "stop"
	}
}

// cleanGeminiSchema removes unsupported fields from JSON Schema for Gemini
func cleanGeminiSchema(schema map[string]interface{}) map[string]interface{} {
	// Remove fields Gemini doesn't support
	delete(schema, "$schema")
	delete(schema, "additionalProperties")
	delete(schema, "default")
	delete(schema, "examples")
	delete(schema, "const")
	delete(schema, "enum") // keep if needed, but Gemini may not support all

	// Recursively clean nested properties
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		for key, val := range props {
			if subSchema, ok := val.(map[string]interface{}); ok {
				props[key] = cleanGeminiSchema(subSchema)
			}
		}
	}

	// Clean items for array types
	if items, ok := schema["items"].(map[string]interface{}); ok {
		schema["items"] = cleanGeminiSchema(items)
	}

	return schema
}

// getAnigravityProjectID returns the stored project ID for Anigravity
func getAnigravityProjectID() string {
	var projectID string
	db.DB.QueryRow(`SELECT COALESCE(project_id, '') FROM provider_connections 
		WHERE provider_id='builtin-anigravity' AND is_active=1 ORDER BY created_at DESC LIMIT 1`).Scan(&projectID)
	if projectID == "" {
		return "cloud-code-antigravity-default"
	}
	return projectID
}
