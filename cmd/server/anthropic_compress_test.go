package main

import (
	"strings"
	"testing"

	"github.com/dolvin/paap/cmd/server/compression"
)

// bigToolText builds a tool result body well past anthropicMinCompressSize that
// the Lite pass can actually shrink (repeated blank lines + trailing spaces).
func bigToolText(tag string) string {
	var b strings.Builder
	for b.Len() < anthropicMinCompressSize*2 {
		b.WriteString(tag + " line with trailing spaces   \n\n\n")
	}
	return b.String()
}

// setCompressLevel drives resolveCompressionLevel via the setting it reads first.
func setCompressLevel(t *testing.T, level string) {
	t.Helper()
	setSetting("compress_level", level)
	t.Cleanup(func() { setSetting("compress_level", "off") })
}

func TestResolveCompressionLevelUsesOnlyCompressLevel(t *testing.T) {
	setSetting("compress_level", "")
	setSetting("compression_mode", "caveman:ultra")
	t.Cleanup(func() {
		setSetting("compression_mode", "")
	})
	if got := resolveCompressionLevel(); got != compression.LevelOff {
		t.Fatalf("resolveCompressionLevel() = %s, want off", got.String())
	}
}

func toolResultMsg(text string, isError bool) map[string]interface{} {
	block := map[string]interface{}{
		"type":        "tool_result",
		"tool_use_id": "call_1",
		"content":     text,
	}
	if isError {
		block["is_error"] = true
	}
	return map[string]interface{}{
		"role":    "user",
		"content": []interface{}{block},
	}
}

func blockOf(t *testing.T, msg interface{}) map[string]interface{} {
	t.Helper()
	m, ok := msg.(map[string]interface{})
	if !ok {
		t.Fatalf("message is not a map: %T", msg)
	}
	arr, ok := m["content"].([]interface{})
	if !ok {
		t.Fatalf("content is not an array: %T", m["content"])
	}
	b, ok := arr[0].(map[string]interface{})
	if !ok {
		t.Fatalf("block is not a map: %T", arr[0])
	}
	return b
}

func contentString(t *testing.T, block map[string]interface{}) string {
	t.Helper()
	switch v := block["content"].(type) {
	case string:
		return v
	case []interface{}:
		var b strings.Builder
		for _, item := range v {
			if im, ok := item.(map[string]interface{}); ok {
				if s, ok := im["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String()
	default:
		t.Fatalf("unexpected content type: %T", block["content"])
		return ""
	}
}

// TestCompressAnthropicToolResults is the guard for the Anthropic compression
// path: only non-error tool results shrink, everything else survives verbatim.
func TestCompressAnthropicToolResults(t *testing.T) {
	setCompressLevel(t, "lite")

	okText := bigToolText("payload")
	errText := bigToolText("boom")
	assistantText := bigToolText("thinking")

	// CompressRawMessages keeps its 6 newest entries untouched, so pad with extra
	// tool results and assert on the oldest one.
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "you are a helpful assistant"},
		toolResultMsg(okText, false), // oldest -> must compress
		toolResultMsg(errText, true), // is_error -> verbatim
		map[string]interface{}{"role": "assistant", "content": assistantText},
	}
	for i := 0; i < 7; i++ {
		messages = append(messages, toolResultMsg(bigToolText("pad"), false))
	}

	out := compressAnthropicToolResults(messages, "claude-sonnet-5")

	if len(out) != len(messages) {
		t.Fatalf("message count changed: got %d want %d", len(out), len(messages))
	}

	got := contentString(t, blockOf(t, out[1]))
	if len(got) >= len(okText) {
		t.Errorf("non-error tool result was not compressed: %d -> %d bytes", len(okText), len(got))
	}
	if !strings.Contains(got, "payload") {
		t.Error("compression dropped the payload text entirely")
	}

	if gotErr := contentString(t, blockOf(t, out[2])); gotErr != errText {
		t.Errorf("error tool result was modified: %d -> %d bytes", len(errText), len(gotErr))
	}

	if assistant := out[3].(map[string]interface{}); assistant["content"] != assistantText {
		t.Error("assistant message was modified; only tool results should change")
	}
	if sys := out[0].(map[string]interface{}); sys["content"] != "you are a helpful assistant" {
		t.Error("system message was modified")
	}
}

// TestCompressAnthropicToolResults_Off proves the kill switch is real.
func TestCompressAnthropicToolResults_Off(t *testing.T) {
	setCompressLevel(t, "off")

	text := bigToolText("payload")
	var messages []interface{}
	for i := 0; i < 8; i++ {
		messages = append(messages, toolResultMsg(text, false))
	}

	out := compressAnthropicToolResults(messages, "claude-sonnet-5")
	for i, msg := range out {
		if got := contentString(t, blockOf(t, msg)); got != text {
			t.Fatalf("msg[%d] changed while compression disabled", i)
		}
	}
}

// TestCompressAnthropicToolResults_ArrayShape locks the content shape:
// an array-shaped tool result must stay an array, not collapse to a string.
func TestCompressAnthropicToolResults_ArrayShape(t *testing.T) {
	setCompressLevel(t, "lite")

	text := bigToolText("payload")
	arrayBlock := func() map[string]interface{} {
		return map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "call_arr",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": text},
					},
				},
			},
		}
	}

	messages := []interface{}{arrayBlock()}
	for i := 0; i < 7; i++ {
		messages = append(messages, arrayBlock())
	}

	out := compressAnthropicToolResults(messages, "claude-sonnet-5")

	block := blockOf(t, out[0])
	if _, ok := block["content"].([]interface{}); !ok {
		t.Fatalf("array content collapsed to %T", block["content"])
	}
	if got := contentString(t, block); len(got) >= len(text) {
		t.Errorf("array-shaped tool result was not compressed: %d -> %d bytes", len(text), len(got))
	}
}
