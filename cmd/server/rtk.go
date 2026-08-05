package main

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// RTK tuning constants. Values mirror rtk's own defaults so PAAP-side
// pre-filtering agrees with what the binary would decide on its own.
const (
	rtkDetectWindow    = 1024             // only the head is inspected for a filter match
	rtkMinCompressSize = 500              // below this, compression isn't worth a subprocess
	rtkMaxCompressSize = 10 * 1024 * 1024 // 10 MiB hard cap
)

// Filter-detection patterns, ported from 9router's open-sse/rtk/autodetect.js
// (itself a port of rtk's Rust auto_detect_filter).
var (
	reGitLog      = regexp.MustCompile(`(?m)^[*|/\\ ]*commit [0-9a-f]{7,40}$`)
	reGitDiff     = regexp.MustCompile(`(?m)^diff --git `)
	reGitDiffHunk = regexp.MustCompile(`(?m)^@@ `)
	reGitStatus   = regexp.MustCompile(`(?m)^On branch |^nothing to commit|^Changes (not |to be )|^Untracked files:`)
	reGoTest      = regexp.MustCompile(`(?m)^(=== RUN|--- (PASS|FAIL|SKIP)|(ok|FAIL)\s+\S+\s)`)
	reGoBuild     = regexp.MustCompile(`(?m)^(# \S+|\S+\.go:\d+:\d+: )`)
	reTsc         = regexp.MustCompile(`(?m)^\S+\.tsx?\(\d+,\d+\): error TS\d+`)
	rePytest      = regexp.MustCompile(`(?m)^(=+ (test session starts|FAILURES|short test summary)|\S+\.py (PASSED|FAILED|\.+))`)
)

var (
	rtkBin       string
	rtkOnce      sync.Once
	rtkAvailable = false
)

// findRTKBinary locates the rtk binary
func findRTKBinary() string {
	rtkOnce.Do(func() {
		// Check common locations
		locations := []string{
			"/usr/local/bin/rtk",
			"/usr/bin/rtk",
			os.Getenv("HOME") + "/.local/bin/rtk",
		}

		for _, loc := range locations {
			if _, err := exec.LookPath(loc); err == nil {
				rtkBin = loc
				rtkAvailable = true
				log.Printf("[PAAP] RTK found at %s", rtkBin)
				return
			}
		}

		// Try which
		if path, err := exec.LookPath("rtk"); err == nil {
			rtkBin = path
			rtkAvailable = true
			log.Printf("[PAAP] RTK found at %s", rtkBin)
			return
		}

		log.Printf("[PAAP] RTK not found — tool output compression disabled")
	})

	return rtkBin
}

// IsRTKAvailable checks if RTK is installed
func IsRTKAvailable() bool {
	findRTKBinary()
	return rtkAvailable
}

// detectFilter maps tool output to an rtk `pipe --filter` name.
// Returns "" when no filter fits — caller must then skip compression, because
// `rtk pipe` with no filter is a passthrough that saves nothing.
// Valid names come from `rtk pipe --filter bogus`; do not invent new ones.
// Ordering mirrors 9router's autoDetectFilter: git-log, git-diff, git-status,
// build output, grep, find, then generic log.
func detectFilter(content string) string {
	// Peek at the head only — matches rtk's own DETECT_WINDOW.
	head := content
	if len(head) > rtkDetectWindow {
		head = head[:rtkDetectWindow]
	}

	switch {
	case reGitLog.MatchString(head):
		return "git-log"
	case reGitDiff.MatchString(head), reGitDiffHunk.MatchString(head):
		return "git-diff"
	case reGitStatus.MatchString(head):
		return "git-status"
	case reGoTest.MatchString(head):
		return "go-test"
	case reGoBuild.MatchString(head):
		return "go-build"
	case reTsc.MatchString(head):
		return "tsc"
	case rePytest.MatchString(head):
		return "pytest"
	}

	lines := strings.Split(head, "\n")
	var nonEmpty []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}

	// grep: any of the first 5 non-empty lines look like "file:lineno:content"
	limit := 5
	if len(nonEmpty) < limit {
		limit = len(nonEmpty)
	}
	for _, l := range nonEmpty[:limit] {
		if isGrepLine(l) {
			return "grep"
		}
	}

	// find: every non-empty line is path-like, at least 3 of them
	if len(nonEmpty) >= 3 {
		allPaths := true
		for _, l := range nonEmpty {
			if !isPathLike(l) {
				allPaths = false
				break
			}
		}
		if allPaths {
			return "find"
		}
	}

	// Generic multi-line noise — rtk's log filter dedupes and truncates.
	if len(nonEmpty) >= 10 {
		return "log"
	}

	return ""
}

