package main

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// ── Token counters ───────────────────────────────────────────
//
// A request's input side is not one number. Providers report fresh prompt
// tokens, cache reads, and cache writes at different prices (Anthropic cache
// read is 0.1x input, cache write 1.25x). Collapsing them into a single
// tokens_in and pricing it at the input rate overcharges every cached request.
//
// tokens_in is retained in the DB as the context-window figure (TotalIn) so the
// dashboard and client-facing prompt_tokens keep their meaning. Billing reads
// the split fields only.

type tokenCounts struct {
	InFresh    int // prompt tokens the provider actually processed fresh
	CacheRead  int // prompt tokens served from provider cache
	CacheWrite int // prompt tokens written into provider cache
	Out        int // completion tokens excluding reasoning
	Reasoning  int // reasoning/thinking tokens billed at the output rate
	Estimated  bool
}

// TotalIn is the context-window input figure: everything the model saw.
func (t tokenCounts) TotalIn() int { return t.InFresh + t.CacheRead + t.CacheWrite }

// TotalOut is the billable output figure including reasoning tokens.
func (t tokenCounts) TotalOut() int { return t.Out + t.Reasoning }

// IsEmpty reports whether the provider gave us nothing to record.
func (t tokenCounts) IsEmpty() bool { return t.TotalIn() == 0 && t.TotalOut() == 0 }

// simpleTokens builds a tokenCounts from a legacy in/out pair. Call sites that
// have no cache or reasoning breakdown use this; everything lands in InFresh.
func simpleTokens(tokensIn, tokensOut int) tokenCounts {
	return tokenCounts{InFresh: tokensIn, Out: tokensOut}
}

// ── Pricing ──────────────────────────────────────────────────

// Multipliers applied when a model has no explicit cache price. These are the
// Anthropic published ratios and are the most common convention across
// providers that support prompt caching.
const (
	cacheReadMultiplier  = 0.10
	cacheWriteMultiplier = 1.25
)

// Pricing source labels stored in logs.pricing_source.
const (
	pricingSourceExact        = "exact"
	pricingSourceBase         = "base"
	pricingSourceGlobal       = "global"
	pricingSourcePrefix       = "prefix"
	pricingSourceFree         = "free"
	pricingSourceSubscription = "subscription"
	pricingSourceMissing      = "missing"
)

type pricingRate struct {
	InputPer1M      float64
	OutputPer1M     float64
	CacheReadPer1M  float64
	CacheWritePer1M float64
	Source          string
}

// pricingKey namespaces a rate by provider. providerID "" is the global tier.
func pricingKey(providerID, modelID string) string {
	return providerID + "\x00" + strings.ToLower(modelID)
}

var (
	pricingMu        sync.RWMutex
	pricingTable     map[string]pricingRate
	pricingGlobalKey []string // lowercase global-tier model IDs, longest first
	pricingLoadedAt  time.Time
)

const pricingTTL = 60 * time.Second

// Fallback rates for models absent from model_pricing. These are merged into the
// global tier of the lookup table, not consulted as a separate branch, so the
// resolution ladder stays single-pass and deterministic.
var modelPricingFallback = map[string][2]float64{
	"mimo-v2.5":                  {0.14, 0.28},
	"mimo-v2.5-pro":              {0.435, 0.87},
	"deepseek-v4-flash":          {0.14, 0.28},
	"deepseek-v4-pro":            {0.435, 0.87},
	"minimax-m3":                 {0.51, 2.04},
	"gemini-2.5-flash":           {0.15, 0.60},
	"gemini-2.5-pro":             {1.25, 10.00},
	"muse-spark-1.1":             {1.25, 4.25},
	"deepseek/deepseek-v4-flash": {0.098, 0.196},
	"deepseek/deepseek-v4-pro":   {0.435, 0.87},
	"moonshotai/kimi-k2.6":       {0.61, 3.07},
	"z-ai/glm-5.1":               {0.88, 2.80},
	"deepseek/deepseek-v3.1":     {0.19, 0.71},
	"deepseek/deepseek-v3.2":     {0.217, 0.326},
	"minimax/minimax-m2.5":       {0.14, 0.81},
	"qwen/qwen3.5-397b-a17b":     {0.40, 2.65},
	"z-ai/glm-5":                 {0.48, 1.54},
	"z-ai/glm-5.2":               {1.26, 3.96},
}

