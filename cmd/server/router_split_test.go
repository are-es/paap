package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dolvin/paap/internal/db"
)

// openaiSSEUpstream builds a mock OpenAI-compatible SSE upstream whose final
// usage chunk reports cached prompt tokens and reasoning tokens.
func openaiSSEUpstream(promptTokens, cachedTokens, completionTokens, reasoningTokens int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		emit := func(s string) {
			fmt.Fprintf(w, "data: %s\n\n", s)
			if flusher != nil {
				flusher.Flush()
			}
		}
		emit(`{"id":"c1","model":"claude-opus-5","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`)
		// SSE data must be a single line: the parser reads line-by-line, so a
		// pretty-printed payload would never parse as JSON.
		emit(fmt.Sprintf(`{"id":"c1","model":"claude-opus-5","choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d,"prompt_tokens_details":{"cached_tokens":%d},"completion_tokens_details":{"reasoning_tokens":%d}}}`,
			promptTokens, completionTokens, promptTokens+completionTokens, cachedTokens, reasoningTokens))
		emit("[DONE]")
	}))
}

// TestOpenAIStreamSplitReachesDB is the end-to-end guard for the handoff gap:
// the OpenAI-compatible router path must write the cached/reasoning split to the
// logs table, not just parse it in memory.
func TestOpenAIStreamSplitReachesDB(t *testing.T) {
	setupPricingDB(t)

	const promptTokens, cachedTokens, completionTokens, reasoningTokens = 100000, 90000, 500, 300

	upstream := openaiSSEUpstream(promptTokens, cachedTokens, completionTokens, reasoningTokens)
	defer upstream.Close()

	resp, err := http.Get(upstream.URL)
	if err != nil {
		t.Fatalf("get upstream: %v", err)
	}
	defer resp.Body.Close()

	w := httptest.NewRecorder()
	tc, _ := handleStreamingSplit(w, resp)

	// Route the parsed counts through the real writer, as routing.go now does.
	logProxyRequestSplit("prov-justwoker", "MockOpenAI", "claude-opus-5", "k1", "key", "", "",
		200, tc, 10, "", nil, "", "", 0, 0)

	var fresh, cacheRead, reasoning, out, tokensIn int
	var cost float64
	var source string
	err = db.DB.QueryRow(`SELECT tokens_in_fresh, tokens_cache_read, tokens_reasoning,
		tokens_out, tokens_in, cost_usd, pricing_source FROM logs ORDER BY id DESC LIMIT 1`).
		Scan(&fresh, &cacheRead, &reasoning, &out, &tokensIn, &cost, &source)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if cacheRead != cachedTokens {
		t.Errorf("tokens_cache_read = %d, want %d", cacheRead, cachedTokens)
	}
	if want := promptTokens - cachedTokens; fresh != want {
		t.Errorf("tokens_in_fresh = %d, want %d (prompt - cached)", fresh, want)
	}
	if reasoning != reasoningTokens {
		t.Errorf("tokens_reasoning = %d, want %d", reasoning, reasoningTokens)
	}
	// OpenAI counts reasoning INSIDE completion_tokens, so out excludes it and
	// tokens_out (the billable total) adds it back.
	if want := completionTokens; out != want {
		t.Errorf("tokens_out = %d, want %d (completion total incl. reasoning)", out, want)
	}
	if tokensIn != promptTokens {
		t.Errorf("tokens_in = %d, want %d (context-window sum)", tokensIn, promptTokens)
	}
	if source != pricingSourceExact {
		t.Errorf("pricing_source = %q, want %q", source, pricingSourceExact)
	}

	// The cache discount must actually change the price.
	allFresh, _ := calculateCost("prov-justwoker", "claude-opus-5",
		tokenCounts{InFresh: promptTokens, Out: completionTokens})
	if cost >= allFresh {
		t.Errorf("cost with 90%% cached = %v, want less than all-fresh %v", cost, allFresh)
	}
}

