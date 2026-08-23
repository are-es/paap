package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestDumpFullPrompt(t *testing.T) {
	longContent := strings.Repeat("x", 50000)
	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are helpful."},
			map[string]interface{}{"role": "user", "content": longContent},
		},
	}

	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	if d == nil {
		t.Fatal("BeginRequestDump returned nil")
	}
	if d.Prompt == nil {
		t.Fatal("Prompt is nil")
	}
	content, ok := d.Prompt["content"].(string)
	if !ok {
		t.Fatal("Prompt content is not a string")
	}
	// Prompt should be truncated to dumpPreviewLen (200) runes
	runes := []rune(content)
	if len(runes) > dumpPreviewLen {
		t.Errorf("Prompt content not truncated: got %d runes, want max %d", len(runes), dumpPreviewLen)
	}
	// content_len should reflect the FULL original length
	if d.Prompt["content_len"].(int) != len(longContent) {
		t.Errorf("content_len mismatch: got %d, want %d", d.Prompt["content_len"].(int), len(longContent))
	}
}

// TestRequestDumpMsgCountAndToolCount validates the new contract: MsgCount
// replaces the old History slice, ToolCount is populated from tools array.
func TestRequestDumpMsgCountAndToolCount(t *testing.T) {
	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "System prompt here."},
			map[string]interface{}{"role": "user", "content": "First user message."},
			map[string]interface{}{"role": "assistant", "content": "Assistant reply."},
			map[string]interface{}{"role": "user", "content": "Final user message."},
		},
		"tools": []interface{}{
			map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "tool1"}},
			map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "tool2"}},
			map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "tool3"}},
		},
	}

	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	if d == nil {
		t.Fatal("BeginRequestDump returned nil")
	}

	// MsgCount should be total messages (4)
	if d.MsgCount != 4 {
		t.Errorf("MsgCount = %d, want 4", d.MsgCount)
	}

	// ToolCount should be 3
	if d.ToolCount != 3 {
		t.Errorf("ToolCount = %d, want 3", d.ToolCount)
	}

	// Prompt should hold the LAST user message
	if d.Prompt == nil {
		t.Fatal("Prompt is nil")
	}
	content, ok := d.Prompt["content"].(string)
	if !ok {
		t.Fatal("Prompt content is not a string")
	}
	if content != "Final user message." {
		t.Errorf("Prompt content = %q, want %q", content, "Final user message.")
	}

	// No History field should exist (it was removed)
	// Verify by checking the struct doesn't retain full message bodies
	data, _ := json.Marshal(d)
	s := string(data)
	if strings.Contains(s, "System prompt here.") {
		t.Error("struct retained full system prompt body — should only keep last user message")
	}
	if strings.Contains(s, "First user message.") {
		t.Error("struct retained full first user message body — should only keep last user message")
	}
	if strings.Contains(s, "Assistant reply.") {
		t.Error("struct retained full assistant reply body — should only keep last user message")
	}
}

// TestRequestDumpPromptTruncationMultibyte proves rune-safe truncation with
// multi-byte UTF-8 characters (not byte-slicing).
func TestRequestDumpPromptTruncationMultibyte(t *testing.T) {
	// 300 emoji chars = 1200 bytes but 300 runes
	emoji := "🎉"
	longContent := strings.Repeat(emoji, 300) // 300 runes, 1200 bytes

	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": longContent},
		},
	}

	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	if d == nil {
		t.Fatal("BeginRequestDump returned nil")
	}
	if d.Prompt == nil {
		t.Fatal("Prompt is nil")
	}

	content, ok := d.Prompt["content"].(string)
	if !ok {
		t.Fatal("Prompt content is not a string")
	}

	// Should be truncated to 200 runes
	runes := []rune(content)
	if len(runes) != dumpPreviewLen {
		t.Errorf("Prompt rune count = %d, want %d", len(runes), dumpPreviewLen)
	}

	// content_len should be 300 (full original)
	if d.Prompt["content_len"].(int) != 300 {
		t.Errorf("content_len = %d, want 300", d.Prompt["content_len"].(int))
	}

	// Verify no broken runes at the truncation point
	if !json.Valid([]byte(`"`+content+`"`)) {
		t.Error("truncated content contains invalid UTF-8 — rune safety broken")
	}
}

