package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// ── Sub-router for /api/logs/* ──────────────────────────────

func logRoutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	switch trimmed {
	case "cost":
		logCostSummary(w, r)
	case "export":
		logExport(w, r)
	default:
		writeError(w, 404, "unknown logs endpoint")
	}
}

// ── GET /api/logs — list with filters + pagination ──────────

func logList(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, 405, "method not allowed")
		return
	}
	q := r.URL.Query()

	// Pagination
	page := 1
	if p := q.Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	perPage := 500
	if l := q.Get("per_page"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			perPage = v
			if perPage > 500 {
				perPage = 500
			}
		}
	}
	offset := (page - 1) * perPage

	// Build WHERE clause
	var conds []string
	var args []interface{}

	if p := q.Get("provider"); p != "" {
		conds = append(conds, "provider_id = ?")
		args = append(args, p)
	}
	if m := q.Get("model"); m != "" {
		conds = append(conds, "model_id = ?")
		args = append(args, m)
	}
	if s := q.Get("status"); s != "" {
		filters := map[string]string{
			"success": "status_code >= 200 AND status_code < 300",
			"200":     "status_code >= 200 AND status_code < 300",
			"2xx":     "status_code >= 200 AND status_code < 300",
			"error":   "(status_code IS NULL OR status_code >= 400)",
			"4xx":     "status_code >= 400 AND status_code < 500",
			"5xx":     "status_code >= 500",
		}
		if cond, ok := filters[s]; ok {
			conds = append(conds, cond)
		} else if v, err := strconv.Atoi(s); err == nil {
			conds = append(conds, "status_code = ?")
			args = append(args, v)
		}
	}
	if from := q.Get("from"); from != "" {
		conds = append(conds, "timestamp >= ?")
		args = append(args, from)
	}
	if to := q.Get("to"); to != "" {
		conds = append(conds, "timestamp <= ?")
		args = append(args, to)
	}
	if search := q.Get("search"); search != "" {
		conds = append(conds, "(provider_name LIKE ? OR model_id LIKE ? OR key_name LIKE ? OR error LIKE ?)")
		like := "%" + search + "%"
		args = append(args, like, like, like, like)
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	// Count total
	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	db.DB.QueryRow("SELECT COUNT(*) FROM logs"+where, countArgs...).Scan(&total)

	// Fetch page
	query := `SELECT id, timestamp, COALESCE(provider_id,''), COALESCE(provider_name,''),
		COALESCE(model_id,''), COALESCE(key_id,''), COALESCE(key_name,''),
		COALESCE(group_name,''), COALESCE(framework,''),
		status_code, COALESCE(race_status,''), COALESCE(race_id,''),
		tokens_in, tokens_out, latency_ms, cost_usd,
		COALESCE(compression_ratio,0), COALESCE(skills_used,'[]'),
		COALESCE(error,''), COALESCE(proxy_used,''),
		COALESCE(tool_used,''), COALESCE(original_model,''),
		COALESCE(tokens_in_fresh,0), COALESCE(tokens_cache_read,0),
		COALESCE(tokens_cache_write,0), COALESCE(tokens_reasoning,0),
		COALESCE(tokens_estimated,0), COALESCE(pricing_source,'')
		FROM logs` + where + " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, perPage, offset)

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, tokensIn, tokensOut, latency int
		var ts, providerID, providerName, modelID, keyID, keyName string
		var groupName, framework, raceStatus, raceID string
		var skillsUsed, errMsg, proxyUsed string
		var toolUsed, originalModel string
		var statusCode *int
		var cost, compRatio float64
		var tokensInFresh, tokensCacheRead, tokensCacheWrite, tokensReasoning int
		var tokensEstimated int
		var pricingSource string

		rows.Scan(&id, &ts, &providerID, &providerName, &modelID, &keyID, &keyName,
			&groupName, &framework, &statusCode, &raceStatus, &raceID,
			&tokensIn, &tokensOut, &latency, &cost, &compRatio, &skillsUsed,
			&errMsg, &proxyUsed, &toolUsed, &originalModel,
			&tokensInFresh, &tokensCacheRead, &tokensCacheWrite, &tokensReasoning,
			&tokensEstimated, &pricingSource)

		list = append(list, map[string]interface{}{
			"id": id, "timestamp": ts,
			"provider_id": providerID, "provider_name": providerName,
			"model_id": modelID, "key_id": keyID, "key_name": keyName,
			"group_name": groupName, "framework": framework,
			"status_code": statusCode, "race_status": raceStatus, "race_id": raceID,
			"tokens_in": tokensIn, "tokens_out": tokensOut,
			"latency_ms": latency, "cost_usd": cost,
			"compression_ratio": compRatio, "skills_used": skillsUsed,
			"error": errMsg, "proxy_used": proxyUsed,
			"tool_used": toolUsed, "original_model": originalModel,
			// Token split: tokens_in is the context-window sum of the three input
			// figures. Billing uses the split, which is priced at different rates.
			"tokens_in_fresh":    tokensInFresh,
			"tokens_cache_read":  tokensCacheRead,
			"tokens_cache_write": tokensCacheWrite,
			"tokens_reasoning":   tokensReasoning,
			"tokens_estimated":   tokensEstimated == 1,
			// pricing_source: exact|base|global|prefix|free|subscription|missing|legacy.
			// 'missing' means no price row matched — cost_usd is 0, not a guess.
			"pricing_source": pricingSource,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, map[string]interface{}{
		"data":        list,
		"total":       total,
		"page":        page,
		"per_page":    perPage,
		"total_pages": (total + perPage - 1) / perPage,
	})
}

// ── DELETE /api/logs — clear logs ONLY, cost_summary untouched

func logClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		writeError(w, 405, "method not allowed")
		return
	}
	_, err := db.DB.Exec("DELETE FROM logs")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{
		"status":  "cleared",
		"message": "All logs deleted. Cost summary preserved.",
	})
}

