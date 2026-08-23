package translator

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicToOpenAIRequest_BasicText(t *testing.T) {
	body := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": float64(1024),
		"system":     "You are a helpful assistant.",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "Hello!",
			},
		},
	}

	result, err := AnthropicToOpenAIRequest(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model = %v, want claude-sonnet-4-20250514", result["model"])
	}

	msgs, ok := result["messages"].([]interface{})
	if !ok {
		t.Fatal("messages not a slice")
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}

	// First message should be system
	sysMsg := msgs[0].(map[string]interface{})
	if sysMsg["role"] != "system" {
		t.Errorf("first message role = %v, want system", sysMsg["role"])
	}
	if sysMsg["content"] != "You are a helpful assistant." {
		t.Errorf("system content = %v", sysMsg["content"])
	}

	// Second message should be user
	userMsg := msgs[1].(map[string]interface{})
	if userMsg["role"] != "user" {
		t.Errorf("second message role = %v, want user", userMsg["role"])
	}
	if userMsg["content"] != "Hello!" {
		t.Errorf("user content = %v", userMsg["content"])
	}
}

func TestAnthropicToOpenAIRequest_ToolUse(t *testing.T) {
	body := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": float64(1024),
		"messages": []interface{}{
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "I'll search for that.",
					},
					map[string]interface{}{
						"type":  "tool_use",
						"id":    "toolu_123",
						"name":  "web_search",
						"input": map[string]interface{}{"query": "test"},
					},
				},
			},
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "toolu_123",
						"content":     "Search results here",
					},
				},
			},
		},
	}

	result, err := AnthropicToOpenAIRequest(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := result["messages"].([]interface{})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// Assistant message should have tool_calls
	assistantMsg := msgs[0].(map[string]interface{})
	if assistantMsg["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", assistantMsg["role"])
	}
	toolCalls, ok := assistantMsg["tool_calls"].([]interface{})
	if !ok || len(toolCalls) == 0 {
		t.Fatal("assistant message missing tool_calls")
	}
	tc := toolCalls[0].(map[string]interface{})
	if tc["id"] != "toolu_123" {
		t.Errorf("tool_call id = %v, want toolu_123", tc["id"])
	}

	// User message should become tool message
	toolMsg := msgs[1].(map[string]interface{})
	if toolMsg["role"] != "tool" {
		t.Errorf("role = %v, want tool", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "toolu_123" {
		t.Errorf("tool_call_id = %v, want toolu_123", toolMsg["tool_call_id"])
	}
	if toolMsg["content"] != "Search results here" {
		t.Errorf("content = %v", toolMsg["content"])
	}
}

func TestAnthropicToOpenAIRequest_Tools(t *testing.T) {
	body := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": float64(1024),
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "Hello",
			},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"name":        "get_weather",
				"description": "Get weather for a location",
				"input_schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type": "string",
						},
					},
					"required": []interface{}{"location"},
				},
			},
		},
	}

	result, err := AnthropicToOpenAIRequest(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools, ok := result["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatal("missing tools")
	}

	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" {
		t.Errorf("tool type = %v, want function", tool["type"])
	}
	fn := tool["function"].(map[string]interface{})
	if fn["name"] != "get_weather" {
		t.Errorf("function name = %v, want get_weather", fn["name"])
	}
}

func TestOpenAIToAnthropicResponse_BasicText(t *testing.T) {
	openaiResp := map[string]interface{}{
		"id":    "chatcmpl-123",
		"model": "gpt-4",
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "Hello world!",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     float64(10),
			"completion_tokens": float64(5),
		},
	}

	result := OpenAIToAnthropicResponse(openaiResp)

	if result["type"] != "message" {
		t.Errorf("type = %v, want message", result["type"])
	}
	if result["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", result["role"])
	}

	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("missing content")
	}
	block := content[0].(map[string]interface{})
	if block["type"] != "text" {
		t.Errorf("content block type = %v, want text", block["type"])
	}
	if block["text"] != "Hello world!" {
		t.Errorf("text = %v, want Hello world!", block["text"])
	}

	if result["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", result["stop_reason"])
	}
}

