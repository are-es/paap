package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestExtractUsageOpenAICachedAndReasoning covers the OpenAI chat-completions
// shape: cached prompt tokens live in prompt_tokens_details.cached_tokens and
// reasoning tokens are counted INSIDE completion_tokens.
func TestExtractUsageOpenAICachedAndReasoning(t *testing.T) {
	body := []byte(`{
		"usage": {
			"prompt_tokens": 12000,
			"completion_tokens": 2300,
			"total_tokens": 14300,
			"prompt_tokens_details": {"cached_tokens": 10000},
			"completion_tokens_details": {"reasoning_tokens": 1500}
		}
	}`)

	var got tokenCounts
	parseUsageJSONSplit(body, &got)

	want := tokenCounts{InFresh: 2000, CacheRead: 10000, Out: 800, Reasoning: 1500}
	if got != want {
		t.Fatalf("parseUsageJSONSplit() = %+v, want %+v", got, want)
	}
	if got.TotalIn() != 12000 {
		t.Errorf("TotalIn() = %d, want 12000", got.TotalIn())
	}
	if got.TotalOut() != 2300 {
		t.Errorf("TotalOut() = %d, want 2300", got.TotalOut())
	}
}

// TestExtractUsageGoRouterZeroedAnthropicPair guards the precedence rule
// documented on extractUsage: some gateways (go-router) emit BOTH the OpenAI and
// the Anthropic field names, with the Anthropic pair zeroed. The zeroed
// input_tokens/output_tokens must not overwrite the real values.
func TestExtractUsageGoRouterZeroedAnthropicPair(t *testing.T) {
	usage := map[string]interface{}{
		"prompt_tokens":     float64(910),
		"completion_tokens": float64(430),
		"total_tokens":      float64(1340),
		"input_tokens":      float64(0),
		"output_tokens":     float64(0),
	}

	var got tokenCounts
	extractUsage(usage, &got)

	if got.TotalOut() != 430 {
		t.Fatalf("output tokens zeroed by Anthropic fallback: TotalOut() = %d, want 430", got.TotalOut())
	}
	if got.TotalIn() != 910 {
		t.Fatalf("input tokens zeroed by Anthropic fallback: TotalIn() = %d, want 910", got.TotalIn())
	}
	want := tokenCounts{InFresh: 910, Out: 430}
	if got != want {
		t.Fatalf("extractUsage() = %+v, want %+v", got, want)
	}
}

// TestExtractUsageAnthropicNamesAreFallback confirms the Anthropic names still
// work when the OpenAI names are absent.
func TestExtractUsageAnthropicNamesAreFallback(t *testing.T) {
	usage := map[string]interface{}{
		"input_tokens":  float64(77),
		"output_tokens": float64(21),
	}
	var got tokenCounts
	extractUsage(usage, &got)
	want := tokenCounts{InFresh: 77, Out: 21}
	if got != want {
		t.Fatalf("extractUsage() = %+v, want %+v", got, want)
	}
}

// TestExtractUsageClampsNegativeSplits covers providers that report a cached or
// reasoning count larger than the parent total.
func TestExtractUsageClampsNegativeSplits(t *testing.T) {
	usage := map[string]interface{}{
		"prompt_tokens":             float64(900),
		"completion_tokens":         float64(50),
		"prompt_tokens_details":     map[string]interface{}{"cached_tokens": float64(1000)},
		"completion_tokens_details": map[string]interface{}{"reasoning_tokens": float64(80)},
	}
	var got tokenCounts
	extractUsage(usage, &got)
	want := tokenCounts{InFresh: 0, CacheRead: 1000, Out: 0, Reasoning: 80}
	if got != want {
		t.Fatalf("extractUsage() = %+v, want %+v", got, want)
	}
}

