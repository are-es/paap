package main

import (
	"testing"

	"github.com/dolvin/paap/internal/db"
)

// insertLegacyLog writes a row shaped like one written by the pre-migration
// logger: pricing_source='legacy', no split token columns.
func insertLegacyLog(t *testing.T, providerID, modelID string, tokensIn, tokensOut int, oldCost float64) {
	t.Helper()
	_, err := db.DB.Exec(`INSERT INTO logs
		(provider_id, provider_name, model_id, status_code, tokens_in, tokens_out,
		 latency_ms, cost_usd, pricing_source)
		VALUES (?, ?, ?, 200, ?, ?, 100, ?, 'legacy')`,
		providerID, providerID, modelID, tokensIn, tokensOut, oldCost)
	if err != nil {
		t.Fatalf("insert legacy log: %v", err)
	}
}

// redirectTrashDir points reconcile snapshots at a temp directory so tests do not
// write backups into the repository.
func redirectTrashDir(t *testing.T) {
	t.Helper()
	original := trashDir
	trashDir = t.TempDir()
	t.Cleanup(func() { trashDir = original })
}

// TestReconcileZeroesFabricatedCost reproduces the real defect: a
// gemini-3.7-flash-tiered row logged $80.115357 from the fabricated $1/$3 default
// rate. After reconciliation that row must read $0 with pricing_source='missing'.
func TestReconcileZeroesFabricatedCost(t *testing.T) {
	setupPricingDB(t)
	redirectTrashDir(t)

	// Exact token counts and cost from the production log row.
	insertLegacyLog(t, "prov-anigravity-metered", "gemini-3.7-flash-tiered", 79952946, 54137, 80.115357)

	res, err := reconcileHistoricalCost(false)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if res.LegacyRowsFound != 1 {
		t.Fatalf("legacy rows found = %d, want 1", res.LegacyRowsFound)
	}
	if res.RowsZeroed != 1 {
		t.Errorf("rows zeroed = %d, want 1", res.RowsZeroed)
	}
	if res.BackupPath == "" {
		t.Error("no backup path recorded; reconcile must snapshot before writing")
	}

	var cost float64
	var source string
	if err := db.DB.QueryRow("SELECT cost_usd, pricing_source FROM logs ORDER BY id DESC LIMIT 1").
		Scan(&cost, &source); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if cost != 0 {
		t.Errorf("cost_usd = %v, want 0 (the $80.12 was fabricated)", cost)
	}
	if source != pricingSourceMissing {
		t.Errorf("pricing_source = %q, want %q", source, pricingSourceMissing)
	}
}

// TestReconcileRepricesKnownModel asserts a legacy row for a model that now
// resolves to a real provider-scoped price is recomputed rather than left alone.
func TestReconcileRepricesKnownModel(t *testing.T) {
	setupPricingDB(t)
	redirectTrashDir(t)

	// Legacy cost used the global 6.5/32.5 reseller rate; the provider-scoped
	// price is 3.0/15.0.
	insertLegacyLog(t, "prov-justwoker", "claude-opus-5", 1_000_000, 100_000, 9.75)

	res, err := reconcileHistoricalCost(false)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RowsRepriced != 1 {
		t.Errorf("rows repriced = %d, want 1", res.RowsRepriced)
	}

	var cost float64
	var source string
	if err := db.DB.QueryRow("SELECT cost_usd, pricing_source FROM logs ORDER BY id DESC LIMIT 1").
		Scan(&cost, &source); err != nil {
		t.Fatalf("read back: %v", err)
	}
	// 1M fresh input at $3 + 100k output at $15 = 3.00 + 1.50 = 4.50
	if diff := cost - 4.5; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost_usd = %v, want 4.50 at the provider-scoped rate", cost)
	}
	if source != pricingSourceExact {
		t.Errorf("pricing_source = %q, want %q", source, pricingSourceExact)
	}
}

