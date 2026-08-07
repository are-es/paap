package compression

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

// ChatMessage mirrors the minimal proxy message structure.
// Importing the proxy package would create a cycle, so we define
// a narrow interface that callers satisfy.
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

// compressOneMessage is the core: compress content for a single message.
// Returns the compressed content and stats.
func compressOneMessage(content string, role string, level Level, toolName string) (string, ProcessedResult) {
	cfg := getConfig(level)
	originalSize := len(content)

	if originalSize < cfg.MinCompressSize {
		return content, ProcessedResult{OriginalSize: originalSize, CompressedSize: originalSize}
	}

	compressed := compressSingle(content, cfg, toolName)
	compressedSize := len(compressed)
	saved := originalSize - compressedSize
	if saved < 0 {
		saved = 0
	}

	return compressed, ProcessedResult{
		OriginalSize:   originalSize,
		CompressedSize: compressedSize,
		Savings:        saved,
	}
}

// recentKeepN is the number of most-recent messages to SKIP from compression.
const recentKeepN = 6

// levelBatchSize returns how many old messages to compress per level.
func levelBatchSize(level Level) int {
	switch level {
	case LevelLite:
		return 10
	case LevelMedium:
		return 20
	case LevelHigh:
		return 30
	default:
		return 10
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

// CompressRawMessages compresses raw message maps ([]map[string]interface{}).
// Lite: 5 oldest tool outputs | Medium: 10 tool+user | High: 15 all except assistant.
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
	type candidate struct {
		idx int
		msg map[string]interface{}
	}
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

	log.Printf("[compression] level=%s total=%d cutoff=%d candidates=%d batch=%d roles=%v",
		level.String(), total, cutoff, len(candidates), len(batch), allowedRoles)

	results := make([]ProcessedResult, total)
	var wg sync.WaitGroup
	var mu sync.Mutex
	compressed := 0

	for _, c := range batch {
		wg.Add(1)
		go func(idx int, m map[string]interface{}) {
			defer wg.Done()

			content, _ := m["content"].(string)
			role, _ := m["role"].(string)
			compressedContent, result := compressOneMessage(content, role, level, "")
			if compressedContent != content {
				m["content"] = compressedContent
				mu.Lock()
				compressed++
				mu.Unlock()
				log.Printf("[compression] msg[%d] role=%s COMPRESSED %d->%d (saved %d tokens)",
					idx, role, result.OriginalSize/4, result.CompressedSize/4, result.Savings/4)
			}

			mu.Lock()
			results[idx] = result
			mu.Unlock()
		}(c.idx, c.msg)
	}

	wg.Wait()

	if compressed > 0 {
		log.Printf("[compression] done: %d messages compressed", compressed)
	}

	return results
}

// CompressInterfaceMessages compresses messages via interface (for pipeline compat).
// ASSISTANT MESSAGES ARE NEVER TOUCHED.
func CompressInterfaceMessages(msgs []ChatMessage, level Level) []ProcessedResult {
	if level == LevelOff {
		return nil
	}

	cfg := getConfig(level)
	results := make([]ProcessedResult, len(msgs))

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, msg := range msgs {
		if msg.GetRole() == "assistant" {
			continue
		}

		wg.Add(1)
		go func(idx int, m ChatMessage) {
			defer wg.Done()

			content := m.GetContent()
			if len(content) < cfg.MinCompressSize {
				return
			}

			compressed, result := compressOneMessage(content, m.GetRole(), level, "")
			if compressed != content {
				m.SetContent(compressed)
			}

			mu.Lock()
			results[idx] = result
			mu.Unlock()
		}(i, msg)
	}

	wg.Wait()
	logCompressionStats(level, results)
	return results
}

// logCompressionStats summarizes total compression output.
func logCompressionStats(level Level, results []ProcessedResult) {
	var totalOrig, totalComp int
	for _, r := range results {
		totalOrig += r.OriginalSize
		totalComp += r.CompressedSize
	}
	if totalOrig > 0 && totalComp < totalOrig {
		pct := (totalOrig - totalComp) * 100 / totalOrig
		log.Printf("[compression] level=%s original=%d compressed=%d saved=%d%%",
			level.String(), totalOrig, totalComp, pct)
	}
}

// compressSingle applies the full pipeline for one piece of text.
func compressSingle(content string, cfg levelConfig, toolName string) string {
	// Phase 1: always safe transforms
	if cfg.RunANSI {
		content = StripAnsi(content)
	}
	if cfg.RunBlankCollapse {
		content = CollapseBlanks(content)
	}

	// Phase 2: detect format and route
	format := DetectFormat(content)
	origLen := len(content)
	switch format {
	case formatJSON:
		if cfg.RunStoneTablet {
			compressed := StoneTablet(content, toolName, "")
			if len(compressed) < len(content) {
				content = compressed
			}
		}
	case formatXML:
		if cfg.RunStoneTablet {
			compressed, ok := compressXMLCompat(content, cfg)
			if ok {
				content = compressed
			}
		}
	default:
		if IsLogLike(content) {
			content = ApplyLogStrategy(content, cfg)
		} else {
			if cfg.RunFlintChipper {
				chipped := FlintChipper(content, toolName)
				if len(chipped) < len(content) {
					content = chipped
				}
			}
			if cfg.RunProseFilter && !isStructuredOutputCompat(content) {
				content = ApplyProseFilter(content)
			}
		}
		// Char limit: truncate long single-line content (JSON, etc.)
		maxChars := cfg.HeadLines * 200  // rough estimate
		if len(content) > maxChars && maxChars > 0 {
			keep := maxChars / 2
			content = content[:keep] + fmt.Sprintf("\n\n[... %d chars omitted ...]\n\n", len(content)-maxChars) + content[len(content)-keep:]
		}
	}

	// Phase 3: final cleanup
	if cfg.RunBlankCollapse {
		content = CollapseBlanks(content)
	}

	result := strings.TrimSpace(content)
	if len(result) < origLen {
		log.Printf("[compress] format=%d orig=%d final=%d saved=%d", format, origLen, len(result), origLen-len(result))
	} else {
		log.Printf("[compress] format=%d orig=%d final=NO-CHANGE", format, origLen)
	}
	return result
}

// isStructuredOutputCompat checks for JSON/XML without importing pipeline internals.
func isStructuredOutputCompat(s string) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 20 {
		return false
	}
	if (trimmed[0] == '{' || trimmed[0] == '[') &&
		(strings.HasSuffix(trimmed, "}") || strings.HasSuffix(trimmed, "]")) {
		return true
	}
	if trimmed[0] == '<' && strings.Contains(trimmed, "</") {
		return true
	}
	return false
}

// compressXMLCompat strips xmlns and collapses blanks.
func compressXMLCompat(content string, cfg levelConfig) (string, bool) {
	lines := strings.Split(content, "\n")
	var out []string
	for _, line := range lines {
		if idx := strings.Index(line, " xmlns"); idx >= 0 {
			end := idx
			for end < len(line) && line[end] != '>' {
				end++
			}
			if end > idx {
				line = line[:idx] + line[end:]
			}
		}
		out = append(out, line)
	}
	compressed := strings.Join(out, "\n")
	compressed = CollapseBlanks(compressed)
	return compressed, len(compressed) < len(content)
}