func TestRequestDumpParamDiff(t *testing.T) {
	body := map[string]interface{}{
		"model":       "test-model",
		"stream":      false,
		"temperature": 0.3,
		"top_p":       0.9,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}

	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	if d == nil {
		t.Fatal("BeginRequestDump returned nil")
	}

	upstreamBody := map[string]interface{}{
		"temperature":    1.0,
		"topP":           0.95,
		"maxOutputTokens": 64000,
	}
	d.SetUpstream("TestProvider", "https://example.com", upstreamBody)
	d.Finish(200, 100, 100, 50, nil)

	diff := d.ParamDiff
	if diff == nil {
		t.Fatal("ParamDiff is nil")
	}

	dropped, ok := diff["dropped"].([]string)
	if !ok {
		t.Fatal("dropped is not []string")
	}
	for _, d := range dropped {
		if d == "top_p" {
			t.Error("top_p should not be dropped (upstream has topP)")
		}
	}

	injected, ok := diff["injected"].(map[string]interface{})
	if !ok {
		t.Fatal("injected is not a map")
	}
	if _, ok := injected["max_tokens"]; !ok {
		t.Error("max_tokens should be injected (upstream has maxOutputTokens)")
	}

	changed, ok := diff["changed"].(map[string]interface{})
	if !ok {
		t.Fatal("changed is not a map")
	}
	tempDiff, ok := changed["temperature"].(map[string]interface{})
	if !ok {
		t.Fatal("temperature diff not found in changed")
	}
	if tempDiff["client"].(float64) != 0.3 {
		t.Errorf("client temperature = %v, want 0.3", tempDiff["client"])
	}
	if tempDiff["upstream"].(float64) != 1.0 {
		t.Errorf("upstream temperature = %v, want 1.0", tempDiff["upstream"])
	}
}

func TestRequestDumpSnakeCamelNotSpurious(t *testing.T) {
	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"top_p":  0.9,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}

	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	if d == nil {
		t.Fatal("BeginRequestDump returned nil")
	}

	upstreamBody := map[string]interface{}{"topP": 0.95}
	d.SetUpstream("TestProvider", "https://example.com", upstreamBody)
	d.Finish(200, 100, 100, 50, nil)

	diff := d.ParamDiff
	if diff == nil {
		t.Fatal("ParamDiff is nil")
	}
	dropped := diff["dropped"].([]string)
	injected := diff["injected"].(map[string]interface{})

	for _, d := range dropped {
		if d == "top_p" {
			t.Error("top_p should not be dropped when upstream has topP")
		}
	}
	for k := range injected {
		if k == "top_p" {
			t.Error("top_p should not be injected when client has top_p")
		}
	}
}

func TestRequestDumpNoAPIKeyLeakage(t *testing.T) {
	apiKey := "sk-1234567890abcdef1234567890abcdef"
	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}

	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	if d == nil {
		t.Fatal("BeginRequestDump returned nil")
	}

	upstreamBody := map[string]interface{}{"temperature": 1.0}
	d.SetUpstream("TestProvider", "https://example.com", upstreamBody)
	d.Finish(200, 100, 100, 50, nil)

	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if strings.Contains(string(data), apiKey) {
		t.Error("API key value found in dump output")
	}
}

func TestApplyClientSamplingGemini(t *testing.T) {
	body := map[string]interface{}{
		"temperature": 0.3,
		"top_p":       0.8,
	}

	gc := map[string]interface{}{
		"thinkingConfig": map[string]interface{}{"includeThoughts": true},
	}
	applyClientSampling(body, gc, geminiSamplingMappings)

	if gc["temperature"].(float64) != 0.3 {
		t.Errorf("temperature = %v, want 0.3", gc["temperature"])
	}
	if gc["topP"].(float64) != 0.8 {
		t.Errorf("topP = %v, want 0.8", gc["topP"])
	}
	if _, exists := gc["maxOutputTokens"]; exists {
		t.Errorf("maxOutputTokens should be absent when client didn't specify it")
	}
}

func TestApplyClientSamplingGeminiDefaults(t *testing.T) {
	body := map[string]interface{}{}
	gc := map[string]interface{}{}
	applyClientSampling(body, gc, geminiSamplingMappings)

	if len(gc) != 0 {
		t.Errorf("expected empty target map when client provides no sampling params, got: %v", gc)
	}
}

// ── Single-file smart rolling tests ────────────────────────────────────────

// testResetReqLog resets the log file handle for isolated tests.
func testResetReqLog(t *testing.T, logPath string) {
	t.Helper()
	reqLogMu.Lock()
	defer reqLogMu.Unlock()
	if reqLogFile != nil {
		reqLogFile.Close()
	}
	reqLogPath = logPath
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("failed to open test log: %v", err)
	}
	reqLogFile = f
	info, _ := f.Stat()
	if info != nil {
		reqLogSize = info.Size()
	} else {
		reqLogSize = 0
	}
}

