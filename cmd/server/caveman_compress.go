package main

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CavemanCompressor applies caveman compression rules to text
type CavemanCompressor struct {
	rules []CompressionRule
}

type CompressionRule struct {
	Name        string
	Pattern     *regexp.Regexp
	Replacement string
}

// Singleton — compile regex once, reuse per request
var defaultCompressor = NewCavemanCompressor()

// NewCavemanCompressor creates a compressor with default rules
func NewCavemanCompressor() *CavemanCompressor {
	c := &CavemanCompressor{}
	c.loadDefaultRules()
	return c
}

// loadDefaultRules loads the standard caveman compression rules
func (c *CavemanCompressor) loadDefaultRules() {
	rules := []CompressionRule{
		// Articles
		{Name: "articles", Pattern: regexp.MustCompile(`\b(a|an|the)\b\s+`), Replacement: ""},

		// Filler words
		{Name: "filler", Pattern: regexp.MustCompile(`\b(just|really|basically|actually|simply|essentially|generally|literally|currently)\b\s+`), Replacement: ""},

		// Pleasantries
		{Name: "pleasantries", Pattern: regexp.MustCompile(`(?i)\b(sure|certainly|of course|happy to|I'd be glad to|I would be happy to|no problem|you're welcome|absolutely|definitely)\b[,.]?\s*`), Replacement: ""},

		// Hedging
		{Name: "hedging", Pattern: regexp.MustCompile(`(?i)\b(it might be worth|you could consider|it would be good to|perhaps|maybe|it seems like|it appears that|I think that|I believe that|probably|possibly)\b\s+`), Replacement: ""},

		// Verbose phrases → short
		{Name: "verbose_in_order", Pattern: regexp.MustCompile(`(?i)\bin order to\b`), Replacement: "to"},
		{Name: "verbose_make_sure", Pattern: regexp.MustCompile(`(?i)\bmake sure to\b`), Replacement: "ensure"},
		{Name: "verbose_reason_because", Pattern: regexp.MustCompile(`(?i)\bthe reason is because\b`), Replacement: "because"},
		{Name: "verbose_should", Pattern: regexp.MustCompile(`(?i)\byou should\b`), Replacement: ""},
		{Name: "verbose_remember", Pattern: regexp.MustCompile(`(?i)\bremember to\b`), Replacement: ""},

		// Connective fluff
		{Name: "connective", Pattern: regexp.MustCompile(`(?i)\b(however|furthermore|additionally|in addition|moreover|consequently|nevertheless|nonetheless)\b[,.]?\s*`), Replacement: ""},

		// Redundant phrases
		{Name: "redundant_due_to", Pattern: regexp.MustCompile(`(?i)\bdue to the fact that\b`), Replacement: "because"},
		{Name: "redundant_at_this_point", Pattern: regexp.MustCompile(`(?i)\bat this point in time\b`), Replacement: "now"},
		{Name: "redundant_in_the_event", Pattern: regexp.MustCompile(`(?i)\bin the event that\b`), Replacement: "if"},
		{Name: "redundant_for_the_purpose", Pattern: regexp.MustCompile(`(?i)\bfor the purpose of\b`), Replacement: "to"},

		// Wordy → short synonyms
		{Name: "synonym_utilize", Pattern: regexp.MustCompile(`(?i)\butilize\b`), Replacement: "use"},
		{Name: "synonym_implement", Pattern: regexp.MustCompile(`(?i)\bimplement\b`), Replacement: "do"},
		{Name: "synonym_facilitate", Pattern: regexp.MustCompile(`(?i)\bfacilitate\b`), Replacement: "help"},
		{Name: "synonym_demonstrate", Pattern: regexp.MustCompile(`(?i)\bdemonstrate\b`), Replacement: "show"},
		{Name: "?synonym_commence", Pattern: regexp.MustCompile(`(?i)\bcommence\b`), Replacement: "start"},
		{Name: "synonym_terminate", Pattern: regexp.MustCompile(`(?i)\bterminate\b`), Replacement: "stop"},
		{Name: "synonym_subsequent", Pattern: regexp.MustCompile(`(?i)\bsubsequent\b`), Replacement: "next"},
		{Name: "synonym_prior", Pattern: regexp.MustCompile(`(?i)\bprior to\b`), Replacement: "before"},
		{Name: "synonym_endeavor", Pattern: regexp.MustCompile(`(?i)\bendeavor\b`), Replacement: "try"},
		{Name: "synonym_regarding", Pattern: regexp.MustCompile(`(?i)\bregarding\b`), Replacement: "about"},
		{Name: "synonym_concerning", Pattern: regexp.MustCompile(`(?i)\bconcerning\b`), Replacement: "about"},
		{Name: "synonym_extensive", Pattern: regexp.MustCompile(`(?i)\bextensive\b`), Replacement: "big"},
		{Name: "synonym_numerous", Pattern: regexp.MustCompile(`(?i)\bnumerous\b`), Replacement: "many"},
		{Name: "synonym_sufficient", Pattern: regexp.MustCompile(`(?i)\bsufficient\b`), Replacement: "enough"},
		{Name: "synonym_approximately", Pattern: regexp.MustCompile(`(?i)\bapproximately\b`), Replacement: "about"},
	}
	c.rules = rules
}