// ── DELETE /api/clear-all — clear all data tables

func clearAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		writeError(w, 405, "method not allowed")
		return
	}
	tables := []string{"logs", "usage_stats", "cost_summary", "race_logs", "compression_logs"}
	for _, t := range tables {
		if _, err := db.DB.Exec("DELETE FROM " + t); err != nil {
			writeError(w, 500, fmt.Sprintf("failed to clear %s: %v", t, err))
			return
		}
	}
	sessionBefore = 0
	sessionSaved = 0
	writeJSON(w, map[string]string{"status": "cleared", "message": "All logs and usage data deleted"})
}

// ── GET /api/logs/cost — aggregated cost summary ────────────

func logCostSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, 405, "method not allowed")
		return
	}

	// Time period aggregations
	type periodSummary struct {
		ReqCount  int     `json:"req_count"`
		CostUSD   float64 `json:"cost_usd"`
		TokensIn  int     `json:"tokens_in"`
		TokensOut int     `json:"tokens_out"`
	}
	periods := map[string]string{
		"today": "date = date('now')",
		"7d":    "date >= date('now', '-7 days')",
		"30d":   "date >= date('now', '-30 days')",
		"all":   "1=1",
	}
	summary := map[string]periodSummary{}
	for name, cond := range periods {
		var ps periodSummary
		db.DB.QueryRow(fmt.Sprintf(
			"SELECT COALESCE(SUM(req_count),0), COALESCE(SUM(total_cost_usd),0), COALESCE(SUM(total_tokens_in),0), COALESCE(SUM(total_tokens_out),0) FROM cost_summary WHERE %s", cond,
		)).Scan(&ps.ReqCount, &ps.CostUSD, &ps.TokensIn, &ps.TokensOut)
		summary[name] = ps
	}

	// Per-provider breakdown
	pRows, err := db.DB.Query(`SELECT COALESCE(provider_name,''), COALESCE(provider_id,''),
		SUM(req_count), SUM(total_cost_usd), SUM(total_tokens_in), SUM(total_tokens_out)
		FROM cost_summary GROUP BY provider_id ORDER BY SUM(total_cost_usd) DESC`)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer pRows.Close()
	var byProvider []map[string]interface{}
	for pRows.Next() {
		var name, id string
		var reqs, tin, tout int
		var cost float64
		pRows.Scan(&name, &id, &reqs, &cost, &tin, &tout)
		byProvider = append(byProvider, map[string]interface{}{
			"provider_name": name, "provider_id": id,
			"req_count": reqs, "cost_usd": cost,
			"tokens_in": tin, "tokens_out": tout,
		})
	}
	if byProvider == nil {
		byProvider = []map[string]interface{}{}
	}

	// Per-model breakdown
	mRows, err := db.DB.Query(`SELECT COALESCE(model_id,''), COALESCE(provider_name,''),
		SUM(req_count), SUM(total_cost_usd), SUM(total_tokens_in), SUM(total_tokens_out)
		FROM cost_summary GROUP BY model_id ORDER BY SUM(total_cost_usd) DESC`)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer mRows.Close()
	var byModel []map[string]interface{}
	for mRows.Next() {
		var modelID, providerName string
		var reqs, tin, tout int
		var cost float64
		mRows.Scan(&modelID, &providerName, &reqs, &cost, &tin, &tout)
		byModel = append(byModel, map[string]interface{}{
			"model_id": modelID, "provider_name": providerName,
			"req_count": reqs, "cost_usd": cost,
			"tokens_in": tin, "tokens_out": tout,
		})
	}
	if byModel == nil {
		byModel = []map[string]interface{}{}
	}

	writeJSON(w, map[string]interface{}{
		"summary":     summary,
		"by_provider": byProvider,
		"by_model":    byModel,
	})
}

