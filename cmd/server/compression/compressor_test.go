package compression

import (
	"strings"
	"testing"
)

func TestCompressLiteConsecutiveDedup(t *testing.T) {
	cfg := getConfig(LevelLite)

	// Consecutive duplicates should be collapsed
	input := "line 1\nline 1\nline 1\nline 2\nline 3\nline 3"
	got := compressLite(input, cfg)
	want := "line 1\nline 2\nline 3"
	if got != want {
		t.Errorf("compressLite consecutive dedup = %q, want %q", got, want)
	}

	// Non-consecutive duplicates (e.g. repeated code braces or returns) must be preserved
	codeBlock := "func a() {\n\treturn nil\n}\nfunc b() {\n\treturn nil\n}"
	gotCode := compressLite(codeBlock, cfg)
	if !strings.Contains(gotCode, "func a()") || !strings.Contains(gotCode, "func b()") {
		t.Errorf("compressLite corrupted code functions: %q", gotCode)
	}
	if strings.Count(gotCode, "return nil") != 2 {
		t.Errorf("compressLite dropped non-consecutive duplicate line 'return nil', got %q", gotCode)
	}
	if strings.Count(gotCode, "}") != 2 {
		t.Errorf("compressLite dropped non-consecutive duplicate line '}', got %q", gotCode)
	}
}

func TestCompressLiteSizeGuard(t *testing.T) {
	cfg := getConfig(LevelLite)

	// Clean input with no whitespace or duplicates: compressed size == orig size -> returns orig
	input := "clean single line without duplicates"
	got := compressLite(input, cfg)
	if got != input {
		t.Errorf("compressLite should retain original when size >= original, got %q", got)
	}
}

type testChatMessage struct {
	role    string
	content string
}

func (m *testChatMessage) GetRole() string     { return m.role }
func (m *testChatMessage) GetContent() string  { return m.content }
func (m *testChatMessage) SetContent(s string) { m.content = s }

func TestCompressSizeGuardRetainsOriginal(t *testing.T) {
	// 1. Raw Messages with LevelLite
	rawMsgs := []map[string]interface{}{
		{"role": "tool", "content": "alpha beta gamma delta epsilon zeta eta theta iota kappa"}, // >= 50 bytes, no compressable content
		{"role": "tool", "content": "pad 1"},
		{"role": "tool", "content": "pad 2"},
		{"role": "tool", "content": "pad 3"},
		{"role": "tool", "content": "pad 4"},
		{"role": "tool", "content": "pad 5"},
		{"role": "tool", "content": "pad 6"},
	}
	origContent := rawMsgs[0]["content"].(string)
	results := CompressRawMessages(rawMsgs, LevelLite, "")
	if rawMsgs[0]["content"] != origContent {
		t.Errorf("CompressRawMessages modified content when size would not shrink: got %q, want %q", rawMsgs[0]["content"], origContent)
	}
	if results[0].Savings != 0 {
		t.Errorf("CompressRawMessages reported savings %d, want 0", results[0].Savings)
	}
	if results[0].CompressedSize != len(origContent) {
		t.Errorf("CompressRawMessages CompressedSize = %d, want %d", results[0].CompressedSize, len(origContent))
	}

	// 2. CompressInterfaceMessages with uncompressible text
	interfaceMsgs := []ChatMessage{
		&testChatMessage{role: "tool", content: "some long uncompressible content that has nothing to shrink and exceeds min size"},
	}
	origIfaceContent := interfaceMsgs[0].GetContent()
	resultsIface := CompressInterfaceMessages(interfaceMsgs, LevelMedium, "")
	if interfaceMsgs[0].GetContent() != origIfaceContent {
		t.Errorf("CompressInterfaceMessages modified uncompressible content: got %q, want %q", interfaceMsgs[0].GetContent(), origIfaceContent)
	}
	if resultsIface[0].Savings != 0 {
		t.Errorf("CompressInterfaceMessages savings = %d, want 0", resultsIface[0].Savings)
	}
}

func TestCompressShrinksWhenPossible(t *testing.T) {
	rawMsgs := []map[string]interface{}{
		{"role": "tool", "content": "duplicate line here\nduplicate line here\nduplicate line here\nduplicate line here"},
		{"role": "tool", "content": "pad 1"},
		{"role": "tool", "content": "pad 2"},
		{"role": "tool", "content": "pad 3"},
		{"role": "tool", "content": "pad 4"},
		{"role": "tool", "content": "pad 5"},
		{"role": "tool", "content": "pad 6"},
	}
	origLen := len(rawMsgs[0]["content"].(string))
	results := CompressRawMessages(rawMsgs, LevelLite, "")
	compressedContent := rawMsgs[0]["content"].(string)
	if len(compressedContent) >= origLen {
		t.Errorf("expected compression to reduce size: orig %d, got %d", origLen, len(compressedContent))
	}
	if results[0].Savings <= 0 {
		t.Errorf("expected positive savings, got %d", results[0].Savings)
	}
	if compressedContent != "duplicate line here" {
		t.Errorf("expected single line, got %q", compressedContent)
	}
}
