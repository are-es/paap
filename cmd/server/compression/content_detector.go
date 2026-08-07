package compression

import "strings"

// ContentType represents the detected content type for Headroom-style compression.
type ContentType int

const (
	ContentText         ContentType = iota // Plain text (fallback)
	ContentJSON                            // JSON object/array
	ContentSearchResults                   // grep -n format
	ContentGitDiff                         // git diff output
	ContentSourceCode                      // Source code (any language)
	ContentBuildOutput                     // Build/log output with timestamps
	ContentHTML                            // HTML tags
)

func (c ContentType) String() string {
	switch c {
	case ContentJSON:
		return "json"
	case ContentSearchResults:
		return "search"
	case ContentGitDiff:
		return "diff"
	case ContentSourceCode:
		return "code"
	case ContentBuildOutput:
		return "build"
	case ContentHTML:
		return "html"
	default:
		return "text"
	}
}

// DetectContentType detects the content type using pure regex (no ML).
// Ported from Headroom's content_detector.rs.
func DetectContentType(text string) ContentType {
	if len(text) < 10 {
		return ContentText
	}

	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return ContentText
	}

	// Check first few lines for patterns
	checkLines := lines
	if len(checkLines) > 20 {
		checkLines = checkLines[:20]
	}

	// JSON: starts with { or [
	trimmed := strings.TrimLeft(text, " \t\n\r")
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		// Quick JSON check: look for key-value pattern
		if strings.Contains(trimmed, "\"") && (strings.Contains(trimmed, ":") || strings.Contains(trimmed, ",")) {
			return ContentJSON
		}
	}

	// GitDiff: starts with "diff --git" or contains "@@"
	for _, line := range checkLines {
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "diff --cc") {
			return ContentGitDiff
		}
		if strings.HasPrefix(line, "@@") && strings.Contains(line, "@@") {
			return ContentGitDiff
		}
	}

	// SearchResults: "file:line:" pattern (grep -n format)
	searchCount := 0
	for _, line := range checkLines {
		if len(line) > 3 {
			// Pattern: "path/file.ext:123:content" or "file:123:content"
			parts := strings.SplitN(line, ":", 3)
			if len(parts) >= 3 && len(parts[0]) > 0 {
				// Check if second part is a number
				isLineNum := true
				for _, c := range parts[1] {
					if c < '0' || c > '9' {
						isLineNum = false
						break
					}
				}
				if isLineNum && len(parts[1]) > 0 {
					searchCount++
				}
			}
		}
	}
	if searchCount > len(checkLines)/2 {
		return ContentSearchResults
	}

	// BuildOutput: ERROR/FAIL/WARN/INFO/DEBUG with timestamps
	buildCount := 0
	buildKeywords := []string{"ERROR", "FAIL", "WARN", "INFO", "DEBUG", "error:", "failed:", "warning:"}
	for _, line := range checkLines {
		for _, kw := range buildKeywords {
			if strings.Contains(line, kw) {
				buildCount++
				break
			}
		}
	}
	if buildCount > len(checkLines)/3 {
		return ContentBuildOutput
	}

	// HTML: contains tags
	htmlTagCount := 0
	for _, line := range checkLines {
		if strings.Contains(line, "<") && strings.Contains(line, ">") {
			htmlTagCount++
		}
	}
	if htmlTagCount > len(checkLines)/3 {
		return ContentHTML
	}

	// SourceCode: check for code patterns
	codeKeywords := []string{
		"func ", "def ", "class ", "import ", "from ", "function ",
		"const ", "var ", "let ", "type ", "struct ", "interface ",
		"package ", "module ", "require(", "async ", "await ",
		"return ", "if ", "for ", "while ", "switch ",
	}
	codeCount := 0
	for _, line := range checkLines {
		trimmed := strings.TrimSpace(line)
		for _, kw := range codeKeywords {
			if strings.HasPrefix(trimmed, kw) || strings.Contains(trimmed, " "+kw) {
				codeCount++
				break
			}
		}
	}
	if codeCount > len(checkLines)/3 {
		return ContentSourceCode
	}

	// Fallback: plain text
	return ContentText
}
