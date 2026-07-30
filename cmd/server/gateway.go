package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/dolvin/paap/internal/db"
)

func gatewayKeyList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query("SELECT id, key, name, is_active, created_at FROM gateway_keys ORDER BY created_at DESC")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, key, name, createdAt string
		var isActive int
		rows.Scan(&id, &key, &name, &isActive, &createdAt)
		list = append(list, map[string]interface{}{
			"id": id, "key": key, "name": name, "is_active": isActive == 1, "created_at": createdAt,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

func gatewayKeyCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	parseBody(r, &body)

	id := genID()
	// Generate sk-xxx key
	raw := make([]byte, 24)
	rand.Read(raw)
	key := "sk-" + hex.EncodeToString(raw)

	_, err := db.DB.Exec("INSERT INTO gateway_keys (id, name, key, is_active) VALUES (?, ?, ?, 1)", id, body.Name, key)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"id": id, "name": body.Name, "key": key, "is_active": true})
}

func gatewayKeyRoutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/gateway/keys/")
	id := strings.Trim(trimmed, "/")
	if id == "" {
		writeError(w, 400, "missing key id")
		return
	}
	if r.Method != "DELETE" {
		writeError(w, 405, "method not allowed")
		return
	}
	result, err := db.DB.Exec("DELETE FROM gateway_keys WHERE id=?", id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, 404, "not found")
		return
	}
	writeJSON(w, map[string]string{"status": "deleted", "id": id})
}

// ── System Settings ─────────────────────────────────────

func setSetting(key, value string) {
	db.DB.Exec(`INSERT INTO system_settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value)
}

func getSettingStr(key, defaultVal string) string {
	var val string
	err := db.DB.QueryRow("SELECT value FROM system_settings WHERE key=?", key).Scan(&val)
	if err != nil {
		return defaultVal
	}
	return val
}

func getSettingInt(key string, defaultVal int) int {
	var val string
	err := db.DB.QueryRow("SELECT value FROM system_settings WHERE key=?", key).Scan(&val)
	if err != nil {
		return defaultVal
	}
	var n int
	if _, err := fmt.Sscanf(val, "%d", &n); err != nil {
		return defaultVal
	}
	return n
}

func settingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		rows, err := db.DB.Query("SELECT key, value FROM system_settings")
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		defer rows.Close()
		settings := map[string]interface{}{}
		for rows.Next() {
			var k, v string
			rows.Scan(&k, &v)
			settings[k] = v
		}

		// Include gateway key info
		var gwKey, gwName string
		err = db.DB.QueryRow("SELECT key, name FROM gateway_keys WHERE is_active=1 ORDER BY created_at DESC LIMIT 1").Scan(&gwKey, &gwName)
		if err == nil && gwKey != "" {
			settings["gateway_key"] = gwKey
			settings["gateway_key_name"] = gwName
			if len(gwKey) > 12 {
				settings["gateway_key_masked"] = gwKey[:8] + "..." + gwKey[len(gwKey)-4:]
			} else {
				settings["gateway_key_masked"] = gwKey
			}
		}

		// Include base_url
		settings["base_url"] = getSettingStr("base_url", "http://localhost:9090/v1")

		writeJSON(w, settings)
	case "PUT":
		var body map[string]interface{}
		if err := parseBody(r, &body); err != nil {
			writeError(w, 400, "invalid json")
			return
		}
		for k, v := range body {
			val := fmt.Sprintf("%v", v)
			db.DB.Exec(`INSERT INTO system_settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
				ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, k, val)
		}
		settingsHandler(w, r) // return updated settings
		default:
			writeError(w, 405, "method not allowed")
		}
		}
