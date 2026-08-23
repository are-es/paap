package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// ── Historical cost reconciliation ───────────────────────────
//
// Every logs row written before the token-billing migration was priced by the
// old resolver: provider-blind lookups, a randomized substring fallback, and a
// fabricated $1/$3-per-1M default for unknown models. Those numbers are not
// measurements and must not be silently blended with corrected ones.
//
// The migration marked those rows pricing_source='legacy'. This endpoint
// recomputes them against the current pricing table and reports exactly what
// changed, including what could not be recovered.

type reconcileResult struct {
	DryRun bool `json:"dry_run"`

	BackupPath string `json:"backup_path,omitempty"`

	LegacyRowsFound int `json:"legacy_rows_found"`
	RowsRepriced    int `json:"rows_repriced"`
	RowsZeroed      int `json:"rows_zeroed"`
	RowsUnchanged   int `json:"rows_unchanged"`

	OldTotalUSD float64 `json:"old_total_usd"`
	NewTotalUSD float64 `json:"new_total_usd"`

	// CostSummaryRebuilt reports whether the aggregate tables could be rebuilt
	// from logs. logs auto-prunes to the newest 500 rows while cost_summary does
	// not, so older periods have no source data to recompute from.
	CostSummaryRebuilt bool    `json:"cost_summary_rebuilt"`
	CostSummaryNote    string  `json:"cost_summary_note"`
	UnrecoverableUSD   float64 `json:"unrecoverable_usd"`

	BySource map[string]int `json:"by_source"`
}

// reconcileCostHandler handles POST /api/logs/reconcile.
//
// Query parameters:
//
//	dry_run=1  compute and report without writing (default: writes)
func reconcileCostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	dryRun := r.URL.Query().Get("dry_run") == "1"

	res, err := reconcileHistoricalCost(dryRun)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// reconcileHistoricalCost recomputes cost_usd for legacy rows. Idempotent:
// reconciled rows lose the 'legacy' marker, so a second run finds nothing to do.
func reconcileHistoricalCost(dryRun bool) (*reconcileResult, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("database not initialised")
	}

	res := &reconcileResult{DryRun: dryRun, BySource: map[string]int{}}

	if !dryRun {
		path, err := snapshotToTrash()
		if err != nil {
			return nil, fmt.Errorf("refusing to reconcile without a backup: %w", err)
		}
		res.BackupPath = path
	}

	type legacyRow struct {
		id         int
		providerID string
		modelID    string
		oldCost    float64
		counts     tokenCounts
	}

	rows, err := db.DB.Query(`SELECT id, COALESCE(provider_id,''), COALESCE(model_id,''), COALESCE(cost_usd,0),
		COALESCE(tokens_in_fresh,0), COALESCE(tokens_cache_read,0), COALESCE(tokens_cache_write,0),
		COALESCE(tokens_out,0), COALESCE(tokens_reasoning,0), COALESCE(tokens_in,0)
		FROM logs WHERE COALESCE(pricing_source,'') = 'legacy'`)
	if err != nil {
		return nil, err
	}

	var pending []legacyRow
	for rows.Next() {
		var lr legacyRow
		var tokensIn int
		if err := rows.Scan(&lr.id, &lr.providerID, &lr.modelID, &lr.oldCost,
			&lr.counts.InFresh, &lr.counts.CacheRead, &lr.counts.CacheWrite,
			&lr.counts.Out, &lr.counts.Reasoning, &tokensIn); err != nil {
			rows.Close()
			return nil, err
		}
		// Legacy rows predate the split columns, so the breakdown is empty. The
		// only recoverable input figure is the old tokens_in, which conflated
		// fresh and cached tokens — attribute it to fresh and note that cache
		// discounts cannot be recovered for these rows.
		if lr.counts.TotalIn() == 0 && tokensIn > 0 {
			lr.counts.InFresh = tokensIn
		}
		pending = append(pending, lr)
	}
	rows.Close()

	res.LegacyRowsFound = len(pending)

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for _, lr := range pending {
		newCost, source := calculateCost(lr.providerID, lr.modelID, lr.counts)
		res.OldTotalUSD += lr.oldCost
		res.NewTotalUSD += newCost
		res.BySource[source]++

		switch {
		case source == pricingSourceMissing:
			res.RowsZeroed++
		case newCost != lr.oldCost:
			res.RowsRepriced++
		default:
			res.RowsUnchanged++
		}

		if dryRun {
			continue
		}
		if _, err := tx.Exec("UPDATE logs SET cost_usd=?, pricing_source=? WHERE id=?",
			newCost, source, lr.id); err != nil {
			return nil, err
		}
	}

	// Rebuild the aggregates from the rows we still have. logs is pruned to 500
	// rows, so anything older is simply gone; report the gap instead of faking a
	// backfill.
	var aggregateCost float64
	tx.QueryRow("SELECT COALESCE(SUM(total_cost_usd),0) FROM cost_summary").Scan(&aggregateCost)

	var logsCost float64
	tx.QueryRow("SELECT COALESCE(SUM(cost_usd),0) FROM logs").Scan(&logsCost)

	var oldestLog, oldestSummary string
	tx.QueryRow("SELECT COALESCE(MIN(date(timestamp)),'') FROM logs").Scan(&oldestLog)
	tx.QueryRow("SELECT COALESCE(MIN(date),'') FROM cost_summary").Scan(&oldestSummary)

	if oldestSummary != "" && oldestLog != "" && oldestSummary < oldestLog {
		res.CostSummaryRebuilt = false
		res.UnrecoverableUSD = aggregateCost
		res.CostSummaryNote = fmt.Sprintf(
			"cost_summary starts %s but logs only reach back to %s (logs auto-prune to 500 rows). "+
				"Aggregate totals for the missing period cannot be recomputed and remain legacy values.",
			oldestSummary, oldestLog)
	} else if !dryRun {
		if err := rebuildCostSummaryFromLogs(tx); err != nil {
			return nil, err
		}
		res.CostSummaryRebuilt = true
		res.CostSummaryNote = "cost_summary rebuilt from logs"
	} else {
		res.CostSummaryNote = "cost_summary would be rebuilt from logs"
	}

	if !dryRun {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		log.Printf("[PAAP] reconcile: %d legacy rows, %d repriced, %d zeroed, old=$%.2f new=$%.2f",
			res.LegacyRowsFound, res.RowsRepriced, res.RowsZeroed, res.OldTotalUSD, res.NewTotalUSD)
	}

	return res, nil
}

