package main

import (
	"testing"
)

func TestTranslateCodebuddyRequest_InjectsSystemWhenMissing(t *testing.T) {
	rawBody := map[string]interface{}{
		"model":    "glm-5.2",
		"stream":   false,
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}
	out, err := translateToCodebuddyRequest(rawBody)
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := out["messages"].([]interface{})
	if len(messages) < 2 {
		t.Fatalf("expected system prepended, got %d messages", len(messages))
	}
	first, _ := messages[0].(map[string]interface{})
	if first["role"] != "system" {
		t.Fatalf("first role = %v, want system", first["role"])
	}
	if first["content"] != codebuddyDefaultSystem {
		t.Fatalf("system content = %v", first["content"])
	}
}

func TestTranslateCodebuddyRequest_KeepsExistingSystem(t *testing.T) {
	rawBody := map[string]interface{}{
		"model": "glm-5.2",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "custom instructions"},
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	out, err := translateToCodebuddyRequest(rawBody)
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := out["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d (system must not be duplicated)", len(messages))
	}
	first, _ := messages[0].(map[string]interface{})
	if first["content"] != "custom instructions" {
		t.Fatalf("system content = %v, want original", first["content"])
	}
}

func TestTranslateCodebuddyRequest_ForcesStreamAndUsage(t *testing.T) {
	rawBody := map[string]interface{}{
		"model":    "kimi-k3",
		"stream":   false,
		"messages": []interface{}{map[string]interface{}{"role": "system", "content": "s"}, map[string]interface{}{"role": "user", "content": "hi"}},
	}
	out, err := translateToCodebuddyRequest(rawBody)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := out["stream"].(bool); !v {
		t.Fatal("stream must be forced to true upstream")
	}
	so, _ := out["stream_options"].(map[string]interface{})
	if so == nil || so["include_usage"] != true {
		t.Fatal("stream_options.include_usage must be true for token accounting")
	}
}

func TestTranslateCodebuddyRequest_StripsProviderPrefix(t *testing.T) {
	rawBody := map[string]interface{}{
		"model":    "builtin-codebuddy/glm-5.2",
		"messages": []interface{}{map[string]interface{}{"role": "system", "content": "s"}},
	}
	out, err := translateToCodebuddyRequest(rawBody)
	if err != nil {
		t.Fatal(err)
	}
	if out["model"] != "glm-5.2" {
		t.Fatalf("model = %v, want glm-5.2", out["model"])
	}
}

func TestTranslateCodebuddyRequest_NoMessages(t *testing.T) {
	if _, err := translateToCodebuddyRequest(map[string]interface{}{"model": "x"}); err == nil {
		t.Fatal("expected error when no messages")
	}
}
