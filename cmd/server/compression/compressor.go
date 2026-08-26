package compression

import (
	"log"
	"strings"

	"github.com/dolvin/paap/internal/tokens"
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
// System messages are never compressed: lossy truncation silently drops
// instructions and invalidates provider prompt cache. User messages only get
// lossless transforms (Lite pass); the full lossy pipeline is reserved for tool.
func levelRoles(level Level) map[string]bool {
	switch level {
	case LevelLite:
		return map[string]bool{"tool": true}
	case LevelMedium:
		return map[string]bool{"tool": true, "user": true}
	case LevelHigh:
		return map[string]bool{"tool": true}
	default:
		return map[string]bool{"tool": true}
	}
}

// candidate holds a message index and reference for compression.
type candidate struct {
	idx  int
	msg  map[string]interface{}
	size int
}

// CompressRawMessages compresses raw message maps ([]map[string]interface{}).
// Lite: per-message Caveman | Medium: per-message Headroom | High: full pipeline.
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

	// Collect eligible messages with size-based thresholds
	var candidates []candidate

	for i, msg := range messages {
		if i >= cutoff {
			break
		}
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		size := len(content)

		// Size-based threshold
		if size < 50 {
			continue // SKIP: overhead > saving
		}

		// Role eligibility
		if role == "assistant" {
			// Assistant: eligible only outside cutoff, size ≥ 300
			if i >= cutoff {
				continue
			}
			if size < 300 {
				continue
			}
			// Eligible for Medium pass (both Medium and High levels)
			candidates = append(candidates, candidate{idx: i, msg: msg, size: size})
			continue
		}

		// Tool/User/System: check if role is allowed for this level
		if !allowedRoles[role] {
			continue
		}

		candidates = append(candidates, candidate{idx: i, msg: msg, size: size})
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
	for _, c := range batch {
		content, _ := c.msg["content"].(string)
		role, _ := c.msg["role"].(string)
		toolName, _ := c.msg["name"].(string)
		size := len(content)

		var compressedContent string

		switch level {
		case LevelLite:
			// Lite: ANSI strip, whitespace collapse, dedup
			compressedContent = compressLite(content, cfg)

		case LevelMedium:
			// Medium: tiered thresholds
			if size < 200 {
				// 50-200 bytes: Lite pass only (reuse Lite logic)
				compressedContent = compressLite(content, cfg)
			} else {
				// ≥200 bytes: full Medium pipeline
				if role == "assistant" {
					// Assistant: max Bloat Offload (no FlintChipper/BM25)
					compressedContent = compressMediumAssistant(content, cfg)
				} else {
					compressedContent = compressMedium(content, cfg)
				}
			}

		case LevelHigh:
			// High: full pipeline
			if role == "assistant" {
				// Assistant: max Medium pass (don't break reasoning)
				compressedContent = compressMedium(content, cfg)
			} else {
				// Tool/User: full pipeline
				compressedContent = compressHigh(content, cfg, toolName)
			}
		}

		// Size guard: if compressed size >= original size, retain original content
		if len(compressedContent) >= size {
			compressedContent = content
		}

		if compressedContent != content {
			c.msg["content"] = compressedContent
			compressed++
			// Estimated token counts — used for savings reporting only, never billing.
			origTokens := tokens.Estimate(content)
			newTokens := tokens.Estimate(compressedContent)
			log.Printf("[compression] msg[%d] role=%s %s %d→%d tokens (saved %d)",
				c.idx, role, level.String(), origTokens, newTokens, origTokens-newTokens)
		}

		results[c.idx] = ProcessedResult{
			OriginalSize:   size,
			CompressedSize: len(compressedContent),
			Savings:        size - len(compressedContent),
		}
	}

	if compressed > 0 {
		log.Printf("[compression] done: %d messages compressed", compressed)
	}

	return results
}

// compressLite: ANSI strip, whitespace collapse, consecutive duplicate collapsing
func compressLite(content string, cfg levelConfig) string {
	orig := content
	if cfg.RunANSI {
		content = StripAnsi(content)
	}
	if cfg.RunBlankCollapse {
		content = CollapseBlanks(content)
	}
	// Collapse consecutive duplicate lines
	lines := strings.Split(content, "\n")
	var result []string
	var prev string
	var hasPrev bool
	for _, line := range lines {
		if hasPrev && line == prev {
			continue
		}
		result = append(result, line)
		prev = line
		hasPrev = true
	}
	res := strings.TrimSpace(strings.Join(result, "\n"))
	if len(res) >= len(orig) {
		return orig
	}
	return res
}

// compressMedium: structural (JSON minify, tabular, dedup)
func compressMedium(content string, cfg levelConfig) string {
	orig := content
	// Phase 1: safe transforms
	if cfg.RunANSI {
		content = StripAnsi(content)
	}
	if cfg.RunBlankCollapse {
		content = CollapseBlanks(content)
	}

	// Phase 2: Headroom pipeline (content detection + reformat)
	contentType := DetectContentType(content)
	compressed := HeadroomCompress(content, contentType, 0.7) // Keep 70%

	// Phase 3: cleanup
	if cfg.RunBlankCollapse {
		compressed = CollapseBlanks(compressed)
	}

	res := strings.TrimSpace(compressed)
	if len(res) >= len(orig) {
		return orig
	}
	return res
}

// compressMediumAssistant: Medium pass for assistant messages (stop at Bloat Offload)
// No FlintChipper, BM25, Pattern Collapse, Code Dedup, List Compaction, Cross-msg Dedup
func compressMediumAssistant(content string, cfg levelConfig) string {
	orig := content
	// Phase 1: safe transforms
	if cfg.RunANSI {
		content = StripAnsi(content)
	}
	if cfg.RunBlankCollapse {
		content = CollapseBlanks(content)
	}

	// Phase 2: Headroom reformat (lossless)
	contentType := DetectContentType(content)
	reformatted := phase1Reformat(content, contentType)

	// Phase 3: Headroom bloat offload (lossy)
	offloaded := phase2Offload(reformatted, contentType, 0.7) // Keep 70%

	// Phase 4: cleanup
	if cfg.RunBlankCollapse {
		offloaded = CollapseBlanks(offloaded)
	}

	res := strings.TrimSpace(offloaded)
	if len(res) >= len(orig) {
		return orig
	}
	return res
}

// compressHigh: full pipeline (Headroom + SmartCrusher + Cache Stability + BM25)
// toolName selects the FlintChipper line budget; empty means skip truncation.
func compressHigh(content string, cfg levelConfig, toolName string) string {
	orig := content
	// Step 1: Threshold gate (size-based)
	size := len(content)
	if size < 50 {
		return content // SKIP: overhead > saving
	}

	// Step 2: Cache stability gate — skip compression for volatile content
	if cfg.RunCacheStability && IsVolatile(content) {
		log.Printf("[compression] HIGH: skipping volatile content (%d bytes)", size)
		return content
	}

	// Step 3: Safe transforms
	if cfg.RunANSI {
		content = StripAnsi(content)
	}
	if cfg.RunBlankCollapse {
		content = CollapseBlanks(content)
	}

	// Step 4: Content-type detection
	contentType := DetectContentType(content)

	// Step 5: Headroom reformat (JSON minify, log dedup, diff strip) — lossless
	reformatted := phase1Reformat(content, contentType)

	// Step 6: SmartCrusher (JSON) — statistical compression
	if cfg.RunSmartCrusher && contentType == ContentJSON {
		crushed := SmartCrushJSON(reformatted, cfg.SmartCrusherTargetRatio)
		if len(crushed) < len(reformatted) {
			reformatted = crushed
		}
	}

	// Step 7: Headroom bloat offload (score-based) — lossy
	offloaded := phase2Offload(reformatted, contentType, 0.5)

	// Step 8: Repeated pattern collapse
	collapsed := collapseRepeatedPatterns(offloaded)

	// Step 9: Code block dedup
	deduped := dedupCodeBlocks(collapsed)

	// Step 10: List compaction
	compacted := compactLists(deduped)

	// Step 11: FlintChipper head+tail
	// Only when a real per-tool budget exists; empty toolName would silently
	// apply the 40-line fallback to everything.
	if cfg.RunFlintChipper {
		if _, known := DefaultToolBudgets[toolName]; known {
			chipped := FlintChipper(compacted, toolName)
			if len(chipped) < len(compacted) {
				compacted = chipped
			}
		}
	}

	// ponytail: Step 12 reasoning trim removed — compressHigh is never called
	// for assistant messages (caller routes them to compressMedium).

	// Step 13: BM25 extractive
	if cfg.RunBM25 {
		extracted := BM25Extractive(compacted, cfg.BM25TargetRatio)
		if len(extracted) < len(compacted) {
			compacted = extracted
		}
	}

	// Step 14: Cross-message field dedup
	dedupedFields := dedupCrossMessageFields(compacted)

	// Step 15: Final cleanup
	if cfg.RunBlankCollapse {
		dedupedFields = CollapseBlanks(dedupedFields)
	}

	res := strings.TrimSpace(dedupedFields)
	if len(res) >= len(orig) {
		return orig
	}
	return res
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

		compressed := compressMedium(content, cfg)
		if len(compressed) >= len(content) {
			compressed = content
		}
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
