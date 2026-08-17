package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// codexDefaultModels is the curated fallback list when live discovery fails.
var codexDefaultModels = []string{
	"gpt-5.6-sol",
	"gpt-5.6-sol-pro",
	"gpt-5.6-terra",
	"gpt-5.6-terra-pro",
	"gpt-5.6-luna",
	"gpt-5.6-luna-pro",
	"gpt-5.5",
	"gpt-5.4-mini",
	"gpt-5.4",
	"gpt-5.3-codex",
	"gpt-5.3-codex-spark",
}

// extractChatGPTAccountID extracts chatgpt_account_id from a JWT access token.
func extractChatGPTAccountID(token string) string {
	if token == "" {
		return ""
	}
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	padLen := (4 - len(parts[1])%4) % 4
	payloadB64 := parts[1] + strings.Repeat("=", padLen)
	payload, err := base64.URLEncoding.DecodeString(payloadB64)
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	auth, ok := claims["https://api.openai.com/auth"].(map[string]interface{})
	if !ok {
		return ""
	}
	acctID, ok := auth["chatgpt_account_id"].(string)
	if !ok || acctID == "" {
		return ""
	}
	return acctID
}

// extractCodexEmail extracts email from a Codex JWT access token.
func extractCodexEmail(token string) string {
	if token == "" {
		return ""
	}
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	padLen := (4 - len(parts[1])%4) % 4
	payloadB64 := parts[1] + strings.Repeat("=", padLen)
	payload, err := base64.URLEncoding.DecodeString(payloadB64)
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	profile, ok := claims["https://api.openai.com/profile"].(map[string]interface{})
	if !ok {
		return ""
	}
	email, _ := profile["email"].(string)
	return email
}

// codexCloudflareHeaders returns headers required to avoid Cloudflare 403s.
func codexCloudflareHeaders(accessToken, accountID string) map[string]string {
	h := map[string]string{
		"User-Agent": "codex_cli_rs/0.0.0 (PAAP)",
		"originator": "codex_cli_rs",
	}
	if accountID == "" {
		accountID = extractChatGPTAccountID(accessToken)
	}
	if accountID != "" {
		h["ChatGPT-Account-ID"] = accountID
	}
	return h
}

// translateToCodexRequest converts Chat Completions to Responses API format.
func translateToCodexRequest(rawBody map[string]interface{}) (map[string]interface{}, error) {
	model, _ := rawBody["model"].(string)
	messages, _ := rawBody["messages"].([]interface{})
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages")
	}
	if idx := strings.Index(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	var instructions string
	var inputItems []interface{}

	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		switch role {
		case "system", "developer":
			if s, ok := msg["content"].(string); ok {
				instructions = s
			}
		case "user":
			if item := convertUserMessage(msg); item != nil {
				inputItems = append(inputItems, item)
			}
		case "assistant":
			inputItems = append(inputItems, convertAssistantMessage(msg)...)
		case "tool":
			toolCallID, _ := msg["tool_call_id"].(string)
			toolContent, _ := msg["content"].(string)
			inputItems = append(inputItems, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": toolCallID,
				"output":  toolContent,
			})
		}
	}

	reqBody := map[string]interface{}{
		"model":  model,
		"input":  inputItems,
		"store":  false,
		"stream": true,
	}
	if instructions != "" {
		reqBody["instructions"] = instructions
	}
	if tools, ok := rawBody["tools"].([]interface{}); ok && len(tools) > 0 {
		if ct := convertChatToolsToResponses(tools); len(ct) > 0 {
			reqBody["tools"] = ct
		}
	}
	if re, ok := rawBody["reasoning_effort"].(string); ok {
		reqBody["reasoning"] = map[string]interface{}{"effort": re, "summary": "auto"}
	}
	if reasoning, ok := rawBody["reasoning"].(map[string]interface{}); ok {
		if _, hasSummary := reasoning["summary"]; !hasSummary {
			reasoning["summary"] = "auto"
		}
		reqBody["reasoning"] = reasoning
	}
	return reqBody, nil
}

