package compression

import (
	"encoding/json"
	"strings"
)

// HeadroomCompress applies Headroom-style 2-phase compression.
// Phase 1: reformat (lossless)
// Phase 2: bloat offload (lossy, gated by score)
func HeadroomCompress(text string, contentType ContentType, targetRatio float64) string {
	if len(text) < 200 {
		return text
	}

	// Phase 1: REFORMAT (lossless, always runs)
	reformatted := phase1Reformat(text, contentType)

	// Check if reformat already achieved target
	if float64(len(reformatted))/float64(len(text)) <= targetRatio {
		return reformatted
	}

	// Phase 2: BLOAT OFFLOAD (lossy, gated by score)
	offloaded := phase2Offload(reformatted, contentType, targetRatio)

	// Acceptance gate: only return if we actually saved bytes
	if len(offloaded) < len(text) {
		return offloaded
	}
	return text
}

// phase1Reformat applies lossless transforms based on content type.
func phase1Reformat(text string, contentType ContentType) string {
	switch contentType {
	case ContentJSON:
		return reformatJSON(text)
	case ContentBuildOutput:
		return reformatLog(text)
	case ContentGitDiff:
		return reformatDiff(text)
	default:
		return text
	}
}

// reformatJSON minifies JSON (removes whitespace, compact keys).
func reformatJSON(text string) string {
	var parsed interface{}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return text // Not valid JSON, return as-is
	}
	compact, err := json.Marshal(parsed)
	if err != nil {
		return text
	}
	return string(compact)
}

// reformatLog deduplicates repeated log patterns.
func reformatLog(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= 5 {
		return text
	}

	seen := make(map[string]int)
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			result = append(result, line)
			continue
		}
		// Normalize: remove timestamps, numbers, paths
		normalized := normalizeLogLine(trimmed)
		seen[normalized]++
		if seen[normalized] <= 2 { // Keep first 2 occurrences
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

// normalizeLogLine removes timestamps and variable parts for dedup.
func normalizeLogLine(line string) string {
	// Remove common timestamp patterns
	tsPatterns := []string{
		"2026-", "2025-", "2024-",
		"[INFO]", "[DEBUG]", "[WARN]", "[ERROR]",
	}
	result := line
	for _, p := range tsPatterns {
		if idx := strings.Index(result, p); idx >= 0 {
			result = result[:idx] + result[idx+len(p):]
		}
	}
	return strings.TrimSpace(result)
}

// reformatDiff strips context lines from diff output.
func reformatDiff(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		// Keep: diff headers, hunk headers, added/removed lines
		// Strip: context lines (starting with space)
		if strings.HasPrefix(line, " ") && len(line) > 1 {
			continue // Strip context lines
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// phase2Offload applies lossy compression based on content type and bloat score.
func phase2Offload(text string, contentType ContentType, targetRatio float64) string {
	// Score each offload strategy
	type offload struct {
		name  string
		score float64
		fn    func(string) string
	}

	offloads := []offload{
		{"log_truncate", scoreLogBloat(text, contentType), offloadLogTruncate},
		{"json_drop", scoreJSONBloat(text, contentType), offloadJSONDrop},
		{"prose_bm25", scoreProseBloat(text, contentType), offloadProseBM25},
		{"diff_strip", scoreDiffBloat(text, contentType), offloadDiffStrip},
	}

	// Run offloads with score >= threshold (sorted by score desc)
	// Simple bubble sort for 4 items
	for i := 0; i < len(offloads); i++ {
		for j := i + 1; j < len(offloads); j++ {
			if offloads[j].score > offloads[i].score {
				offloads[i], offloads[j] = offloads[j], offloads[i]
			}
		}
	}

	result := text
	bloatThreshold := 0.3

	for _, o := range offloads {
		if o.score < bloatThreshold {
			continue
		}
		compressed := o.fn(result)
		if len(compressed) < len(result) {
			result = compressed
		}
		// Early exit if target reached
		if float64(len(result))/float64(len(text)) <= targetRatio {
			break
		}
	}

	return result
}

// Bloat scoring functions

func scoreLogBloat(text string, contentType ContentType) float64 {
	if contentType == ContentBuildOutput {
		return 0.8 // High bloat likely
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 50 {
		return 0.6
	}
	return 0.2
}

func scoreJSONBloat(text string, contentType ContentType) float64 {
	if contentType == ContentJSON {
		return 0.7
	}
	if strings.Contains(text, "{") && strings.Contains(text, "}") {
		return 0.5
	}
	return 0.1
}

func scoreProseBloat(text string, contentType ContentType) float64 {
	if contentType == ContentText {
		words := strings.Fields(text)
		if len(words) > 100 {
			return 0.6
		}
	}
	return 0.2
}

func scoreDiffBloat(text string, contentType ContentType) float64 {
	if contentType == ContentGitDiff {
		return 0.9 // Very high bloat (context lines)
	}
	return 0.1
}

// Offload functions

func offloadLogTruncate(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= 30 {
		return text
	}
	// Keep first 20 + last 10
	head := lines[:20]
	tail := lines[len(lines)-10:]
	omitted := len(lines) - 30
	result := append(head, "")
	result = append(result, "[... "+itoa(omitted)+" lines omitted ...]")
	result = append(result, tail...)
	return strings.Join(result, "\n")
}

func offloadJSONDrop(text string) string {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return text
	}
	// Drop common verbose fields
	dropFields := []string{
		"stackTrace", "stack_trace", "traceback",
		"debug", "verbose", "raw", "full_response",
		"request_body", "response_body",
		"html", "content_html", "rendered",
	}
	for _, field := range dropFields {
		delete(parsed, field)
	}
	compact, err := json.Marshal(parsed)
	if err != nil {
		return text
	}
	return string(compact)
}

func offloadProseBM25(text string) string {
	// Use BM25 extractive with 60% target
	return BM25Extractive(text, 0.6)
}

func offloadDiffStrip(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		// Keep: diff headers, hunk headers, added/removed lines
		// Strip: context lines (starting with space) and @@ hunk bodies
		if strings.HasPrefix(line, " ") && len(line) > 1 {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			// Keep hunk header but strip the body
			result = append(result, line)
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// Helper: simple int to string (avoid strconv import)
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	return result
}
