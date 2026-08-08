package compression

import (
	"strconv"
	"strings"
)

// ── Repeated Tool-Call Pattern Collapse ──────────────────────
// Detects exploratory sequences (ls→cat→cat→cat) that are no longer
// relevant to current topic. Collapses into one-line summary.

// collapseRepeatedPatterns detects repeated tool-call patterns in messages.
// Returns collapsed content if pattern detected, original otherwise.
func collapseRepeatedPatterns(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 5 {
		return content
	}

	// Detect repeated file exploration patterns
	// Pattern: "ls", "cat file1", "cat file2", "cat file3"...
	fileRefs := make(map[string]int)
	toolLines := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		// Detect file operations
		if strings.HasPrefix(trimmed, "cat ") || strings.HasPrefix(trimmed, "head ") ||
			strings.HasPrefix(trimmed, "tail ") || strings.HasPrefix(trimmed, "read ") {
			parts := strings.Fields(trimmed)
			if len(parts) > 1 {
				fileRefs[parts[1]]++
				toolLines++
			}
		}
	}

	// If multiple files explored sequentially, collapse
	if toolLines >= 4 && len(fileRefs) >= 3 {
		var fileList []string
		for f := range fileRefs {
			fileList = append(fileList, f)
		}
		if len(fileList) > 5 {
			fileList = fileList[:5]
		}
		collapsed := "[explored " + strconv.Itoa(len(fileRefs)) + " files: " + strings.Join(fileList, ", ") + " — contents omitted]"
		return collapsed
	}

	return content
}

// ── Code Block Deduplication ──────────────────────────────────
// When multiple code blocks appear sequentially, keep only the latest version.

// dedupCodeBlocks finds repeated code blocks and keeps only the last version.
func dedupCodeBlocks(content string) string {
	// Split by code block markers
	blocks := splitCodeBlocks(content)
	if len(blocks) < 2 {
		return content
	}

	// Group by language tag and content similarity
	type blockInfo struct {
		lang    string
		content string
		idx     int
	}

	var blockInfos []blockInfo
	for i, b := range blocks {
		lang, body := parseCodeBlock(b)
		blockInfos = append(blockInfos, blockInfo{lang: lang, content: body, idx: i})
	}

	// Find superseded blocks (keep last version)
	seen := make(map[string]int) // lang+hash -> last index
	for i, bi := range blockInfos {
		key := bi.lang + ":" + hashContent(bi.content)
		seen[key] = i
	}

	// Rebuild with deduped blocks
	var result []string
	for i, b := range blocks {
		lang, body := parseCodeBlock(b)
		key := lang + ":" + hashContent(body)
		if lastIdx, ok := seen[key]; ok && lastIdx != i {
			// This is an older version, replace with summary
			lines := strings.Split(body, "\n")
			result = append(result, "[code block, "+strconv.Itoa(len(lines))+" lines, superseded by later revision]")
		} else {
			result = append(result, b)
		}
	}

	return strings.Join(result, "")
}

// splitCodeBlocks splits content by ``` markers.
func splitCodeBlocks(content string) []string {
	var blocks []string
	inBlock := false
	start := 0

	for i := 0; i < len(content)-2; i++ {
		if content[i] == '`' && content[i+1] == '`' && content[i+2] == '`' {
			if !inBlock {
				start = i
				inBlock = true
			} else {
				blocks = append(blocks, content[start:i+3])
				inBlock = false
			}
		}
	}

	// Add remaining content outside blocks
	if len(blocks) == 0 {
		return []string{content}
	}

	return blocks
}

// parseCodeBlock extracts language tag and content from a code block.
func parseCodeBlock(block string) (lang, body string) {
	if !strings.HasPrefix(block, "```") {
		return "", block
	}
	// Find language tag after ```
	firstLine := strings.Index(block, "\n")
	if firstLine < 0 {
		return "", block
	}
	lang = strings.TrimSpace(block[3:firstLine])
	body = block[firstLine+1:]
	// Remove trailing ```
	if idx := strings.LastIndex(body, "```"); idx >= 0 {
		body = body[:idx]
	}
	return lang, strings.TrimSpace(body)
}