// isStructuredOutput returns true for content caveman must not touch.
// JSON and similar machine output would never yield caveman savings (>=10%
// required) but costs ~200ms on a 186KB log (see .agent/bench/PROFILE.md).
// The check is a one-byte peek, so prose pays near 0.
func isStructuredOutput(s string) bool {
	t := strings.TrimLeftFunc(s, unicode.IsSpace)
	if t == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(t)
	return r == '{' || r == '['
}

// Compress applies caveman rules to text
func (c *CavemanCompressor) Compress(text string, level string) string {
	if text == "" {
		return text
	}
	// Prose compressor only. JSON tool outputs cost ~200ms for <10% savings
	// which is then discarded (see PROFILE.md sec "PAAP-level A/B").
	if isStructuredOutput(text) {
		return text
	}

	// Preserve code blocks
	codeBlocks := extractCodeBlocks(text)
	cleaned := removeCodeBlocks(text)

	// Apply rules based on level
	result := cleaned
	switch level {
	case "lite":
		// Lite: only basic filler removal
		for _, rule := range c.rules {
			if rule.Name == "articles" || rule.Name == "filler" || rule.Name == "pleasantries" || rule.Name == "hedging" {
				result = rule.Pattern.ReplaceAllString(result, rule.Replacement)
			}
		}
	case "full":
		// Full: all rules except extreme synonyms
		for _, rule := range c.rules {
			if !strings.HasPrefix(rule.Name, "synonym_") {
				result = rule.Pattern.ReplaceAllString(result, rule.Replacement)
			}
		}
	case "ultra":
		// Ultra: all rules including synonyms
		for _, rule := range c.rules {
			result = rule.Pattern.ReplaceAllString(result, rule.Replacement)
		}
	}

	// Clean up multiple spaces
	result = regexp.MustCompile(`\s{2,}`).ReplaceAllString(result, " ")
	result = strings.TrimSpace(result)

	// Restore code blocks
	result = restoreCodeBlocks(result, codeBlocks)

	return result
}

// extractCodeBlocks extracts code blocks and replaces with placeholders
func extractCodeBlocks(text string) map[string]string {
	blocks := make(map[string]string)
	// Match fenced code blocks
	re := regexp.MustCompile("(?s)```[^\n]*\n.*?```")

	counter := 0
	result := re.ReplaceAllStringFunc(text, func(match string) string {
		key := "___CODE_BLOCK_" + string(rune('A'+counter)) + "___"
		blocks[key] = match
		counter++
		return key
	})

	_ = result
	return blocks
}

// removeCodeBlocks removes code blocks from text for processing
func removeCodeBlocks(text string) string {
	re := regexp.MustCompile("(?s)```[^\n]*\n.*?```")
	return re.ReplaceAllString(text, "___CODE_BLOCK___")
}

// restoreCodeBlocks restores code blocks after processing
func restoreCodeBlocks(text string, blocks map[string]string) string {
	result := text
	for key, block := range blocks {
		result = strings.Replace(result, key, block, 1)
	}
	// Also handle single placeholder
	result = strings.Replace(result, "___CODE_BLOCK___", "", 1)
	return result
}

// EstimateSavings estimates token savings from compression
func EstimateSavings(original, compressed string) float64 {
	if len(original) == 0 {
		return 0
	}
	return float64(len(original)-len(compressed)) / float64(len(original)) * 100
}
