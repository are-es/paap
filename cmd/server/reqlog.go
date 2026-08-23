package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ── Single-file request log with smart rolling ─────────────────────────────
// All requests log to <dataDir>/logs/requests.log. When the file exceeds 1MB,
// the oldest entries are discarded by keeping the latest ~768KB (trimmed at a
// clean "→ " boundary) and rewriting the file. With ~400-byte entries, 1MB
// holds ~2500 requests; keeping 768KB discards only the oldest 25% per roll.

const (
	reqLogMaxSize   = 1 * 1024 * 1024 // 1MB hard cap
	reqLogKeepSize  = 768 * 1024      // ~768KB retained after roll
	reqLogSeparator = "\n→ "          // entry boundary marker
)

var (
	reqLogFile    *os.File
	reqLogPath    string
	reqLogOnce    sync.Once
	reqLogMu      sync.Mutex
	reqLogSize    int64 // tracked incrementally to avoid Stat() on every write
	reqLogRolling bool  // true while a roll is in progress
)

func initReqLog() {
	reqLogOnce.Do(func() {
		dir := filepath.Join(dataDirPath(), "logs")
		os.MkdirAll(dir, 0755)
		reqLogPath = filepath.Join(dir, "requests.log")
		openReqLog()
	})
}

func openReqLog() {
	f, err := os.OpenFile(reqLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("[REQLOG] Failed to open %s: %v", reqLogPath, err)
		return
	}
	info, err := f.Stat()
	if err != nil {
		log.Printf("[REQLOG] Failed to stat %s: %v", reqLogPath, err)
		f.Close()
		return
	}
	reqLogFile = f
	reqLogSize = info.Size()
	log.Printf("[REQLOG] Request logging to %s (%d bytes)", reqLogPath, reqLogSize)
}

// writeReqLogEntry appends a block to the log file. If the file would exceed
// 1MB, it performs a smart roll: read the file, find the latest entry
// boundary within the last ~768KB, and rewrite keeping only that tail.
func writeReqLogEntry(block string) {
	reqLogMu.Lock()
	defer reqLogMu.Unlock()

	if reqLogFile == nil {
		return
	}

	blockBytes := int64(len(block))

	// Check if we need to roll
	if reqLogSize+blockBytes > reqLogMaxSize {
		if !smartRoll() {
			log.Printf("[REQLOG] Smart roll failed, skipping entry")
			return
		}
	}

	n, err := reqLogFile.WriteString(block)
	if err != nil {
		log.Printf("[REQLOG] Write failed: %v", err)
		return
	}
	reqLogSize += int64(n)
}

// smartRoll reads the current log, finds the latest entry boundary
// within the retained window (~768KB from the end), and rewrites the file
// with only that tail. Returns true on success.
func smartRoll() bool {
	if reqLogRolling {
		return false // prevent re-entrant rolls
	}
	reqLogRolling = true
	defer func() { reqLogRolling = false }()

	// Close current file
	reqLogFile.Close()
	reqLogFile = nil

	data, err := os.ReadFile(reqLogPath)
	if err != nil {
		log.Printf("[REQLOG] Failed to read log for roll: %v", err)
		openReqLog() // try to reopen
		return false
	}

	if len(data) == 0 {
		openReqLog()
		return true
	}

	// Find the cut point: the latest entry boundary within the
	// retained window (last ~768KB).
	windowStart := len(data) - reqLogKeepSize
	if windowStart < 0 {
		windowStart = 0
	}

	// Search forward from windowStart for the first entry boundary
	cutIdx := -1
	searchFrom := windowStart
	for {
		idx := strings.Index(string(data[searchFrom:]), reqLogSeparator)
		if idx < 0 {
			break
		}
		absIdx := searchFrom + idx
		cutIdx = absIdx
		// Move past this marker to find the next one
		searchFrom = absIdx + len(reqLogSeparator)
	}

	if cutIdx <= 0 {
		// No boundary found in window — keep the last reqLogKeepSize bytes
		// but try to find ANY boundary
		cutIdx = strings.Index(string(data), reqLogSeparator)
		if cutIdx < 0 {
			// No boundaries at all — just truncate to keep size
			cutIdx = len(data) - reqLogKeepSize
			if cutIdx < 0 {
				cutIdx = 0
			}
		}
	}

	kept := data[cutIdx:]

	// Rewrite the file with only the kept tail
	if err := os.WriteFile(reqLogPath, kept, 0644); err != nil {
		log.Printf("[REQLOG] Failed to rewrite after roll: %v", err)
		openReqLog()
		return false
	}

	openReqLog()
	log.Printf("[REQLOG] Smart roll: discarded %d bytes, kept %d bytes", cutIdx, len(kept))
	return true
}

