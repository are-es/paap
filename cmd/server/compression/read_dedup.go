package compression

import (
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"sync"
)

// ============================================================================
// Read Deduplication Cache
// ============================================================================

// ReadCacheEntry stores a fingerprint and the sequential read index for a file.
type ReadCacheEntry struct {
	// Fingerprint: SHA256 hash of content
	Fingerprint [32]byte
	// ReadIndex: sequential read number when this file was first read
	ReadIndex int
}

// ReadDeduplicationCache is a session-scoped cache for deduplicating repeated
// file reads. When the LLM re-reads an unchanged file, the full content is
// replaced with a one-line stub, saving context tokens.
//
// Invalidated on write/edit to the same path.
type ReadDeduplicationCache struct {
	mu        sync.Mutex
	cache     map[string]ReadCacheEntry
	readCount int
}

// NewReadDeduplicationCache creates a new empty cache.
func NewReadDeduplicationCache() *ReadDeduplicationCache {
	return &ReadDeduplicationCache{
		cache: make(map[string]ReadCacheEntry),
	}
}

// Reset clears the cache (call on session start or new branch).
func (c *ReadDeduplicationCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]ReadCacheEntry)
	c.readCount = 0
}

// CheckRead checks a read result against the cache.
// Returns a stub string if content is unchanged, or "" if new/changed.
// Side effect: updates the cache with the current content on first/changed read.
func (c *ReadDeduplicationCache) CheckRead(filePath, content string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	fingerprint := sha256.Sum256([]byte(content))
	existing, exists := c.cache[filePath]

	if exists && existing.Fingerprint == fingerprint {
		stub := fmt.Sprintf("[File unchanged since read #%d. Content identical to prior read. Reference that context.]", existing.ReadIndex)
		log.Printf("[PAAP] Read dedup hit: %s -> stub (read #%d)", filePath, existing.ReadIndex)
		return stub
	}

	// New or changed — update cache
	c.readCount++
	c.cache[filePath] = ReadCacheEntry{
		Fingerprint: fingerprint,
		ReadIndex:   c.readCount,
	}
	return ""
}

// Invalidate removes a cache entry (call when a file is written/edited).
func (c *ReadDeduplicationCache) Invalidate(filePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, filePath)
}

// CheckReadToolMessages applies read deduplication to tool messages in a request.
// For each tool message that looks like a file read result, checks if the content
// is identical to a prior read. Returns modified messages and count of stubs applied.
func (c *ReadDeduplicationCache) CheckReadToolMessages(messages []map[string]interface{}) ([]map[string]interface{}, int) {
	stubs := 0

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if role != "tool" {
			continue
		}

		content, ok := msg["content"].(string)
		if !ok || len(content) < 200 {
			// Skip small outputs — dedup not worth it
			continue
		}

		// Try to extract the file path from the tool message.
		// Look for common patterns in tool output or metadata.
		filePath := extractReadFilePath(msg)
		if filePath == "" {
			continue
		}

		stub := c.CheckRead(filePath, content)
		if stub != "" {
			msg["content"] = stub
			stubs++
		}
	}

	if stubs > 0 {
		log.Printf("[PAAP] Read dedup: %d tool messages replaced with stubs", stubs)
	}

	return messages, stubs
}

// extractReadFilePath tries to find the file path from a tool message.
// Checks tool_call_id, name, and content patterns.
func extractReadFilePath(msg map[string]interface{}) string {
	// Check if there's a file path in the message name/metadata
	if name, ok := msg["name"].(string); ok {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "read") || strings.Contains(lower, "cat") || strings.Contains(lower, "file") {
			// This is likely a read operation — use the tool call ID or name as key
			if id, ok := msg["tool_call_id"].(string); ok && id != "" {
				return id
			}
			return name
		}
	}

	// Fallback: use tool_call_id if present (most tool messages have this)
	if id, ok := msg["tool_call_id"].(string); ok && id != "" {
		return id
	}

	return ""
}

// Global session cache — reset between sessions.
// In production, this would be per-session; for now, a package-level singleton.
var globalReadCache = NewReadDeduplicationCache()

// GetGlobalReadCache returns the package-level read dedup cache.
func GetGlobalReadCache() *ReadDeduplicationCache {
	return globalReadCache
}

// ResetGlobalReadCache resets the global cache (call on new session).
func ResetGlobalReadCache() {
	globalReadCache.Reset()
}
