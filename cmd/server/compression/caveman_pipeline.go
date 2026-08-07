// Package compression implements the Caveman Compression Pipeline — a set of
// deterministic, token-saving transforms that run on tool output (role=tool
// messages) before the request is sent to the LLM provider.
//
// Ported from Caveman Code (TypeScript) to Go for PAAP.
package compression

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
)

// ============================================================================
// Per-Tool Output Budgets (Flint Chipper)
// ============================================================================

// ToolBudget defines head/tail line budgets for a specific tool type.
type ToolBudget struct {
	MaxLines  int
	HeadLines int
	TailLines int
}

// DefaultToolBudgets are the per-tool line budgets, matching Caveman Code.
var DefaultToolBudgets = map[string]ToolBudget{
	"bash": {MaxLines: 30, HeadLines: 20, TailLines: 10},
	"read": {MaxLines: 80, HeadLines: 50, TailLines: 30},
	"grep": {MaxLines: 50, HeadLines: 30, TailLines: 20},
	"find": {MaxLines: 30, HeadLines: 20, TailLines: 10},
	"ls":   {MaxLines: 30, HeadLines: 20, TailLines: 10},
}

// FallbackBudget is used when the tool type is unknown.
var FallbackBudget = ToolBudget{MaxLines: 40, HeadLines: 25, TailLines: 15}

// GetToolBudget returns the budget for a tool, falling back to FallbackBudget.
func GetToolBudget(toolName string) ToolBudget {
	if b, ok := DefaultToolBudgets[toolName]; ok {
		return b
	}
	return FallbackBudget
}

// FlintChipper truncates text using per-tool budget (head+tail preservation).
// Returns the original text if within budget.
func FlintChipper(text, toolName string) string {
	budget := GetToolBudget(toolName)
	lines := strings.Split(text, "\n")
	if len(lines) <= budget.MaxLines {
		return text
	}

	omitted := len(lines) - budget.HeadLines - budget.TailLines
	head := lines[:budget.HeadLines]
	tail := lines[len(lines)-budget.TailLines:]

	var sb strings.Builder
	for i, l := range head {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(l)
	}
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("[... %d lines omitted (%s budget: %d) ...]", omitted, toolName, budget.MaxLines))
	sb.WriteString("\n")
	for _, l := range tail {
		sb.WriteByte('\n')
		sb.WriteString(l)
	}

	return sb.String()
}

// ============================================================================
// ANSI Strip
// ============================================================================

// ansiEscapeRE matches ANSI/VT100 escape sequences.
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripAnsi removes ANSI escape codes (colors, cursor movement) from text.
func StripAnsi(text string) string {
	return ansiEscapeRE.ReplaceAllString(text, "")
}

// ============================================================================
// Blank Collapse
// ============================================================================

// blankCollapseRE matches 3+ consecutive blank lines (including \r\n variants).
var blankCollapseRE = regexp.MustCompile(`(\r?\n){3,}`)

// CollapseBlanks collapses 3+ consecutive blank lines into a single blank line.
func CollapseBlanks(text string) string {
	return blankCollapseRE.ReplaceAllString(text, "\n\n")
}

// ============================================================================
// General Truncation (500-line cap with head+tail)
// ============================================================================

const (
	maxLines  = 500
	headLines = 200
	tailLines = 100
)

// TruncateLongOutput truncates text to at most maxLines, preserving headLines
// from the start and tailLines from the end with a truncation marker between.
func TruncateLongOutput(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}

	omitted := len(lines) - headLines - tailLines
	head := lines[:headLines]
	tail := lines[len(lines)-tailLines:]

	var sb strings.Builder
	for i, l := range head {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(l)
	}
	sb.WriteString(fmt.Sprintf("\n\n[... %d lines omitted (cave mode truncation) ...]\n\n", omitted))
	for i, l := range tail {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(l)
	}

	return sb.String()
}

// ============================================================================
// Stone Tablet — Semantic JSON/XML compression
// ============================================================================

// outputFormat is detected content type for Stone Tablet.
type outputFormat int

const (
	formatText outputFormat = iota
	formatJSON
	formatXML
)