func TestOpenAIToAnthropicResponse_ToolCalls(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{"location": "NYC"})
	openaiResp := map[string]interface{}{
		"id":    "chatcmpl-456",
		"model": "gpt-4",
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []interface{}{
						map[string]interface{}{
							"id":   "call_abc",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "get_weather",
								"arguments": string(args),
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	}

	result := OpenAIToAnthropicResponse(openaiResp)

	if result["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", result["stop_reason"])
	}

	content := result["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}
	block := content[0].(map[string]interface{})
	if block["type"] != "tool_use" {
		t.Errorf("block type = %v, want tool_use", block["type"])
	}
	if block["name"] != "get_weather" {
		t.Errorf("name = %v, want get_weather", block["name"])
	}
}

func TestMapFinishReason(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"stop", "end_turn"},
		{"length", "max_tokens"},
		{"tool_calls", "tool_use"},
		{"content_filter", "end_turn"},
		{"unknown", "end_turn"},
	}

	for _, tt := range tests {
		got := mapFinishReason(tt.input)
		if got != tt.want {
			t.Errorf("mapFinishReason(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectFormat(t *testing.T) {
	// Anthropic format (has system field)
	anthBody := map[string]interface{}{
		"system":   "You are helpful",
		"messages": []interface{}{},
	}
	if DetectFormat(anthBody) != FormatAnthropic {
		t.Error("expected Anthropic format")
	}

	// OpenAI format
	openaiBody := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "system",
				"content": "You are helpful",
			},
		},
	}
	if DetectFormat(openaiBody) != FormatOpenAI {
		t.Error("expected OpenAI format")
	}
}

func TestConvertAnthropicToolChoiceToOpenAI(t *testing.T) {
	tests := []struct {
		input map[string]interface{}
		want  string
	}{
		{
			map[string]interface{}{"type": "auto"},
			"auto",
		},
		{
			map[string]interface{}{"type": "any"},
			"required",
		},
	}

	for _, tt := range tests {
		result := convertAnthropicToolChoiceToOpenAI(tt.input)
		if result != tt.want {
			t.Errorf("convertAnthropicToolChoiceToOpenAI(%v) = %v, want %v", tt.input, result, tt.want)
		}
	}

	// tool type with name
	toolChoice := map[string]interface{}{
		"type": "tool",
		"name": "get_weather",
	}
	result := convertAnthropicToolChoiceToOpenAI(toolChoice)
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	fn := resultMap["function"].(map[string]interface{})
	if fn["name"] != "get_weather" {
		t.Errorf("function name = %v, want get_weather", fn["name"])
	}
}

func TestConvertAnthropicUserMessage_Image(t *testing.T) {
	msg := map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{
				"type": "image",
				"source": map[string]interface{}{
					"type":       "base64",
					"media_type": "image/png",
					"data":       "iVBORw0KGgo=",
				},
			},
			map[string]interface{}{
				"type": "text",
				"text": "What's in this image?",
			},
		},
	}

	result, err := convertAnthropicMessageToOpenAI(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	userMsg := result[0].(map[string]interface{})
	if userMsg["role"] != "user" {
		t.Errorf("role = %v, want user", userMsg["role"])
	}

	content, ok := userMsg["content"].([]interface{})
	if !ok {
		t.Fatal("content not a slice")
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(content))
	}

	// First should be image
	imgPart := content[0].(map[string]interface{})
	if imgPart["type"] != "image_url" {
		t.Errorf("first part type = %v, want image_url", imgPart["type"])
	}
	url := imgPart["image_url"].(map[string]interface{})["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("image url should start with data:image/png;base64, got: %s", url[:30])
	}
}

// ── CleanToolIDForAnthropic Tests ──────────────────────────────────────────