// LogRequestDump writes a complete request+response block to the single log.
// Called from RequestDump.Finish() or RequestDump.FinishError().
//
// Format (success):
//
//	→ 2026-08-23T02:16:11Z [6c2ef5] POST /v1/chat/completions
//	  model=justwoker/claude-opus-5 upstream=Justwoker client=cli-key stream=true
//	  msgs=117 tools=42 params={max_completion_tokens:32768 temperature:0.2} diff=none
//	  prompt: bisa gak
//	← 200 3121ms tokens=55206/67
//
// Format (error):
//
//	→ 2026-08-23T02:16:11Z [a91f2c] POST /v1/chat/completions
//	  model=justwoker/claude-opus-5 upstream=Justwoker client=cli-key stream=true
//	  msgs=12 tools=42 params={temperature:0.2} diff=none
//	  prompt: kenapa gagal
//	← 502 1204ms ERROR: all keys exhausted for provider Justwoker
func LogRequestDump(d *RequestDump) {
	initReqLog()
	if reqLogFile == nil {
		return
	}

	var b strings.Builder

	// Line 1: → <RFC3339 UTC timestamp> [<request_id>] <METHOD> <path>
	fmt.Fprintf(&b, "\n→ %s [%s] %s %s\n",
		d.Timestamp,
		d.RequestID,
		d.Method,
		d.Path,
	)

	// Line 2: model=, upstream=, client=, stream=
	fmt.Fprintf(&b, "  model=%s", d.Model)
	if upstream, ok := d.Upstream["provider"].(string); ok && upstream != "" {
		fmt.Fprintf(&b, " upstream=%s", upstream)
	}
	if d.ClientKey != "" {
		fmt.Fprintf(&b, " client=%s", d.ClientKey)
	}
	fmt.Fprintf(&b, " stream=%v\n", d.Stream)

	// Line 3: msgs=, tools=, params={}, diff=
	fmt.Fprintf(&b, "  msgs=%d", d.MsgCount)
	if d.ToolCount > 0 {
		fmt.Fprintf(&b, " tools=%d", d.ToolCount)
	}
	fmt.Fprintf(&b, " params=%s", formatParams(d.InboundParams))
	fmt.Fprintf(&b, " diff=%s\n", formatParamDiff(d.ParamDiff))

	// Line 4: prompt (only if present)
	if d.Prompt != nil {
		if content, ok := d.Prompt["content"].(string); ok && content != "" {
			fullLen, _ := d.Prompt["content_len"].(int)
			truncated := fullLen > dumpPreviewLen
			// Collapse newlines to single space
			collapsed := strings.ReplaceAll(content, "\n", " ")
			collapsed = strings.ReplaceAll(collapsed, "\r", "")
			fmt.Fprintf(&b, "  prompt: %s", collapsed)
			if truncated {
				fmt.Fprintf(&b, "…(%d runes)", fullLen)
			}
			b.WriteByte('\n')
		}
	}

	// Final line: response
	if d.Response != nil {
		status, _ := d.Response["status"].(int)
		latency, _ := d.Response["latency_ms"].(int64)

		if status < 200 || status >= 300 {
			// Error response
			errMsg, _ := d.Response["error"].(string)
			// Truncate error message to 300 runes, collapse newlines
			errRunes := []rune(errMsg)
			if len(errRunes) > 300 {
				errMsg = string(errRunes[:300])
			}
			errMsg = strings.ReplaceAll(errMsg, "\n", " ")
			errMsg = strings.ReplaceAll(errMsg, "\r", "")
			fmt.Fprintf(&b, "← %d %dms ERROR: %s\n", status, latency, errMsg)
		} else {
			// Success response
			tin, _ := d.Response["tokens_in"].(int)
			tout, _ := d.Response["tokens_out"].(int)
			fmt.Fprintf(&b, "← %d %dms tokens=%d/%d\n", status, latency, tin, tout)
		}
	}

	writeReqLogEntry(b.String())
}

