package main

import "net/http"

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// ── Backup & Restore ─────────────────────────────────────────

func backupDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".paap", "paap.db")

	// Read database file
	dbData, err := os.ReadFile(dbPath)
	if err != nil {
		writeError(w, 500, "failed to read database: "+err.Error())
		return
	}

	// Create backup filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("paap-backup-%s.db", timestamp)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(dbData)))
	w.Write(dbData)
}

func restoreDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(50 << 20); err != nil { // 50MB max
		writeError(w, 400, "failed to parse form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("database")
	if err != nil {
		writeError(w, 400, "missing database file")
		return
	}
	defer file.Close()

	// Validate file extension
	if !strings.HasSuffix(header.Filename, ".db") {
		writeError(w, 400, "invalid file type, expected .db")
		return
	}

	// Read uploaded file
	dbData, err := io.ReadAll(file)
	if err != nil {
		writeError(w, 500, "failed to read file: "+err.Error())
		return
	}

	// Validate SQLite header
	if len(dbData) < 16 || string(dbData[:15]) != "SQLite format 3" {
		writeError(w, 400, "invalid SQLite database")
		return
	}

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".paap", "paap.db")
	backupPath := dbPath + ".bak." + time.Now().Format("20060102-150405")

	// Backup current database
	if err := os.Rename(dbPath, backupPath); err != nil {
		writeError(w, 500, "failed to backup current database: "+err.Error())
		return
	}

	// Write new database
	if err := os.WriteFile(dbPath, dbData, 0600); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, dbPath)
		writeError(w, 500, "failed to restore database: "+err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"status":      "ok",
		"message":     "Database restored successfully. Restart PAAP to apply.",
		"backup_file": backupPath,
	})
}

func clearAllData(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}

	// Clear logs only
	db.DB.Exec("DELETE FROM logs")
	db.DB.Exec("DELETE FROM cost_summary")
	db.DB.Exec("DELETE FROM usage_stats")
	db.DB.Exec("DELETE FROM race_logs")
	db.DB.Exec("DELETE FROM compression_logs")
	sessionBefore = 0
	sessionSaved = 0

	// Keep providers, keys, models, groups, system_settings, gateway_keys

	writeJSON(w, map[string]interface{}{
		"status":  "ok",
		"message": "All data cleared (providers, keys, groups, logs). Settings and gateway keys preserved.",
	})
}