// TestReconcileSubscriptionProviderZeroed asserts OAuth/CLI provider history is
// zeroed, since those requests were never billed per token.
func TestReconcileSubscriptionProviderZeroed(t *testing.T) {
	setupPricingDB(t)
	redirectTrashDir(t)

	insertLegacyLog(t, "prov-anigravity", "claude-opus-5", 20_000_000, 25_000, 130.81)

	if _, err := reconcileHistoricalCost(false); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var cost float64
	var source string
	if err := db.DB.QueryRow("SELECT cost_usd, pricing_source FROM logs ORDER BY id DESC LIMIT 1").
		Scan(&cost, &source); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if cost != 0 || source != pricingSourceSubscription {
		t.Errorf("cost=%v source=%q, want 0/%q", cost, source, pricingSourceSubscription)
	}
}

// TestReconcileIsIdempotent asserts a second run finds nothing, because
// reconciled rows no longer carry the 'legacy' marker.
func TestReconcileIsIdempotent(t *testing.T) {
	setupPricingDB(t)
	redirectTrashDir(t)

	insertLegacyLog(t, "prov-justwoker", "claude-opus-5", 1_000_000, 10_000, 6.83)

	first, err := reconcileHistoricalCost(false)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if first.LegacyRowsFound != 1 {
		t.Fatalf("first run found %d legacy rows, want 1", first.LegacyRowsFound)
	}

	second, err := reconcileHistoricalCost(false)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if second.LegacyRowsFound != 0 {
		t.Errorf("second run found %d legacy rows, want 0 (must be idempotent)", second.LegacyRowsFound)
	}
}

// TestReconcileDryRunWritesNothing asserts dry_run reports without mutating.
func TestReconcileDryRunWritesNothing(t *testing.T) {
	setupPricingDB(t)
	redirectTrashDir(t)

	insertLegacyLog(t, "prov-anigravity-metered", "gemini-3.7-flash-tiered", 79952946, 54137, 80.115357)

	res, err := reconcileHistoricalCost(true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !res.DryRun {
		t.Error("DryRun flag not set on the result")
	}
	if res.BackupPath != "" {
		t.Errorf("dry run took a backup (%s); it must not write anything", res.BackupPath)
	}
	if res.LegacyRowsFound != 1 || res.RowsZeroed != 1 {
		t.Errorf("dry run reported found=%d zeroed=%d, want 1/1", res.LegacyRowsFound, res.RowsZeroed)
	}

	var cost float64
	var source string
	if err := db.DB.QueryRow("SELECT cost_usd, pricing_source FROM logs ORDER BY id DESC LIMIT 1").
		Scan(&cost, &source); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if source != "legacy" {
		t.Errorf("pricing_source = %q after dry run, want unchanged 'legacy'", source)
	}
	if diff := cost - 80.115357; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost_usd = %v after dry run, want unchanged 80.115357", cost)
	}
}

// TestReconcileReportsTotals asserts the before/after totals are reported so the
// operator understands why spend dropped.
func TestReconcileReportsTotals(t *testing.T) {
	setupPricingDB(t)
	redirectTrashDir(t)

	insertLegacyLog(t, "prov-anigravity-metered", "gemini-3.7-flash-tiered", 79952946, 54137, 80.115357)
	insertLegacyLog(t, "prov-anigravity", "claude-opus-5", 20_000_000, 25_000, 130.81)
	insertLegacyLog(t, "prov-justwoker", "claude-opus-5", 1_000_000, 100_000, 9.75)

	res, err := reconcileHistoricalCost(true)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	wantOld := 80.115357 + 130.81 + 9.75
	if diff := res.OldTotalUSD - wantOld; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("OldTotalUSD = %v, want %v", res.OldTotalUSD, wantOld)
	}
	// Only the metered Justwoker row survives repricing: 4.50.
	if diff := res.NewTotalUSD - 4.5; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("NewTotalUSD = %v, want 4.50", res.NewTotalUSD)
	}
	if res.NewTotalUSD >= res.OldTotalUSD {
		t.Error("new total is not lower than the old fabricated total")
	}
	if res.CostSummaryNote == "" {
		t.Error("CostSummaryNote empty; the aggregate-table caveat must be reported")
	}
}
