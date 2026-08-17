package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dolvin/paap/internal/db"
)

var (
	tsCacheMu sync.RWMutex
	tsCache   = make(map[string]string)
	latestTs  string = "ErIFCq8FARFNMg8p7FEUyMRblRrnnnf1ksKhFIEj28WB+p6bNvRS+Qdr/zpVq98S175EB3/nXBH3uxsMvZl+7sjmK8oPdZ7vdWJXA8U9XbFZjH6SSDVoMNZlvzbSIS2nqQLBsE8CgQIYW98T0Y81uJO9djeJhfEK8Hkr2gJOJ+BwXPbhvZP6mLkvMIv5OmLhCr5XTx5iVcOl4HNqOZ4c8Ps4ePy0M9hkYDcl3MBzW3SFPo4cAGtAVYlzAmN8rFQt17bCHFYKdJJo6HSvNcWJTTgEO5ljeetChe5sr7XDJLbdETz+6O4teuVjE+rFMGiwS4Y23uP6qCaZMo4v9cqT+rgB/UlBbexuMTf/ZxI4VpRsWMAnhf4M1sUd+PCQKAvujCmAnAgWYYKhIG8zq7820qn9662F1ZY4eXWwXPolQGlwgUCeuC/Zl+u5XT+gWse76sNg9ZoueKTcwDkHclUjxTGKj7er4CQcW0IFABHNPuSO5z0YPW6amD9b2hPK5cIMzb8HDUT3ThYQF29hnvtRlGaE2Fo2PGQmLk2+Lj3MCbdb47g0ySL9u5eDobHFIuCuylDwmQ2TUHKYaqYJumhSSrV6vmKoo2HGfCw9Fbn920uQfULwGp1RgHJsmK9bYqgw6MlXgSTdKaAEKEJI8a9cOU6/EmFUXl/s7eJD33OAm1Ow164a55iAXEwcBbTKWwqDmaciRvbCwNYNilTT86WI5qHckPKOMZM55YHPTthN54i18drZmk1+9ykvw3fTWLHMEw3jnema7EWSc9rjNqolTWAzMYEtbp7ZA54A9HE4G/bqEapifCbwDi3fLCo2U5qbaSIjqUsORByVujzZvLemOWXOPYu/KEEw+40tsblDlDbox1nk0PLkLgX0PyG6qxN0NWa9yff7P76ckxJpo7Ca9jX78Gzu"
)

func saveThoughtSignature(funcName, sig string) {
	if sig == "" {
		return
	}
	tsCacheMu.Lock()
	defer tsCacheMu.Unlock()
	latestTs = sig
	if funcName != "" {
		tsCache[funcName] = sig
	}
}

func getThoughtSignature(funcName, toolCallID string, msg, tm map[string]interface{}) string {
	// 1. Check embedded in toolCallID (call_123___ts___<sig>)
	if idx := strings.Index(toolCallID, "___ts___"); idx != -1 {
		sig := toolCallID[idx+len("___ts___"):]
		if sig != "" {
			return sig
		}
	}
	// 2. Check tm (tool_call map)
	if tm != nil {
		if s, ok := tm["thought_signature"].(string); ok && s != "" {
			return s
		}
		if s, ok := tm["thoughtSignature"].(string); ok && s != "" {
			return s
		}
	}
	// 3. Check msg (assistant message)
	if msg != nil {
		if s, ok := msg["thought_signature"].(string); ok && s != "" {
			return s
		}
		if s, ok := msg["thoughtSignature"].(string); ok && s != "" {
			return s
		}
		if ec, ok := msg["extra_content"].(map[string]interface{}); ok {
			if g, ok := ec["google"].(map[string]interface{}); ok {
				if s, ok := g["thought_signature"].(string); ok && s != "" {
					return s
				}
				if s, ok := g["thoughtSignature"].(string); ok && s != "" {
					return s
				}
			}
		}
		if rc, ok := msg["reasoning_content"].(string); ok && strings.HasPrefix(rc, "E") && len(rc) > 50 && !strings.Contains(rc, " ") {
			return rc
		}
	}
	// 4. Check cache by funcName
	tsCacheMu.RLock()
	defer tsCacheMu.RUnlock()
	if sig, ok := tsCache[funcName]; ok && sig != "" {
		return sig
	}
	// 5. Fallback to latest seen or default valid signature
	return latestTs
}

