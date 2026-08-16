package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── JWT Account ID Extraction ──────────────────────────────

func TestExtractChatGPTAccountID_ValidJWT(t *testing.T) {
	claims := map[string]interface{}{
		"https://api.openai.com/auth": map[string]interface{}{
			"chatgpt_account_id": "acct_12345",
		},
	}
	payloadBytes, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	token := "header." + payloadB64 + ".sig"

	got := extractChatGPTAccountID(token)
	if got != "acct_12345" {
		t.Errorf("got %q, want %q", got, "acct_12345")
	}
}

func TestExtractChatGPTAccountID_NoAuthClaim(t *testing.T) {
	claims := map[string]interface{}{"sub": "user123"}
	payloadBytes, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	token := "header." + payloadB64 + ".sig"

	got := extractChatGPTAccountID(token)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractChatGPTAccountID_MalformedToken(t *testing.T) {
	for _, token := range []string{"", "not-a-jwt", "only.two", "header.base64url==.sig"} {
		if got := extractChatGPTAccountID(token); got != "" {
			t.Errorf("token %q: got %q, want empty", token, got)
		}
	}
}

func TestExtractChatGPTAccountID_EmptyAccountID(t *testing.T) {
	claims := map[string]interface{}{
		"https://api.openai.com/auth": map[string]interface{}{"chatgpt_account_id": ""},
	}
	payloadBytes, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	token := "header." + payloadB64 + ".sig"
	if got := extractChatGPTAccountID(token); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ── Request Translation ─────────────────────────────────────

func TestTranslateToCodexRequest_SimpleText(t *testing.T) {
	body := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are helpful."},
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
	}
	got, err := translateToCodexRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if got["instructions"] != "You are helpful." {
		t.Errorf("instructions = %v", got["instructions"])
	}
	if got["model"] != "gpt-5.4" {
		t.Errorf("model = %v", got["model"])
	}
	if got["store"] != false {
		t.Errorf("store = %v", got["store"])
	}
	input := got["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("input length = %d, want 1", len(input))
	}
	item := input[0].(map[string]interface{})
	if item["role"] != "user" {
		t.Errorf("role = %v", item["role"])
	}
}

func TestTranslateToCodexRequest_DeveloperInstructions(t *testing.T) {
	body := map[string]interface{}{
		"model": "gpt-5.6-sol",
		"messages": []interface{}{
			map[string]interface{}{"role": "developer", "content": "Name: ARES."},
			map[string]interface{}{"role": "user", "content": "Who are you?"},
		},
	}

	got, err := translateToCodexRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if got["instructions"] != "Name: ARES." {
		t.Errorf("instructions = %v, want developer content", got["instructions"])
	}
}

func TestTranslateToCodexRequest_MultimodalUserContent(t *testing.T) {
	body := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "What is this?"},
					map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": "https://example.com/img.png", "detail": "high"},
					},
				},
			},
		},
	}
	got, err := translateToCodexRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	input := got["input"].([]interface{})
	item := input[0].(map[string]interface{})
	content := item["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("content length = %d, want 2", len(content))
	}
	textPart := content[0].(map[string]interface{})
	if textPart["type"] != "input_text" || textPart["text"] != "What is this?" {
		t.Errorf("text part = %v", textPart)
	}
	imgPart := content[1].(map[string]interface{})
	if imgPart["type"] != "input_image" || imgPart["image_url"] != "https://example.com/img.png" {
		t.Errorf("image part = %v", imgPart)
	}
}

func TestTranslateToCodexRequest_ToolCalls(t *testing.T) {
	body := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "weather in NYC"},
			map[string]interface{}{
				"role": "assistant", "content": nil,
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id": "call_1", "type": "function",
						"function": map[string]interface{}{"name": "get_weather", "arguments": `{"city":"NYC"}`},
					},
				},
			},
			map[string]interface{}{
				"role": "tool", "tool_call_id": "call_1", "content": `{"temp":72}`,
			},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": "get_weather", "description": "Get weather",
					"parameters": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
					},
				},
			},
		},
	}
	got, err := translateToCodexRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	input := got["input"].([]interface{})
	if len(input) != 3 {
		t.Fatalf("input length = %d, want 3", len(input))
	}
	assistantItem := input[1].(map[string]interface{})
	if assistantItem["type"] != "function_call" || assistantItem["name"] != "get_weather" {
		t.Errorf("assistant item = %v", assistantItem)
	}
	if assistantItem["call_id"] != "call_1" {
		t.Errorf("assistant call_id = %v", assistantItem["call_id"])
	}
	if assistantItem["type"] != "function_call" || assistantItem["name"] != "get_weather" {
		t.Errorf("assistant function call = %v", assistantItem)
	}
	toolItem := input[2].(map[string]interface{})
	if toolItem["type"] != "function_call_output" {
		t.Errorf("tool item type = %v", toolItem["type"])
	}
	tools := got["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	if tool["name"] != "get_weather" {
		t.Errorf("tool name = %v", tool["name"])
	}
}

