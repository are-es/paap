package db

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// legacyPricingSchema mirrors the pre-migration model_pricing shape that exists
// in deployed databases: single-column primary key, plus the two columns added
// out-of-band by the pricing scraper.
const legacyPricingSchema = `CREATE TABLE model_pricing (
	model_id TEXT PRIMARY KEY,
	input_per_1m REAL NOT NULL,
	output_per_1m REAL NOT NULL,
	source TEXT DEFAULT 'scraped',
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`

// TestMigrateModelPricingKeyFromLegacy builds a database with the legacy
// model_pricing shape, runs Init twice, and asserts the rebuild preserved every
// row, moved them to the global tier, and is idempotent.
func TestMigrateModelPricingKeyFromLegacy(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "paap.db")

	seed := legacyPricingSchema + `
	INSERT INTO model_pricing (model_id, input_per_1m, output_per_1m, source) VALUES
		('claude-opus-5', 6.5, 32.5, 'aimlapi'),
		('z-ai/glm-5', 0.48, 1.54, 'aimlapi'),
		('z-ai/glm-5.1', 0.88, 2.80, 'aimlapi');`
	cmd := exec.Command("sqlite3", dbPath)
	cmd.Stdin = stringsReader(seed)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("sqlite3 CLI unavailable, skipping legacy-shape test: %v (%s)", err, out)
	}

	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM model_pricing").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("row count after migration = %d, want 3", count)
	}

	var globalRows int
	if err := DB.QueryRow("SELECT COUNT(*) FROM model_pricing WHERE provider_id=''").Scan(&globalRows); err != nil {
		t.Fatalf("global tier count: %v", err)
	}
	if globalRows != 3 {
		t.Fatalf("rows in global tier = %d, want 3", globalRows)
	}

	// Values and source must survive the rebuild untouched.
	var in, out float64
	var source string
	if err := DB.QueryRow("SELECT input_per_1m, output_per_1m, source FROM model_pricing WHERE model_id='z-ai/glm-5.1'").
		Scan(&in, &out, &source); err != nil {
		t.Fatalf("scan glm-5.1: %v", err)
	}
	if in != 0.88 || out != 2.80 || source != "aimlapi" {
		t.Fatalf("glm-5.1 = %v/%v/%q, want 0.88/2.8/\"aimlapi\"", in, out, source)
	}

	// Cache columns exist and default to NULL so callers can derive them.
	var cacheRead, cacheWrite *float64
	if err := DB.QueryRow("SELECT cache_read_per_1m, cache_write_per_1m FROM model_pricing WHERE model_id='claude-opus-5'").
		Scan(&cacheRead, &cacheWrite); err != nil {
		t.Fatalf("scan cache cols: %v", err)
	}
	if cacheRead != nil || cacheWrite != nil {
		t.Fatalf("cache columns = %v/%v, want NULL/NULL", cacheRead, cacheWrite)
	}

	// Provider-scoped rows must be insertable alongside the global tier.
	if _, err := DB.Exec(`INSERT INTO model_pricing (provider_id, model_id, input_per_1m, output_per_1m, source)
		VALUES ('builtin-justwoker', 'claude-opus-5', 3.0, 15.0, 'manual')`); err != nil {
		t.Fatalf("insert provider-scoped row: %v", err)
	}

	// Second Init must be a no-op, not a second rebuild.
	Close()
	if err := Init(dir); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if err := DB.QueryRow("SELECT COUNT(*) FROM model_pricing").Scan(&count); err != nil {
		t.Fatalf("count after second Init: %v", err)
	}
	if count != 4 {
		t.Fatalf("row count after second Init = %d, want 4", count)
	}
	Close()
}

// TestMigrationAddsTokenSplitColumns asserts the additive column set landed on a
// fresh database.
func TestMigrationAddsTokenSplitColumns(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	want := map[string][]string{
		"logs": {
			"tokens_in_fresh", "tokens_cache_read", "tokens_cache_write",
			"tokens_reasoning", "tokens_estimated", "pricing_source",
		},
		"usage_stats": {
			"tokens_in_fresh", "tokens_cache_read", "tokens_cache_write",
			"tokens_reasoning", "unpriced_req_count",
		},
		"cost_summary": {
			"total_tokens_in_fresh", "total_tokens_cache_read",
			"total_tokens_cache_write", "total_tokens_reasoning", "unpriced_req_count",
		},
		"providers": {"billing_mode"},
		"model_pricing": {
			"provider_id", "cache_read_per_1m", "cache_write_per_1m",
		},
	}

	for table, cols := range want {
		have := tableColumns(t, table)
		for _, c := range cols {
			if !have[c] {
				t.Errorf("%s is missing column %s", table, c)
			}
		}
	}
}

func tableColumns(t *testing.T, table string) map[string]bool {
	t.Helper()
	rows, err := DB.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, ctype string
		var dflt *string
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		cols[name] = true
	}
	return cols
}

// stringsReader avoids importing strings just for one reader in tests.
func stringsReader(s string) *os.File {
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	go func() {
		w.WriteString(s)
		w.Close()
	}()
	return r
}
