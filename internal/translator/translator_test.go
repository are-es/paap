package translator

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicToOpenAIRequest_BasicText(t *testing.T) {
	body := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens":  float64(1024),
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
		"max_tokens":  float64(1024),
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
		"max_tokens":  float64(1024),
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
