package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Headroom is an external Python HTTP service (`headroom-ai`). PAAP calls its
// POST /v1/compress endpoint as a side-call: we hand it messages[], get
// compressed messages[] back, then PAAP forwards to the provider itself.
// Headroom never talks to a provider and never sees an API key.
//
// Benchmark (.agent/bench/REPORT.md): RTK-only 19.0% -> RTK+Headroom 50.5%.
// It must run AFTER RTK (RTK-first 50.5% vs Headroom-first 47.0%).
//
// Deferred: the Anthropic /v1/messages path. /v1/compress only speaks the
// OpenAI messages[] shape and PAAP has no Claude<->OpenAI translator.
const (
	headroomMinCompressSize = 8 * 1024         // below this, the round-trip costs more than it saves
	headroomMaxCompressSize = 10 * 1024 * 1024 // 10 MiB hard cap, mirrors rtkMaxCompressSize
	headroomHealthTTL       = 30 * time.Second //
	headroomPhantomRatio    = 0.95             // reject result if it didn't shrink at least 5%

	// 3s was the plan, but measured e2e: a 186 KB tool output takes ~4s to
	// compress (see .agent/bench/REPORT.md — warm calls were 30-340ms on
	// fixtures up to 170 KB, but SmartCrusher scales with array size and the
	// full message array here is 214 KB). 3s timed out and fell open, wasting
	// the round-trip. 15s covers the largest realistic payload; fail-open still
	// protects the request if Headroom hangs.
	headroomDefaultTimeoutMS = 15000
)

// One shared client. A remote service means connection reuse matters, and a
// per-call client would leak idle conns.
var headroomClient = &http.Client{
	Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	},
}

var (
	headroomHealthMu      sync.Mutex
	headroomHealthy       bool
	headroomHealthExpires time.Time
	headroomHealthKnown   bool // false until the first probe, so the first result logs
)

// Compressed tool output cache keyed by sha256(content).
// An agent loop often re-sends the same tool returns: turn 1 pays 200ms,
// turn 2..N should be 0ms. Headroom itself does no content-keyed caching
// (see PROFILE.md sec "Server-side cache: absent"). LRU on content size, not
// entry count, so a single 200KB entry can't unbound the process.
//
// This is deliberately content→content, not message-array → message-array.
// A full-array cache would miss on every turn because model messages change
// even when the tool output stays identical.
const (
	headroomCacheMaxBytes = 8 * 1024 * 1024 // 8 MiB
)

type hrCache struct {
	mu    sync.Mutex
	by    map[string]hrCacheSlot // key = hex(sha256(content))
	lru   []string               // oldest first, for eviction
	bytes int
}

type hrCacheSlot struct {
	compressed string
	size       int // len(content)+len(compressed), book-keeping only
}

var hrCacheStore = &hrCache{by: map[string]hrCacheSlot{}}

func hrCacheKey(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:])
}

func (c *hrCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.by[key]
	if !ok {
		return "", false
	}
	return e.compressed, true
}

func (c *hrCache) set(key, compressed string, contentSize int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.by[key]; ok {
		return // duplicate within TTL — rare but keep it cheap
	}
	e := hrCacheSlot{compressed: compressed, size: contentSize + len(compressed)}
	c.by[key] = e
	c.lru = append(c.lru, key)
	c.bytes += e.size
	// Evict oldest until under cap.
	for c.bytes > headroomCacheMaxBytes && len(c.lru) > 0 {
		old := c.lru[0]
		c.lru = c.lru[1:]
		if ent, ok := c.by[old]; ok {
			c.bytes -= ent.size
			delete(c.by, old)
		}
	}
}

type headroomResponse struct {
	Messages          []map[string]interface{} `json:"messages"`
	TokensBefore      int                      `json:"tokens_before"`
	TokensAfter       int                      `json:"tokens_after"`
	TokensSaved       int                      `json:"tokens_saved"`
	CompressionRatio  float64                  `json:"compression_ratio"`
	TransformsApplied []string                 `json:"transforms_applied"`
}

func headroomURL() string {
	return strings.TrimRight(getSettingStrCached("headroom_url", "http://127.0.0.1:8787"), "/")
}

func compressEndpoint(base string) string {
	return strings.TrimRight(base, "/") + "/v1/compress"
}