func TestTranslateToCodexRequest_ReasoningEffort(t *testing.T) {
	body := map[string]interface{}{
		"model": "gpt-5.4", "reasoning_effort": "high",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "think"}},
	}
	got, err := translateToCodexRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	reasoning := got["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "high" {
		t.Errorf("effort = %v", reasoning["effort"])
	}
	if reasoning["summary"] != "auto" {
		t.Errorf("summary = %v, want auto", reasoning["summary"])
	}
}

func TestTranslateToCodexRequest_ReasoningAddsSummary(t *testing.T) {
	body := map[string]interface{}{
		"model": "gpt-5.6-sol",
		"reasoning": map[string]interface{}{
			"effort": "medium",
		},
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "think"}},
	}
	got, err := translateToCodexRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	reasoning := got["reasoning"].(map[string]interface{})
	if reasoning["summary"] != "auto" {
		t.Errorf("summary = %v, want auto", reasoning["summary"])
	}
}

func TestTranslateToCodexRequest_StripsModelPrefix(t *testing.T) {
	body := map[string]interface{}{
		"model":    "openai-codex/gpt-5.4",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}
	got, _ := translateToCodexRequest(body)
	if got["model"] != "gpt-5.4" {
		t.Errorf("model = %v", got["model"])
	}
}

// ── Response Translation: Non-Streaming ─────────────────────

func TestTranslateFromCodexResponse_TextNonStreaming(t *testing.T) {
	codexResp := map[string]interface{}{
		"output": []interface{}{
			map[string]interface{}{
				"type": "message",
				"content": []interface{}{
					map[string]interface{}{"type": "output_text", "text": "Hello world"},
				},
			},
		},
		"usage": map[string]interface{}{"input_tokens": float64(10), "output_tokens": float64(5)},
	}
	var outputText string
	if output, ok := codexResp["output"].([]interface{}); ok {
		for _, item := range output {
			m := item.(map[string]interface{})
			if m["type"] == "message" {
				for _, c := range m["content"].([]interface{}) {
					p := c.(map[string]interface{})
					outputText += p["text"].(string)
				}
			}
		}
	}
	if outputText != "Hello world" {
		t.Errorf("outputText = %q", outputText)
	}
	usage := codexResp["usage"].(map[string]interface{})
	if int(usage["input_tokens"].(float64)) != 10 {
		t.Errorf("input_tokens = %v", usage["input_tokens"])
	}
}

func TestTranslateFromCodexResponse_ToolCallNonStreaming(t *testing.T) {
	codexResp := map[string]interface{}{
		"output": []interface{}{
			map[string]interface{}{
				"type":      "function_call",
				"id":        "fc_1",
				"name":      "get_weather",
				"arguments": `{"city":"NYC"}`,
			},
		},
		"usage": map[string]interface{}{"input_tokens": float64(20), "output_tokens": float64(10)},
	}
	var toolCalls []interface{}
	if output, ok := codexResp["output"].([]interface{}); ok {
		for _, item := range output {
			m := item.(map[string]interface{})
			if m["type"] == "function_call" {
				toolCalls = append(toolCalls, map[string]interface{}{
					"id": m["id"], "type": "function",
					"function": map[string]interface{}{"name": m["name"], "arguments": m["arguments"]},
				})
			}
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(toolCalls))
	}
	tc := toolCalls[0].(map[string]interface{})
	fn := tc["function"].(map[string]interface{})
	if fn["name"] != "get_weather" {
		t.Errorf("name = %v", fn["name"])
	}
}

// ── SSE Streaming Translation ──────────────────────────────