// DetectFormat determines if content is JSON, XML, or plain text.
// Only triggers on outputs > 50 lines.
func DetectFormat(text string) outputFormat {
	lines := strings.Split(text, "\n")
	if len(lines) <= 10 {
		return formatText
	}

	trimmed := strings.TrimLeft(text, " \t\n\r")

	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		if json.Valid([]byte(trimmed)) {
			return formatJSON
		}
		// Heuristic: starts with { or [ and ends with } or ]
		if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) &&
			(strings.HasSuffix(strings.TrimRight(text, " \t\n\r"), "}") || strings.HasSuffix(strings.TrimRight(text, " \t\n\r"), "]")) {
			return formatJSON
		}
	}

	if strings.HasPrefix(trimmed, "<?xml") ||
		(strings.HasPrefix(trimmed, "<") && !strings.HasPrefix(trimmed, "<!DOCTYPE html")) {
		if strings.Contains(trimmed, "</") || strings.Contains(trimmed, "/>") {
			return formatXML
		}
	}

	return formatText
}

// commandKeyHints maps command substrings to relevant JSON keys.
var commandKeyHints = map[string][]string{
	"docker inspect": {"State", "Config", "NetworkSettings", "Mounts", "HostConfig"},
	"docker ps":      {"Names", "Status", "Ports", "Image"},
	"npm ls":         {"name", "version", "dependencies"},
	"package.json":   {"name", "version", "scripts", "dependencies", "devDependencies"},
	"tsconfig":       {"compilerOptions", "include", "exclude"},
	"kubectl":        {"metadata", "spec", "status"},
	"aws ":           {"Arn", "Name", "Status", "State", "Id"},
}

// extractKeyHints returns relevant keys for a command hint.
func extractKeyHints(commandHint string) map[string]bool {
	hints := make(map[string]bool)
	if commandHint == "" {
		return hints
	}
	lower := strings.ToLower(commandHint)
	for pattern, keys := range commandKeyHints {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			for _, k := range keys {
				hints[k] = true
			}
		}
	}
	return hints
}

const (
	maxDepth        = 4
	maxArrayElements = 3
)

// compressValue recursively compresses a parsed JSON value.
func compressValue(value interface{}, relevantKeys map[string]bool, depth int) interface{} {
	if depth > maxDepth {
		switch v := value.(type) {
		case []interface{}:
			return fmt.Sprintf("[Array(%d)]", len(v))
		case map[string]interface{}:
			return fmt.Sprintf("{Object(%d keys)}", len(v))
		default:
			return value
		}
	}

	switch v := value.(type) {
	case []interface{}:
		if len(v) <= maxArrayElements {
			result := make([]interface{}, len(v))
			for i, item := range v {
				result[i] = compressValue(item, relevantKeys, depth+1)
			}
			return result
		}
		kept := make([]interface{}, 0, maxArrayElements+1)
		for i := 0; i < maxArrayElements; i++ {
			kept = append(kept, compressValue(v[i], relevantKeys, depth+1))
		}
		kept = append(kept, fmt.Sprintf("... %d more items (%d total)", len(v)-maxArrayElements, len(v)))
		return kept

	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}

		// If we have relevant key hints, prioritize those
		if len(relevantKeys) > 0 && depth <= 1 {
			result := make(map[string]interface{})
			kept := 0
			omitted := make([]string, 0)

			for _, key := range keys {
				if relevantKeys[key] {
					result[key] = compressValue(v[key], relevantKeys, depth+1)
					kept++
				} else {
					omitted = append(omitted, key)
				}
			}

			if kept == 0 {
				// No hints matched — keep first 5 keys
				for i, key := range keys {
					if i >= 5 {
						break
					}
					result[key] = compressValue(v[key], relevantKeys, depth+1)
				}
				if len(keys) > 5 {
					result["..."] = fmt.Sprintf("%d more keys omitted", len(keys)-5)
				}
			} else if len(omitted) > 0 {
				sample := omitted
				if len(sample) > 5 {
					sample = sample[:5]
				}
				suffix := ""
				if len(omitted) > 5 {
					suffix = "..."
				}
				result["..."] = fmt.Sprintf("%d keys omitted: %s%s", len(omitted), strings.Join(sample, ", "), suffix)
			}

			return result
		}

		// No hints or deeper level — keep first 8 keys
		maxKeys := 8
		if len(keys) <= maxKeys {
			result := make(map[string]interface{})
			for _, key := range keys {
				result[key] = compressValue(v[key], relevantKeys, depth+1)
			}
			return result
		}

		result := make(map[string]interface{})
		for i := 0; i < maxKeys; i++ {
			result[keys[i]] = compressValue(v[keys[i]], relevantKeys, depth+1)
		}
		result["..."] = fmt.Sprintf("%d more keys omitted", len(keys)-maxKeys)
		return result

	case string:
		if len(v) > 200 {
			return fmt.Sprintf("%s... (%d chars)", v[:200], len(v))
		}
		return value

	default:
		return value
	}
}