func convertUserMessage(msg map[string]interface{}) map[string]interface{} {
	content := msg["content"]
	if s, ok := content.(string); ok {
		return map[string]interface{}{
			"type":    "message",
			"role":    "user",
			"content": []interface{}{map[string]interface{}{"type": "input_text", "text": s}},
		}
	}
	parts, ok := content.([]interface{})
	if !ok {
		return nil
	}
	var converted []interface{}
	for _, part := range parts {
		p, ok := part.(map[string]interface{})
		if !ok {
			continue
		}
		ptype, _ := p["type"].(string)
		switch ptype {
		case "text":
			if text, ok := p["text"].(string); ok && text != "" {
				converted = append(converted, map[string]interface{}{"type": "input_text", "text": text})
			}
		case "image_url":
			imageRef, _ := p["image_url"].(map[string]interface{})
			url, _ := imageRef["url"].(string)
			if url != "" {
				imgPart := map[string]interface{}{"type": "input_image", "image_url": url}
				if detail, ok := imageRef["detail"].(string); ok && detail != "" {
					imgPart["detail"] = detail
				}
				converted = append(converted, imgPart)
			}
		}
	}
	if len(converted) == 0 {
		return nil
	}
	return map[string]interface{}{
		"type":    "message",
		"role":    "user",
		"content": converted,
	}
}

func convertAssistantMessage(msg map[string]interface{}) []interface{} {
	var items []interface{}
	content := msg["content"]
	toolCalls, _ := msg["tool_calls"].([]interface{})

	if len(toolCalls) > 0 {
		for _, tc := range toolCalls {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}
			fn, _ := tcMap["function"].(map[string]interface{})
			name, _ := fn["name"].(string)
			args, _ := fn["arguments"].(string)
			id, _ := tcMap["id"].(string)
			items = append(items, map[string]interface{}{
				"type":      "function_call",
				"call_id":   id,
				"name":      name,
				"arguments": args,
			})
		}
		return items
	}

	if content == nil {
		return nil
	}
	if s, ok := content.(string); ok {
		return []interface{}{map[string]interface{}{
			"type":    "message",
			"role":    "assistant",
			"content": []interface{}{map[string]interface{}{"type": "output_text", "text": s}},
		}}
	}
	parts, ok := content.([]interface{})
	if !ok {
		return nil
	}
	var converted []interface{}
	for _, part := range parts {
		p, ok := part.(map[string]interface{})
		if !ok {
			continue
		}
		if text, ok := p["text"].(string); ok && text != "" {
			converted = append(converted, map[string]interface{}{"type": "output_text", "text": text})
		}
	}
	if len(converted) == 0 {
		return nil
	}
	return []interface{}{map[string]interface{}{
		"type":    "message",
		"role":    "assistant",
		"content": converted,
	}}
}

func convertChatToolsToResponses(tools []interface{}) []interface{} {
	var converted []interface{}
	for _, t := range tools {
		m, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		fn, ok := m["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		params := fn["parameters"]
		if params == nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		converted = append(converted, map[string]interface{}{
			"type":        "function",
			"name":        name,
			"description": fn["description"],
			"strict":      false,
			"parameters":  params,
		})
	}
	return converted
}

func parseCodexModels(resp map[string]interface{}) []string {
	if resp == nil {
		return nil
	}
	entries, _ := resp["models"].([]interface{})
	type modelEntry struct {
		rank int
		slug string
	}
	var sortable []modelEntry
	for _, item := range entries {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		slug, _ := m["slug"].(string)
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		visibility, _ := m["visibility"].(string)
		if strings.ToLower(strings.TrimSpace(visibility)) == "hide" ||
			strings.ToLower(strings.TrimSpace(visibility)) == "hidden" {
			continue
		}
		rank := 10000
		if p, ok := m["priority"]; ok {
			if v, ok := p.(float64); ok {
				rank = int(v)
			}
		}
		sortable = append(sortable, modelEntry{rank: rank, slug: slug})
	}
	sort.Slice(sortable, func(i, j int) bool {
		if sortable[i].rank != sortable[j].rank {
			return sortable[i].rank < sortable[j].rank
		}
		return sortable[i].slug < sortable[j].slug
	})
	var result []string
	for _, e := range sortable {
		result = append(result, e.slug)
	}
	return result
}

func fetchCodexModelsLive(accessToken, accountID, baseURL string) []string {
	if baseURL == "" {
		baseURL = "https://chatgpt.com"
	}
	url := strings.TrimRight(baseURL, "/") + "/backend-api/codex/models?client_version=1.0.0"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil
	}
	return parseCodexModels(body)
}

func handleCodexProxy(w http.ResponseWriter, r *http.Request, providerID, keyValue, baseURL string) {
	var rawBody map[string]interface{}
	if err := parseBody(r, &rawBody); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	handleCodexProxyBody(w, r, rawBody, keyValue, baseURL, "", "builtin-openai-codex", "OpenAI Codex", "", "")
}

