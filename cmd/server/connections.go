package main

import (
	"net/http"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// ── Connection Management ──────────────────────────────────

// connectionList returns all connections for a provider
func connectionList(w http.ResponseWriter, r *http.Request, providerID string) {
	rows, err := db.DB.Query(`SELECT id, auth_type, name, email, 
		CASE WHEN api_key != '' THEN '***' || SUBSTR(api_key, -4) ELSE '' END as key_preview,
		CASE WHEN access_token != '' THEN '***' || SUBSTR(access_token, -4) ELSE '' END as token_preview,
		test_status, fail_count, COALESCE(last_error,''), is_active, created_at
		FROM provider_connections WHERE provider_id=? ORDER BY created_at DESC`, providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, authType, name, email, keyPreview, tokenPreview, testStatus, lastError string
		var failCount, isActive int
		var createdAt int64
		rows.Scan(&id, &authType, &name, &email, &keyPreview, &tokenPreview, &testStatus, &failCount, &lastError, &isActive, &createdAt)
		item := map[string]interface{}{
			"id":          id,
			"auth_type":   authType,
			"name":        name,
			"email":       email,
			"test_status": testStatus,
			"fail_count":  failCount,
			"last_error":  lastError,
			"is_active":   isActive == 1,
			"created_at":  createdAt,
		}
		if keyPreview != "" {
			item["key_preview"] = keyPreview
		}
		if tokenPreview != "" {
			item["token_preview"] = tokenPreview
		}
		list = append(list, item)
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

// connectionCreate adds a new connection (OAuth or device flow)
func connectionCreate(w http.ResponseWriter, r *http.Request, providerID string) {
	// Verify provider exists
	var pName string
	err := db.DB.QueryRow("SELECT name FROM providers WHERE id=?", providerID).Scan(&pName)
	if err != nil {
		writeError(w, 404, "provider not found")
		return
	}

	var body struct {
		AuthType     string `json:"auth_type"` // "apikey" or "oauth"
		Name         string `json:"name"`
		Email        string `json:"email"`
		APIKey       string `json:"api_key"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    int64  `json:"expires_at"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.AuthType == "" {
		body.AuthType = "apikey"
	}
	if body.AuthType == "apikey" && body.APIKey == "" {
		writeError(w, 400, "api_key required for auth_type=apikey")
		return
	}
	if body.AuthType == "oauth" && body.AccessToken == "" {
		writeError(w, 400, "access_token required for auth_type=oauth")
		return
	}

	id := genID()
	now := time.Now().Unix()
	_, err = db.DB.Exec(`INSERT INTO provider_connections 
		(id, provider_id, auth_type, name, email, api_key, access_token, refresh_token, expires_at, test_status, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'untested', 1, ?, ?)`,
		id, providerID, body.AuthType, body.Name, body.Email,
		body.APIKey, body.AccessToken, body.RefreshToken, body.ExpiresAt,
		now, now)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"id":        id,
		"auth_type": body.AuthType,
		"name":      body.Name,
		"is_active": true,
	})
}

// connectionDelete removes a connection
func connectionDelete(w http.ResponseWriter, r *http.Request, providerID, connID string) {
	result, err := db.DB.Exec("DELETE FROM provider_connections WHERE id=? AND provider_id=?", connID, providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, 404, "connection not found")
		return
	}
	writeJSON(w, map[string]string{"status": "disconnected", "id": connID})
}

// connectionToggle toggles is_active for a connection
func connectionToggle(w http.ResponseWriter, r *http.Request, providerID, connID string) {
	var current int
	err := db.DB.QueryRow("SELECT COALESCE(is_active,0) FROM provider_connections WHERE id=? AND provider_id=?", connID, providerID).Scan(&current)
	if err != nil {
		writeError(w, 404, "connection not found")
		return
	}
	newVal := 1
	if current == 1 {
		newVal = 0
	}
	db.DB.Exec("UPDATE provider_connections SET is_active=?, updated_at=? WHERE id=? AND provider_id=?", newVal, time.Now().Unix(), connID, providerID)
	writeJSON(w, map[string]interface{}{"id": connID, "is_active": newVal == 1})
}

// connectionEnableAll activates all connections for a provider
func connectionEnableAll(w http.ResponseWriter, r *http.Request, providerID string) {
	result, err := db.DB.Exec("UPDATE provider_connections SET is_active=1, updated_at=? WHERE provider_id=? AND is_active=0", time.Now().Unix(), providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	n, _ := result.RowsAffected()
	writeJSON(w, map[string]interface{}{"status": "ok", "enabled": n})
}

// connectionDeleteDisabled deletes all inactive connections for a provider
func connectionDeleteDisabled(w http.ResponseWriter, r *http.Request, providerID string) {
	result, err := db.DB.Exec("DELETE FROM provider_connections WHERE provider_id=? AND is_active=0", providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	n, _ := result.RowsAffected()
	writeJSON(w, map[string]interface{}{"status": "ok", "deleted": n})
}