// CompressJSON compresses JSON text using semantic extraction.
func CompressJSON(text, commandHint string) string {
	trimmed := strings.TrimSpace(text)
	var parsed interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return text // Not valid JSON — pass through
	}

	relevantKeys := extractKeyHints(commandHint)
	compressed := compressValue(parsed, relevantKeys, 0)
	resultBytes, err := json.MarshalIndent(compressed, "", "  ")
	if err != nil {
		return text
	}
	result := string(resultBytes)

	originalLines := len(strings.Split(text, "\n"))
	resultLines := len(strings.Split(result, "\n"))
	if resultLines >= originalLines*60/100 {
		return text // Compression didn't help much
	}

	var retainedInfo string
	if len(relevantKeys) > 0 {
		keys := make([]string, 0, len(relevantKeys))
		for k := range relevantKeys {
			keys = append(keys, k)
		}
		retainedInfo = "Keys retained: " + strings.Join(keys, ", ")
	} else {
		retainedInfo = "Top-level keys retained"
	}

	return fmt.Sprintf("%s\n\n[JSON compressed: %d of %d lines. %s]", result, resultLines, originalLines, retainedInfo)
}

// xmlnsRE matches xmlns namespace declarations.
var xmlnsRE = regexp.MustCompile(`\s+xmlns(?::\w+)?="[^"]*"`)

// tagMatchRE matches opening XML tags.
var tagMatchRE = regexp.MustCompile(`^\s*<(\w+)[\s>]`)

// CompressXML compresses XML by stripping namespace boilerplate and collapsing
// repetitive sibling elements.
func CompressXML(text string) string {
	lines := strings.Split(text, "\n")
	originalCount := len(lines)

	result := make([]string, 0, len(lines))
	repetitionCount := 0
	lastTagName := ""
	skipping := false

	for _, line := range lines {
		// Strip xmlns namespace declarations
		cleaned := xmlnsRE.ReplaceAllString(line, "")

		// Detect repetitive sibling elements
		if m := tagMatchRE.FindStringSubmatch(cleaned); m != nil {
			tagName := m[1]
			if tagName == lastTagName {
				repetitionCount++
				if repetitionCount > 3 {
					if !skipping {
						result = append(result, fmt.Sprintf("    ... (repeated <%s> elements)", tagName))
						skipping = true
					}
					continue
				}
			} else {
				if skipping {
					result = append(result, fmt.Sprintf("    [%d total <%s> elements]", repetitionCount, lastTagName))
					skipping = false
				}
				lastTagName = tagName
				repetitionCount = 1
			}
		}

		result = append(result, cleaned)
	}

	if skipping {
		result = append(result, fmt.Sprintf("    [%d total <%s> elements]", repetitionCount, lastTagName))
	}

	resultCount := len(result)
	if resultCount >= originalCount*60/100 {
		return text // Not enough compression
	}

	return fmt.Sprintf("%s\n\n[XML compressed: %d of %d lines]", strings.Join(result, "\n"), resultCount, originalCount)
}

// StoneTablet applies structured output compression (JSON/XML semantic extraction).
// Only processes bash tool output. Returns original text for non-bash tools.
func StoneTablet(text, toolName, commandHint string) string {
	if toolName != "bash" {
		return text
	}

	format := DetectFormat(text)
	switch format {
	case formatJSON:
		return CompressJSON(text, commandHint)
	case formatXML:
		return CompressXML(text)
	default:
		return text
	}
}

// ============================================================================
// Pipeline Config
// ============================================================================