// ── GET /api/logs/export?format=csv|json ────────────────────

func logExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, 405, "method not allowed")
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	// Reuse same filters as logList
	var conds []string
	var args []interface{}
	if p := r.URL.Query().Get("provider"); p != "" {
		conds = append(conds, "provider_id = ?")
		args = append(args, p)
	}
	if m := r.URL.Query().Get("model"); m != "" {
		conds = append(conds, "model_id = ?")
		args = append(args, m)
	}
	if s := r.URL.Query().Get("status"); s != "" {
		switch s {
		case "success", "200", "2xx":
			conds = append(conds, "status_code >= 200 AND status_code < 300")
		case "4xx":
			conds = append(conds, "status_code >= 400 AND status_code < 500")
		case "5xx":
			conds = append(conds, "status_code >= 500")
		default:
			if s == "error" {
				conds = append(conds, "(status_code IS NULL OR status_code >= 400)")
			} else if strings.HasSuffix(s, "xx") {
				prefix := strings.TrimSuffix(s, "xx")
				if v, err := strconv.Atoi(prefix); err == nil {
					conds = append(conds, fmt.Sprintf("status_code >= %d AND status_code < %d", v*100, v*100+100))
				}
			} else if v, err := strconv.Atoi(s); err == nil {
				conds = append(conds, "status_code = ?")
				args = append(args, v)
			}
		}
	}
	if from := r.URL.Query().Get("from"); from != "" {
		conds = append(conds, "timestamp >= ?")
		args = append(args, from)
	}
	if to := r.URL.Query().Get("to"); to != "" {
		conds = append(conds, "timestamp <= ?")
		args = append(args, to)
	}
	if search := r.URL.Query().Get("search"); search != "" {
		conds = append(conds, "(provider_name LIKE ? OR model_id LIKE ? OR key_name LIKE ? OR error LIKE ?)")
		like := "%" + search + "%"
		args = append(args, like, like, like, like)
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	rows, err := db.DB.Query(`SELECT id, timestamp, COALESCE(provider_id,''), COALESCE(provider_name,''),
		COALESCE(model_id,''), COALESCE(key_name,''), COALESCE(group_name,''),
		status_code, COALESCE(race_status,''),
		tokens_in, tokens_out, latency_ms, cost_usd,
		COALESCE(compression_ratio,0), COALESCE(skills_used,'[]'),
		COALESCE(error,''), COALESCE(proxy_used,'')
		FROM logs`+where+" ORDER BY timestamp DESC", args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	type logRow struct {
		ID               int
		Timestamp        string
		ProviderID       string
		ProviderName     string
		ModelID          string
		KeyName          string
		GroupName        string
		StatusCode       *int
		RaceStatus       string
		TokensIn         int
		TokensOut        int
		LatencyMs        int
		CostUSD          float64
		CompressionRatio float64
		SkillsUsed       string
		Error            string
		ProxyUsed        string
	}

	var data []logRow
	for rows.Next() {
		var row logRow
		rows.Scan(&row.ID, &row.Timestamp, &row.ProviderID, &row.ProviderName,
			&row.ModelID, &row.KeyName, &row.GroupName,
			&row.StatusCode, &row.RaceStatus,
			&row.TokensIn, &row.TokensOut, &row.LatencyMs, &row.CostUSD,
			&row.CompressionRatio, &row.SkillsUsed,
			&row.Error, &row.ProxyUsed)
		data = append(data, row)
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=\"paap-logs-export.csv\"")
		writer := csv.NewWriter(w)
		writer.Write([]string{
			"id", "timestamp", "provider_id", "provider_name", "model_id",
			"key_name", "group_name", "status_code", "race_status",
			"tokens_in", "tokens_out", "latency_ms", "cost_usd",
			"compression_ratio", "skills_used", "error", "proxy_used",
		})
		for _, row := range data {
			statusStr := ""
			if row.StatusCode != nil {
				statusStr = strconv.Itoa(*row.StatusCode)
			}
			writer.Write([]string{
				strconv.Itoa(row.ID), row.Timestamp, row.ProviderID, row.ProviderName,
				row.ModelID, row.KeyName, row.GroupName,
				statusStr, row.RaceStatus,
				strconv.Itoa(row.TokensIn), strconv.Itoa(row.TokensOut),
				strconv.Itoa(row.LatencyMs), fmt.Sprintf("%.6f", row.CostUSD),
				fmt.Sprintf("%.2f", row.CompressionRatio), row.SkillsUsed,
				row.Error, row.ProxyUsed,
			})
		}
		writer.Flush()
		return
	}

	// Default: JSON
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\"paap-logs-export.json\"")
	var list []map[string]interface{}
	for _, row := range data {
		list = append(list, map[string]interface{}{
			"id": row.ID, "timestamp": row.Timestamp,
			"provider_id": row.ProviderID, "provider_name": row.ProviderName,
			"model_id": row.ModelID, "key_name": row.KeyName,
			"group_name":  row.GroupName,
			"status_code": row.StatusCode, "race_status": row.RaceStatus,
			"tokens_in": row.TokensIn, "tokens_out": row.TokensOut,
			"latency_ms": row.LatencyMs, "cost_usd": row.CostUSD,
			"compression_ratio": row.CompressionRatio, "skills_used": row.SkillsUsed,
			"error": row.Error, "proxy_used": row.ProxyUsed,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(list)
}

// ── Model pricing ───────────────────────────────────────────
// Pricing resolution lives in pricing.go: resolvePricing / calculateCost.
// The old provider-blind lookup with a fabricated $1/$3 default rate was
// removed — see .ares/token-billing/prd.md defects 1, 2 and 4.

// ── Log writer — inserts log + updates usage_stats + cost_summary

// logProxyRequest is the legacy call shape: a bare in/out token pair with no
// cache or reasoning breakdown. Everything lands in InFresh.
func logProxyRequest(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed string, statusCode, tokensIn, tokensOut int, latencyMs int64, errMsg string, responseBody []byte) {
	logProxyRequestSplit(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed,
		statusCode, simpleTokens(tokensIn, tokensOut), latencyMs, errMsg, responseBody, "", "", 0, 0)
}

// logProxyRequestWithTool is the legacy tool-aware call shape.
func logProxyRequestWithTool(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed string, statusCode, tokensIn, tokensOut int, latencyMs int64, errMsg string, responseBody []byte, toolUsed, originalModel string, tokensBefore, tokensSaved int) {
	logProxyRequestSplit(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed,
		statusCode, simpleTokens(tokensIn, tokensOut), latencyMs, errMsg, responseBody,
		toolUsed, originalModel, tokensBefore, tokensSaved)
}

// logProxyRequestSplit is the real writer. It takes the full token breakdown so
// cache reads and reasoning tokens are priced at their own rates instead of
// being folded into the input rate.
//
// tokens_in is stored as t.TotalIn() (the context-window figure the dashboard
// and clients expect); billing uses the split columns.
func logProxyRequestSplit(providerID, providerName, modelID, keyID, keyName, groupName, proxyUsed string,
	statusCode int, t tokenCounts, latencyMs int64, errMsg string, responseBody []byte,
	toolUsed, originalModel string, tokensBefore, tokensSaved int) {

	cost, pricingSource := calculateCost(providerID, modelID, t)
	tokensIn := t.TotalIn()
	tokensOut := t.TotalOut()

	estimated := 0
	if t.Estimated {
		estimated = 1
	}
	unpriced := 0
	if pricingSource == pricingSourceMissing {
		unpriced = 1
	}

	// tokens_before is the caller's measured pre-compression size. It is NOT
	// derived from tokensIn: the provider's prompt_tokens includes tools, system
	// scaffolding and template overhead that compression never saw, so the two
	// are not comparable quantities.
	res, err := db.DB.Exec(`INSERT INTO logs
		(provider_id, provider_name, model_id, key_id, key_name, group_name, framework, status_code,
		 tokens_in, tokens_out, latency_ms, cost_usd, error, proxy_used, tool_used, original_model,
		 tokens_before, tokens_saved,
		 tokens_in_fresh, tokens_cache_read, tokens_cache_write, tokens_reasoning,
		 tokens_estimated, pricing_source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		providerID, providerName, modelID, keyID, keyName, groupName, "openai", statusCode,
		tokensIn, tokensOut, latencyMs, cost, errMsg, proxyUsed, toolUsed, originalModel,
		tokensBefore, tokensSaved,
		t.InFresh, t.CacheRead, t.CacheWrite, t.Reasoning,
		estimated, pricingSource)
	if err != nil {
		log.Printf("Failed to log request: %v", err)
	} else {
		// Push the row to any live dashboard. Non-blocking: a stalled subscriber
		// drops rows rather than delaying this proxied request.
		id, _ := res.LastInsertId()
		publishLogRow(id, providerID, providerName, modelID, keyID, keyName,
			groupName, proxyUsed, statusCode, t, latencyMs, cost, errMsg,
			toolUsed, originalModel, pricingSource, t.Estimated)
	}

	// Auto-clear: keep only newest 500 logs (cost_summary untouched).
	// Single conditional DELETE — no COUNT(*) round-trip on every request.
	res2, err := db.DB.Exec(`DELETE FROM logs WHERE
		(SELECT COUNT(*) FROM logs) > 500
		AND id NOT IN (SELECT id FROM logs ORDER BY timestamp DESC LIMIT 500)`)
	if err == nil {
		if n, _ := res2.RowsAffected(); n > 0 {
			log.Printf("[PAAP] Auto-cleared %d old log rows", n)
		}
	}

	// Update usage_stats (existing — survives log clear)
	isSuccess := 0
	if statusCode == 200 {
		isSuccess = 1
	}
	isError := 0
	if errMsg != "" || (statusCode != 0 && statusCode != 200) {
		isError = 1
	}
	today := time.Now().UTC().Format("2006-01-02")
	db.DB.Exec(`INSERT INTO usage_stats (date, provider_id, provider_name, model_id, request_count,
			success_count, error_count, tokens_in, tokens_out, total_cost_usd, avg_latency_ms,
			tokens_in_fresh, tokens_cache_read, tokens_cache_write, tokens_reasoning, unpriced_req_count)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(date, provider_id, model_id) DO UPDATE SET
			request_count = request_count + 1,
			success_count = success_count + excluded.success_count,
			error_count = error_count + excluded.error_count,
			tokens_in = tokens_in + excluded.tokens_in,
			tokens_out = tokens_out + excluded.tokens_out,
			total_cost_usd = total_cost_usd + excluded.total_cost_usd,
			tokens_in_fresh = tokens_in_fresh + excluded.tokens_in_fresh,
			tokens_cache_read = tokens_cache_read + excluded.tokens_cache_read,
			tokens_cache_write = tokens_cache_write + excluded.tokens_cache_write,
			tokens_reasoning = tokens_reasoning + excluded.tokens_reasoning,
			unpriced_req_count = unpriced_req_count + excluded.unpriced_req_count,
			avg_latency_ms = (avg_latency_ms * request_count + excluded.avg_latency_ms) / (request_count + 1)`,
		today, providerID, providerName, modelID, isSuccess, isError, tokensIn, tokensOut, cost, latencyMs,
		t.InFresh, t.CacheRead, t.CacheWrite, t.Reasoning, unpriced)

	// Update cost_summary (survives log clear — separate from logs table)
	tx, txErr := db.DB.Begin()
	if txErr != nil {
		log.Printf("Failed to begin cost_summary tx: %v", txErr)
		return
	}
	var existing int
	tx.QueryRow("SELECT COUNT(*) FROM cost_summary WHERE date=? AND provider_id=? AND model_id=?",
		today, providerID, modelID).Scan(&existing)
	if existing > 0 {
		tx.Exec(`UPDATE cost_summary SET
			req_count = req_count + 1,
			total_cost_usd = total_cost_usd + ?,
			total_tokens_in = total_tokens_in + ?,
			total_tokens_out = total_tokens_out + ?,
			total_tokens_in_fresh = total_tokens_in_fresh + ?,
			total_tokens_cache_read = total_tokens_cache_read + ?,
			total_tokens_cache_write = total_tokens_cache_write + ?,
			total_tokens_reasoning = total_tokens_reasoning + ?,
			unpriced_req_count = unpriced_req_count + ?
			WHERE date = ? AND provider_id = ? AND model_id = ?`,
			cost, tokensIn, tokensOut,
			t.InFresh, t.CacheRead, t.CacheWrite, t.Reasoning, unpriced,
			today, providerID, modelID)
	} else {
		tx.Exec(`INSERT INTO cost_summary (id, date, provider_id, provider_name, model_id, req_count,
				total_cost_usd, total_tokens_in, total_tokens_out,
				total_tokens_in_fresh, total_tokens_cache_read, total_tokens_cache_write,
				total_tokens_reasoning, unpriced_req_count)
			VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?)`,
			genID(), today, providerID, providerName, modelID, cost, tokensIn, tokensOut,
			t.InFresh, t.CacheRead, t.CacheWrite, t.Reasoning, unpriced)
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Failed to update cost_summary: %v", err)
	}
}

// ── Race task logger ────────────────────────────────────────

func logRaceTask(raceID, groupName string, totalModels, totalTasks int, provider, model, keyName, status string, tokensIn, tokensOut int, latencyMs int64, proxyUsed, errMsg string) {
	// Determine status_code: 200 for winner, 0 for cancelled/error
	statusCode := 0
	if status == "winner" || status == "completed" {
		statusCode = 200
	}
	raceCost, racePricingSource := calculateCost(provider, model, simpleTokens(tokensIn, tokensOut))
	_, dbErr := db.DB.Exec(`INSERT INTO logs
		(provider_id, provider_name, model_id, key_id, key_name, group_name, framework, status_code, race_status, race_id, tokens_in, tokens_out, latency_ms, cost_usd, error, proxy_used, tokens_in_fresh, pricing_source)
		VALUES (?, ?, ?, '', ?, ?, 'race', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		provider, provider, model, keyName, groupName, statusCode, status, raceID, tokensIn, tokensOut, latencyMs, raceCost, errMsg, proxyUsed, tokensIn, racePricingSource)
	if dbErr != nil {
		log.Printf("Failed to log race task: %v", dbErr)
	}
}

// ── modelList: /v1/models (OpenAI + Anthropic dual format) ──

func modelList(w http.ResponseWriter, r *http.Request) {

	if r.Method != "GET" {
		writeError(w, 405, "method not allowed")
		return
	}
	var models []map[string]interface{}

	// 1) Groups as virtual models
	gRows, err := db.DB.Query("SELECT name FROM groups ORDER BY name")
	if err == nil {
		defer gRows.Close()
		for gRows.Next() {
			var gName string
			gRows.Scan(&gName)
			models = append(models, map[string]interface{}{
				"id": gName, "object": "model", "owned_by": "paap",
			})
		}
	}

	// 2) Individual selected models from providers
	mRows, err := db.DB.Query("SELECT m.id, m.model_id, p.id as provider_id, p.name as provider_name FROM models m JOIN providers p ON m.provider_id=p.id WHERE m.is_selected=1 AND p.is_active=1 ORDER BY p.name, m.model_id")
	if err == nil {
		defer mRows.Close()
		for mRows.Next() {
			var id, modelID, providerID, providerName string
			mRows.Scan(&id, &modelID, &providerID, &providerName)
			// Use slugified provider name for human-readable model IDs
			// routeByModel() matches on provider ID, builtin_id, AND slugified name
			slug := strings.ToLower(providerName)
			slug = strings.ReplaceAll(slug, " ", "-")
			slug = strings.ReplaceAll(slug, "_", "-")
			slug = strings.ReplaceAll(slug, "(", "-")
			slug = strings.ReplaceAll(slug, ")", "")
			slug = strings.ReplaceAll(slug, "--", "-")
			slug = strings.Trim(slug, "-")
			fqModelID := slug + "/" + modelID
			models = append(models, map[string]interface{}{
				"id": fqModelID, "object": "model", "owned_by": strings.ToLower(providerName),
			})
		}
	}

	if models == nil {
		models = []map[string]interface{}{}
	}

	// Claude Code gateway discovery: return Anthropic format when requested
	// Claude Code sends "anthropic-version" header for /v1/models discovery
	if r.Header.Get("anthropic-version") != "" || r.URL.Query().Get("format") == "anthropic" {
		var anthropicModels []map[string]interface{}
		for _, m := range models {
			fqID, ok := m["id"].(string)
			if !ok || fqID == "" {
				continue // skip models with nil/empty id
			}
			// Add claude- prefix for Claude Code discovery + [1m] suffix for 1M context
			// Claude Code only shows models with claude- prefix in its model picker
			// routeByModel() strips the prefix and [1m] suffix when routing requests
			claudeID := fqID
			if !strings.HasPrefix(fqID, "claude-") {
				claudeID = "claude-" + fqID
			}
			if !strings.HasSuffix(claudeID, "[1m]") && !strings.HasSuffix(claudeID, "[1M]") {
				claudeID = claudeID + "[1m]"
			}
			entry := map[string]interface{}{
				"type":           "model",
				"id":             claudeID,
				"display_name":   fqID,
				"created_at":     "2025-01-01T00:00:00Z",
				"context_window": 200000,
				"max_tokens":     8192,
			}
			// [1m] suffix = 1M context window
			if strings.HasSuffix(claudeID, "[1m]") || strings.HasSuffix(claudeID, "[1M]") {
				entry["context_window"] = 1000000
			}
			anthropicModels = append(anthropicModels, entry)
		}
		writeJSON(w, map[string]interface{}{
			"data": anthropicModels,
		})
		return
	}

	// Default: OpenAI format
	writeJSON(w, map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

// ── modelListDB: /api/models ────────────────────────────────

func modelListDB(w http.ResponseWriter, r *http.Request) {
	providerID := r.URL.Query().Get("provider_id")
	var rows *sql.Rows
	var err error
	if providerID != "" {
		rows, err = db.DB.Query("SELECT m.id, m.model_id, m.provider_id, p.name as provider_name FROM models m JOIN providers p ON m.provider_id=p.id WHERE m.provider_id=? ORDER BY m.model_id", providerID)
	} else {
		rows, err = db.DB.Query("SELECT m.id, m.model_id, m.provider_id, p.name as provider_name FROM models m JOIN providers p ON m.provider_id=p.id WHERE m.is_selected=1 ORDER BY p.name, m.model_id")
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	var list []map[string]interface{}
	for rows.Next() {
		var id, modelID, pID, pName string
		rows.Scan(&id, &modelID, &pID, &pName)
		list = append(list, map[string]interface{}{
			"id": id, "model_id": modelID, "provider_id": pID, "provider_name": pName,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

// ── usageSummary: /api/usage/summary ────────────────────────

func usageSummary(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	groupBy := r.URL.Query().Get("group_by")

	query := `SELECT date, provider_name, model_id, request_count, success_count, error_count,
		tokens_in, tokens_out, total_cost_usd, avg_latency_ms FROM usage_stats`
	var args []interface{}
	var conds []string

	if from != "" {
		conds = append(conds, "date >= ?")
		args = append(args, from)
	}
	if to != "" {
		conds = append(conds, "date <= ?")
		args = append(args, to)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}

	switch groupBy {
	case "provider":
		query = `SELECT provider_name, '' as model_id, SUM(request_count), SUM(success_count), SUM(error_count),
			SUM(tokens_in), SUM(tokens_out), SUM(total_cost_usd), AVG(avg_latency_ms) FROM usage_stats`
		if len(conds) > 0 {
			query += " WHERE " + strings.Join(conds, " AND ")
		}
		query += " GROUP BY provider_name ORDER BY SUM(total_cost_usd) DESC"
	case "model":
		query = `SELECT provider_name, model_id, SUM(request_count), SUM(success_count), SUM(error_count),
			SUM(tokens_in), SUM(tokens_out), SUM(total_cost_usd), AVG(avg_latency_ms) FROM usage_stats`
		if len(conds) > 0 {
			query += " WHERE " + strings.Join(conds, " AND ")
		}
		query += " GROUP BY provider_name, model_id ORDER BY SUM(total_cost_usd) DESC"
	default:
		query += " ORDER BY date DESC, total_cost_usd DESC"
	}

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	var totalReqs, totalSuccess, totalErrors, totalIn, totalOut int
	var totalCost float64

	for rows.Next() {
		var date, providerName, modelID string
		var reqs, success, errors, tin, tout, avgLatency int
		var cost float64
		rows.Scan(&date, &providerName, &modelID, &reqs, &success, &errors, &tin, &tout, &cost, &avgLatency)
		list = append(list, map[string]interface{}{
			"date": date, "provider_name": providerName, "model_id": modelID,
			"request_count": reqs, "success_count": success, "error_count": errors,
			"tokens_in": tin, "tokens_out": tout, "total_cost_usd": cost, "avg_latency_ms": avgLatency,
		})
		totalReqs += reqs
		totalSuccess += success
		totalErrors += errors
		totalIn += tin
		totalOut += tout
		totalCost += cost
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, map[string]interface{}{
		"entries": list,
		"totals": map[string]interface{}{
			"request_count": totalReqs, "success_count": totalSuccess,
			"error_count": totalErrors, "tokens_in": totalIn, "tokens_out": totalOut,
			"total_cost_usd": totalCost,
		},
	})
}