// rebuildCostSummaryFromLogs recomputes cost_summary from the surviving logs
// rows. Only called when logs covers the full cost_summary date range.
func rebuildCostSummaryFromLogs(tx *sql.Tx) error {
	if _, err := tx.Exec("DELETE FROM cost_summary"); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO cost_summary
		(id, date, provider_id, provider_name, model_id, req_count, total_cost_usd,
		 total_tokens_in, total_tokens_out,
		 total_tokens_in_fresh, total_tokens_cache_read, total_tokens_cache_write,
		 total_tokens_reasoning, unpriced_req_count)
		SELECT
			lower(hex(randomblob(8))),
			date(timestamp),
			COALESCE(provider_id,''),
			COALESCE(provider_name,''),
			COALESCE(model_id,''),
			COUNT(*),
			COALESCE(SUM(cost_usd),0),
			COALESCE(SUM(tokens_in),0),
			COALESCE(SUM(tokens_out),0),
			COALESCE(SUM(tokens_in_fresh),0),
			COALESCE(SUM(tokens_cache_read),0),
			COALESCE(SUM(tokens_cache_write),0),
			COALESCE(SUM(tokens_reasoning),0),
			COALESCE(SUM(CASE WHEN pricing_source='missing' THEN 1 ELSE 0 END),0)
		FROM logs
		WHERE COALESCE(race_status,'') = ''
		GROUP BY date(timestamp), COALESCE(provider_id,''), COALESCE(model_id,'')`)
	return err
}

// trashDir is where reconcile snapshots are written before destructive writes.
// Overridden in tests so they do not litter the repository.
var trashDir = ".trash"

// snapshotToTrash writes a consistent database snapshot into the project .trash
// directory before a destructive operation. VACUUM INTO is required rather than
// a file copy: the connection runs in WAL mode, so recent writes live in the
// -wal sidecar and a raw copy can produce an empty-looking database.
func snapshotToTrash() (string, error) {
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return "", err
	}
	// Nanosecond precision: two reconcile runs in the same second must not
	// collide, and VACUUM INTO refuses to overwrite an existing path.
	path := filepath.Join(trashDir, fmt.Sprintf("paap.db.reconcile-%d", time.Now().UnixNano()))
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("backup path already exists: %s", abs)
	}
	if _, err := db.DB.Exec("VACUUM INTO ?", abs); err != nil {
		return "", err
	}
	log.Printf("[PAAP] reconcile backup written to %s", abs)
	return abs, nil
}
