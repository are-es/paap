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
)

// CodeBuddy (Tencent CodeBuddy CLI) provider.
//
// The upstream chat API at https://www.codebuddy.ai/v2/chat/completions speaks
// standard OpenAI Chat Completions over SSE, with three hard constraints:
//
//  1. The first messages entry MUST be role=system, else error code 11128
//     "first message is not system prompt".
//  2. stream:true is required upstream — non-stream requests get 400. We
//     always stream upstream and aggregate SSE into a single completion when
//     the PAAP client requested non-stream.
//  3. Requests must carry IDE-fingerprint headers (User-Agent + X-IDE-*) or
//     the edge gateway rejects with code:12403 "check ua".
//
// Auth is an OAuth Bearer access token (issued by the external-link login flow
// in oauth.go). Tokens are valid ~1 year; refresh is handled by
// refreshCodebuddyConnection.

// codebuddyDefaultSystem is prepended when the client's message list lacks a
// leading system message. The upstream rejects non-system-first payloads.
const codebuddyDefaultSystem = "You are a helpful assistant."

// translateToCodebuddyRequest clones the client's OpenAI Chat Completions
// body, ensures a leading system message, and forces stream:true upstream.
func translateToCodebuddyRequest(rawBody map[string]interface{}) (map[string]interface{}, error) {
	messages, ok := rawBody["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return nil, fmt.Errorf("no messages")
	}
	// Ensure the first message is a system message.
	first, _ := messages[0].(map[string]interface{})
	if first == nil || first["role"] != "system" {
		systemMsg := map[string]interface{}{
			"role":    "system",
			"content": codebuddyDefaultSystem,
		}
		messages = append([]interface{}{systemMsg}, messages...)
	}

	out := make(map[string]interface{})
	for k, v := range rawBody {
		out[k] = v
	}
	out["messages"] = messages
	out["stream"] = true
	// Include usage in the final SSE chunk so extractUsage can account tokens.
	out["stream_options"] = map[string]interface{}{"include_usage": true}
	if model, ok := out["model"].(string); ok {
		if idx := strings.Index(model, "/"); idx >= 0 {
			out["model"] = model[idx+1:]
		}
	}
	return out, nil
}

// handleCodebuddyProxyBody forwards a request body already parsed by the main
// router. It is the CodeBuddy equivalent of handleCodexProxyBody.
func handleCodebuddyProxyBody(w http.ResponseWriter, r *http.Request, rawBody map[string]interface{}, keyValue, baseURL, providerID, providerName, keyID, keyName string, reqDump *RequestDump) {
	startTime := time.Now()
	clientKey := ""
	if k := r.Context().Value("gateway_key_name"); k != nil {
		clientKey, _ = k.(string)
	}
	_ = clientKey

	messages, _ := rawBody["messages"].([]interface{})
	if len(messages) == 0 {
		writeError(w, 400, "no messages")
		return
	}
	model, _ := rawBody["model"].(string)
	if idx := strings.Index(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	reqBody, err := translateToCodebuddyRequest(rawBody)
	if err != nil {
		writeError(w, 400, "failed to translate request: "+err.Error())
		return
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		writeError(w, 500, "failed to marshal request body")
		return
	}

	upstreamURL := strings.TrimRight(baseURL, "/") + "/v2/chat/completions"
	reqDump.SetUpstream(providerName, upstreamURL, reqBody)

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, 500, "failed to create upstream request")
		return
	}
	for k, v := range codebuddyFingerprintHeaders() {
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
	log.Printf("[PAAP] [CODEBUDDY-REQ] model=%s stream=%v latency=%dms status=%d", model, isStream, latencyMs, resp.StatusCode)

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		log.Printf("[PAAP] [CODEBUDDY-RESP] status=%d body=%s", resp.StatusCode, truncateStr(string(errBody), 500))
		logProxyRequest(providerID, providerName, model, keyID, keyName, "", "", resp.StatusCode, 0, 0, latencyMs, string(errBody), nil)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(errBody)
		return
	}

	if isStream {
		counts, _ := handleStreamingSplit(w, resp)
		latencyMs = time.Since(startTime).Milliseconds()
		logProxyRequestSplit(providerID, providerName, model, keyID, keyName, "", "", 200, counts, latencyMs, "", nil, "", "", 0, 0)
		reqDump.Finish(200, latencyMs, counts.TotalIn(), counts.TotalOut(), nil)
	} else {
		counts := handleCodebuddyNonStreamingResponse(w, resp, model)
		latencyMs = time.Since(startTime).Milliseconds()
		logProxyRequestSplit(providerID, providerName, model, keyID, keyName, "", "", 200, counts, latencyMs, "", nil, "", "", 0, 0)
		reqDump.Finish(200, latencyMs, counts.TotalIn(), counts.TotalOut(), nil)
	}
}