func TestCleanToolIDForAnthropic(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // empty means just check it's valid
	}{
		{
			name:  "clean ID unchanged",
			input: "call_abc123",
			want:  "call_abc123",
		},
		{
			name:  "strips thought signature",
			input: "call_1234_0___ts___c2lnbmF0dXJl",
			want:  "call_1234_0",
		},
		{
			name:  "strips long thought signature",
			input: "call_1724000000000_0___ts___dGhpcyBpcyBhIHZlcnkgbG9uZyBzaWduYXR1cmUgdGhhdCBnb2VzIGJleW9uZCA2NCBjaGFycw==",
			want:  "call_1724000000000_0",
		},
		{
			name:  "removes invalid chars",
			input: "call@abc#123",
			want:  "callabc123",
		},
		{
			name:  "preserves hyphens and underscores",
			input: "tool_use-id_123",
			want:  "tool_use-id_123",
		},
		{
			name:  "clamps to 64 chars",
			input: "a" + strings.Repeat("b", 100),
			want:  "a" + strings.Repeat("b", 63), // first 64 chars of original
		},
		{
			name:  "empty input gets generated ID",
			input: "",
			want:  "", // will check prefix
		},
		{
			name:  "only invalid chars gets generated ID",
			input: "@#$%^&*()",
			want:  "", // will check prefix
		},
		{
			name:  "thought sig with nothing before",
			input: "___ts___abc123",
			want:  "", // empty before ___ts___, gets generated
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanToolIDForAnthropic(tt.input)
			if tt.want == "" {
				// Just check it matches Anthropic regex and is <= 64 chars
				if len(got) == 0 || len(got) > 64 {
					t.Errorf("length = %d, want 1-64", len(got))
				}
				for _, c := range got {
					if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
						t.Errorf("invalid char %c in %q", c, got)
					}
				}
			} else {
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// ── OpenAIToAnthropicMessages Tests ────────────────────────────────────────

func TestOpenAIToAnthropicMessages_ToolCallsAndResults(t *testing.T) {
	// Simulate OpenAI conversation with tool calls and results
	openaiMessages := []interface{}{
		map[string]interface{}{
			"role":    "user",
			"content": "What's the weather in NYC?",
		},
		map[string]interface{}{
			"role":    "assistant",
			"content": "",
			"tool_calls": []interface{}{
				map[string]interface{}{
					"id":   "call_1234_0___ts___c2lnbmF0dXJl",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "get_weather",
						"arguments": `{"location":"NYC"}`,
					},
				},
			},
		},
		map[string]interface{}{
			"role":         "tool",
			"tool_call_id": "call_1234_0___ts___c2lnbmF0dXJl",
			"content":      `{"temp":72,"condition":"sunny"}`,
		},
	}

	result := OpenAIToAnthropicMessages(openaiMessages)

	// user, assistant (with tool_use), user (with tool_result)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	// Message 0: user
	if result[0]["role"] != "user" {
		t.Errorf("msg[0] role = %v, want user", result[0]["role"])
	}
	if result[0]["content"] != "What's the weather in NYC?" {
		t.Errorf("msg[0] content = %v", result[0]["content"])
	}

	// Message 1: assistant with tool_use blocks
	if result[1]["role"] != "assistant" {
		t.Errorf("msg[1] role = %v, want assistant", result[1]["role"])
	}
	blocks, ok := result[1]["content"].([]interface{})
	if !ok {
		t.Fatalf("msg[1] content is not a slice: %T", result[1]["content"])
	}
	if len(blocks) != 1 {
		t.Fatalf("msg[1] expected 1 block (tool_use), got %d", len(blocks))
	}
	toolBlock := blocks[0].(map[string]interface{})
	if toolBlock["type"] != "tool_use" {
		t.Errorf("block type = %v, want tool_use", toolBlock["type"])
	}
	if toolBlock["name"] != "get_weather" {
		t.Errorf("tool name = %v, want get_weather", toolBlock["name"])
	}
	// ID should be sanitized (___ts___ stripped)
	if toolBlock["id"] != "call_1234_0" {
		t.Errorf("tool id = %v, want call_1234_0", toolBlock["id"])
	}
	input := toolBlock["input"].(map[string]interface{})
	if input["location"] != "NYC" {
		t.Errorf("input.location = %v, want NYC", input["location"])
	}

	// Message 2: tool result → user with tool_result block
	if result[2]["role"] != "user" {
		t.Errorf("msg[2] role = %v, want user", result[2]["role"])
	}
	resultBlocks, ok := result[2]["content"].([]interface{})
	if !ok {
		t.Fatalf("msg[2] content is not a slice: %T", result[2]["content"])
	}
	if len(resultBlocks) != 1 {
		t.Fatalf("msg[2] expected 1 block, got %d", len(resultBlocks))
	}
	trBlock := resultBlocks[0].(map[string]interface{})
	if trBlock["type"] != "tool_result" {
		t.Errorf("block type = %v, want tool_result", trBlock["type"])
	}
	if trBlock["tool_use_id"] != "call_1234_0" {
		t.Errorf("tool_use_id = %v, want call_1234_0", trBlock["tool_use_id"])
	}
	if trBlock["content"] != `{"temp":72,"condition":"sunny"}` {
		t.Errorf("content = %v", trBlock["content"])
	}
}

func TestOpenAIToAnthropicMessages_ConsecutiveToolResultsMerged(t *testing.T) {
	// Multiple consecutive tool results should merge into one user message
	openaiMessages := []interface{}{
		map[string]interface{}{
			"role":    "assistant",
			"content": nil,
			"tool_calls": []interface{}{
				map[string]interface{}{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "search",
						"arguments": `{"q":"weather"}`,
					},
				},
				map[string]interface{}{
					"id":   "call_2",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "search",
						"arguments": `{"q":"news"}`,
					},
				},
			},
		},
		map[string]interface{}{
			"role":         "tool",
			"tool_call_id": "call_1",
			"content":      "weather result",
		},
		map[string]interface{}{
			"role":         "tool",
			"tool_call_id": "call_2",
			"content":      "news result",
		},
	}

	result := OpenAIToAnthropicMessages(openaiMessages)

	// Should be: assistant (with 2 tool_use blocks), user (with 2 tool_result blocks)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (assistant + merged user), got %d", len(result))
	}

	// Assistant with 2 tool_use blocks
	if result[0]["role"] != "assistant" {
		t.Errorf("msg[0] role = %v, want assistant", result[0]["role"])
	}
	blocks := result[0]["content"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("assistant expected 2 tool_use blocks, got %d", len(blocks))
	}

	// Merged user with 2 tool_result blocks
	if result[1]["role"] != "user" {
		t.Errorf("msg[1] role = %v, want user", result[1]["role"])
	}
	resultBlocks := result[1]["content"].([]interface{})
	if len(resultBlocks) != 2 {
		t.Fatalf("user expected 2 tool_result blocks, got %d", len(resultBlocks))
	}

	tr1 := resultBlocks[0].(map[string]interface{})
	if tr1["tool_use_id"] != "call_1" {
		t.Errorf("first tool_result tool_use_id = %v, want call_1", tr1["tool_use_id"])
	}
	tr2 := resultBlocks[1].(map[string]interface{})
	if tr2["tool_use_id"] != "call_2" {
		t.Errorf("second tool_result tool_use_id = %v, want call_2", tr2["tool_use_id"])
	}
}

