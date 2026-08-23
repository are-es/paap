package main

import (
	"testing"

	"github.com/dolvin/paap/internal/db"
)

// setupPricingDB spins up a real SQLite DB with a known pricing table so the
// resolver is exercised against actual rows rather than a stubbed map.
//
// db.DB is package-global and TestMain opens a shared handle for the rest of the
// suite, so cleanup reopens that shared directory instead of leaving the global
// handle closed.
func setupPricingDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		if testSharedDBDir != "" {
			db.Init(testSharedDBDir)
		}
		InvalidatePricingCache()
		InvalidateBillingModeCache()
	})

	// Wipe scraped/seeded rows so the fixture is the only source of truth.
	if _, err := db.DB.Exec("DELETE FROM model_pricing"); err != nil {
		t.Fatalf("clear model_pricing: %v", err)
	}
	if _, err := db.DB.Exec("DELETE FROM providers"); err != nil {
		t.Fatalf("clear providers: %v", err)
	}

	rows := []struct {
		provider string
		model    string
		in, out  float64
	}{
		// Overlapping names: the exact defect that made the old substring scan
		// non-deterministic under Go's randomized map iteration.
		{"", "z-ai/glm-5", 0.48, 1.54},
		{"", "z-ai/glm-5.1", 0.88, 2.80},
		{"", "z-ai/glm-5.2", 1.26, 3.96},
		{"", "claude-opus-5", 6.5, 32.5},
		{"", "deepseek/deepseek-v4-flash", 0.098, 0.196},
		{"", "deepseek-v4-flash", 0.14, 0.28},
		// Provider-scoped override: same model name, different real price.
		{"prov-justwoker", "claude-opus-5", 3.0, 15.0},
	}
	for _, r := range rows {
		if _, err := db.DB.Exec(`INSERT INTO model_pricing (provider_id, model_id, input_per_1m, output_per_1m)
			VALUES (?, ?, ?, ?)`, r.provider, r.model, r.in, r.out); err != nil {
			t.Fatalf("insert pricing %s/%s: %v", r.provider, r.model, err)
		}
	}

	providers := []struct {
		id, mode string
	}{
		{"prov-justwoker", billingModePerToken},
		{"prov-anigravity", billingModeSubscription},
		{"prov-localfree", billingModeFree},
	}
	for _, p := range providers {
		if _, err := db.DB.Exec(`INSERT INTO providers (id, name, base_url, is_active, billing_mode)
			VALUES (?, ?, '', 1, ?)`, p.id, p.id, p.mode); err != nil {
			t.Fatalf("insert provider %s: %v", p.id, err)
		}
	}

	InvalidatePricingCache()
	InvalidateBillingModeCache()
}

// TestResolvePricingDeterministic runs the resolver repeatedly over names that
// overlap as substrings. The pre-fix implementation scanned a Go map with
// strings.Contains in both directions, so glm-5 could resolve to glm-5.1 or
// glm-5.2 depending on iteration order. Results must now be stable.
func TestResolvePricingDeterministic(t *testing.T) {
	setupPricingDB(t)

	models := []string{
		"z-ai/glm-5", "z-ai/glm-5.1", "z-ai/glm-5.2",
		"claude-opus-5", "claude-opus-5-thinking",
		"deepseek/deepseek-v4-flash", "gemini-3.7-flash-tiered",
	}

	type result struct {
		in, out float64
		source  string
		found   bool
	}
	first := map[string]result{}

	for i := 0; i < 100; i++ {
		// Force a fresh table load every few iterations so map ordering is
		// re-randomized by the runtime between reads.
		if i%10 == 0 {
			InvalidatePricingCache()
		}
		for _, m := range models {
			r, found := resolvePricing("", m)
			got := result{r.InputPer1M, r.OutputPer1M, r.Source, found}
			if i == 0 {
				first[m] = got
				continue
			}
			if first[m] != got {
				t.Fatalf("iteration %d: %s resolved to %+v, first run gave %+v", i, m, got, first[m])
			}
		}
	}

	// Exact matches must win over any prefix logic.
	if r := first["z-ai/glm-5"]; r.in != 0.48 || r.source != pricingSourceGlobal {
		t.Errorf("glm-5 = %+v, want in=0.48 source=global", r)
	}
	if r := first["z-ai/glm-5.1"]; r.in != 0.88 {
		t.Errorf("glm-5.1 input = %v, want 0.88", r.in)
	}
	if r := first["z-ai/glm-5.2"]; r.in != 1.26 {
		t.Errorf("glm-5.2 input = %v, want 1.26", r.in)
	}
	// Suffixed variant resolves via longest-prefix at a name boundary.
	if r := first["claude-opus-5-thinking"]; !r.found || r.in != 6.5 || r.source != pricingSourcePrefix {
		t.Errorf("claude-opus-5-thinking = %+v, want found in=6.5 source=prefix", r)
	}
	// Genuinely unknown model must stay unpriced.
	if r := first["gemini-3.7-flash-tiered"]; r.found {
		t.Errorf("gemini-3.7-flash-tiered resolved to %+v, want not found", r)
	}
}