// handleCodexProxyBody forwards a request body already parsed by the main router.
func handleCodexProxyBody(w http.ResponseWriter, r *http.Request, rawBody map[string]interface{}, keyValue, baseURL, accountID, providerID, providerName, keyID, keyName string) {
	startTime := time.Now()
	messages, _ := rawBody["messages"].([]interface{})
	if len(messages) == 0 {
		writeError(w, 400, "no messages")
		return
	}
	model, _ := rawBody["model"].(string)
	if idx := strings.Index(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	reqBody, err := translateToCodexRequest(rawBody)
	if err != nil {
		writeError(w, 400, "failed to translate request: "+err.Error())
		return
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		writeError(w, 500, "failed to marshal request body")
		return
	}
	upstreamURL := strings.TrimRight(baseURL, "/") + "/responses"
	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, 500, "failed to create upstream request")
		return
	}
	for k, v := range codexCloudflareHeaders(keyValue, accountID) {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyValue)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, 502, "upstream error: "+err.Error())
		return
	}
	defer resp.Body.Close()
	isStream, _ := rawBody["stream"].(bool)
	latencyMs := time.Since(startTime).Milliseconds()
	log.Printf("[PAAP] [CODEX-REQ] model=%s stream=%v latency=%dms status=%d", model, isStream, latencyMs, resp.StatusCode)
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		log.Printf("[PAAP] [CODEX-RESP] status=%d body=%s", resp.StatusCode, truncateStr(string(errBody), 500))
		latencyMs := time.Since(startTime).Milliseconds()
		logProxyRequest(providerID, providerName, model, keyID, keyName, "", "", resp.StatusCode, 0, 0, latencyMs, string(errBody), nil)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(errBody)
		return
	}
	if isStream {
		tIn, tOut := handleCodexStreamingResponse(w, resp, model)
		latencyMs := time.Since(startTime).Milliseconds()
		logProxyRequest(providerID, providerName, model, keyID, keyName, "", "", 200, tIn, tOut, latencyMs, "", nil)
	} else {
		tIn, tOut := handleCodexNonStreamingResponse(w, resp, model)
		latencyMs := time.Since(startTime).Milliseconds()
		logProxyRequest(providerID, providerName, model, keyID, keyName, "", "", 200, tIn, tOut, latencyMs, "", nil)
	}
}

func handleCodexProxyWithUpstream(w http.ResponseWriter, r *http.Request, providerID, keyValue, upstreamURL string) {
	startTime := time.Now()
	var rawBody map[string]interface{}
	if err := parseBody(r, &rawBody); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	messages, _ := rawBody["messages"].([]interface{})
	if len(messages) == 0 {
		writeError(w, 400, "no messages")
		return
	}
	model, _ := rawBody["model"].(string)
	if idx := strings.Index(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	reqBody, err := translateToCodexRequest(rawBody)
	if err != nil {
		writeError(w, 400, "failed to translate request: "+err.Error())
		return
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		writeError(w, 500, "failed to marshal request body")
		return
	}
	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, 500, "failed to create upstream request")
		return
	}
	for k, v := range codexCloudflareHeaders(keyValue, "") {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyValue)
	req.Header.Set("Accept", "text/event-stream")
	isStream, _ := rawBody["stream"].(bool)
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, 502, "upstream error: "+err.Error())
		return
	}
	defer resp.Body.Close()
	latencyMs := time.Since(startTime).Milliseconds()
	log.Printf("[PAAP] [CODEX-REQ] model=%s stream=%v latency=%dms status=%d", model, isStream, latencyMs, resp.StatusCode)
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(errBody)
		return
	}
	if isStream {
		handleCodexStreamingResponse(w, resp, model)
	} else {
		handleCodexNonStreamingResponse(w, resp, model)
	}
}