func TestTranslateCodexSSE_TextStream(t *testing.T) {
	events := []map[string]interface{}{
		{"type": "response.output_text.delta", "delta": "Hello"},
		{"type": "response.output_text.delta", "delta": " world"},
		{"type": "response.completed", "response": map[string]interface{}{"usage": map[string]interface{}{"input_tokens": float64(10), "output_tokens": float64(5)}}},
		{"type": "done"},
	}
	var chunks []map[string]interface{}
	roleSent := false
	for _, event := range events {
		eventType, _ := event["type"].(string)
		switch eventType {
		case "response.output_text.delta":
			if !roleSent {
				roleDelta := map[string]interface{}{"role": "assistant"}
				choice := map[string]interface{}{"index": 0, "delta": roleDelta, "finish_reason": nil}
				chunks = append(chunks, map[string]interface{}{"choices": []interface{}{choice}})
				roleSent = true
			}
			delta, _ := event["delta"].(string)
			contentDelta := map[string]interface{}{"content": delta}
			choice := map[string]interface{}{"index": 0, "delta": contentDelta, "finish_reason": nil}
			chunks = append(chunks, map[string]interface{}{"choices": []interface{}{choice}})
		case "response.completed":
			choice := map[string]interface{}{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"}
			chunks = append(chunks, map[string]interface{}{"choices": []interface{}{choice}})
		}
	}
	if len(chunks) < 3 {
		t.Fatalf("chunks = %d, want >= 3", len(chunks))
	}
	// Check role chunk
	firstChoices := chunks[0]["choices"].([]interface{})
	firstDelta := firstChoices[0].(map[string]interface{})["delta"].(map[string]interface{})
	if firstDelta["role"] != "assistant" {
		t.Errorf("role = %v", firstDelta["role"])
	}
	// Check content chunk
	secondChoices := chunks[1]["choices"].([]interface{})
	secondDelta := secondChoices[0].(map[string]interface{})["delta"].(map[string]interface{})
	if secondDelta["content"] != "Hello" {
		t.Errorf("content = %v", secondDelta["content"])
	}
}

func TestTranslateCodexSSE_ToolCallStream(t *testing.T) {
	events := []map[string]interface{}{
		{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "name": "get_weather", "delta": `{"ci`},
		{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "delta": `ty":`},
		{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "delta": `"NYC"}`},
		{"type": "response.function_call_arguments.done", "item_id": "fc_1", "name": "get_weather", "arguments": `{"city":"NYC"}`},
		{"type": "response.completed", "response": map[string]interface{}{"usage": map[string]interface{}{"input_tokens": float64(20), "output_tokens": float64(10)}}},
		{"type": "done"},
	}
	var chunks []map[string]interface{}
	roleSent := false
	for _, event := range events {
		eventType, _ := event["type"].(string)
		switch eventType {
		case "response.function_call_arguments.delta":
			if !roleSent {
				roleDelta := map[string]interface{}{"role": "assistant"}
				choice := map[string]interface{}{"index": 0, "delta": roleDelta, "finish_reason": nil}
				chunks = append(chunks, map[string]interface{}{"choices": []interface{}{choice}})
				roleSent = true
			}
			itemID, _ := event["item_id"].(string)
			delta, _ := event["delta"].(string)
			tcDelta := map[string]interface{}{
				"id": itemID, "type": "function",
				"function": map[string]interface{}{"arguments": delta},
			}
			choice := map[string]interface{}{"index": 0, "delta": map[string]interface{}{"tool_calls": []interface{}{tcDelta}}, "finish_reason": nil}
			chunks = append(chunks, map[string]interface{}{"choices": []interface{}{choice}})
		case "response.function_call_arguments.done":
			name, _ := event["name"].(string)
			arguments, _ := event["arguments"].(string)
			finalFC := map[string]interface{}{
				"id": "fc_1", "type": "function",
				"function": map[string]interface{}{"name": name, "arguments": arguments},
			}
			choice := map[string]interface{}{"index": 0, "delta": map[string]interface{}{"tool_calls": []interface{}{finalFC}}, "finish_reason": nil}
			chunks = append(chunks, map[string]interface{}{"choices": []interface{}{choice}})
		}
	}
	if len(chunks) < 3 {
		t.Fatalf("chunks = %d, want >= 3", len(chunks))
	}
	// Find function_call_item chunk
	foundFC := false
	for _, chunk := range chunks {
		choices := chunk["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]interface{})
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		tcs, ok := delta["tool_calls"].([]interface{})
		if !ok || len(tcs) == 0 {
			continue
		}
		tc := tcs[0].(map[string]interface{})
		fn := tc["function"].(map[string]interface{})
		if fn["name"] == "get_weather" {
			foundFC = true
			break
		}
	}
	if !foundFC {
		t.Error("no function_call_item with name=get_weather")
	}
}

// ── Model Parsing & Fallback ───────────────────────────────

func TestParseCodexModels_HidesHidden(t *testing.T) {
	resp := map[string]interface{}{
		"models": []interface{}{
			map[string]interface{}{"slug": "gpt-5.4", "priority": float64(1), "visibility": "visible"},
			map[string]interface{}{"slug": "gpt-5.3-codex", "priority": float64(2), "visibility": "hide"},
			map[string]interface{}{"slug": "gpt-5.4-mini", "priority": float64(3)},
		},
	}
	models := parseCodexModels(resp)
	if len(models) != 2 {
		t.Fatalf("got %d, want 2", len(models))
	}
	if models[0] != "gpt-5.4" || models[1] != "gpt-5.4-mini" {
		t.Errorf("models = %v", models)
	}
}

func TestParseCodexModels_EmptyResponse(t *testing.T) {
	if m := parseCodexModels(nil); len(m) != 0 {
		t.Errorf("nil: got %d", len(m))
	}
	if m := parseCodexModels(map[string]interface{}{}); len(m) != 0 {
		t.Errorf("empty: got %d", len(m))
	}
}

func TestCodexDefaultModels_NotStale(t *testing.T) {
	stale := []string{"o3", "o3-pro", "o4-mini", "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano"}
	for _, m := range stale {
		for _, dm := range codexDefaultModels {
			if dm == m {
				t.Errorf("stale model %q found", m)
			}
		}
	}
	if len(codexDefaultModels) == 0 {
		t.Error("codexDefaultModels is empty")
	}
}

// ── Live Model Fetch ────────────────────────────────────────

func TestFetchCodexModels_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/models" {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Authorization") == "" || r.Header.Get("ChatGPT-Account-ID") == "" {
			w.WriteHeader(400)
			return
		}
		resp := map[string]interface{}{
			"models": []interface{}{
				map[string]interface{}{"slug": "gpt-5.4", "priority": float64(1)},
				map[string]interface{}{"slug": "gpt-5.4-mini", "priority": float64(2)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	models := fetchCodexModelsLive("fake-token", "acct_123", srv.URL)
	if len(models) != 2 || models[0] != "gpt-5.4" {
		t.Errorf("models = %v", models)
	}
}

func TestFetchCodexModels_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	if m := fetchCodexModelsLive("tok", "acct", srv.URL); len(m) != 0 {
		t.Errorf("got %d on error", len(m))
	}
}

func TestFetchCodexModels_EmptyJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if m := fetchCodexModelsLive("tok", "acct", srv.URL); len(m) != 0 {
		t.Errorf("got %d on empty", len(m))
	}
}

func TestFetchCodexModels_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{broken`))
	}))
	defer srv.Close()
	if m := fetchCodexModelsLive("tok", "acct", srv.URL); len(m) != 0 {
		t.Errorf("got %d on malformed", len(m))
	}
}

// ── Cloudflare Headers ──────────────────────────────────────

func TestCodexCloudflareHeaders(t *testing.T) {
	claims := map[string]interface{}{
		"https://api.openai.com/auth": map[string]interface{}{"chatgpt_account_id": "acct_abc"},
	}
	payloadBytes, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	token := "header." + payloadB64 + ".sig"
	h := codexCloudflareHeaders(token, "")
	if h["originator"] != "codex_cli_rs" {
		t.Errorf("originator = %v", h["originator"])
	}
	if !strings.HasPrefix(h["User-Agent"], "codex_cli_rs/") {
		t.Errorf("User-Agent = %v", h["User-Agent"])
	}
	if h["ChatGPT-Account-ID"] != "acct_abc" {
		t.Errorf("ChatGPT-Account-ID = %v", h["ChatGPT-Account-ID"])
	}
}

func TestCodexCloudflareHeaders_NoToken(t *testing.T) {
	h := codexCloudflareHeaders("", "")
	if h["originator"] != "codex_cli_rs" {
		t.Errorf("originator = %v", h["originator"])
	}
	if _, ok := h["ChatGPT-Account-ID"]; ok {
		t.Error("should not set ChatGPT-Account-ID for empty token")
	}
}

// ── Integration: handleCodexProxyWithUpstream ──────────────

func TestHandleCodexProxy_TextNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody["instructions"] != "You are helpful." {
			t.Errorf("instructions = %v", reqBody["instructions"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{}}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello!\"}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	chatBody := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are helpful."},
			map[string]interface{}{"role": "user", "content": "Hi"},
		},
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", srv.URL+"/responses")

	if w.Code != 200 {
		t.Fatalf("status = %d. Body: %s", w.Code, w.Body.String())
	}
	var chatResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &chatResp)
	if chatResp["model"] != "gpt-5.4" {
		t.Errorf("model = %v", chatResp["model"])
	}
	choices := chatResp["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["content"] != "Hello!" {
		t.Errorf("content = %v", msg["content"])
	}
}

func TestHandleCodexProxy_ReasoningNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"**Checking facts**\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Done\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	chatBody := map[string]interface{}{
		"model": "gpt-5.6-sol",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Think first"},
		},
		"reasoning_effort": "medium",
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", srv.URL+"/responses")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d. Body: %s", w.Code, w.Body.String())
	}
	var chatResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &chatResp); err != nil {
		t.Fatal(err)
	}
	choices := chatResp["choices"].([]interface{})
	message := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	if message["reasoning_content"] != "**Checking facts**" {
		t.Errorf("reasoning_content = %v", message["reasoning_content"])
	}
	if message["content"] != "Done" {
		t.Errorf("content = %v", message["content"])
	}
}

func TestHandleCodexProxy_TextStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		events := []string{
			`{"type":"response.output_text.delta","delta":"Hi"}`,
			`{"type":"response.output_text.delta","delta":" there"}`,
			`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`,
			`[DONE]`,
		}
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}))
	defer srv.Close()

	chatBody := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hi"},
		},
		"stream": true,
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", srv.URL+"/responses")

	if w.Code != 200 {
		t.Fatalf("status = %d. Body: %s", w.Code, w.Body.String())
	}
	var chatChunks []map[string]interface{}
	for _, line := range strings.Split(w.Body.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			chatChunks = append(chatChunks, chunk)
		}
	}
	if len(chatChunks) < 3 {
		t.Fatalf("chunks = %d, want >= 3", len(chatChunks))
	}
	// First chunk should have role
	firstChoices := chatChunks[0]["choices"].([]interface{})
	firstDelta := firstChoices[0].(map[string]interface{})["delta"].(map[string]interface{})
	if firstDelta["role"] != "assistant" {
		t.Errorf("first role = %v", firstDelta["role"])
	}
	// Last chunk should have finish_reason
	lastChoices := chatChunks[len(chatChunks)-1]["choices"].([]interface{})
	lastChoice := lastChoices[0].(map[string]interface{})
	if lastChoice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", lastChoice["finish_reason"])
	}
}

func TestHandleCodexProxy_ReasoningStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		events := []string{
			`{"type":"response.reasoning_summary_text.delta","delta":"**Checking facts**"}`,
			`{"type":"response.output_text.delta","delta":"Done"}`,
			`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`,
			`[DONE]`,
		}
		for _, event := range events {
			fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	defer srv.Close()

	chatBody := map[string]interface{}{
		"model": "gpt-5.6-sol",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Think first"},
		},
		"reasoning_effort": "medium",
		"stream":           true,
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", srv.URL+"/responses")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d. Body: %s", w.Code, w.Body.String())
	}
	var reasoning, content strings.Builder
	for _, chunk := range parseSSEChunks(w.Body.String()) {
		choices, _ := chunk["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		delta, _ := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
		if text, ok := delta["reasoning_content"].(string); ok {
			reasoning.WriteString(text)
		}
		if text, ok := delta["content"].(string); ok {
			content.WriteString(text)
		}
	}
	if reasoning.String() != "**Checking facts**" {
		t.Errorf("reasoning_content = %q", reasoning.String())
	}
	if content.String() != "Done" {
		t.Errorf("content = %q", content.String())
	}
}

func TestHandleCodexProxy_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	chatBody := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hi"},
		},
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", srv.URL+"/responses")
	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestHandleCodexProxy_EmptyMessages(t *testing.T) {
	chatBody := map[string]interface{}{
		"model": "gpt-5.4", "messages": []interface{}{},
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", "")
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── Streaming Tool-Call Corruption Regression ─────────────

// parseSSEChunks extracts all chat.completion.chunk objects from a raw SSE body,
// exactly as a real Hermes client would parse them.
func parseSSEChunks(body string) []map[string]interface{} {
	var chunks []map[string]interface{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

// extractToolCallArgs concatenates argument deltas from parsed chunks exactly like
// Hermes does: every tool_calls[].function.arguments string is joined in order.
// Also returns the function name, call ID, index, and final finish_reason.
func extractToolCallArgs(chunks []map[string]interface{}) (args string, name string, callID string, idx int, finishReason string) {
	var argParts []string
	for _, chunk := range chunks {
		choices, _ := chunk["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]interface{})
		fr, _ := choice["finish_reason"].(string)
		if fr != "" {
			finishReason = fr
		}
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		tcs, _ := delta["tool_calls"].([]interface{})
		if len(tcs) == 0 {
			continue
		}
		tc := tcs[0].(map[string]interface{})
		fn, _ := tc["function"].(map[string]interface{})
		if n, ok := fn["name"].(string); ok && n != "" {
			name = n
		}
		if id, ok := tc["id"].(string); ok && id != "" {
			callID = id
		}
		if i, ok := tc["index"].(float64); ok {
			idx = int(i)
		}
		if a, ok := fn["arguments"].(string); ok {
			argParts = append(argParts, a)
		}
	}
	args = strings.Join(argParts, "")
	return
}

// TestHandleCodexStreaming_ToolCallNoDuplication exercises the real Codex event
// shape through the HTTP streaming handler and asserts argument chunks are NOT
// duplicated — the root cause bug that caused Hermes to retry 4× with
// finish_reason=length.
func TestHandleCodexStreaming_ToolCallNoDuplication(t *testing.T) {
	// Real Codex event sequence: output_item.added → argument deltas →
	// function_call_arguments.done → output_item.done → response.completed.
	events := []string{
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_abc123","call_id":"call_xyz789","name":"shell"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_abc123","delta":"{\"cm"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_abc123","delta":"d\":\"l"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_abc123","delta":"s\"}"}`,
		// .done provides full arguments — must NOT re-emit after deltas
		`{"type":"response.function_call_arguments.done","item_id":"fc_abc123","arguments":"{\"cmd\":\"ls\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_abc123"}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":50}}}`,
		`[DONE]`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}))
	defer srv.Close()

	chatBody := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "run ls"},
		},
		"stream": true,
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", srv.URL+"/responses")

	if w.Code != 200 {
		t.Fatalf("status = %d. Body:\n%s", w.Code, w.Body.String())
	}

	chunks := parseSSEChunks(w.Body.String())
	joinedArgs, funcName, tcID, tcIdx, finReason := extractToolCallArgs(chunks)

	// Core assertion: joined arguments must be exactly one valid JSON object,
	// not duplicated like {"cmd":"ls"}{"cmd":"ls"}.
	if joinedArgs != `{"cmd":"ls"}` {
		t.Fatalf("tool arguments duplicated or wrong: got %q, want exactly `{\"cmd\":\"ls\"}`", joinedArgs)
	}

	// Verify it parses as valid JSON.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(joinedArgs), &parsed); err != nil {
		t.Fatalf("joined args not valid JSON: %v", err)
	}
	if parsed["cmd"] != "ls" {
		t.Errorf("parsed cmd = %v, want 'ls'", parsed["cmd"])
	}

	// Function name must come from output_item.added (first delta carries it).
	if funcName != "shell" {
		t.Errorf("function name = %q, want 'shell'", funcName)
	}

	// Tool call ID must be the call_* from output_item.added.
	if tcID != "call_xyz789" {
		t.Errorf("tool call ID = %q, want 'call_xyz789'", tcID)
	}

	// Finish reason must be tool_calls.
	if finReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want 'tool_calls'", finReason)
	}

	// First tool call must have index 0.
	if tcIdx != 0 {
		t.Errorf("tool_calls index = %d, want 0", tcIdx)
	}

	// Verify every emitted tool_calls chunk carries an index field.
	for _, chunk := range chunks {
		choices, _ := chunk["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]interface{})
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		tcs, _ := delta["tool_calls"].([]interface{})
		for _, tc := range tcs {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}
			if _, has := tcMap["index"]; !has {
				t.Errorf("tool_calls chunk missing index field: %v", tcMap)
			}
		}
	}
}