// PipelineConfig holds per-pipeline enable/disable toggles.
type PipelineConfig struct {
	FlintChipper    bool
	AnsiStrip       bool
	StoneTablet     bool
	BlankCollapse   bool
	GeneralTruncate bool
}

// DefaultPipelineConfig has all pipelines enabled.
var DefaultPipelineConfig = PipelineConfig{
	FlintChipper:    true,
	AnsiStrip:       true,
	StoneTablet:     true,
	BlankCollapse:   true,
	GeneralTruncate: true,
}

// ApplyCavemanPipeline runs the full Caveman Compression Pipeline on a single
// tool output string. Pipelines run in order:
//   1. ANSI Strip — remove terminal escape codes
//   2. Flint Chipper — per-tool line budgets
//   3. Stone Tablet — JSON/XML semantic compression (bash only)
//   4. Blank Collapse — collapse 3+ blank lines
//   5. General Truncation — 500-line head+tail cap
func ApplyCavemanPipeline(text, toolName, commandHint string, cfg PipelineConfig) string {
	originalLen := len(text)

	// 1. ANSI Strip
	if cfg.AnsiStrip {
		text = StripAnsi(text)
	}

	// 2. Flint Chipper — per-tool budget truncation
	if cfg.FlintChipper {
		text = FlintChipper(text, toolName)
	}

	// 3. Stone Tablet — structured output compression (bash only)
	if cfg.StoneTablet {
		text = StoneTablet(text, toolName, commandHint)
	}

	// 4. Blank Collapse
	if cfg.BlankCollapse {
		text = CollapseBlanks(text)
	}

	// 5. General Truncation
	if cfg.GeneralTruncate {
		text = TruncateLongOutput(text)
	}

	if len(text) < originalLen {
		log.Printf("[PAAP] Caveman pipeline %s: %d -> %d bytes (%.1f%% saved)",
			toolName, originalLen, len(text),
			float64(originalLen-len(text))/float64(originalLen)*100)
	}

	return text
}

// ApplyPipelineToMessages applies the full caveman pipeline to all tool messages
// in a request. Returns the modified messages slice.
func ApplyPipelineToMessages(messages []map[string]interface{}, cfg PipelineConfig) []map[string]interface{} {
	compressed := 0
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if role != "tool" {
			continue
		}

		// Skip error traces — model needs them verbatim
		if v, ok := msg["is_error"].(bool); ok && v {
			continue
		}
		if s, ok := msg["status"].(string); ok && s == "error" {
			continue
		}

		content, ok := msg["content"].(string)
		if !ok || content == "" {
			continue
		}

		// Try to detect tool name from the message metadata.
		// PAAP tool messages may carry tool_call_id or name.
		toolName := detectToolName(msg)

		compressedContent := ApplyCavemanPipeline(content, toolName, "", cfg)
		if compressedContent != content {
			msg["content"] = compressedContent
			compressed++
		}
	}

	if compressed > 0 {
		log.Printf("[PAAP] Caveman pipeline compressed %d tool messages", compressed)
	}

	return messages
}

// detectToolName tries to extract the tool name from a message's metadata.
func detectToolName(msg map[string]interface{}) string {
	// Direct name field
	if name, ok := msg["name"].(string); ok && name != "" {
		return normalizeToolName(name)
	}
	// tool_call_id may contain hints
	if id, ok := msg["tool_call_id"].(string); ok && id != "" {
		// Tool IDs sometimes embed the tool name — check common prefixes
		for _, prefix := range []string{"bash", "read", "grep", "find", "ls"} {
			if strings.HasPrefix(strings.ToLower(id), prefix) {
				return prefix
			}
		}
	}
	return ""
}

// normalizeToolName maps common tool name variants to our budget keys.
func normalizeToolName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lower, "bash") || strings.Contains(lower, "terminal") || strings.Contains(lower, "execute"):
		return "bash"
	case strings.Contains(lower, "read") || strings.Contains(lower, "cat"):
		return "read"
	case strings.Contains(lower, "grep") || strings.Contains(lower, "search") || strings.Contains(lower, "rg"):
		return "grep"
	case strings.Contains(lower, "find"):
		return "find"
	case strings.Contains(lower, "ls") || strings.Contains(lower, "list"):
		return "ls"
	default:
		return lower
	}
}