// TestNoFabricatedPricing is the regression guard for the defect that produced a
// logged $80.115357 on gemini-3.7-flash-tiered: an unknown model silently priced
// at a hardcoded $1/$3 per 1M.
func TestNoFabricatedPricing(t *testing.T) {
	setupPricingDB(t)

	unknown := []string{"gemini-3.7-flash-tiered", "gemini-pro-agent", "totally-made-up-model"}
	for _, m := range unknown {
		rate, found := resolvePricing("", m)
		if found {
			t.Errorf("%s: resolvePricing found=%v rate=%+v, want not found", m, found, rate)
		}
		// The exact token counts from the real log row that exposed this.
		cost, source := calculateCost("", m, tokenCounts{InFresh: 79952946, Out: 54137})
		if cost != 0 {
			t.Errorf("%s: cost = %v, want 0 (was $80.115357 with the fabricated default rate)", m, cost)
		}
		if source != pricingSourceMissing {
			t.Errorf("%s: pricing_source = %q, want %q", m, source, pricingSourceMissing)
		}
	}
}

// TestProviderScopedPricingWins asserts a provider-specific rate beats the
// global tier, so a reseller price is no longer applied to unrelated providers.
func TestProviderScopedPricingWins(t *testing.T) {
	setupPricingDB(t)

	global, ok := resolvePricing("", "claude-opus-5")
	if !ok || global.InputPer1M != 6.5 {
		t.Fatalf("global claude-opus-5 = %+v ok=%v, want in=6.5", global, ok)
	}
	scoped, ok := resolvePricing("prov-justwoker", "claude-opus-5")
	if !ok {
		t.Fatal("provider-scoped claude-opus-5 not found")
	}
	if scoped.InputPer1M != 3.0 || scoped.Source != pricingSourceExact {
		t.Errorf("scoped = %+v, want in=3.0 source=exact", scoped)
	}
}

// TestSubscriptionAndFreeProvidersCostZero covers OAuth/CLI providers such as
// Anigravity and Codex, which are not billed per token at all.
func TestSubscriptionAndFreeProvidersCostZero(t *testing.T) {
	setupPricingDB(t)

	big := tokenCounts{InFresh: 79952946, Out: 54137}

	cost, source := calculateCost("prov-anigravity", "claude-opus-5", big)
	if cost != 0 || source != pricingSourceSubscription {
		t.Errorf("subscription provider: cost=%v source=%q, want 0/%q", cost, source, pricingSourceSubscription)
	}

	cost, source = calculateCost("prov-localfree", "claude-opus-5", big)
	if cost != 0 || source != pricingSourceFree {
		t.Errorf("free provider: cost=%v source=%q, want 0/%q", cost, source, pricingSourceFree)
	}

	// A metered provider with the same model must still be charged.
	cost, source = calculateCost("prov-justwoker", "claude-opus-5", big)
	if cost <= 0 || source != pricingSourceExact {
		t.Errorf("per_token provider: cost=%v source=%q, want >0/%q", cost, source, pricingSourceExact)
	}
}

