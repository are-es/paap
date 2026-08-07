package compression

import (
	"regexp"
)

var collapseRe = regexp.MustCompile(`\n{3,}`)

// CollapseBlankLines replaces 3+ consecutive newlines with 2.
func CollapseBlankLines(s string) string {
	return collapseRe.ReplaceAllString(s, "\n\n")
}

// StripANSI removes ANSI escape codes from text.
// Always applied (safe, lossless).
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)
