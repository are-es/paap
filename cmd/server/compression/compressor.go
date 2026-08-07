package compression

import (
	"log"
	"strings"
)

// ChatMessage mirrors the minimal proxy message structure.
type ChatMessage interface {
	GetRole() string
	GetContent() string
	SetContent(string)
}

// ProcessedResult tracks per-message compression stats.
type ProcessedResult struct {
	OriginalSize   int
	CompressedSize int
	Savings        int
}

// recentKeepN is the number of most-recent messages to SKIP from compression.
const recentKeepN = 6

// levelBatchSize returns how many old messages to compress per level.
func levelBatchSize(level Level) int {
	switch level {
	case LevelLite:
		return 25
	case LevelMedium:
		return 50
	case LevelHigh:
		return 100
	default:
		return 25
	}
}

// levelRoles returns which roles to compress for each level.
func levelRoles(level Level) map[string]bool {
	switch level {
	case LevelLite:
		return map[string]bool{"tool": true}
	case LevelMedium:
		return map[string]bool{"tool": true, "user": true}
	case LevelHigh:
		return map[string]bool{"tool": true, "user": true, "system": true}
	default:
		return map[string]bool{"tool": true}
	}
}

// candidate holds a message index and reference for compression.
type candidate struct {
	idx int
	msg map[string]interface{}
}

// CompressRawMessages compresses raw message maps ([]map[string]interface{}).
// Lite: per-message Caveman | Medium: per-message Headroom | High: chunk-based both.
func CompressRawMessages(messages []map[string]interface{}, level Level, modelName string) []ProcessedResult {
	if level == LevelOff {
		return nil
	}

	cfg := getConfig(level)
	total := len(messages)
	batchLimit := levelBatchSize(level)
	allowedRoles := levelRoles(level)

	// Calculate cutoff: only compress messages before this index
	cutoff := total - recentKeepN
	if cutoff < 0 {
		cutoff = 0
	}

	// Collect eligible messages (oldest first)
	var candidates []candidate

	for i, msg := range messages {
		if i >= cutoff {
			break
		}
		role, _ := msg["role"].(string)
		if !allowedRoles[role] {
			continue
		}
		content, _ := msg["content"].(string)
		if len(content) >= cfg.MinCompressSize {
			candidates = append(candidates, candidate{idx: i, msg: msg})
		}
	}

	// Take oldest N
	batch := candidates
	if len(batch) > batchLimit {
		batch = candidates[:batchLimit]
	}

	log.Printf("[compression] level=%s total=%d cutoff=%d candidates=%d batch=%d",
		level.String(), total, cutoff, len(candidates), len(batch))

	results := make([]ProcessedResult, total)
	compressed := 0

	// Per-message compression for all levels (sequential)
	compressed = compressPerMessage(batch, level, cfg, results)

	if compressed > 0 {
		log.Printf("[compression] done: %d messages compressed", compressed)
	}

	return results
}

// compressPerMessage compresses messages one by one (sequential).
// Lite: Caveman strategies | Medium: Headroom strategies.
func compressPerMessage(batch []candidate, level Level, cfg levelConfig, results []ProcessedResult) int {
	compressed := 0
	for _, c := range batch {
		content, _ := c.msg["content"].(string)
		role, _ := c.msg["role"].(string)

		var compressedContent string
		switch level {
		case LevelLite:
			// Caveman only
			compressedContent = compressCaveman(content, role, cfg)
		case LevelMedium:
			// Headroom only
			compressedContent = compressHeadroom(content, cfg)
		case LevelHigh:
			// Both: Headroom first, then Caveman
			compressedContent = compressHeadroom(content, cfg)
			compressedContent = compressCaveman(compressedContent, role, cfg)
		}

		if compressedContent != content {
			c.msg["content"] = compressedContent
			compressed++
			origTokens := len(content) / 4
			newTokens := len(compressedContent) / 4
			log.Printf("[compression] msg[%d] role=%s %s %d→%d tokens (saved %d)",
				c.idx, role, level.String(), origTokens, newTokens, origTokens-newTokens)
		}

		results[c.idx] = ProcessedResult{
			OriginalSize:   len(content),
			CompressedSize: len(compressedContent),
			Savings:        len(content) - len(compressedContent),
		}
	}
	return compressed
}

// compressCavender applies Caveman strategies (for Lite level).
func compressCaveman(content, role string, cfg levelConfig) string {
	// Phase 1: safe transforms
	if cfg.RunANSI {
		content = StripAnsi(content)
	}
	if cfg.RunBlankCollapse {
		content = CollapseBlanks(content)
	}

	// Phase 2: Caveman-specific
	if cfg.RunFlintChipper {
		chipped := FlintChipper(content, "")
		if len(chipped) < len(content) {
			content = chipped
		}
	}
	if cfg.RunProseFilter && !isStructuredOutputCompat(content) {
		content = ApplyProseFilter(content)
	}

	// Phase 3: final cleanup
	if cfg.RunBlankCollapse {
		content = CollapseBlanks(content)
	}

	return strings.TrimSpace(content)
}