// makeTestEntry builds a compact entry in the new format for testing.
func makeTestEntry(id, model, client, prompt string) string {
	var b strings.Builder
	b.WriteString("\n→ 2026-08-23T00:00:00Z [")
	b.WriteString(id)
	b.WriteString("] POST /v1/chat/completions\n")
	b.WriteString("  model=" + model + " client=" + client + " stream=false\n")
	b.WriteString("  msgs=1 tools=0 params={} diff=none\n")
	b.WriteString("  prompt: " + prompt + "\n")
	b.WriteString("← 200 100ms tokens=50/10\n")
	return b.String()
}

func TestSmartRollKeepsLatestEntries(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()

	testResetReqLog(t, logPath)

	entry := makeTestEntry("abc123", "test", "key1", strings.Repeat("x", 100))

	for i := 0; i < 5000; i++ {
		writeReqLogEntry(entry)
	}

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("log file missing: %v", err)
	}
	if info.Size() > reqLogMaxSize {
		t.Errorf("log file too large after roll: %d bytes (max %d)", info.Size(), reqLogMaxSize)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("log file is empty after roll")
	}
	if !strings.Contains(string(data), "→ ") {
		t.Error("log file missing entry markers after roll")
	}
}

func TestSmartRollPreservesRecentEntries(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()

	testResetReqLog(t, logPath)

	oldEntry := makeTestEntry("old-old", "old", "old", strings.Repeat("A", 100))

	for i := 0; i < 4000; i++ {
		writeReqLogEntry(oldEntry)
	}

	newEntry := makeTestEntry("new-new", "new", "new", "UNIQUE_MARKER_CONTENT")
	writeReqLogEntry(newEntry)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if !strings.Contains(string(data), "UNIQUE_MARKER_CONTENT") {
		t.Error("most recent entry was lost during roll")
	}
	if !strings.Contains(string(data), "new-new") {
		t.Error("most recent request ID was lost during roll")
	}
}

func TestSmartRollNeverEmpty(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()

	testResetReqLog(t, logPath)

	entry := makeTestEntry("test", "m", "k", strings.Repeat("B", 200))

	for i := 0; i < 6000; i++ {
		writeReqLogEntry(entry)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log file missing: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("log file is empty — roll wiped everything")
	}
	if int64(len(data)) > reqLogMaxSize {
		t.Errorf("log file exceeds max: %d > %d", len(data), reqLogMaxSize)
	}
}

// ── New tests for the compact format ───────────────────────────────────────

// Test 1: Prompt longer than 200 runes is truncated with …(N runes) marker.
func TestReqLogPromptTruncationRuneSafe(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()
	testResetReqLog(t, logPath)

	// 300 emoji chars = 300 runes, 1200 bytes
	emoji := "🎉"
	longPrompt := strings.Repeat(emoji, 300)

	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": longPrompt},
		},
	}
	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	d.Finish(200, 100, 50, 10, nil)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	s := string(data)

	// Should contain the truncation marker with true rune count
	if !strings.Contains(s, "…(300 runes)") {
		t.Errorf("missing truncation marker '…(300 runes)' in output:\n%s", s)
	}

	// Should NOT contain the full 300-emoji string
	fullMarker := strings.Repeat(emoji, 201)
	if strings.Contains(s, fullMarker) {
		t.Error("output contains more than 200 runes — truncation failed")
	}
}

// Test 2: history_summary no longer appears; msgs=<n> does.
func TestReqLogNoHistorySummaryHasMsgCount(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()
	testResetReqLog(t, logPath)

	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "sys"},
			map[string]interface{}{"role": "user", "content": "u1"},
			map[string]interface{}{"role": "assistant", "content": "a1"},
			map[string]interface{}{"role": "user", "content": "u2"},
			map[string]interface{}{"role": "assistant", "content": "a2"},
		},
	}
	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	d.Finish(200, 100, 50, 10, nil)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	s := string(data)

	if strings.Contains(s, "history_summary") {
		t.Error("output contains 'history_summary' — should be removed")
	}
	if !strings.Contains(s, "msgs=5") {
		t.Errorf("output missing 'msgs=5', got:\n%s", s)
	}
}