// loadPricingTable refreshes the in-memory pricing table from model_pricing.
// Cache prices left NULL in the DB are derived from the input rate here so the
// resolver never has to branch on NULL.
func loadPricingTable() {
	pricingMu.RLock()
	fresh := pricingTable != nil && time.Since(pricingLoadedAt) < pricingTTL
	pricingMu.RUnlock()
	if fresh {
		return
	}

	table := map[string]pricingRate{}

	if db.DB != nil {
		rows, err := db.DB.Query(`SELECT COALESCE(provider_id,''), model_id, input_per_1m, output_per_1m,
			cache_read_per_1m, cache_write_per_1m FROM model_pricing`)
		if err != nil {
			log.Printf("[PAAP] Failed to load model_pricing: %v", err)
		} else {
			for rows.Next() {
				var providerID, modelID string
				var in, out float64
				var cacheRead, cacheWrite *float64
				if err := rows.Scan(&providerID, &modelID, &in, &out, &cacheRead, &cacheWrite); err != nil {
					continue
				}
				r := pricingRate{InputPer1M: in, OutputPer1M: out}
				if cacheRead != nil {
					r.CacheReadPer1M = *cacheRead
				} else {
					r.CacheReadPer1M = in * cacheReadMultiplier
				}
				if cacheWrite != nil {
					r.CacheWritePer1M = *cacheWrite
				} else {
					r.CacheWritePer1M = in * cacheWriteMultiplier
				}
				table[pricingKey(providerID, modelID)] = r
			}
			rows.Close()
		}
	}

	// Merge fallbacks into the global tier without overriding DB rows.
	for modelID, p := range modelPricingFallback {
		k := pricingKey("", modelID)
		if _, exists := table[k]; exists {
			continue
		}
		table[k] = pricingRate{
			InputPer1M:      p[0],
			OutputPer1M:     p[1],
			CacheReadPer1M:  p[0] * cacheReadMultiplier,
			CacheWritePer1M: p[0] * cacheWriteMultiplier,
		}
	}

	// Global-tier model IDs sorted longest-first. Sorting (rather than iterating
	// the map) is what makes prefix resolution deterministic: Go randomizes map
	// iteration order, so the previous substring scan could resolve glm-5 to
	// glm-5.1 or glm-5.2 depending on the run.
	globalPrefix := "\x00"
	var keys []string
	for k := range table {
		if strings.HasPrefix(k, globalPrefix) {
			keys = append(keys, strings.TrimPrefix(k, globalPrefix))
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})

	pricingMu.Lock()
	pricingTable = table
	pricingGlobalKey = keys
	pricingLoadedAt = time.Now()
	pricingMu.Unlock()
	log.Printf("[PAAP] Loaded %d model pricing entries (%d global tier)", len(table), len(keys))
}

// InvalidatePricingCache forces the next lookup to re-read model_pricing.
func InvalidatePricingCache() {
	pricingMu.Lock()
	pricingTable = nil
	pricingMu.Unlock()
}

// modelBaseName returns the segment after the last '/', e.g.
// "deepseek/deepseek-v4-flash" -> "deepseek-v4-flash".
func modelBaseName(modelID string) string {
	if i := strings.LastIndex(modelID, "/"); i >= 0 && i+1 < len(modelID) {
		return modelID[i+1:]
	}
	return ""
}

// minPrefixLen guards against absurdly short keys matching everything.
const minPrefixLen = 6

// prefixSeparators are the characters allowed immediately after a prefix match.
// Requiring one prevents "gpt-4" from claiming "gpt-45-ultra" while still
// letting "claude-opus-5" price "claude-opus-5-thinking".
func isPrefixBoundary(b byte) bool {
	switch b {
	case '-', '.', ':', '/', '_', '@':
		return true
	}
	return false
}

// lookupPrefix finds the longest global-tier key that is a prefix of modelID at
// a name boundary. Deterministic: pricingGlobalKey is sorted longest-first.
func lookupPrefix(modelID string) (pricingRate, bool) {
	lower := strings.ToLower(modelID)
	pricingMu.RLock()
	defer pricingMu.RUnlock()
	for _, k := range pricingGlobalKey {
		if len(k) < minPrefixLen || len(k) >= len(lower) {
			continue
		}
		if !strings.HasPrefix(lower, k) {
			continue
		}
		if !isPrefixBoundary(lower[len(k)]) {
			continue
		}
		r := pricingTable[pricingKey("", k)]
		r.Source = pricingSourcePrefix
		return r, true
	}
	return pricingRate{}, false
}

func lookupExact(providerID, modelID, source string) (pricingRate, bool) {
	if modelID == "" {
		return pricingRate{}, false
	}
	pricingMu.RLock()
	r, ok := pricingTable[pricingKey(providerID, modelID)]
	pricingMu.RUnlock()
	if !ok {
		return pricingRate{}, false
	}
	r.Source = source
	return r, true
}