// TestExtractUsageSSEStreamSplit checks the SSE variant picks up the split from
// the final usage-bearing chunk.
func TestExtractUsageSSEStreamSplit(t *testing.T) {
	body := []byte(strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"hi"}}]}`,
		`data: {"usage":{"prompt_tokens":500,"completion_tokens":300,"prompt_tokens_details":{"cached_tokens":120},"completion_tokens_details":{"reasoning_tokens":200}}}`,
		`data: [DONE]`,
		``,
	}, "\n"))

	var got tokenCounts
	parseUsageSSESplit(body, &got)
	want := tokenCounts{InFresh: 380, CacheRead: 120, Out: 100, Reasoning: 200}
	if got != want {
		t.Fatalf("parseUsageSSESplit() = %+v, want %+v", got, want)
	}
}

// TestParseUsageLegacyWrappersTotals confirms the legacy int-pair wrappers still
// report the flat context-window and billable-output totals.
func TestParseUsageLegacyWrappersTotals(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":12000,"completion_tokens":2300,` +
		`"prompt_tokens_details":{"cached_tokens":10000},` +
		`"completion_tokens_details":{"reasoning_tokens":1500}}}`)

	var tokensIn, tokensOut int
	parseUsageJSON(body, &tokensIn, &tokensOut)
	if tokensIn != 12000 || tokensOut != 2300 {
		t.Fatalf("parseUsageJSON() = (%d, %d), want (12000, 2300)", tokensIn, tokensOut)
	}
}

// TestCodexResponseCompletedCachedTokens covers the Codex Responses API shape on
// the non-streaming collect path: input_tokens_details.cached_tokens and
// output_tokens_details.reasoning_tokens.
func TestCodexResponseCompletedCachedTokens(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"usage":{` +
			`"input_tokens":8000,"output_tokens":1200,` +
			`"input_tokens_details":{"cached_tokens":7000},` +
			`"output_tokens_details":{"reasoning_tokens":900}}}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	resp := &http.Response{StatusCode: 200, Body: newStringReadCloser(sse)}
	w := httptest.NewRecorder()

	got := handleCodexNonStreamingResponse(w, resp, "gpt-5-codex")

	want := tokenCounts{InFresh: 1000, CacheRead: 7000, Out: 300, Reasoning: 900}
	if got != want {
		t.Fatalf("handleCodexNonStreamingResponse() = %+v, want %+v", got, want)
	}

	var chatResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &chatResp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	usage, ok := chatResp["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("no usage object in response: %s", w.Body.String())
	}
	if usage["prompt_tokens"] != float64(8000) {
		t.Errorf("prompt_tokens = %v, want 8000", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != float64(1200) {
		t.Errorf("completion_tokens = %v, want 1200", usage["completion_tokens"])
	}
	if usage["total_tokens"] != float64(9200) {
		t.Errorf("total_tokens = %v, want 9200", usage["total_tokens"])
	}
}

// TestCodexStreamingResponseCachedTokens covers the same split on the streaming
// translation path and checks the client-facing usage chunk keeps flat totals.
func TestCodexStreamingResponseCachedTokens(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		`data: {"type":"response.completed","response":{"usage":{` +
			`"input_tokens":500,"output_tokens":400,` +
			`"input_tokens_details":{"cached_tokens":300},` +
			`"output_tokens_details":{"reasoning_tokens":250}}}}`,
		``,
	}, "\n")

	resp := &http.Response{StatusCode: 200, Body: newStringReadCloser(sse)}
	w := httptest.NewRecorder()

	got := handleCodexStreamingResponse(w, resp, "gpt-5-codex")

	want := tokenCounts{InFresh: 200, CacheRead: 300, Out: 150, Reasoning: 250}
	if got != want {
		t.Fatalf("handleCodexStreamingResponse() = %+v, want %+v", got, want)
	}
	out := w.Body.String()
	if !strings.Contains(out, `"prompt_tokens":500`) {
		t.Errorf("usage chunk missing flat prompt_tokens: %s", out)
	}
	if !strings.Contains(out, `"completion_tokens":400`) {
		t.Errorf("usage chunk missing flat completion_tokens: %s", out)
	}
	if !strings.Contains(out, `"total_tokens":900`) {
		t.Errorf("usage chunk missing flat total_tokens: %s", out)
	}
}

func newStringReadCloser(s string) *stringReadCloser {
	return &stringReadCloser{Reader: strings.NewReader(s)}
}

type stringReadCloser struct {
	*strings.Reader
}

func (s *stringReadCloser) Close() error { return nil }