// IsHeadroomAvailable probes GET <url>/health, cached for headroomHealthTTL.
// Not sync.Once like findRTKBinary: a remote service can come and go.
func IsHeadroomAvailable() bool {
	headroomHealthMu.Lock()
	defer headroomHealthMu.Unlock()

	if headroomHealthKnown && time.Now().Before(headroomHealthExpires) {
		return headroomHealthy
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	up := false
	req, err := http.NewRequestWithContext(ctx, "GET", headroomURL()+"/health", nil)
	if err == nil {
		if res, err := headroomClient.Do(req); err == nil {
			res.Body.Close()
			up = res.StatusCode == 200
		}
	}

	// Log on state transition only — this runs on every request.
	if !headroomHealthKnown || headroomHealthy != up {
		if up {
			log.Printf("[PAAP] Headroom reachable at %s", headroomURL())
		} else {
			log.Printf("[PAAP] Headroom unreachable at %s — compression stage skipped", headroomURL())
		}
	}
	headroomHealthy = up
	headroomHealthKnown = true
	headroomHealthExpires = time.Now().Add(headroomHealthTTL)
	return up
}

// CompressWithHeadroom compresses large tool outputs via Headroom's /v1/compress.
// Fail-open everywhere: any error returns the original messages untouched.
// model is the routed model name; Headroom uses it only to pick a tokenizer.
//
// Repeated tool outputs are deduped via an in-process sha256 cache: agent
// loops re-send the same tool returns every turn, and Headroom itself does no
// content-keyed caching (see PROFILE.md). A cache hit is ~0ms vs ~250ms.
//
// TODO: item #3 (send only candidates, map result back by index) would save
// ~177ms on long histories where message array padding is 180KB of useless
// context (see PROFILE.md sec "Where the 200 ms goes"). Needed careful index
// mapping so it survives Headroom reordering or dropping messages; deferred as
// higher-risk than #1 and #6.
func CompressWithHeadroom(messages []map[string]interface{}, model string) []map[string]interface{} {
	if getSettingStrCached("headroom_enabled", "false") != "true" {
		return messages
	}
	if !IsHeadroomAvailable() {
		return messages
	}

	// Candidates: string-content tool messages in the size window. Error traces
	// are never compressed — the model needs them verbatim.
	var candidates []int
	for i, msg := range messages {
		if role, _ := msg["role"].(string); role != "tool" {
			continue
		}
		if isErrorToolResult(msg) {
			continue
		}
		content, ok := msg["content"].(string)
		if !ok || len(content) < headroomMinCompressSize || len(content) > headroomMaxCompressSize {
			continue
		}
		candidates = append(candidates, i)
	}
	if len(candidates) == 0 {
		return messages
	}

	// ── Dedup cache: fill hits immediately, batch misses into one HTTP call ──
	type cached struct {
		key        string
		newContent string
		contentLen int
	}
	var hits []cached
	var missIdx []int // indexes into `candidates` that need a network round-trip
	for _, c := range candidates {
		content, _ := messages[c]["content"].(string)
		k := hrCacheKey(content)
		if comp, ok := hrCacheStore.get(k); ok {
			hits = append(hits, cached{k, comp, len(content)})
		} else {
			missIdx = append(missIdx, c)
		}
	}

	// Fast path: everything cached, no HTTP at all.
	if len(missIdx) == 0 {
		out := make([]map[string]interface{}, len(messages))
		copy(out, messages)
		for j, c := range candidates {
			h := hits[j]
			// Phantom guard per-entry: don't write back if it didn't actually shrink.
			if h.newContent == "" || len(h.newContent) >= h.contentLen {
				continue
			}
			cl := make(map[string]interface{}, len(messages[c]))
			for k, v := range messages[c] {
				cl[k] = v
			}
			cl["content"] = h.newContent
			out[c] = cl
		}
		return out
	}

	before, err := json.Marshal(messages)
	if err != nil {
		log.Printf("[PAAP] Headroom: cannot marshal request messages: %v", err)
		return messages
	}
	body, err := json.Marshal(map[string]interface{}{
		"messages": messages,
		"model":    model,
	})
	if err != nil {
		log.Printf("[PAAP] Headroom: cannot marshal request body: %v", err)
		return messages
	}

	timeout := time.Duration(getSettingInt("headroom_timeout_ms", headroomDefaultTimeoutMS)) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", compressEndpoint(headroomURL()), bytes.NewReader(body))
	if err != nil {
		log.Printf("[PAAP] Headroom: bad request: %v", err)
		return messages
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := headroomClient.Do(req)
	if err != nil {
		log.Printf("[PAAP] Headroom: request failed: %v", err)
		return messages
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		log.Printf("[PAAP] Headroom: HTTP %d — using original messages", res.StatusCode)
		return messages
	}

	var hr headroomResponse
	if err := json.NewDecoder(res.Body).Decode(&hr); err != nil {
		log.Printf("[PAAP] Headroom: malformed response: %v", err)
		return messages
	}
	if len(hr.Messages) != len(messages) {
		log.Printf("[PAAP] Headroom: message count mismatch (%d != %d) — discarded", len(hr.Messages), len(messages))
		return messages
	}
	for i := range messages {
		want, _ := messages[i]["role"].(string)
		got, _ := hr.Messages[i]["role"].(string)
		if want != got {
			log.Printf("[PAAP] Headroom: role order mismatch at %d (%q != %q) — discarded", i, got, want)
			return messages
		}
	}

	// Only candidate tool contents are taken. Every other message stays
	// byte-identical, so the system prompt PAAP injected upstream is untouched —
	// Headroom's CacheAligner must not fight PAAP for the prefix.
	out := make([]map[string]interface{}, len(messages))
	copy(out, messages)
	for _, i := range candidates {
		newContent, ok := hr.Messages[i]["content"].(string)
		old, _ := messages[i]["content"].(string)
		if !ok || newContent == "" || len(newContent) >= len(old) {
			continue
		}
		clone := make(map[string]interface{}, len(messages[i]))
		for k, v := range messages[i] {
			clone[k] = v
		}
		clone["content"] = newContent
		out[i] = clone
	}

	// Populate cache with entries we just fetched.
	for _, i := range missIdx {
		orig, _ := messages[i]["content"].(string)
		comp, _ := out[i]["content"].(string)
		if comp == "" || comp == orig {
			continue
		}
		hrCacheStore.set(hrCacheKey(orig), comp, len(orig))
	}

	after, err := json.Marshal(out)
	if err != nil {
		log.Printf("[PAAP] Headroom: cannot marshal result: %v", err)
		return messages
	}
	if float64(len(after)) >= float64(len(before))*headroomPhantomRatio {
		log.Printf("[PAAP] Headroom: rejected as phantom savings (%d -> %d bytes)", len(before), len(after))
		return messages
	}

	log.Printf("[PAAP] Headroom: %d -> %d bytes (%.1f%% saved)",
		len(before), len(after),
		float64(len(before)-len(after))/float64(len(before))*100)
	return out
}

// findHeadroomBinary locates the headroom CLI. Detected on demand, never cached
// in the DB: a stored path goes stale the moment a venv moves, and PAAP only
// needs it to print a copy-pasteable command — it never executes the binary.
// Search order covers the venvs and user-local dirs that a non-login systemd
// service's PATH misses.
func findHeadroomBinary() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/mnt/hdd/venv/bin/headroom",
		filepath.Join(home, ".local/bin/headroom"),
		filepath.Join(home, ".venv/bin/headroom"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("headroom"); err == nil {
		return p
	}
	return ""
}

// headroomStatus reports whether the compression stage can actually run, and if
// not, the exact command the user should run. Reachability is what decides — a
// remote sidecar works with no local binary, and a local binary is useless while
// the proxy is down.
func headroomStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, 405, "method not allowed")
		return
	}

	url := headroomURL()
	reachable := IsHeadroomAvailable()
	binary := findHeadroomBinary()

	resp := map[string]interface{}{
		"enabled":   getSettingStrCached("headroom_enabled", "false") == "true",
		"url":       url,
		"reachable": reachable,
		"installed": binary != "",
	}

	switch {
	case reachable:
		// Nothing to do. Settings TTL is 5s and the health probe caches for
		// 30s, so a proxy started just now is picked up on its own — no PAAP
		// restart needed.
	case binary == "":
		resp["hint"] = "Headroom belum terinstall"
		resp["command"] = "pip install headroom-ai"
	default:
		host, port := "127.0.0.1", "8787"
		if u, err := neturl.Parse(url); err == nil {
			if h := u.Hostname(); h != "" {
				host = h
			}
			if p := u.Port(); p != "" {
				port = p
			}
		}
		resp["hint"] = "Headroom terinstall tapi proxy belum jalan"
		resp["command"] = binary + " proxy --host " + host + " --port " + port
	}

	writeJSON(w, resp)
}
