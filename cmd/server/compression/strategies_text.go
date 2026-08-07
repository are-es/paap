package compression

import (
	"regexp"
	"strings"
)

// proseRule is a compiled filler-removal pattern with its replacement.
type proseRule struct {
	Pattern *regexp.Regexp
	Replace string
}

// Compile all prose rules once at init.
// These are ported from caveman_compress.go — lossy filler removal.
var proseRules = []proseRule{
	// Opening pleasantries (case-insensitive, multiline)
	{regexp.MustCompile(`(?i)^(sure|certainly|of course|great|wonderful|fantastic|excellent|absolutely|definitely|perfectly|understood)\b[.,!\s]*`), ""},
	{regexp.MustCompile(`(?i)^(I'd be happy to|I'll help you|I can help you|let me help you|I will help you|I'm here to assist you|I'm ready to help)\b[^.]*[.]\s*`), ""},
	{regexp.MustCompile(`(?i)^no problem[.!?\s]*`), ""},
	{regexp.MustCompile(`(?i)^(here is|here are|here's)\s+`), ""},
	{regexp.MustCompile(`(?i)^(let me|allow me to|I'll)\s+(just\s+)?(show you|help you|explain|assist)[^.]*[.]\s*`), ""},

	// Closing filler (case-insensitive, multiline)
	{regexp.MustCompile(`(?i)\s*(?:please\s+)?(?:don't hesitate to|feel free to)\s+[^.]+[.]\s*$`), ""},
	{regexp.MustCompile(`(?i)\s*let me know if you (?:need|have|would like)[^.]+[.]\s*$`), ""},
	{regexp.MustCompile(`(?i)\s*if you have any (?:other\s+)?(?:questions|concerns|issues)[^.]+[.]\s*$`), ""},
	{regexp.MustCompile(`(?i)\s*is there anything else I can help (?:you with|with)\??\s*$`), ""},
	{regexp.MustCompile(`(?i)\s*hope this (?:helps|is helpful)[.!]?\s*$`), ""},
	{regexp.MustCompile(`(?i)\s*(?:happy|glad) to (?:help|assist)[.!]?\s*$`), ""},

	// Middle filler phrases
	{regexp.MustCompile(`(?i)\b(I think that|I believe that|in my opinion|it's worth noting that|it's important to note that|it should be noted that)\b`), ""},
	{regexp.MustCompile(`(?i)\b(basically|actually|essentially|fundamentally|really|truly|honestly|certainly|definitely|obviously|clearly|naturally)\b`), ""},
	{regexp.MustCompile(`(?i)\b(just|simply|merely|only|quite|rather|somewhat|fairly|pretty much)\b`), ""},
	{regexp.MustCompile(`(?i)\b(basically)\b`), ""},
}

// proseSentEnders marks sentence boundaries for the sentence scanner.
var proseSentEnders = []string{". ", ".\n", "! ", "!\n", "? ", "?\n"}

// ApplyProseFilter removes filler words/phrases from prose text.
// Skips JSON/XML/code blocks entirely.
func ApplyProseFilter(s string) string {
	if len(s) < 100 {
		return s
	}
	if IsStructuredOutput(s) {
		return s
	}

	for _, rule := range proseRules {
		s = rule.Pattern.ReplaceAllString(s, rule.Replace)
	}

	// Collapse resulting multi-blank lines
	s = CollapseBlankLines(s)
	return strings.TrimSpace(s)
}

// IsStructuredOutput returns true if the content is JSON, XML, or code.
// Skips compression to avoid breaking structure.
func IsStructuredOutput(s string) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 {
		return false
	}

	// JSON detection
	if (trimmed[0] == '{' || trimmed[0] == '[') &&
		(strings.HasSuffix(trimmed, "}") || strings.HasSuffix(trimmed, "]")) &&
		len(trimmed) > 20 {
		return true
	}

	// XML detection
	if strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") && len(trimmed) > 20 {
		return true
	}

	// Code detection — high density of { } ( ) ; :
	braceCount := 0
	parenCount := 0
	for _, c := range trimmed {
		switch c {
		case '{':
			braceCount++
		case '}':
			braceCount--
		case '(':
			parenCount++
		case ')':
			parenCount--
		}
	}
	if braceCount == 0 && parenCount == 0 && braceCount+parenCount != 0 {
		return true // balanced braces = likely code
	}

	return false
}

// ApplyTextStrategy runs prose filter (if enabled) and blank collapse.
func ApplyTextStrategy(s string, cfg levelConfig) string {
	if len(s) < cfg.MinCompressSize {
		return s
	}

	if cfg.RunProseFilter && !IsStructuredOutput(s) {
		s = ApplyProseFilter(s)
	}

	if cfg.RunBlankCollapse {
		s = CollapseBlankLines(s)
	}

	return strings.TrimSpace(s)
}