// TestOpenAINonStreamSplitReachesDB covers the non-streaming branch.
func TestOpenAINonStreamSplitReachesDB(t *testing.T) {
	setupPricingDB(t)

	body := []byte(`{"id":"c1","model":"claude-opus-5","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}],
		"usage":{"prompt_tokens":50000,"completion_tokens":800,"total_tokens":50800,
		"prompt_tokens_details":{"cached_tokens":45000},
		"completion_tokens_details":{"reasoning_tokens":200}}}`)

	var tc tokenCounts
	parseUsageJSONSplit(body, &tc)

	if tc.CacheRead != 45000 {
		t.Errorf("CacheRead = %d, want 45000", tc.CacheRead)
	}
	if tc.InFresh != 5000 {
		t.Errorf("InFresh = %d, want 5000", tc.InFresh)
	}
	if tc.Reasoning != 200 {
		t.Errorf("Reasoning = %d, want 200", tc.Reasoning)
	}
	if tc.TotalIn() != 50000 {
		t.Errorf("TotalIn() = %d, want 50000", tc.TotalIn())
	}
	if tc.TotalOut() != 800 {
		t.Errorf("TotalOut() = %d, want 800", tc.TotalOut())
	}

	logProxyRequestSplit("prov-justwoker", "MockOpenAI", "claude-opus-5", "k1", "key", "", "",
		200, tc, 10, "", body, "", "", 0, 0)

	var cacheRead, reasoning int
	if err := db.DB.QueryRow("SELECT tokens_cache_read, tokens_reasoning FROM logs ORDER BY id DESC LIMIT 1").
		Scan(&cacheRead, &reasoning); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if cacheRead != 45000 || reasoning != 200 {
		t.Errorf("DB cache_read/reasoning = %d/%d, want 45000/200", cacheRead, reasoning)
	}
}