func handleCodexNonStreamingResponse(w http.ResponseWriter, resp *http.Response, model string) (int, int) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	var outputText strings.Builder
	var reasoningText strings.Builder
	var totalInputTokens, totalOutputTokens int
	var toolCalls []interface{}
	fcName := ""
	fcArgs := ""
	responseIncompleted := false
	var incompleteReason string

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var event map[string]interface{}
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		evtType, _ := event["type"].(string)
		switch evtType {
		case "response.reasoning_summary_text.delta":
			if delta, ok := event["delta"].(string); ok {
				reasoningText.WriteString(delta)
			}
		case "response.output_text.delta":
			if delta, ok := event["delta"].(string); ok {
				outputText.WriteString(delta)
			}
		case "response.output_item.done":
			if item, ok := event["item"].(map[string]interface{}); ok {
				if item["type"] == "function_call" {
					name, _ := item["name"].(string)
					args, _ := item["arguments"].(string)
					callID, _ := item["call_id"].(string)
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   callID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      name,
							"arguments": args,
						},
					})
					_ = fcName
					_ = fcArgs
				}
			}
		case "response.completed", "response.incomplete":
			if respData, ok := event["response"].(map[string]interface{}); ok {
				if usage, ok := respData["usage"].(map[string]interface{}); ok {
					if v, ok := usage["input_tokens"].(float64); ok {
						totalInputTokens = int(v)
					}
					if v, ok := usage["output_tokens"].(float64); ok {
						totalOutputTokens = int(v)
					}
				}
				if evtType == "response.incomplete" {
					responseIncompleted = true
					incompleteReason = "max_output_tokens"
				} else if incDetails, ok := respData["incomplete_details"].(map[string]interface{}); ok {
					if reason, ok := incDetails["reason"].(string); ok && reason != "" {
						responseIncompleted = true
						incompleteReason = reason
					}
				}
			}
		}
	}

	finishReason := "stop"
	if responseIncompleted && incompleteReason == "max_output_tokens" {
		finishReason = "length"
	} else if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	usage := map[string]interface{}{
		"prompt_tokens":     totalInputTokens,
		"completion_tokens": totalOutputTokens,
		"total_tokens":      totalInputTokens + totalOutputTokens,
	}
	msg := map[string]interface{}{
		"role":    "assistant",
		"content": outputText.String(),
	}
	if reasoningText.Len() > 0 {
		msg["reasoning_content"] = reasoningText.String()
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		msg["content"] = nil
	}
	chatResp := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"message":       msg,
				"finish_reason": finishReason,
			},
		},
		"usage": usage,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chatResp)
	return totalInputTokens, totalOutputTokens
}

// SSE chunk helpers — avoids Go composite literal type inference issues

func makeChatChunkDelta(chatID, model string, delta map[string]interface{}, finishReason interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":      chatID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         delta,
				"finish_reason": finishReason,
			},
		},
	}
}

func makeChatChunkDeltaWithUsage(chatID, model string, delta map[string]interface{}, finishReason interface{}, usage map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":      chatID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         delta,
				"finish_reason": finishReason,
			},
		},
		"usage": usage,
	}
}