// handleCodebuddyNonStreamingResponse consumes the upstream SSE stream (which
// is always streamed) and emits a single OpenAI chat.completion JSON object
// for clients that requested non-streaming. Returns the token breakdown for
// logging. Modeled on handleCodexNonStreamingResponse (codex.go:532) but for
// standard OpenAI SSE rather than the Responses API.
func handleCodebuddyNonStreamingResponse(w http.ResponseWriter, resp *http.Response, model string) tokenCounts {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	var outputText strings.Builder
	var reasoningText strings.Builder
	var counts tokenCounts
	var toolCalls []interface{}
	var finishReason string
	var id, respModel string
	var created int64

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk map[string]interface{}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if v, ok := chunk["id"].(string); ok && v != "" {
			id = v
		}
		if v, ok := chunk["model"].(string); ok && v != "" {
			respModel = v
		}
		if v, ok := chunk["created"].(float64); ok {
			created = int64(v)
		}
		if usage, ok := chunk["usage"].(map[string]interface{}); ok {
			extractUsage(usage, &counts)
		}
		choices, _ := chunk["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]interface{})
		if choice == nil {
			continue
		}
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			finishReason = fr
		}
		delta, _ := choice["delta"].(map[string]interface{})
		if delta == nil {
			continue
		}
		if c, ok := delta["content"].(string); ok {
			outputText.WriteString(c)
		}
		if rc, ok := delta["reasoning_content"].(string); ok {
			reasoningText.WriteString(rc)
		}
		if tcs, ok := delta["tool_calls"].([]interface{}); ok {
			toolCalls = append(toolCalls, tcs...)
		}
	}

	if respModel == "" {
		respModel = model
	}

	message := map[string]interface{}{
		"role":    "assistant",
		"content": outputText.String(),
	}
	if reasoningText.Len() > 0 {
		message["reasoning_content"] = reasoningText.String()
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	completion := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   respModel,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
	}
	if counts.TotalIn() > 0 || counts.TotalOut() > 0 {
		completion["usage"] = map[string]interface{}{
			"prompt_tokens":     counts.TotalIn(),
			"completion_tokens":  counts.TotalOut(),
			"total_tokens":       counts.TotalIn() + counts.TotalOut(),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(completion)
	return counts
}

// detectCodebuddyModels fetches the live model catalog from /v3/config. The
// upstream does not expose a /v2/models endpoint; the catalog is bundled in
// the config response under data.agents[].models. Alias entries (default-model,
// fast-model, etc.) are filtered out — only concrete model IDs are returned.
func detectCodebuddyModels(accessToken string) []string {
	req, err := http.NewRequest("GET", codebuddyBaseURL+"/v3/config", nil)
	if err != nil {
		return nil
	}
	for k, v := range codebuddyFingerprintHeaders() {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PAAP] CodeBuddy model detect error: %v", err)
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		log.Printf("[PAAP] CodeBuddy config returned %d: %s", resp.StatusCode, truncateStr(string(body), 200))
		return nil
	}
	var wrapper struct {
		Code int `json:"code"`
		Data struct {
			Agents []struct {
				Models []string `json:"models"`
			} `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		log.Printf("[PAAP] CodeBuddy config parse error: %v", err)
		return nil
	}
	if wrapper.Code != 0 {
		return nil
	}
	aliases := map[string]bool{
		"default-model": true, "fast-model": true, "balanced-model": true,
		"primary-model": true, "deep-model": true, "lite": true,
	}
	seen := make(map[string]bool)
	var models []string
	for _, a := range wrapper.Data.Agents {
		for _, m := range a.Models {
			if aliases[m] || seen[m] {
				continue
			}
			seen[m] = true
			models = append(models, m)
		}
	}
	return models
}
