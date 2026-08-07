package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// compressionLogEntry is a single compression event for the API response.
type compressionLogEntry struct {
	Timestamp      string  `json:"timestamp"`
	ContentType    string  `json:"content_type"`
	Level          string  `json:"level"`
	OriginalSize   int     `json:"original_size"`
	CompressedSize int     `json:"compressed_size"`
	SavedPercent   float64 `json:"saved_percent"`
	OriginalTokens int     `json:"original_tokens"`
	CompressedTokens int   `json:"compressed_tokens"`
	SavedTokens    int     `json:"saved_tokens"`
}

// compressionLogsHandler handles GET /api/compression/logs.
func compressionLogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		clearCompressionLogsHandler(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 300
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	database := db.DB
	if database == nil {
		http.Error(w, "Database not initialized", http.StatusInternalServerError)
		return
	}

	rows, err := database.Query(`
		SELECT timestamp, model_id, intensity, orig_bytes, new_bytes, saved_percent
		FROM compression_logs
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		log.Printf("[PAAP] compression logs query error: %v", err)
		http.Error(w, "Query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var entries []compressionLogEntry
	for rows.Next() {
		var e compressionLogEntry
		var ts string
		var modelID, intensity string
		var origBytes, newBytes int
		var savedPct float64

		if err := rows.Scan(&ts, &modelID, &intensity, &origBytes, &newBytes, &savedPct); err != nil {
			log.Printf("[PAAP] compression logs scan error: %v", err)
			continue
		}

		// Parse and return ISO timestamp for frontend
		for _, layout := range []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05Z",
		} {
			if t, err := time.Parse(layout, ts); err == nil {
				e.Timestamp = t.Format(time.RFC3339)
				break
			}
		}
		if e.Timestamp == "" {
			e.Timestamp = ts
		}

		e.ContentType = modelID
		if e.ContentType == "" {
			e.ContentType = "tool"
		}
		e.Level = intensity
		e.OriginalSize = origBytes
		e.CompressedSize = newBytes
		e.SavedPercent = savedPct
		e.OriginalTokens = origBytes / 4
		e.CompressedTokens = newBytes / 4
		e.SavedTokens = e.OriginalTokens - e.CompressedTokens

		entries = append(entries, e)
	}

	if entries == nil {
		entries = []compressionLogEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// logCompressionEvent inserts a compression event into the DB.
func logCompressionEvent(contentType, level string, origBytes, newBytes int) {
	database := db.DB
	if database == nil {
		log.Printf("[compression-LOG] DB is nil!")
		return
	}
	log.Printf("[compression-LOG] inserting: type=%s level=%s orig=%d new=%d", contentType, level, origBytes, newBytes)

	savedPct := 0.0
	if origBytes > 0 {
		savedPct = (1.0 - float64(newBytes)/float64(origBytes)) * 100.0
	}

	_, err := database.Exec(`
		INSERT INTO compression_logs (model_id, intensity, orig_bytes, new_bytes, saved_percent)
		VALUES (?, ?, ?, ?, ?)
	`, contentType, level, origBytes, newBytes, savedPct)
	if err != nil {
		log.Printf("[PAAP] failed to log compression event: %v", err)
	}
}

// clearCompressionLogsHandler handles DELETE /api/compression/logs.
func clearCompressionLogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	database := db.DB
	if database == nil {
		http.Error(w, "Database not initialized", http.StatusInternalServerError)
		return
	}

	result, err := database.Exec("DELETE FROM compression_logs")
	if err != nil {
		http.Error(w, "Delete error", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	log.Printf("[PAAP] Cleared %d compression logs", affected)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"deleted": affected,
	})
}