func handleCodexStreamingResponse(w http.ResponseWriter, resp *http.Response, model string) (int, int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher, canFlush := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	chatID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	roleSent := false
	var totalInputTokens, totalOutputTokens int
	fcName := ""
	fcItemID := ""
	fcCallID := ""
	fcArgs := ""
	fcStarted := false
	sawToolCall := false
	fcToolIndex := 0         // OpenAI tool_calls[].index counter
	fcReceivedDelta := false // true once any argument delta arrives for current tool call

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataStr := strings.TrimPrefix(line, "data: ")
		if dataStr == "[DONE]" {
			break
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &event); err != nil {
			continue
		}
		eventType, _ := event["type"].(string)

		switch eventType {
		case "response.reasoning_summary_text.delta":
			if !roleSent {
				sendCodexSSE(w, makeChatChunkDelta(chatID, model, map[string]interface{}{"role": "assistant"}, nil), flusher, canFlush)
				roleSent = true
			}
			if delta, _ := event["delta"].(string); delta != "" {
				sendCodexSSE(w, makeChatChunkDelta(chatID, model, map[string]interface{}{"reasoning_content": delta}, nil), flusher, canFlush)
			}

		case "response.output_text.delta":
			if !roleSent {
				sendCodexSSE(w, makeChatChunkDelta(chatID, model, map[string]interface{}{"role": "assistant"}, nil), flusher, canFlush)
				roleSent = true
			}
			if delta, _ := event["delta"].(string); delta != "" {
				sendCodexSSE(w, makeChatChunkDelta(chatID, model, map[string]interface{}{"content": delta}, nil), flusher, canFlush)
			}

		case "response.output_item.added":
			if item, ok := event["item"].(map[string]interface{}); ok {
				if item["type"] == "function_call" {
					if n, ok := item["name"].(string); ok && n != "" {
						fcName = n
					}
					if id, ok := item["id"].(string); ok && id != "" {
						fcItemID = id
					}
					if cid, ok := item["call_id"].(string); ok && cid != "" {
						fcCallID = cid
					}
					fcReceivedDelta = false // reset for new tool call
				}
			}
		case "response.function_call_arguments.delta":
			if !roleSent {
				sendCodexSSE(w, makeChatChunkDelta(chatID, model, map[string]interface{}{"role": "assistant"}, nil), flusher, canFlush)
				roleSent = true
			}
			itemID, _ := event["item_id"].(string)
			delta, _ := event["delta"].(string)
			if itemID != "" {
				fcItemID = itemID
			}
			fcArgs += delta
			fcReceivedDelta = true
			callID := fcCallID
			if callID == "" {
				callID = fcItemID
			}
			fcDelta := map[string]interface{}{
				"index": fcToolIndex,
				"id":    callID,
				"type":  "function",
				"function": map[string]interface{}{
					"arguments": delta,
				},
			}
			if !fcStarted && fcName != "" {
				fcDelta["function"].(map[string]interface{})["name"] = fcName
				fcStarted = true
				sawToolCall = true
			}
			sendCodexSSE(w, makeChatChunkDelta(chatID, model, map[string]interface{}{"tool_calls": []interface{}{fcDelta}}, nil), flusher, canFlush)

		case "response.function_call_arguments.done":
			itemID, _ := event["item_id"].(string)
			arguments, _ := event["arguments"].(string)
			if itemID != "" {
				fcItemID = itemID
			}
			if arguments != "" {
				fcArgs = arguments
			}
			// Only emit full arguments here if no delta was received (fallback path).
			// If deltas were emitted, the arguments are already fully delivered
			// and re-emitting would cause duplication when Hermes concatenates chunks.
			if !fcReceivedDelta {
				callID := fcCallID
				if callID == "" {
					callID = fcItemID
				}
				finalFC := map[string]interface{}{
					"index": fcToolIndex,
					"id":    callID,
					"type":  "function",
					"function": map[string]interface{}{
						"name":      fcName,
						"arguments": fcArgs,
					},
				}
				if !fcStarted && fcName != "" {
					fcStarted = true
					sawToolCall = true
				}
				sendCodexSSE(w, makeChatChunkDelta(chatID, model, map[string]interface{}{"tool_calls": []interface{}{finalFC}}, nil), flusher, canFlush)
			}

		case "response.output_item.done":
			// Only reset tool-call state for function_call items.
			// Reasoning/message output_item.done must not shift tool-call indices.
			if item, ok := event["item"].(map[string]interface{}); ok {
				if item["type"] == "function_call" {
					fcName = ""
					fcItemID = ""
					fcCallID = ""
					fcArgs = ""
					fcStarted = false
					fcReceivedDelta = false
					fcToolIndex++
				}
			}

		case "response.completed", "response.incomplete":
			if resp, ok := event["response"].(map[string]interface{}); ok {
				if usage, ok := resp["usage"].(map[string]interface{}); ok {
					if v, ok := usage["input_tokens"].(float64); ok {
						totalInputTokens = int(v)
					}
					if v, ok := usage["output_tokens"].(float64); ok {
						totalOutputTokens = int(v)
					}
				}
			}
			finishReason := "stop"
			if eventType == "response.incomplete" {
				finishReason = "length"
			} else if respMap, ok := event["response"].(map[string]interface{}); ok {
				if incDetails, ok := respMap["incomplete_details"].(map[string]interface{}); ok {
					if reason, ok := incDetails["reason"].(string); ok && reason != "" {
						if reason == "max_output_tokens" {
							finishReason = "length"
						}
					}
				}
			}
			if sawToolCall && finishReason == "stop" {
				finishReason = "tool_calls"
			}
			sendCodexSSE(w, makeChatChunkDeltaWithUsage(chatID, model, map[string]interface{}{}, finishReason, map[string]interface{}{
				"prompt_tokens":     totalInputTokens,
				"completion_tokens": totalOutputTokens,
				"total_tokens":      totalInputTokens + totalOutputTokens,
			}), flusher, canFlush)
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if canFlush {
				flusher.Flush()
			}
		}
	}
	return totalInputTokens, totalOutputTokens
}

func sendCodexSSE(w http.ResponseWriter, data interface{}, flusher http.Flusher, canFlush bool) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	if canFlush {
		flusher.Flush()
	}
}