// TestCacheReadCheaperThanFresh asserts the cache split actually changes the
// price. Previously tokens_in folded cache reads into fresh input and billed
// them all at the input rate.
func TestCacheReadCheaperThanFresh(t *testing.T) {
	setupPricingDB(t)

	const total = 200000
	allFresh, _ := calculateCost("", "claude-opus-5", tokenCounts{InFresh: total})
	mostlyCached, _ := calculateCost("", "claude-opus-5", tokenCounts{InFresh: 1000, CacheRead: total - 1000})

	if mostlyCached >= allFresh {
		t.Fatalf("cached cost %v >= fresh cost %v; cache read must be cheaper", mostlyCached, allFresh)
	}
	// Default derivation is 0.1x input, so a fully cached prompt lands near 10%.
	ratio := mostlyCached / allFresh
	if ratio < 0.09 || ratio > 0.15 {
		t.Errorf("cached/fresh ratio = %.4f, want ~0.10 (cacheReadMultiplier)", ratio)
	}

	// Cache writes are more expensive than fresh input (1.25x).
	cacheWrite, _ := calculateCost("", "claude-opus-5", tokenCounts{CacheWrite: total})
	if cacheWrite <= allFresh {
		t.Errorf("cache write cost %v <= fresh %v, want 1.25x", cacheWrite, allFresh)
	}
}

// TestReasoningTokensBilledAsOutput asserts reasoning tokens are not free.
func TestReasoningTokensBilledAsOutput(t *testing.T) {
	setupPricingDB(t)

	withoutReasoning, _ := calculateCost("", "claude-opus-5", tokenCounts{InFresh: 1000, Out: 500})
	withReasoning, _ := calculateCost("", "claude-opus-5", tokenCounts{InFresh: 1000, Out: 500, Reasoning: 500})

	if withReasoning <= withoutReasoning {
		t.Fatalf("reasoning tokens did not add cost: %v vs %v", withReasoning, withoutReasoning)
	}
	// 500 reasoning tokens at the output rate.
	want := withoutReasoning + 500.0/1_000_000*32.5
	if diff := withReasoning - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost with reasoning = %v, want %v", withReasoning, want)
	}
}

// TestFreeModelNaming covers the :free and @cf/ conventions.
func TestFreeModelNaming(t *testing.T) {
	setupPricingDB(t)

	for _, m := range []string{"some/model:free", "@cf/meta/llama-3-8b"} {
		cost, source := calculateCost("", m, tokenCounts{InFresh: 1_000_000, Out: 1_000_000})
		if cost != 0 || source != pricingSourceFree {
			t.Errorf("%s: cost=%v source=%q, want 0/%q", m, cost, source, pricingSourceFree)
		}
	}
}

// TestPrefixMatchRespectsBoundary asserts prefix resolution cannot claim an
// unrelated longer name (gpt-4 must not price gpt-45-ultra).
func TestPrefixMatchRespectsBoundary(t *testing.T) {
	setupPricingDB(t)

	if _, err := db.DB.Exec(`INSERT INTO model_pricing (provider_id, model_id, input_per_1m, output_per_1m)
		VALUES ('', 'gpt-4o', 2.5, 10.0)`); err != nil {
		t.Fatalf("insert gpt-4o: %v", err)
	}
	InvalidatePricingCache()

	// Boundary-separated suffix: allowed.
	if r, ok := resolvePricing("", "gpt-4o-mini-2026"); !ok || r.InputPer1M != 2.5 {
		t.Errorf("gpt-4o-mini-2026 = %+v ok=%v, want in=2.5", r, ok)
	}
	// Glued suffix with no separator: must not match.
	if r, ok := resolvePricing("", "gpt-4oxturbo"); ok {
		t.Errorf("gpt-4oxturbo resolved to %+v, want not found", r)
	}
}

// TestTokenCountsArithmetic locks the meaning of the aggregate accessors.
func TestTokenCountsArithmetic(t *testing.T) {
	tc := tokenCounts{InFresh: 100, CacheRead: 200, CacheWrite: 50, Out: 30, Reasoning: 20}
	if got := tc.TotalIn(); got != 350 {
		t.Errorf("TotalIn() = %d, want 350", got)
	}
	if got := tc.TotalOut(); got != 50 {
		t.Errorf("TotalOut() = %d, want 50", got)
	}
	if tc.IsEmpty() {
		t.Error("IsEmpty() = true for populated counts")
	}
	if !(tokenCounts{}).IsEmpty() {
		t.Error("IsEmpty() = false for zero counts")
	}
	simple := simpleTokens(10, 5)
	if simple.InFresh != 10 || simple.Out != 5 || simple.CacheRead != 0 {
		t.Errorf("simpleTokens(10,5) = %+v", simple)
	}
}