// Test 3: FinishError produces ← 502 ... ERROR: upstream dead
func TestReqLogFinishError(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()
	testResetReqLog(t, logPath)

	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "test"},
		},
	}
	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	d.FinishError(502, 1204, "upstream dead")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, "← 502") {
		t.Errorf("missing '← 502' in output:\n%s", s)
	}
	if !strings.Contains(s, "ERROR: upstream dead") {
		t.Errorf("missing 'ERROR: upstream dead' in output:\n%s", s)
	}
}

// Test 4: Finish after FinishError (and vice versa) is a no-op.
func TestReqLogIdempotentFinish(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()
	testResetReqLog(t, logPath)

	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "test"},
		},
	}

	// Case A: FinishError then Finish — only first should write
	d1 := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	d1.FinishError(502, 100, "first error")
	d1.Finish(200, 100, 50, 10, nil) // should be no-op

	data, _ := os.ReadFile(logPath)
	s := string(data)
	if strings.Contains(s, "← 200") {
		t.Error("Finish after FinishError should be a no-op but wrote a 200 line")
	}
	if !strings.Contains(s, "← 502") {
		t.Error("FinishError line missing")
	}

	// Reset for case B
	testResetReqLog(t, logPath)
	os.Truncate(logPath, 0)

	// Case B: Finish then FinishError — only first should write
	d2 := BeginRequestDump("POST", "/v1/chat/completions", "test-key2", body)
	d2.Finish(200, 100, 50, 10, nil)
	d2.FinishError(502, 100, "second error") // should be no-op

	data, _ = os.ReadFile(logPath)
	s = string(data)
	if strings.Contains(s, "ERROR: second error") {
		t.Error("FinishError after Finish should be a no-op but wrote an error line")
	}
	if !strings.Contains(s, "← 200") {
		t.Error("Finish line missing")
	}
}

// Test 5: Smart roll keeps file ≤ 1MB and starts at a → boundary.
func TestReqLogSmartRollBoundary(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()
	testResetReqLog(t, logPath)

	entry := makeTestEntry("rolltest", "model", "key", strings.Repeat("x", 200))

	// Write enough entries to exceed 1MB
	for i := 0; i < 8000; i++ {
		writeReqLogEntry(entry)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log file missing: %v", err)
	}

	if int64(len(data)) > reqLogMaxSize {
		t.Errorf("log file exceeds max: %d > %d", len(data), reqLogMaxSize)
	}

	// First bytes should be a clean entry boundary
	s := string(data)
	if !strings.HasPrefix(s, "\n→ ") {
		// Find first → to see what's happening
		idx := strings.Index(s, "→ ")
		if idx >= 0 {
			t.Errorf("log starts with %q instead of entry boundary", s[:idx+20])
		} else {
			t.Error("log contains no entry boundaries at all")
		}
	}
}

// Test 6: A full formatted entry for a realistic request is under 1KB.
func TestReqLogEntrySizeUnder1KB(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()
	testResetReqLog(t, logPath)

	// Realistic request: 117 messages, 42 tools, 40KB prompt
	msgs := make([]interface{}, 117)
	for i := 0; i < 116; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		msgs[i] = map[string]interface{}{"role": role, "content": "message content " + strings.Repeat("x", 100)}
	}
	msgs[116] = map[string]interface{}{"role": "user", "content": strings.Repeat("x", 40000)}

	tools := make([]interface{}, 42)
	for i := 0; i < 42; i++ {
		tools[i] = map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "tool"}}
	}

	body := map[string]interface{}{
		"model":               "justwoker/claude-opus-5",
		"stream":              true,
		"messages":            msgs,
		"tools":               tools,
		"max_completion_tokens": 32768,
		"temperature":         0.2,
	}

	d := BeginRequestDump("POST", "/v1/chat/completions", "cli-key", body)
	d.SetUpstream("Justwoker", "https://example.com/v1/messages", map[string]interface{}{
		"temperature": 0.2,
	})
	d.Finish(200, 3121, 55206, 67, nil)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	size := len(data)
	if size > 1024 {
		t.Errorf("entry size = %d bytes, want < 1024 (1KB). Content:\n%s", size, string(data))
	}
	t.Logf("entry size = %d bytes (under 1KB ✓)", size)
}