func TestOpenAIToAnthropicMessages_AssistantWithTextAndToolCalls(t *testing.T) {
	openaiMessages := []interface{}{
		map[string]interface{}{
			"role":    "assistant",
			"content": "Let me search for that.",
			"tool_calls": []interface{}{
				map[string]interface{}{
					"id":   "call_abc",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "web_search",
						"arguments": `{"query":"test"}`,
					},
				},
			},
		},
	}

	result := OpenAIToAnthropicMessages(openaiMessages)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	blocks := result[0]["content"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (text + tool_use), got %d", len(blocks))
	}

	// First block: text
	textBlock := blocks[0].(map[string]interface{})
	if textBlock["type"] != "text" {
		t.Errorf("block[0] type = %v, want text", textBlock["type"])
	}
	if textBlock["text"] != "Let me search for that." {
		t.Errorf("block[0] text = %v", textBlock["text"])
	}

	// Second block: tool_use
	toolBlock := blocks[1].(map[string]interface{})
	if toolBlock["type"] != "tool_use" {
		t.Errorf("block[1] type = %v, want tool_use", toolBlock["type"])
	}
}

func TestOpenAIToAnthropicMessages_ToolIDSanitization(t *testing.T) {
	// Test that tool IDs with thought signatures are properly sanitized
	openaiMessages := []interface{}{
		map[string]interface{}{
			"role":    "assistant",
			"content": nil,
			"tool_calls": []interface{}{
				map[string]interface{}{
					"id":   "call_1724000000000_0___ts___dGhpcyBpcyBhIHZlcnkgbG9uZyBzaWduYXR1cmUgdGhhdCBnb2VzIGJleW9uZCA2NCBjaGFycw==",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "test_func",
						"arguments": `{}`,
					},
				},
			},
		},
		map[string]interface{}{
			"role":         "tool",
			"tool_call_id": "call_1724000000000_0___ts___dGhpcyBpcyBhIHZlcnkgbG9uZyBzaWduYXR1cmUgdGhhdCBnb2VzIGJleW9uZCA2NCBjaGFycw==",
			"content":      "result",
		},
	}

	result := OpenAIToAnthropicMessages(openaiMessages)

	// Check assistant tool_use ID is sanitized
	blocks := result[0]["content"].([]interface{})
	toolBlock := blocks[0].(map[string]interface{})
	cleanID := toolBlock["id"].(string)
	if len(cleanID) > 64 {
		t.Errorf("tool ID length = %d, want <= 64: %q", len(cleanID), cleanID)
	}
	if strings.Contains(cleanID, "___ts___") {
		t.Errorf("tool ID still contains ___ts___: %q", cleanID)
	}

	// Check tool_result tool_use_id matches
	resultBlocks := result[1]["content"].([]interface{})
	trBlock := resultBlocks[0].(map[string]interface{})
	if trBlock["tool_use_id"] != cleanID {
		t.Errorf("tool_use_id = %v, want %v (must match)", trBlock["tool_use_id"], cleanID)
	}
}

