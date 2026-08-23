package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// countLogRows returns the number of rows currently in the logs table.
func countLogRows(t *testing.T) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM logs").Scan(&n); err != nil {
		t.Fatalf("count logs: %v", err)
	}
	return n
}

// anthropicSSEUpstream builds a mock Anthropic-style SSE upstream. When
// sendDone is true it also appends a `data: [DONE]` sentinel, which real
// Anthropic never sends but OpenAI-compatible gateways in front of it do. That
// combination used to produce two logs rows for one request.
func anthropicSSEUpstream(sendDone bool, inputTokens, cacheRead, cacheWrite, outputTokens int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)

		emit := func(payload string) {
			fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher != nil {
				flusher.Flush()
			}
		}

		emit(fmt.Sprintf(`{"type":"message_start","message":{"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d,"output_tokens":0}}}`,
			inputTokens, cacheRead, cacheWrite))
		emit(`{"type":"content_block_start","content_block":{"type":"text"}}`)
		emit(`{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`)
		emit(`{"type":"content_block_stop"}`)
		emit(fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":%d}}`, outputTokens))
		emit(`{"type":"message_stop"}`)
		if sendDone {
			emit("[DONE]")
		}
	}))
}

// runAnthropicNativeStream drives handleAnthropicNativeFromOpenAI against a mock
// upstream and returns the proxied response body.
func runAnthropicNativeStream(t *testing.T, upstreamURL, providerID, modelID string) string {
	t.Helper()
	reqBody := map[string]interface{}{
		"model":    modelID,
		"stream":   true,
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}
	body, _ := json.Marshal(reqBody)
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	handleAnthropicNativeFromOpenAI(w, r, reqBody,
		providerID, "MockAnthropic", upstreamURL, modelID,
		"key-1", "mock-key", "sk-test", "",
		true, time.Now(), nil)

	return w.Body.String()
}

// TestAnthropicStreamSingleLogRow is the regression guard for the duplicate
// write: the in-loop [DONE] handler used to call logProxyRequest and then fall
// through to the post-loop logger, so a gateway that emits both message_stop and
// data: [DONE] produced two logs rows and double-counted cost_summary.
func TestAnthropicStreamSingleLogRow(t *testing.T) {
	setupPricingDB(t)

	tests := []struct {
		name     string
		sendDone bool
	}{
		{"upstream sends message_stop only", false},
		{"upstream sends message_stop and DONE", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := anthropicSSEUpstream(tt.sendDone, 1000, 0, 0, 50)
			defer upstream.Close()

			before := countLogRows(t)
			out := runAnthropicNativeStream(t, upstream.URL, "prov-justwoker", "claude-opus-5")
			after := countLogRows(t)

			if got := after - before; got != 1 {
				t.Errorf("logs rows written = %d, want exactly 1", got)
			}
			if n := strings.Count(out, "data: [DONE]"); n != 1 {
				t.Errorf("[DONE] sentinel emitted %d times, want exactly 1", n)
			}
		})
	}
}

// TestAnthropicStreamCacheTokensRecorded asserts the three input figures are
// stored separately and that cached reads make the request cheaper than the same
// volume of fresh input.
func TestAnthropicStreamCacheTokensRecorded(t *testing.T) {
	setupPricingDB(t)

	const fresh, cacheRead, cacheWrite, out = 1000, 200000, 5000, 50

	upstream := anthropicSSEUpstream(false, fresh, cacheRead, cacheWrite, out)
	defer upstream.Close()

	body := runAnthropicNativeStream(t, upstream.URL, "prov-justwoker", "claude-opus-5")

	var gotFresh, gotRead, gotWrite, gotOut, gotIn int
	var cost float64
	var pricingSource string
	err := db.DB.QueryRow(`SELECT tokens_in_fresh, tokens_cache_read, tokens_cache_write,
		tokens_out, tokens_in, cost_usd, pricing_source
		FROM logs ORDER BY id DESC LIMIT 1`).
		Scan(&gotFresh, &gotRead, &gotWrite, &gotOut, &gotIn, &cost, &pricingSource)
	if err != nil {
		t.Fatalf("read back log row: %v", err)
	}

	if gotFresh != fresh {
		t.Errorf("tokens_in_fresh = %d, want %d", gotFresh, fresh)
	}
	if gotRead != cacheRead {
		t.Errorf("tokens_cache_read = %d, want %d", gotRead, cacheRead)
	}
	if gotWrite != cacheWrite {
		t.Errorf("tokens_cache_write = %d, want %d", gotWrite, cacheWrite)
	}
	if gotOut != out {
		t.Errorf("tokens_out = %d, want %d", gotOut, out)
	}
	// tokens_in stays the context-window sum for the dashboard and clients.
	if want := fresh + cacheRead + cacheWrite; gotIn != want {
		t.Errorf("tokens_in = %d, want %d (fresh+cache_read+cache_write)", gotIn, want)
	}
	if pricingSource != pricingSourceExact {
		t.Errorf("pricing_source = %q, want %q", pricingSource, pricingSourceExact)
	}

	// Same token volume billed entirely as fresh input must cost more.
	allFresh, _ := calculateCost("prov-justwoker", "claude-opus-5",
		tokenCounts{InFresh: fresh + cacheRead + cacheWrite, Out: out})
	if cost >= allFresh {
		t.Errorf("cost with cache split = %v, want less than all-fresh cost %v", cost, allFresh)
	}

	// The client-facing usage chunk must expose the real cached count, not 0.
	if !strings.Contains(body, `"cached_tokens":200000`) {
		t.Errorf("usage chunk missing cached_tokens=200000; got body:\n%s", body)
	}
}

// TestAnthropicStreamSubscriptionProviderZeroCost asserts an OAuth/CLI provider
// records zero cost even with a large token volume.
func TestAnthropicStreamSubscriptionProviderZeroCost(t *testing.T) {
	setupPricingDB(t)

	upstream := anthropicSSEUpstream(false, 500000, 0, 0, 1000)
	defer upstream.Close()

	runAnthropicNativeStream(t, upstream.URL, "prov-anigravity", "claude-opus-5")

	var cost float64
	var pricingSource string
	var tokensIn int
	if err := db.DB.QueryRow(`SELECT cost_usd, pricing_source, tokens_in FROM logs ORDER BY id DESC LIMIT 1`).
		Scan(&cost, &pricingSource, &tokensIn); err != nil {
		t.Fatalf("read back log row: %v", err)
	}
	if cost != 0 {
		t.Errorf("cost_usd = %v, want 0 for a subscription provider", cost)
	}
	if pricingSource != pricingSourceSubscription {
		t.Errorf("pricing_source = %q, want %q", pricingSource, pricingSourceSubscription)
	}
	// Tokens are still recorded — only the price is zero.
	if tokensIn != 500000 {
		t.Errorf("tokens_in = %d, want 500000 (usage must still be tracked)", tokensIn)
	}
}