// Test 7: LogEarlyError output parses as a well-formed block with → boundary.
func TestLogEarlyError(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()
	testResetReqLog(t, logPath)

	LogEarlyError("POST", "/v1/chat/completions", 401, 5, "missing Authorization header")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	s := string(data)

	// Should start with → boundary
	if !strings.HasPrefix(s, "\n→ ") {
		t.Errorf("output doesn't start with entry boundary, got: %q", s[:min(len(s), 50)])
	}
	if !strings.Contains(s, "POST /v1/chat/completions") {
		t.Error("missing method/path in output")
	}
	if !strings.Contains(s, "← 401") {
		t.Errorf("missing '← 401' in output:\n%s", s)
	}
	if !strings.Contains(s, "ERROR: missing Authorization header") {
		t.Errorf("missing error message in output:\n%s", s)
	}
	if !strings.Contains(s, "[early]") {
		t.Errorf("missing [early] request id:\n%s", s)
	}
}

// TestRequestDumpToolCountZero ensures ToolCount=0 when no tools array.
func TestRequestDumpToolCountZero(t *testing.T) {
	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	if d == nil {
		t.Fatal("BeginRequestDump returned nil")
	}
	if d.ToolCount != 0 {
		t.Errorf("ToolCount = %d, want 0", d.ToolCount)
	}
}

// TestReqLogFinishErrorMultibyteError ensures error messages with multibyte
// chars are rune-truncated to 300 runes.
func TestReqLogFinishErrorMultibyteError(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "requests.log")

	origPath := reqLogPath
	origFile := reqLogFile
	origSize := reqLogSize
	defer func() {
		reqLogMu.Lock()
		if reqLogFile != nil {
			reqLogFile.Close()
		}
		reqLogPath = origPath
		reqLogFile = origFile
		reqLogSize = origSize
		reqLogMu.Unlock()
	}()
	testResetReqLog(t, logPath)

	// 500 emoji = 500 runes, 2000 bytes
	longErr := strings.Repeat("🔥", 500)

	body := map[string]interface{}{
		"model":  "test-model",
		"stream": false,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "test"},
		},
	}
	d := BeginRequestDump("POST", "/v1/chat/completions", "test-key", body)
	d.FinishError(502, 100, longErr)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	s := string(data)

	// Error should be truncated — should not contain 301 fire emojis
	fire301 := strings.Repeat("🔥", 301)
	if strings.Contains(s, fire301) {
		t.Error("error message not truncated at 300 runes")
	}

	// Should contain exactly 300 fire emojis
	fire300 := strings.Repeat("🔥", 300)
	if !strings.Contains(s, fire300) {
		t.Error("error message truncated too aggressively — missing 300 fire emojis")
	}
}

// TestReqLogFormatParamDiffNone verifies diff=none when no changes.
func TestReqLogFormatParamDiffNone(t *testing.T) {
	if formatParamDiff(nil) != "none" {
		t.Error("nil diff should produce 'none'")
	}
	if formatParamDiff(map[string]interface{}{}) != "none" {
		t.Error("empty diff should produce 'none'")
	}
	if formatParamDiff(map[string]interface{}{
		"dropped":  []string{},
		"injected": map[string]interface{}{},
		"changed":  map[string]interface{}{},
	}) != "none" {
		t.Error("diff with empty slices/maps should produce 'none'")
	}
}

// TestReqLogFormatParams verifies compact k:v formatting.
func TestReqLogFormatParams(t *testing.T) {
	params := map[string]interface{}{
		"temperature": 0.2,
		"max_tokens":  32768,
	}
	s := formatParams(params)
	if s != "{max_tokens:32768 temperature:0.2}" {
		t.Errorf("formatParams = %q, want sorted k:v", s)
	}

	if formatParams(nil) != "{}" {
		t.Error("nil params should produce '{}'")
	}
	if formatParams(map[string]interface{}{}) != "{}" {
		t.Error("empty params should produce '{}'")
	}
}

// TestReqLogFinishNilSafe ensures Finish and FinishError are nil-safe.
func TestReqLogFinishNilSafe(t *testing.T) {
	var d *RequestDump
	// Should not panic
	d.Finish(200, 100, 50, 10, nil)
	d.FinishError(502, 100, "error")
}

// TestReqLogEntryBoundaryDetection verifies the separator works for smart roll.
func TestReqLogEntryBoundaryDetection(t *testing.T) {
	entry := makeTestEntry("test", "model", "key", "hello")
	if !strings.Contains(entry, reqLogSeparator) {
		t.Errorf("entry does not contain separator %q", reqLogSeparator)
	}
	// The separator should be at the very start of the entry
	if !strings.HasPrefix(entry, reqLogSeparator) {
		t.Errorf("entry doesn't start with separator, starts with: %q", entry[:20])
	}
}