func TestOpenAIToAnthropicMessages_EmptyInput(t *testing.T) {
	result := OpenAIToAnthropicMessages(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}

	result = OpenAIToAnthropicMessages([]interface{}{})
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestOpenAIToAnthropicMessages_SystemMessagesSkipped(t *testing.T) {
	openaiMessages := []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": "You are helpful",
		},
		map[string]interface{}{
			"role":    "user",
			"content": "Hello",
		},
	}

	result := OpenAIToAnthropicMessages(openaiMessages)

	if len(result) != 1 {
		t.Fatalf("expected 1 message (system skipped), got %d", len(result))
	}
	if result[0]["role"] != "user" {
		t.Errorf("role = %v, want user", result[0]["role"])
	}
}

func TestOpenAIToAnthropicMessages_ToolResultWithInvalidJSONArgs(t *testing.T) {
	// Test that invalid JSON arguments in tool_calls don't crash
	openaiMessages := []interface{}{
		map[string]interface{}{
			"role":    "assistant",
			"content": nil,
			"tool_calls": []interface{}{
				map[string]interface{}{
					"id":   "call_bad",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "test",
						"arguments": "not-json",
					},
				},
			},
		},
	}

	result := OpenAIToAnthropicMessages(openaiMessages)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	blocks := result[0]["content"].([]interface{})
	toolBlock := blocks[0].(map[string]interface{})
	input := toolBlock["input"].(map[string]interface{})
	if len(input) != 0 {
		t.Errorf("expected empty input map for invalid JSON, got %v", input)
	}
}
