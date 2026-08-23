package main

import "testing"

// TestAnigravityGeminiTokenCounts covers the Gemini usage -> tokenCounts mapping.
// The important cases are a thinking response (thoughts + cached content both
// non-zero) and the clamp that keeps fresh input from going negative.
func TestAnigravityGeminiTokenCounts(t *testing.T) {
	cases := []struct {
		name  string
		usage *geminiUsageMetadata
		want  tokenCounts
	}{
		{
			name:  "nil usage yields zero counts",
			usage: nil,
			want:  tokenCounts{},
		},
		{
			name: "thinking response with cached prompt",
			usage: &geminiUsageMetadata{
				PromptTokenCount:        12000,
				CandidatesTokenCount:    800,
				ThoughtsTokenCount:      1500,
				CachedContentTokenCount: 10000,
				TotalTokenCount:         14300,
			},
			want: tokenCounts{InFresh: 2000, CacheRead: 10000, Out: 800, Reasoning: 1500},
		},
		{
			name: "no cache no thinking",
			usage: &geminiUsageMetadata{
				PromptTokenCount:     500,
				CandidatesTokenCount: 120,
				TotalTokenCount:      620,
			},
			want: tokenCounts{InFresh: 500, Out: 120},
		},
		{
			name: "cached larger than prompt clamps fresh input at zero",
			usage: &geminiUsageMetadata{
				PromptTokenCount:        900,
				CandidatesTokenCount:    50,
				CachedContentTokenCount: 1000,
				TotalTokenCount:         950,
			},
			want: tokenCounts{InFresh: 0, CacheRead: 1000, Out: 50},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := geminiTokenCounts(tc.usage)
			if got != tc.want {
				t.Fatalf("geminiTokenCounts() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestAnigravityGeminiUsageMapThinking asserts the client-facing usage object:
// completion_tokens must include thinking tokens so it reconciles with
// total_tokens, while reasoning_tokens and cached_tokens stay available as
// sub-counters.
func TestAnigravityGeminiUsageMapThinking(t *testing.T) {
	usage := &geminiUsageMetadata{
		PromptTokenCount:        12000,
		CandidatesTokenCount:    800,
		ThoughtsTokenCount:      1500,
		CachedContentTokenCount: 10000,
		TotalTokenCount:         14300,
	}

	got := geminiUsageMap(usage)

	if got["prompt_tokens"] != 12000 {
		t.Errorf("prompt_tokens = %v, want 12000", got["prompt_tokens"])
	}
	if got["completion_tokens"] != 2300 {
		t.Errorf("completion_tokens = %v, want 2300 (candidates+thoughts)", got["completion_tokens"])
	}
	if got["total_tokens"] != 14300 {
		t.Errorf("total_tokens = %v, want 14300", got["total_tokens"])
	}

	// completion_tokens + prompt_tokens must equal total_tokens for the client.
	if got["prompt_tokens"].(int)+got["completion_tokens"].(int) != got["total_tokens"].(int) {
		t.Errorf("client usage does not reconcile: %+v", got)
	}

	details, ok := got["completion_tokens_details"].(map[string]interface{})
	if !ok {
		t.Fatalf("completion_tokens_details missing: %+v", got)
	}
	if details["reasoning_tokens"] != 1500 {
		t.Errorf("reasoning_tokens = %v, want 1500", details["reasoning_tokens"])
	}

	promptDetails, ok := got["prompt_tokens_details"].(map[string]interface{})
	if !ok {
		t.Fatalf("prompt_tokens_details missing: %+v", got)
	}
	if promptDetails["cached_tokens"] != 10000 {
		t.Errorf("cached_tokens = %v, want 10000", promptDetails["cached_tokens"])
	}
}

// TestAnigravityGeminiUsageMapOmitsEmptyDetails checks that the optional detail
// blocks are not emitted when the provider reports zero.
func TestAnigravityGeminiUsageMapOmitsEmptyDetails(t *testing.T) {
	got := geminiUsageMap(&geminiUsageMetadata{
		PromptTokenCount:     100,
		CandidatesTokenCount: 40,
		TotalTokenCount:      140,
	})

	if _, present := got["completion_tokens_details"]; present {
		t.Errorf("completion_tokens_details should be absent without thinking tokens: %+v", got)
	}
	if _, present := got["prompt_tokens_details"]; present {
		t.Errorf("prompt_tokens_details should be absent without cached tokens: %+v", got)
	}
	if got["completion_tokens"] != 40 {
		t.Errorf("completion_tokens = %v, want 40", got["completion_tokens"])
	}

	if geminiUsageMap(nil) != nil {
		t.Errorf("geminiUsageMap(nil) should be nil")
	}
}

// TestAnigravityGeminiUsageReconciles asserts the reconciliation identity
// prompt + candidates + thoughts == total, including the cases that are treated
// as reconciled because there is nothing to verify.
func TestAnigravityGeminiUsageReconciles(t *testing.T) {
	cases := []struct {
		name  string
		usage *geminiUsageMetadata
		want  bool
	}{
		{
			name:  "nil usage is reconciled",
			usage: nil,
			want:  true,
		},
		{
			name:  "zero total is reconciled",
			usage: &geminiUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 5},
			want:  true,
		},
		{
			name: "thinking response reconciles",
			usage: &geminiUsageMetadata{
				PromptTokenCount:        12000,
				CandidatesTokenCount:    800,
				ThoughtsTokenCount:      1500,
				CachedContentTokenCount: 10000,
				TotalTokenCount:         14300,
			},
			want: true,
		},
		{
			name: "dropping thoughts breaks the identity",
			usage: &geminiUsageMetadata{
				PromptTokenCount:     12000,
				CandidatesTokenCount: 800,
				ThoughtsTokenCount:   1500,
				TotalTokenCount:      12800,
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := geminiUsageReconciles(tc.usage); got != tc.want {
				t.Fatalf("geminiUsageReconciles() = %v, want %v", got, tc.want)
			}
			// A mismatch must only warn, never panic or alter the request.
			warnGeminiUsageMismatch("gemini-test", tc.usage)
		})
	}
}

// TestAnigravityGeminiCountsMatchTotal ties the split counters back to Gemini's
// own total: TotalIn() + TotalOut() must equal totalTokenCount for a thinking
// response with a cache hit.
func TestAnigravityGeminiCountsMatchTotal(t *testing.T) {
	usage := &geminiUsageMetadata{
		PromptTokenCount:        12000,
		CandidatesTokenCount:    800,
		ThoughtsTokenCount:      1500,
		CachedContentTokenCount: 10000,
		TotalTokenCount:         14300,
	}

	counts := geminiTokenCounts(usage)

	if counts.TotalIn() != usage.PromptTokenCount {
		t.Errorf("TotalIn() = %d, want %d", counts.TotalIn(), usage.PromptTokenCount)
	}
	if counts.TotalOut() != usage.CandidatesTokenCount+usage.ThoughtsTokenCount {
		t.Errorf("TotalOut() = %d, want %d", counts.TotalOut(), usage.CandidatesTokenCount+usage.ThoughtsTokenCount)
	}
	if counts.TotalIn()+counts.TotalOut() != usage.TotalTokenCount {
		t.Errorf("split counters sum to %d, want totalTokenCount %d",
			counts.TotalIn()+counts.TotalOut(), usage.TotalTokenCount)
	}
	if counts.Reasoning == 0 || counts.CacheRead == 0 {
		t.Errorf("thinking response must record reasoning and cache-read tokens: %+v", counts)
	}
}