// isGrepLine reports whether a line looks like "path:lineno:content".
func isGrepLine(line string) bool {
	first := strings.Index(line, ":")
	if first < 0 {
		return false
	}
	rest := line[first+1:]
	second := strings.Index(rest, ":")
	if second < 0 {
		return false
	}
	lineno := rest[:second]
	if lineno == "" {
		return false
	}
	for _, c := range lineno {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isPathLike reports whether a line looks like a bare filesystem path.
func isPathLike(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	if strings.Contains(t, ":") {
		return false
	}
	return strings.HasPrefix(t, ".") || strings.HasPrefix(t, "/") || strings.Contains(t, "/")
}

// CompressToolOutput compresses a single tool output using RTK.
func CompressToolOutput(content string, level string) string {
	if !IsRTKAvailable() {
		return content
	}

	// Skip blobs that are too small to matter or too large to be worth piping
	// through a subprocess.
	if len(content) < rtkMinCompressSize || len(content) > rtkMaxCompressSize {
		return content
	}

	filter := detectFilter(content)
	if filter == "" {
		// No filter fits. `rtk pipe` without --filter is a passthrough, so
		// running it would only cost a subprocess.
		return content
	}

	args := []string{"pipe", "--filter", filter}
	if level == "ultra" {
		args = append(args, "--ultra-compact")
	}

	cmd := exec.Command(rtkBin, args...)
	cmd.Stdin = strings.NewReader(content)

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		// Fail open — log and return the original content untouched.
		log.Printf("[PAAP] RTK compression failed (filter=%s): %v (%s)", filter, err, errOut.String())
		return content
	}

	compressed := out.String()

	// Safety: never return empty, never grow the input.
	if compressed == "" || len(compressed) >= len(content) {
		return content
	}

	log.Printf("[PAAP] RTK %s: %d -> %d bytes (%.1f%% saved)",
		filter, len(content), len(compressed),
		float64(len(content)-len(compressed))/float64(len(content))*100)
	return compressed
}

// ClientUsesRTK checks if the client is using RTK by looking at tool call commands
func ClientUsesRTK(messages []map[string]interface{}) bool {
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if role != "assistant" {
			continue
		}

		toolCalls, ok := msg["tool_calls"].([]interface{})
		if !ok {
			continue
		}

		for _, tc := range toolCalls {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}

			fn, ok := tcMap["function"].(map[string]interface{})
			if !ok {
				continue
			}

			args, ok := fn["arguments"].(string)
			if !ok {
				continue
			}

			// Check if command starts with rtk
			if strings.Contains(args, `"command":`) {
				// Extract command from JSON args
				if strings.Contains(args, `"rtk `) || strings.Contains(args, `"rtk"`) {
					log.Printf("[PAAP] Detected client RTK usage in tool call: %s", args[:min(100, len(args))])
					return true
				}
			}
		}
	}

	log.Printf("[PAAP] No client RTK detected in %d messages", len(messages))
	return false
}

// isErrorToolResult reports whether a tool message carries an error result.
// Error traces must reach the model verbatim, so they are never compressed.
// Mirrors 9router's `is_error` / `status:"error"` skip rules.
func isErrorToolResult(msg map[string]interface{}) bool {
	if v, ok := msg["is_error"].(bool); ok && v {
		return true
	}
	if s, ok := msg["status"].(string); ok && s == "error" {
		return true
	}
	return false
}

// CompressToolOutputs compresses all tool result messages in parallel
func CompressToolOutputs(messages []map[string]interface{}, level string) []map[string]interface{} {
	if !IsRTKAvailable() {
		return messages
	}

	// Collect indices of tool messages to compress
	type task struct {
		index   int
		content string
	}
	var tasks []task
	for i, msg := range messages {
		role, _ := msg["role"].(string)
		if role != "tool" {
			continue
		}
		// Never compress error traces — the model needs them verbatim to debug.
		if isErrorToolResult(msg) {
			continue
		}
		content, ok := msg["content"].(string)
		if !ok || len(content) < rtkMinCompressSize {
			continue
		}
		tasks = append(tasks, task{i, content})
	}

	if len(tasks) == 0 {
		return messages
	}

	// Run RTK compressions in parallel
	type result struct {
		index      int
		compressed string
	}
	results := make(chan result, len(tasks))
	for _, t := range tasks {
		go func(t task) {
			compressed := CompressToolOutput(t.content, level)
			results <- result{t.index, compressed}
		}(t)
	}

	// Collect results
	compressed := 0
	for range tasks {
		r := <-results
		if r.compressed != messages[r.index]["content"].(string) {
			messages[r.index]["content"] = r.compressed
			compressed++
		}
	}

	if compressed > 0 {
		log.Printf("[PAAP] RTK compressed %d tool outputs (parallel)", compressed)
	}

	return messages
}