// TestHandleCodexStreaming_ToolCallFallbackNoDeltas covers the edge case where
// Codex emits function_call_arguments.done without any preceding deltas.
// In this case, .done MUST emit the full arguments (fallback path).
func TestHandleCodexStreaming_ToolCallFallbackNoDeltas(t *testing.T) {
	events := []string{
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_fb1","call_id":"call_fb1","name":"get_weather"}}`,
		// No delta events — .done is the only source of arguments
		`{"type":"response.function_call_arguments.done","item_id":"fc_fb1","arguments":"{\"city\":\"NYC\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_fb1"}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":20,"output_tokens":10}}}`,
		`[DONE]`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}))
	defer srv.Close()

	chatBody := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "weather"},
		},
		"stream": true,
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", srv.URL+"/responses")

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	chunks := parseSSEChunks(w.Body.String())
	joinedArgs, funcName, tcID, tcIdx, finReason := extractToolCallArgs(chunks)

	// Full arguments must appear (fallback path).
	if joinedArgs != `{"city":"NYC"}` {
		t.Fatalf("fallback args = %q, want `{\"city\":\"NYC\"}`", joinedArgs)
	}
	if funcName != "get_weather" {
		t.Errorf("function name = %q, want 'get_weather'", funcName)
	}
	if tcID != "call_fb1" {
		t.Errorf("tool call ID = %q, want 'call_fb1'", tcID)
	}
	if tcIdx != 0 {
		t.Errorf("index = %d, want 0", tcIdx)
	}
	if finReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want 'tool_calls'", finReason)
	}
}