// LogEarlyError records a request rejected before a RequestDump could be built.
// Emits a single compact block with no model or params.
func LogEarlyError(method, path string, status int, latencyMs int64, errMsg string) {
	initReqLog()
	if reqLogFile == nil {
		return
	}

	var b strings.Builder

	// Line 1: → <RFC3339 UTC timestamp> [<no-id>] <METHOD> <path>
	fmt.Fprintf(&b, "\n→ %s [early] %s %s\n",
		time.Now().UTC().Format(time.RFC3339),
		method,
		path,
	)

	// Line 2: minimal info
	b.WriteString("  model= stream=false\n")

	// Line 3: no messages, no params
	b.WriteString("  msgs=0 params={} diff=none\n")

	// Final line: error
	errRunes := []rune(errMsg)
	if len(errRunes) > 300 {
		errMsg = string(errRunes[:300])
	}
	errMsg = strings.ReplaceAll(errMsg, "\n", " ")
	errMsg = strings.ReplaceAll(errMsg, "\r", "")
	fmt.Fprintf(&b, "← %d %dms ERROR: %s\n", status, latencyMs, errMsg)

	writeReqLogEntry(b.String())
}

// formatParams formats inbound params as compact k:v pairs, sorted by key.
// Excludes "tools_count" since we already print tools= separately.
func formatParams(params map[string]interface{}) string {
	if len(params) == 0 {
		return "{}"
	}

	// Collect and sort keys
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "tools_count" {
			continue // already printed as tools=N
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if len(keys) == 0 {
		return "{}"
	}

	var parts []string
	for _, k := range keys {
		v := params[k]
		parts = append(parts, fmt.Sprintf("%s:%v", k, v))
	}
	return "{" + strings.Join(parts, " ") + "}"
}

// formatParamDiff formats the param diff as a compact summary.
// Returns "none" when nothing was dropped/injected/changed.
func formatParamDiff(diff map[string]interface{}) string {
	if diff == nil {
		return "none"
	}

	dropped, _ := diff["dropped"].([]string)
	injected, _ := diff["injected"].(map[string]interface{})
	changed, _ := diff["changed"].(map[string]interface{})

	if len(dropped) == 0 && len(injected) == 0 && len(changed) == 0 {
		return "none"
	}

	var parts []string

	if len(dropped) > 0 {
		parts = append(parts, fmt.Sprintf("dropped[%s]", strings.Join(dropped, ",")))
	}

	if len(injected) > 0 {
		iKeys := make([]string, 0, len(injected))
		for k := range injected {
			iKeys = append(iKeys, k)
		}
		sort.Strings(iKeys)
		var iParts []string
		for _, k := range iKeys {
			iParts = append(iParts, fmt.Sprintf("%s=%v", k, injected[k]))
		}
		parts = append(parts, fmt.Sprintf("injected[%s]", strings.Join(iParts, " ")))
	}

	if len(changed) > 0 {
		cKeys := make([]string, 0, len(changed))
		for k := range changed {
			cKeys = append(cKeys, k)
		}
		sort.Strings(cKeys)
		parts = append(parts, fmt.Sprintf("changed[%s]", strings.Join(cKeys, ",")))
	}

	return strings.Join(parts, " ")
}

// truncateRunes truncates a string to max runes (rune-safe, not byte-safe).
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// collapseNewlines replaces all newlines with spaces and trims.
func collapseNewlines(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.TrimSpace(s)
}

// runeCount returns the rune count of a string.
func runeCount(s string) int {
	return utf8.RuneCountInString(s)
}