// compressHeadroom applies Headroom 2-phase pipeline (for Medium level).
func compressHeadroom(content string, cfg levelConfig) string {
	// Phase 1: safe transforms
	if cfg.RunANSI {
		content = StripAnsi(content)
	}
	if cfg.RunBlankCollapse {
		content = CollapseBlanks(content)
	}

	// Phase 2: Headroom pipeline
	contentType := DetectContentType(content)
	targetRatio := 0.7 // Keep 70% of content
	compressed := HeadroomCompress(content, contentType, targetRatio)

	// Phase 3: final cleanup
	if cfg.RunBlankCollapse {
		compressed = CollapseBlanks(compressed)
	}

	return strings.TrimSpace(compressed)
}

// compressChunkBased combines all eligible messages into one chunk (for High level).
// Uses both Caveman and Headroom strategies.
func compressChunkBased(batch []candidate, cfg levelConfig, results []ProcessedResult) int {
	if len(batch) == 0 {
		return 0
	}

	// Step 1: combine all messages into one chunk
	var parts []string
	for _, c := range batch {
		content, _ := c.msg["content"].(string)
		parts = append(parts, content)
	}
	chunk := strings.Join(parts, "\n---MESSAGE_BOUNDARY---\n")

	// Step 2: apply both Caveman + Headroom
	origLen := len(chunk)

	// Caveman: safe transforms
	if cfg.RunANSI {
		chunk = StripAnsi(chunk)
	}
	if cfg.RunBlankCollapse {
		chunk = CollapseBlanks(chunk)
	}

	// Headroom: content detection + 2-phase
	contentType := DetectContentType(chunk)
	compressed := HeadroomCompress(chunk, contentType, 0.5) // Aggressive: target 50%

	// BM25 extractive for text content
	if contentType == ContentText || contentType == ContentSourceCode {
		extracted := BM25Extractive(compressed, 0.6)
		if len(extracted) < len(compressed) {
			compressed = extracted
		}
	}

	// Caveman: FlintChipper for truncation
	if cfg.RunFlintChipper {
		chipped := FlintChipper(compressed, "")
		if len(chipped) < len(compressed) {
			compressed = chipped
		}
	}

	// Phase 3: final cleanup
	if cfg.RunBlankCollapse {
		compressed = CollapseBlanks(compressed)
	}

	compressed = strings.TrimSpace(compressed)

	// Step 3: split back into messages
	// For chunk-based, we put the entire compressed chunk into the FIRST message
	// and clear the rest (they're now redundant)
	compressedMessages := strings.Split(compressed, "\n---MESSAGE_BOUNDARY---\n")

	compressedCount := 0
	for i, c := range batch {
		if i < len(compressedMessages) {
			newContent := compressedMessages[i]
			if newContent != c.msg["content"] {
				c.msg["content"] = newContent
				compressedCount++
			}
		} else {
			// Extra messages: mark as empty (content was merged)
			c.msg["content"] = "[compressed]"
			compressedCount++
		}

		origContent := parts[i]
		results[c.idx] = ProcessedResult{
			OriginalSize:   len(origContent),
			CompressedSize: len(c.msg["content"].(string)),
			Savings:        len(origContent) - len(c.msg["content"].(string)),
		}
	}

	if origLen > len(compressed) {
		log.Printf("[compression] chunk %d→%d bytes (saved %d bytes, %d%%)",
			origLen, len(compressed), origLen-len(compressed),
			(100*(origLen-len(compressed)))/origLen)
	}

	return compressedCount
}

// CompressInterfaceMessages compresses messages via interface (for pipeline compat).
func CompressInterfaceMessages(messages []ChatMessage, level Level, modelName string) []ProcessedResult {
	if level == LevelOff {
		return nil
	}

	cfg := getConfig(level)
	results := make([]ProcessedResult, len(messages))

	for i, msg := range messages {
		role := msg.GetRole()
		if role == "assistant" {
			continue
		}

		content := msg.GetContent()
		if len(content) < cfg.MinCompressSize {
			continue
		}

		compressed := compressHeadroom(content, cfg)
		if compressed != content {
			msg.SetContent(compressed)
		}

		results[i] = ProcessedResult{
			OriginalSize:   len(content),
			CompressedSize: len(compressed),
			Savings:        len(content) - len(compressed),
		}
	}

	return results
}

// isStructuredOutputCompat checks if content looks like structured output (JSON/XML/code).
func isStructuredOutputCompat(content string) bool {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) < 2 {
		return false
	}
	// JSON
	if (trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}') ||
		(trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']') {
		return true
	}
	// XML
	if strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") {
		return true
	}
	// Code block
	if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
		return true
	}
	return false
}