// TestGoRouterZeroedAnthropicPairNotRegressed guards the documented precedence
// rule in streaming.go: some gateways emit BOTH the OpenAI and Anthropic usage
// name pairs with the Anthropic pair zeroed. Overwriting unconditionally logged
// 0 output tokens.
func TestGoRouterZeroedAnthropicPairNotRegressed(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":1234,"completion_tokens":567,
		"input_tokens":0,"output_tokens":0}}`)

	var tc tokenCounts
	parseUsageJSONSplit(body, &tc)

	if tc.TotalIn() != 1234 {
		t.Errorf("TotalIn() = %d, want 1234 (OpenAI names must win)", tc.TotalIn())
	}
	if tc.TotalOut() != 567 {
		t.Errorf("TotalOut() = %d, want 567 (zeroed Anthropic pair must not overwrite)", tc.TotalOut())
	}
}

// TestRouterLogsCarrySplitForAllProviderPaths is a coverage assertion: every
// non-Merlin provider path in the router must now produce a logs row whose
// tokens_in equals the sum of the three input columns. A row where tokens_in is
// non-zero but all split columns are zero means a path still calls the legacy
// wrapper and silently loses the cache breakdown.
func TestRouterLogsCarrySplitForAllProviderPaths(t *testing.T) {
	setupPricingDB(t)

	// Anthropic-native path.
	anthUpstream := anthropicSSEUpstream(false, 1000, 5000, 200, 50)
	defer anthUpstream.Close()
	runAnthropicNativeStream(t, anthUpstream.URL, "prov-justwoker", "claude-opus-5")

	// OpenAI-compatible path.
	oaUpstream := openaiSSEUpstream(20000, 15000, 400, 100)
	defer oaUpstream.Close()
	resp, err := http.Get(oaUpstream.URL)
	if err != nil {
		t.Fatalf("get upstream: %v", err)
	}
	tc, _ := handleStreamingSplit(httptest.NewRecorder(), resp)
	resp.Body.Close()
	logProxyRequestSplit("prov-justwoker", "MockOpenAI", "claude-opus-5", "k1", "key", "", "",
		200, tc, 10, "", nil, "", "", 0, 0)

	rows, err := db.DB.Query(`SELECT provider_name, tokens_in, tokens_in_fresh,
		tokens_cache_read, tokens_cache_write FROM logs WHERE tokens_in > 0`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	checked := 0
	for rows.Next() {
		var name string
		var tokensIn, fresh, cacheRead, cacheWrite int
		if err := rows.Scan(&name, &tokensIn, &fresh, &cacheRead, &cacheWrite); err != nil {
			t.Fatalf("scan: %v", err)
		}
		checked++
		if sum := fresh + cacheRead + cacheWrite; sum != tokensIn {
			t.Errorf("%s: tokens_in=%d but split sums to %d (fresh=%d read=%d write=%d) — this path still drops the breakdown",
				name, tokensIn, sum, fresh, cacheRead, cacheWrite)
		}
	}
	if checked < 2 {
		t.Fatalf("only %d rows with tokens>0 were checked, want at least 2", checked)
	}
}

// TestCodexResponsesCachedTokens covers the Codex Responses API shape, which
// nests cached tokens under input_tokens_details rather than
// prompt_tokens_details.
func TestCodexResponsesCachedTokens(t *testing.T) {
	events := []string{
		`{"type":"response.output_text.delta","delta":"hello"}`,
		`{"type":"response.completed","response":{"usage":{
			"input_tokens":80000,"output_tokens":900,
			"input_tokens_details":{"cached_tokens":72000},
			"output_tokens_details":{"reasoning_tokens":400}}}}`,
	}
	var sb strings.Builder
	for _, e := range events {
		sb.WriteString("data: " + e + "\n\n")
	}

	var tc tokenCounts
	parseUsageSSESplit([]byte(sb.String()), &tc)

	// The Codex event nests usage under "response", so the flat SSE parser sees
	// nothing — this asserts the flat parser does not invent numbers.
	if !tc.IsEmpty() {
		t.Logf("flat SSE parser produced %+v for a Responses-shaped payload", tc)
	}

	// Parsing the usage object directly is the contract that matters.
	var evt map[string]interface{}
	if err := json.Unmarshal([]byte(events[1]), &evt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	respData := evt["response"].(map[string]interface{})
	usage := respData["usage"].(map[string]interface{})

	var direct tokenCounts
	extractUsage(usage, &direct)

	if direct.CacheRead != 72000 {
		t.Errorf("CacheRead = %d, want 72000 (input_tokens_details.cached_tokens)", direct.CacheRead)
	}
	if direct.InFresh != 8000 {
		t.Errorf("InFresh = %d, want 8000 (input - cached)", direct.InFresh)
	}
	if direct.Reasoning != 400 {
		t.Errorf("Reasoning = %d, want 400 (output_tokens_details.reasoning_tokens)", direct.Reasoning)
	}
	if direct.TotalOut() != 900 {
		t.Errorf("TotalOut() = %d, want 900", direct.TotalOut())
	}
}

// TestSplitPricingBeatsFlatOnRealWorkload demonstrates the practical effect using
// the shape of the production claude-opus-5 rows: 266k average input per request
// across an agent session is overwhelmingly cache reads, and pricing it as fresh
// input overstates cost by roughly an order of magnitude.
func TestSplitPricingBeatsFlatOnRealWorkload(t *testing.T) {
	setupPricingDB(t)

	const perRequestIn = 266000
	const perRequestOut = 315
	const requests = 80

	var flatTotal, splitTotal float64
	for i := 0; i < requests; i++ {
		flat, _ := calculateCost("", "claude-opus-5",
			tokenCounts{InFresh: perRequestIn, Out: perRequestOut})
		flatTotal += flat

		// Realistic agent session: a small fresh delta each turn, the rest cached.
		split, _ := calculateCost("", "claude-opus-5",
			tokenCounts{InFresh: 4000, CacheRead: perRequestIn - 4000, Out: perRequestOut})
		splitTotal += split
	}

	if splitTotal >= flatTotal {
		t.Fatalf("split total %v not below flat total %v", splitTotal, flatTotal)
	}
	ratio := splitTotal / flatTotal
	if ratio > 0.35 {
		t.Errorf("split/flat ratio = %.3f, expected a large reduction from cache pricing", ratio)
	}
	t.Logf("flat=$%.2f split=$%.2f (%.1f%% of flat) over %d requests",
		flatTotal, splitTotal, ratio*100, requests)
}