// isFreeModel matches naming conventions that mean zero cost regardless of any
// price row: OpenRouter ":free" variants and Cloudflare Workers AI "@cf/".
func isFreeModel(modelID string) bool {
	return strings.HasSuffix(modelID, ":free") || strings.HasPrefix(modelID, "@cf/")
}

// resolvePricing returns the rate for a (provider, model) pair and whether a
// real price was found. It never invents a rate: an unmatched model returns
// found=false so the caller records cost 0 with pricing_source="missing"
// instead of writing a fabricated number that is indistinguishable from data.
//
// Ladder, first hit wins:
//  1. provider billing mode is subscription or free -> zero rate
//  2. exact (providerID, modelID)
//  3. exact (providerID, base name)
//  4. free-by-convention model naming
//  5. exact (global, modelID)
//  6. exact (global, base name)
//  7. longest global-tier prefix at a name boundary
func resolvePricing(providerID, modelID string) (pricingRate, bool) {
	loadPricingTable()

	switch getProviderBillingMode(providerID) {
	case billingModeSubscription:
		return pricingRate{Source: pricingSourceSubscription}, true
	case billingModeFree:
		return pricingRate{Source: pricingSourceFree}, true
	}

	base := modelBaseName(modelID)

	if providerID != "" {
		if r, ok := lookupExact(providerID, modelID, pricingSourceExact); ok {
			return r, true
		}
		if r, ok := lookupExact(providerID, base, pricingSourceBase); ok {
			return r, true
		}
	}

	// Free-by-convention beats prefix guessing but not an explicit price row.
	if isFreeModel(modelID) {
		return pricingRate{Source: pricingSourceFree}, true
	}

	if r, ok := lookupExact("", modelID, pricingSourceGlobal); ok {
		return r, true
	}
	if r, ok := lookupExact("", base, pricingSourceGlobal); ok {
		return r, true
	}
	if r, ok := lookupPrefix(modelID); ok {
		return r, true
	}
	if base != "" {
		if r, ok := lookupPrefix(base); ok {
			return r, true
		}
	}

	return pricingRate{Source: pricingSourceMissing}, false
}

// calculateCost prices a request from its split token counters and reports which
// ladder rung produced the rate. An unpriced model yields (0, "missing") — the
// caller must record that rather than substituting an estimate.
func calculateCost(providerID, modelID string, t tokenCounts) (float64, string) {
	rate, found := resolvePricing(providerID, modelID)
	if !found {
		return 0, pricingSourceMissing
	}
	cost := float64(t.InFresh)/1_000_000*rate.InputPer1M +
		float64(t.CacheRead)/1_000_000*rate.CacheReadPer1M +
		float64(t.CacheWrite)/1_000_000*rate.CacheWritePer1M +
		float64(t.TotalOut())/1_000_000*rate.OutputPer1M
	return cost, rate.Source
}

// ── Provider billing mode ────────────────────────────────────

const (
	billingModePerToken     = "per_token"
	billingModeSubscription = "subscription"
	billingModeFree         = "free"
)

var (
	billingModeMu       sync.RWMutex
	billingModeCache    map[string]string
	billingModeLoadedAt time.Time
)

const billingModeTTL = 30 * time.Second

// getProviderBillingMode returns the provider's billing mode, defaulting to
// per_token. Cached because it is read on every logged request.
func getProviderBillingMode(providerID string) string {
	if providerID == "" {
		return billingModePerToken
	}
	billingModeMu.RLock()
	cache := billingModeCache
	fresh := cache != nil && time.Since(billingModeLoadedAt) < billingModeTTL
	billingModeMu.RUnlock()

	if !fresh {
		cache = loadBillingModes()
	}
	if mode, ok := cache[providerID]; ok && mode != "" {
		return mode
	}
	return billingModePerToken
}

func loadBillingModes() map[string]string {
	modes := map[string]string{}
	if db.DB == nil {
		return modes
	}
	rows, err := db.DB.Query("SELECT id, COALESCE(billing_mode,'per_token') FROM providers")
	if err != nil {
		log.Printf("[PAAP] Failed to load provider billing modes: %v", err)
		return modes
	}
	defer rows.Close()
	for rows.Next() {
		var id, mode string
		if err := rows.Scan(&id, &mode); err != nil {
			continue
		}
		modes[id] = mode
	}
	billingModeMu.Lock()
	billingModeCache = modes
	billingModeLoadedAt = time.Now()
	billingModeMu.Unlock()
	return modes
}

// InvalidateBillingModeCache forces the next lookup to re-read providers.
func InvalidateBillingModeCache() {
	billingModeMu.Lock()
	billingModeCache = nil
	billingModeMu.Unlock()
}