// TestHandleCodexStreaming_MultipleToolCallsNoCrossBleed verifies that two
// sequential tool calls get distinct indices and their arguments don't bleed
// into each other.
func TestHandleCodexStreaming_MultipleToolCallsNoCrossBleed(t *testing.T) {
	events := []string{
		// Tool call 1: shell("ls")
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_001","call_id":"call_001","name":"shell"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_001","delta":"{\"cm"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_001","delta":"d\":\"l"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_001","delta":"s\"}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_001","arguments":"{\"cmd\":\"ls\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_001"}}`,
		// Tool call 2: shell("pwd")
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_002","call_id":"call_002","name":"shell"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_002","delta":"{\"cm"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_002","delta":"d\":\"p"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_002","delta":"wd\"}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_002","arguments":"{\"cmd\":\"pwd\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_002"}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":50,"output_tokens":30}}}`,
		`[DONE]`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}))
	defer srv.Close()

	chatBody := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "run ls then pwd"},
		},
		"stream": true,
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", srv.URL+"/responses")

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	chunks := parseSSEChunks(w.Body.String())

	// Collect per-tool-call data by index.
	type tcInfo struct {
		args      string
		name      string
		callID    string
		finReason string
	}
	byIndex := map[int]*tcInfo{}
	var overallFinishReason string

	for _, chunk := range chunks {
		choices, _ := chunk["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]interface{})
		fr, _ := choice["finish_reason"].(string)
		if fr != "" {
			overallFinishReason = fr
		}
		delta, _ := choice["delta"].(map[string]interface{})
		tcs, _ := delta["tool_calls"].([]interface{})
		for _, tc := range tcs {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}
			idx, _ := tcMap["index"].(float64)
			ii := int(idx)
			if byIndex[ii] == nil {
				byIndex[ii] = &tcInfo{}
			}
			info := byIndex[ii]
			fn, _ := tcMap["function"].(map[string]interface{})
			if n, ok := fn["name"].(string); ok && n != "" {
				info.name = n
			}
			if id, ok := tcMap["id"].(string); ok && id != "" {
				info.callID = id
			}
			if a, ok := fn["arguments"].(string); ok {
				info.args += a
			}
		}
	}
	// Propagate the overall finish_reason to all tool call indices.
	for _, info := range byIndex {
		if info.finReason == "" {
			info.finReason = overallFinishReason
		}
	}

	// Assert two distinct tool calls.
	if len(byIndex) < 2 {
		t.Fatalf("expected 2 tool call indices, got %d: %v", len(byIndex), byIndex)
	}

	// Tool call 0: shell / call_001 / {"cmd":"ls"}
	tc0 := byIndex[0]
	if tc0 == nil {
		t.Fatal("missing tool call index 0")
	}
	if tc0.args != `{"cmd":"ls"}` {
		t.Errorf("tc0 args = %q, want `{\"cmd\":\"ls\"}`", tc0.args)
	}
	if tc0.name != "shell" {
		t.Errorf("tc0 name = %q, want 'shell'", tc0.name)
	}
	if tc0.callID != "call_001" {
		t.Errorf("tc0 callID = %q, want 'call_001'", tc0.callID)
	}
	if tc0.finReason != "tool_calls" {
		t.Errorf("tc0 finish_reason = %q, want 'tool_calls'", tc0.finReason)
	}

	// Tool call 1: shell / call_002 / {"cmd":"pwd"}
	tc1 := byIndex[1]
	if tc1 == nil {
		t.Fatal("missing tool call index 1")
	}
	if tc1.args != `{"cmd":"pwd"}` {
		t.Errorf("tc1 args = %q, want `{\"cmd\":\"pwd\"}`", tc1.args)
	}
	if tc1.name != "shell" {
		t.Errorf("tc1 name = %q, want 'shell'", tc1.name)
	}
	if tc1.callID != "call_002" {
		t.Errorf("tc1 callID = %q, want 'call_002'", tc1.callID)
	}
	if tc1.finReason != "tool_calls" {
		t.Errorf("tc1 finish_reason = %q, want 'tool_calls'", tc1.finReason)
	}
}