// hashContent creates a simple hash for content comparison.
func hashContent(content string) string {
	// Simple hash: first 100 chars + length
	if len(content) > 100 {
		return content[:100] + ":" + strconv.Itoa(len(content))
	}
	return content + ":" + strconv.Itoa(len(content))
}

// ── List Compaction ──────────────────────────────────────────
// Lists (numbered/bulleted) >10 items → keep first 5 + ...N more items

// compactLists truncates long lists to first 5 items.
func compactLists(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	listStart := -1
	listCount := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		isListItem := len(trimmed) > 2 && (trimmed[0] == '-' || trimmed[0] == '*' ||
			(trimmed[0] >= '0' && trimmed[0] <= '9' && len(trimmed) > 2 && (trimmed[1] == '.' || trimmed[1] == ')')))

		if isListItem {
			if listStart < 0 {
				listStart = i
				listCount = 0
			}
			listCount++
		} else {
			// End of list
			if listStart >= 0 && listCount > 10 {
				// Compact: keep first 5 + ...N more
				compacted := lines[listStart : listStart+5]
				remaining := listCount - 5
				compacted = append(compacted, "..."+strconv.Itoa(remaining)+" more items")
				result = append(result, compacted...)
			} else if listStart >= 0 {
				// Short list, keep as-is
				result = append(result, lines[listStart:i]...)
			}
			result = append(result, line)
			listStart = -1
			listCount = 0
		}
	}

	// Handle list at end of content
	if listStart >= 0 && listCount > 10 {
		compacted := lines[listStart : listStart+5]
		remaining := listCount - 5
		compacted = append(compacted, "..."+strconv.Itoa(remaining)+" more items")
		result = append(result, compacted...)
	} else if listStart >= 0 {
		result = append(result, lines[listStart:]...)
	}

	return strings.Join(result, "\n")
}

// ── Assistant Reasoning Trim ──────────────────────────────────
// For assistant messages with long reasoning before final answer,
// keep only the conclusion/decision.

// trimReasoning extracts the conclusion from assistant reasoning.
func trimReasoning(content string) string {
	// Look for conclusion patterns
	conclusionMarkers := []string{
		"Therefore,",
		"In conclusion,",
		"The answer is",
		"So the result is",
		"This means",
		"The solution is",
		"Final answer:",
		"Result:",
		"Summary:",
	}

	lines := strings.Split(content, "\n")
	if len(lines) < 5 {
		return content
	}

	// Find the last conclusion marker
	lastConclusion := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, marker := range conclusionMarkers {
			if strings.Contains(trimmed, marker) {
				lastConclusion = i
				break
			}
		}
	}

	// If found conclusion, keep from there
	if lastConclusion > 0 {
		return strings.Join(lines[lastConclusion:], "\n")
	}

	// No conclusion found, keep last 30% of content
	cutoff := len(lines) * 70 / 100
	if cutoff < 3 {
		cutoff = 3
	}
	return strings.Join(lines[cutoff:], "\n")
}

// ── Cross-Message Field Dedup ──────────────────────────────────
// Identical JSON fields repeated across consecutive tool results
// → extract once, others become diff only.

// dedupCrossMessageFields is a placeholder for cross-message dedup.
// This requires access to multiple messages, so it's handled at the
// message array level, not per-message.
func dedupCrossMessageFields(content string) string {
	// Per-message dedup: collapse repeated "status":"ok" patterns
	if !strings.Contains(content, "\"status\"") {
		return content
	}

	lines := strings.Split(content, "\n")
	var result []string
	statusCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "\"status\"") && strings.Contains(trimmed, "\"ok\"") {
			statusCount++
			if statusCount == 1 {
				result = append(result, line)
			} else if statusCount == 2 {
				result = append(result, "...[status:ok repeated "+strconv.Itoa(statusCount)+"x]")
			}
			// Skip subsequent status:ok lines
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}
