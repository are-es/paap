package compression

// Cache stability detection — inspired by Headroom's CacheAligner.
// Detects volatile content (timestamps, UUIDs, request IDs) that can bust
// provider KV cache prefixes. When volatile content is detected, compression
// is skipped for that message to preserve cache stability.

import (
	"regexp"
	"strings"
)

// Volatile patterns that change per-request and bust cache prefixes.
var volatilePatterns = []*regexp.Regexp{
	// Timestamps (ISO 8601, Unix, common formats)
	regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`),
	regexp.MustCompile(`\b\d{10,13}\b`), // Unix timestamp (10=sec, 13=ms)
	regexp.MustCompile(`\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2},?\s+\d{4}\b`),

	// UUIDs
	regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`),

	// Request/trace IDs (common patterns)
	regexp.MustCompile(`\breq[_-]?[a-zA-Z0-9]{12,}\b`),
	regexp.MustCompile(`\btrace[_-]?[a-zA-Z0-9]{12,}\b`),
	regexp.MustCompile(`\bspan[_-]?[a-zA-Z0-9]{12,}\b`),
	regexp.MustCompile(`\btransaction[_-]?[a-zA-Z0-9]{8,}\b`),

	// Session tokens
	regexp.MustCompile(`\bsession[_-]?[a-zA-Z0-9]{16,}\b`),
	regexp.MustCompile(`\btoken[_-]?[a-zA-Z0-9]{16,}\b`),

	// Random hashes (32+ hex chars)
	regexp.MustCompile(`\b[a-f0-9]{32,}\b`),

	// Request IDs (UUID-like or random)
	regexp.MustCompile(`\bX-Request-Id:\s*\S+`),
	regexp.MustCompile(`\bx-amz-[a-z-]+:\s*\S+`),
}

// Vvolatile — too many typos. Let me rename.
// IsVolatile checks if content contains volatile markers that would bust cache.
// Returns true if content should NOT be compressed (to preserve cache stability).
func IsVolatile(content string) bool {
	if len(content) < 20 {
		return false
	}

	// Quick check: only sample first 2000 chars for speed
	sample := content
	if len(sample) > 2000 {
		sample = sample[:2000]
	}

	volatileCount := 0
	for _, pat := range volatilePatterns {
		if pat.MatchString(sample) {
			volatileCount++
			// 2+ volatile patterns = likely cache-busting content
			if volatileCount >= 2 {
				return true
			}
		}
	}

	return false
}

// IsVolatileLight is a lighter check — single pattern match.
// Use when you want to detect but not block compression.
func IsVolatileLight(content string) bool {
	if len(content) < 20 {
		return false
	}

	sample := content
	if len(sample) > 1000 {
		sample = sample[:1000]
	}

	for _, pat := range volatilePatterns {
		if pat.MatchString(sample) {
			return true
		}
	}
	return false
}

// ContainsTimestamps checks specifically for timestamp patterns.
func ContainsTimestamps(content string) bool {
	return timestampRE.MatchString(content)
}

var timestampRE = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)

// ContainsUUIDs checks specifically for UUID patterns.
func ContainsUUIDs(content string) bool {
	return uuidRE.MatchString(content)
}

var uuidRE = regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)

// StripVolatileMetadata removes volatile identifiers from content.
// Use this when you DO want to compress but need to stabilize the prefix.
// WARNING: This modifies the content — use only when cache stability > fidelity.
func StripVolatileMetadata(content string) string {
	// Replace timestamps with placeholder
	content = timestampRE.ReplaceAllString(content, "TIMESTAMP")
	// Replace UUIDs with placeholder
	content = uuidRE.ReplaceAllString(content, "UUID-PLACEHOLDER")
	// Replace request IDs
	content = regexp.MustCompile(`req[_-]?[a-zA-Z0-9]{12,}`).ReplaceAllString(content, "REQ-ID")
	// Replace trace IDs
	content = regexp.MustCompile(`trace[_-]?[a-zA-Z0-9]{12,}`).ReplaceAllString(content, "TRACE-ID")

	return strings.TrimSpace(content)
}