// TestHandleCodexStreaming_IncompletePreserved verifies that
// response.incomplete and response.completed incomplete_details are still
// translated to finish_reason=length.
func TestHandleCodexStreaming_IncompletePreserved(t *testing.T) {
	events := []string{
		`{"type":"response.output_text.delta","delta":"partial"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5},"incomplete_details":{"reason":"max_output_tokens"}}}`,
		`[DONE]`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}))
	defer srv.Close()

	chatBody := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "long output"},
		},
		"stream": true,
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", srv.URL+"/responses")

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	chunks := parseSSEChunks(w.Body.String())
	var finishReason string
	for _, chunk := range chunks {
		choices, _ := chunk["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		fr, _ := choices[0].(map[string]interface{})["finish_reason"].(string)
		if fr != "" {
			finishReason = fr
		}
	}
	if finishReason != "length" {
		t.Errorf("finish_reason = %q, want 'length'", finishReason)
	}
}

// TestHandleCodexStreaming_NonFunctionOutputItemDoneBeforeToolCall verifies that
// a reasoning/message output_item.done event does NOT shift the tool-call index.
// In real Codex streams, a reasoning output_item.done can appear between tool
// calls; only function_call output_item.done should advance the counter.
func TestHandleCodexStreaming_NonFunctionOutputItemDoneBeforeToolCall(t *testing.T) {
	events := []string{
		// Reasoning item: added → done (no function_call type)
		`{"type":"response.output_item.added","item":{"type":"reasoning","id":"rs_001"}}`,
		`{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_001"}}`,
		// Message item: added → done (not a function_call)
		`{"type":"response.output_item.added","item":{"type":"message","id":"msg_001"}}`,
		`{"type":"response.output_item.done","item":{"type":"message","id":"msg_001"}}`,
		// Now a function_call — index must be 0, not 2
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_001","call_id":"call_001","name":"shell"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_001","delta":"{\"cm"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_001","delta":"d\":\"l"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_001","delta":"s\"}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_001","arguments":"{\"cmd\":\"ls\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_001"}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":50,"output_tokens":30}}}`,
		`[DONE]`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}))
	defer srv.Close()

	chatBody := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "think then run ls"},
		},
		"stream": true,
	}
	bodyBytes, _ := json.Marshal(chatBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleCodexProxyWithUpstream(w, req, "builtin-openai-codex", "fake-token", srv.URL+"/responses")

	if w.Code != 200 {
		t.Fatalf("status = %d. Body:\n%s", w.Code, w.Body.String())
	}

	chunks := parseSSEChunks(w.Body.String())
	joinedArgs, funcName, tcID, tcIdx, finReason := extractToolCallArgs(chunks)

	// The function_call index MUST be 0, not shifted by non-function items.
	if tcIdx != 0 {
		t.Errorf("tool_calls index = %d, want 0 (reasoning/message item must not shift index)", tcIdx)
	}

	if joinedArgs != `{"cmd":"ls"}` {
		t.Fatalf("arguments = %q, want `{\"cmd\":\"ls\"}`", joinedArgs)
	}
	if funcName != "shell" {
		t.Errorf("function name = %q, want 'shell'", funcName)
	}
	if tcID != "call_001" {
		t.Errorf("tool call ID = %q, want 'call_001'", tcID)
	}
	if finReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want 'tool_calls'", finReason)
	}
}