// anigravityRequest translates OpenAI format to Google Gemini format for Anigravity
func anigravityRequest(w http.ResponseWriter, r *http.Request, model string, rawBody map[string]interface{}, accessToken string, isStream bool, providerID, providerName, keyID, keyName string) {
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
			cleanToolCallID := toolCallID
			if idx := strings.Index(cleanToolCallID, "___ts___"); idx != -1 {
				cleanToolCallID = cleanToolCallID[:idx]
			}
			contents = append(contents, map[string]interface{}{
				"role": "user",
				"parts": []map[string]interface{}{
					{"functionResponse": map[string]interface{}{
						"id":       cleanToolCallID,
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
							toolCallID, _ := tm["id"].(string)
							if toolCallID == "" {
								toolCallID = "call_" + funcName
							}
							cleanToolCallID := toolCallID
							if idx := strings.Index(cleanToolCallID, "___ts___"); idx != -1 {
								cleanToolCallID = cleanToolCallID[:idx]
							}
							var args map[string]interface{}
							if argsStr, ok := fn["arguments"].(string); ok {
								json.Unmarshal([]byte(argsStr), &args)
							}
							if args == nil {
								args = map[string]interface{}{}
							}

							sig := getThoughtSignature(funcName, toolCallID, msg, tm)

							partMap := map[string]interface{}{
								"functionCall": map[string]interface{}{
									"id":   cleanToolCallID,
									"name": funcName,
									"args": args,
								},
							}
							if sig != "" {
								partMap["thoughtSignature"] = sig
								partMap["thought_signature"] = sig
							}
							parts = append(parts, partMap)
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
			"role":  "user",
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
			"thinkingConfig": map[string]interface{}{
				"includeThoughts": true,
			},
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

	// Extract reasoning effort before stripping fields
	effort := extractReasoningEffort(rawBody, geminiReq)

	// ── Strip blacklisted fields (9router reference) ──
	blacklist := []string{"thinking", "reasoning_effort", "reasoning", "enable_thinking", "thinking_budget", "thinkingConfig", "output_config"}
	for _, key := range blacklist {
		delete(rawBody, key)
	}

	// ── Configure thinkingConfig based on effort ──
	if gc, ok := geminiReq["generationConfig"].(map[string]interface{}); ok {
		if effort == "off" || effort == "none" || effort == "0" {
			gc["thinkingConfig"] = map[string]interface{}{
				"thinkingBudget": 0,
			}
		} else {
			gc["thinkingConfig"] = map[string]interface{}{
				"includeThoughts": true,
			}
		}
	}

	// Resolve model aliases and auto-route according to reasoning effort level (low/medium/high/max)
	model = resolveAnigravityModelWithEffort(model, effort)

	// Build outer wrapper
	projectID := getAnigravityProjectID()
	if projectID == "" {
		writeError(w, 400, "Anigravity provider has no Google Cloud Project ID configured. Please reconnect your account via Google OAuth at /providers/setup?id=builtin-anigravity")
		return
	}
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
	upstreamURL := fmt.Sprintf("https://daily-cloudcode-pa.googleapis.com/v1internal:%s", action)

	req, _ := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "antigravity/ide/2.1.1 linux/amd64")
	req.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudshelleditor/0.1")
	req.Header.Set("Client-Metadata", `{"ideType":9,"platform":2,"pluginType":2}`)

	client := sharedHTTPClient
	if isStream {
		client = streamingHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, 502, "anigravity request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		// Attempt automatic on-demand token refresh and retry once
		var connID, connRefresh string
		db.DB.QueryRow("SELECT id, COALESCE(refresh_token,'') FROM provider_connections WHERE provider_id='builtin-anigravity' AND is_active=1 ORDER BY created_at DESC LIMIT 1").Scan(&connID, &connRefresh)
		if connRefresh != "" {
			if refreshedToken, rErr := ensureAnigravityToken(connID, "", connRefresh, 0); rErr == nil && refreshedToken != "" {
				retryReq, _ := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
				retryReq.Header.Set("Content-Type", "application/json")
				retryReq.Header.Set("Authorization", "Bearer "+refreshedToken)
				retryReq.Header.Set("User-Agent", "antigravity/ide/2.1.1 linux/amd64")
				retryReq.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudshelleditor/0.1")
				retryReq.Header.Set("Client-Metadata", `{"ideType":9,"platform":2,"pluginType":2}`)
				retryResp, retryErr := client.Do(retryReq)
				if retryErr == nil && retryResp.StatusCode == 200 {
					defer retryResp.Body.Close()
					if isStream {
						handleAnigravityStreaming(w, retryResp)
					} else {
						handleAnigravityNonStreaming(w, retryResp)
					}
					return
				}
				if retryResp != nil {
					defer retryResp.Body.Close()
					resp = retryResp
				}
			}
		}
	}

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

// extractReasoningEffort extracts reasoning effort level (low/medium/high/max) from various client parameter formats
func extractReasoningEffort(rawBody map[string]interface{}, geminiReq map[string]interface{}) string {
	// 1. OpenAI reasoning_effort field (e.g. "low", "medium", "high", "max")
	if v, ok := rawBody["reasoning_effort"].(string); ok && v != "" {
		return strings.ToLower(v)
	}
	// 2. reasoning field
	if v, ok := rawBody["reasoning"].(string); ok && v != "" {
		return strings.ToLower(v)
	}
	// 3. Anthropic thinking map (e.g. {"type": "enabled", "budget_tokens": 4000})
	if m, ok := rawBody["thinking"].(map[string]interface{}); ok {
		if b, ok := m["budget_tokens"].(float64); ok {
			if b <= 2000 {
				return "low"
			} else if b <= 4000 {
				return "medium"
			} else {
				return "high"
			}
		}
	}
	// 4. thinking_budget numeric
	if b, ok := rawBody["thinking_budget"].(float64); ok {
		if b <= 2000 {
			return "low"
		} else if b <= 4000 {
			return "medium"
		} else {
			return "high"
		}
	}
	// 5. Google thinkingConfig inside generationConfig
	if gc, ok := geminiReq["generationConfig"].(map[string]interface{}); ok {
		if tc, ok := gc["thinkingConfig"].(map[string]interface{}); ok {
			if b, ok := tc["thinkingBudget"].(float64); ok {
				if b <= 2000 {
					return "low"
				} else if b <= 4000 {
					return "medium"
				} else {
					return "high"
				}
			}
		}
	}
	return ""
}

// resolveAnigravityModelWithEffort maps friendly/unified model names and effort level to upstream models
func resolveAnigravityModelWithEffort(model, effort string) string {
	m := strings.ToLower(strings.TrimSpace(model))

	// If effort not explicitly provided, check if model name itself specifies effort level
	if effort == "" {
		if strings.HasSuffix(m, "-low") {
			effort = "low"
		} else if strings.HasSuffix(m, "-medium") || strings.HasSuffix(m, "-med") {
			effort = "medium"
		} else if strings.HasSuffix(m, "-high") || strings.HasSuffix(m, "-max") {
			effort = "high"
		}
	}

	switch {
	// Gemini 3.7 Flash unified (supports high/max deep thinking)
	case strings.HasPrefix(m, "gemini-3.7") || m == "gemini-3.7-flash" || m == "gemini-3.7-flash-tiered":
		return "gemini-3.7-flash-tiered"

	// Gemini 3.6 Flash unified (auto-routes to low/medium/high by reasoning effort)
	case strings.HasPrefix(m, "gemini-3.6"):
		switch effort {
		case "low":
			return "gemini-3.6-flash-low"
		case "medium":
			return "gemini-3.6-flash-medium"
		case "high", "max":
			return "gemini-3.6-flash-high"
		default:
			return "gemini-3.6-flash-high"
		}

	// Gemini 3.5 Flash unified
	case strings.HasPrefix(m, "gemini-3.5"):
		switch effort {
		case "low":
			return "gemini-3.5-flash-extra-low"
		case "medium":
			return "gemini-3.5-flash-low"
		case "high", "max":
			return "gemini-3-flash-agent"
		default:
			return "gemini-3.5-flash-low"
		}

	// Gemini 3 Flash
	case m == "gemini-3-flash" || m == "gemini-3.0-flash":
		if effort == "high" || effort == "max" {
			return "gemini-3-flash-agent"
		}
		return "gemini-3-flash"

	// Gemini Pro
	case strings.HasPrefix(m, "gemini-3.1-pro") || m == "gemini-pro" || m == "gemini-pro-agent":
		if effort == "high" || effort == "max" {
			return "gemini-pro-agent"
		}
		return "gemini-3.1-pro-low"

	// Gemini 2.5
	case strings.HasPrefix(m, "gemini-2.5-flash"):
		return "gemini-2.5-flash"
	case strings.HasPrefix(m, "gemini-2.5-pro"):
		return "gemini-2.5-pro"

	// Claude
	case strings.HasPrefix(m, "claude-sonnet") || strings.HasPrefix(m, "claude-3-7-sonnet") || strings.HasPrefix(m, "claude-3-5-sonnet") || strings.HasPrefix(m, "claude-4-sonnet"):
		return "claude-sonnet-4-6"
	case strings.HasPrefix(m, "claude-opus") || strings.HasPrefix(m, "claude-4-opus"):
		return "claude-opus-4-6-thinking"

	// GPT OSS
	case strings.HasPrefix(m, "gpt-oss") || strings.HasPrefix(m, "gpt-120b"):
		return "gpt-oss-120b-medium"

	default:
		return model
	}
}

// testAnigravityRequest makes a simple test request to Anigravity and returns content + latency
func testAnigravityRequest(model, prompt, accessToken string) (string, int64, error) {
	model = resolveAnigravityModelWithEffort(model, "")
	projectID := getAnigravityProjectID()
	if projectID == "" {
		return "", 0, fmt.Errorf("no Google Cloud Project ID configured — please reconnect your account via Google OAuth")
	}
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
		"request": map[string]interface{}{
			"sessionId": sessionID,
			"contents": []map[string]interface{}{
				{"role": "user", "parts": []map[string]interface{}{{"text": prompt}}},
			},
			"generationConfig": map[string]interface{}{
				"temperature":     1.0,
				"topP":            0.95,
				"maxOutputTokens": 4000,
				"thinkingConfig": map[string]interface{}{
					"includeThoughts": true,
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(fullBody)
	upstreamURL := "https://daily-cloudcode-pa.googleapis.com/v1internal:generateContent"

	req, _ := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "antigravity/ide/2.1.1 linux/amd64")
	req.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudshelleditor/0.1")
	req.Header.Set("Client-Metadata", `{"ideType":9,"platform":2,"pluginType":2}`)

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
						Text    string `json:"text"`
						Thought bool   `json:"thought"`
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
			if !part.Thought && part.Text != "" {
				content += part.Text
			} else if part.Text != "" && content == "" {
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
	Text             string              `json:"text"`
	ThoughtSignature string              `json:"thoughtSignature"`
	Thought          bool                `json:"thought"`
	FunctionCall     *geminiFunctionCall `json:"functionCall"`
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
	currentThoughtSig := ""
	var toolCalls []map[string]interface{}
	toolCallIdx := 0

	if len(geminiResp.Candidates) > 0 {
		for _, part := range geminiResp.Candidates[0].Content.Parts {
			if part.ThoughtSignature != "" {
				currentThoughtSig = part.ThoughtSignature
				saveThoughtSignature("", currentThoughtSig)
				if reasoningContent == "" {
					reasoningContent = part.ThoughtSignature
				}
			}
			if part.Thought && part.Text != "" {
				reasoningContent += part.Text
			} else if part.Text != "" {
				content += part.Text
			}
			if part.FunctionCall != nil {
				sig := part.ThoughtSignature
				if sig == "" {
					sig = currentThoughtSig
				}
				if sig == "" {
					sig = getThoughtSignature(part.FunctionCall.Name, "", nil, nil)
				}
				saveThoughtSignature(part.FunctionCall.Name, sig)

				callID := fmt.Sprintf("call_%d_%d", time.Now().UnixMilli(), toolCallIdx)
				if sig != "" {
					callID = fmt.Sprintf("call_%d_%d___ts___%s", time.Now().UnixMilli(), toolCallIdx, sig)
				}

				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      part.FunctionCall.Name,
						"arguments": string(argsJSON),
					},
					"thought_signature": sig,
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
		usageMap := map[string]interface{}{
			"prompt_tokens":     geminiResp.UsageMetadata.PromptTokenCount,
			"completion_tokens": geminiResp.UsageMetadata.CandidatesTokenCount,
			"total_tokens":      geminiResp.UsageMetadata.TotalTokenCount,
		}
		if geminiResp.UsageMetadata.ThoughtsTokenCount > 0 {
			usageMap["completion_tokens_details"] = map[string]interface{}{
				"reasoning_tokens": geminiResp.UsageMetadata.ThoughtsTokenCount,
			}
		}
		openaiResp["usage"] = usageMap
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openaiResp)

	// Log the request
	var logConnID, logConnName string
	db.DB.QueryRow("SELECT id, COALESCE(email, name, '') FROM provider_connections WHERE provider_id='builtin-anigravity' AND is_active=1 LIMIT 1").Scan(&logConnID, &logConnName)
	if logConnName == "" {
		logConnName = "anigravity-connection"
	}
	promptTokens, completionTokens := 0, 0
	if geminiResp.UsageMetadata != nil {
		promptTokens = geminiResp.UsageMetadata.PromptTokenCount
		completionTokens = geminiResp.UsageMetadata.CandidatesTokenCount
	}
	logProxyRequest("builtin-anigravity", "Anigravity CLI", "anigravity", logConnID, logConnName, "", "",
		200, promptTokens, completionTokens, 0, "", nil)
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
		ThoughtsTokenCount   int
	}
	var lastFinishReason string
	var streamThoughtSig string
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
				ThoughtsTokenCount   int
			}{
				PromptTokenCount:     geminiChunk.UsageMetadata.PromptTokenCount,
				CandidatesTokenCount: geminiChunk.UsageMetadata.CandidatesTokenCount,
				TotalTokenCount:      geminiChunk.UsageMetadata.TotalTokenCount,
				ThoughtsTokenCount:   geminiChunk.UsageMetadata.ThoughtsTokenCount,
			}
		}

		// Convert finish_reason - only used in the final chunk
		var openaiFinish *string
		if lastFinishReason != "" {
			mapped := mapGeminiFinishReason(lastFinishReason)
			openaiFinish = &mapped
		}

		// Process parts - reasoning, text and function calls
		if len(geminiChunk.Candidates) > 0 {
			for _, part := range geminiChunk.Candidates[0].Content.Parts {
				if part.ThoughtSignature != "" {
					streamThoughtSig = part.ThoughtSignature
					saveThoughtSignature("", streamThoughtSig)
				}
				// Reasoning thought chunk
				if part.Thought && part.Text != "" {
					chunk := map[string]interface{}{
						"id":      fmt.Sprintf("anigravity-%d", time.Now().UnixMilli()),
						"object":  "chat.completion.chunk",
						"created": time.Now().Unix(),
						"model":   "anigravity",
						"choices": []map[string]interface{}{
							{
								"index":         0,
								"delta":         map[string]interface{}{"reasoning_content": part.Text},
								"finish_reason": nil,
							},
						},
					}
					chunkBytes, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
					flusher.Flush()
				} else if part.Text != "" {
					// Regular text chunk
					chunk := map[string]interface{}{
						"id":      fmt.Sprintf("anigravity-%d", time.Now().UnixMilli()),
						"object":  "chat.completion.chunk",
						"created": time.Now().Unix(),
						"model":   "anigravity",
						"choices": []map[string]interface{}{
							{
								"index":         0,
								"delta":         map[string]interface{}{"content": part.Text},
								"finish_reason": nil,
							},
						},
					}
					chunkBytes, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
					flusher.Flush()
				}

				// Function call chunk - NO finish_reason on intermediate chunks
				if part.FunctionCall != nil {
					sig := part.ThoughtSignature
					if sig == "" {
						sig = streamThoughtSig
					}
					if sig == "" {
						sig = getThoughtSignature(part.FunctionCall.Name, "", nil, nil)
					}
					saveThoughtSignature(part.FunctionCall.Name, sig)

					callID := fmt.Sprintf("call_%d_%d", time.Now().UnixMilli(), toolCallIdx)
					if sig != "" {
						callID = fmt.Sprintf("call_%d_%d___ts___%s", time.Now().UnixMilli(), toolCallIdx, sig)
					}

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
											"id":    callID,
											"type":  "function",
											"function": map[string]interface{}{
												"name":      part.FunctionCall.Name,
												"arguments": string(argsJSON),
											},
											"thought_signature": sig,
										},
									},
								},
								"finish_reason": nil,
							},
						},
					}
					chunkBytes, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
					flusher.Flush()
					toolCallIdx++
				}
			}
		}

		// Send final chunk with finish_reason only when stream is actually done
		if lastFinishReason != "" {
			finalChunk := map[string]interface{}{
				"id":      fmt.Sprintf("anigravity-%d", time.Now().UnixMilli()),
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   "anigravity",
				"choices": []map[string]interface{}{
					{
						"index":         0,
						"delta":         map[string]interface{}{},
						"finish_reason": openaiFinish,
					},
				},
			}
			if lastUsage != nil {
				usageMap := map[string]interface{}{
					"prompt_tokens":     lastUsage.PromptTokenCount,
					"completion_tokens": lastUsage.CandidatesTokenCount,
					"total_tokens":      lastUsage.TotalTokenCount,
				}
				if lastUsage.ThoughtsTokenCount > 0 {
					usageMap["completion_tokens_details"] = map[string]interface{}{
						"reasoning_tokens": lastUsage.ThoughtsTokenCount,
					}
				}
				finalChunk["usage"] = usageMap
			}
			chunkBytes, _ := json.Marshal(finalChunk)
			fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
			flusher.Flush()
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
	return projectID
}
