package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// ── Proxy helpers for provider ───────────────────────────

func getProviderProxy(providerID string) string {
	var proxyID, proxyGroupID string
	var proxyEnabled int
	db.DB.QueryRow(
		"SELECT COALESCE(proxy_id,''), COALESCE(proxy_group_id,''), COALESCE(proxy_enabled,0) FROM providers WHERE id=?",
		providerID,
	).Scan(&proxyID, &proxyGroupID, &proxyEnabled)

	// Check global proxy setting if per-provider not set
	if proxyEnabled == 0 {
		var globalEnabled string
		db.DB.QueryRow("SELECT value FROM system_settings WHERE key='proxy_enabled'").Scan(&globalEnabled)
		if globalEnabled != "true" {
			return ""
		}
		// Global proxy is on — auto-pick fastest
		log.Printf("[PAAP] Global proxy enabled — auto-picking fastest for %s", providerID)
	}

	// Warn if proxy enabled but no proxy configured
	if proxyID == "" && proxyGroupID == "" {
		log.Printf("[PAAP] Provider %s proxy enabled but no proxy_id/group — auto-picking fastest", providerID)
	}

	// 1) Direct proxy assignment (proxy_id set)
	if proxyID != "" {
		url := singleProxyURL(proxyID)
		if url != "" {
			return url
		}
		// proxy_id points to deleted/inactive proxy — fall through to auto-pick
		log.Printf("[PAAP] Provider %s proxy_id %s not found/active — auto-picking fastest", providerID, proxyID)
	}

	// 2) Proxy group assignment (proxy_group_id set) — pick fastest active member
	if proxyGroupID != "" {
		rows, err := db.DB.Query(`
			SELECT p.address, p.port, p.proxy_type
			FROM proxy_pools p
			JOIN proxy_group_members pgm ON pgm.proxy_id = p.id
			WHERE pgm.group_id = ? AND p.is_active = 1 AND p.test_status = 'ok'
			ORDER BY p.last_latency_ms ASC
			LIMIT 10
		`, proxyGroupID)
		if err != nil {
			return ""
		}
		defer rows.Close()
		var proxies []struct {
			addr, proxyType string
			port            int
		}
		for rows.Next() {
			var p struct {
				addr, proxyType string
				port            int
			}
			rows.Scan(&p.addr, &p.port, &p.proxyType)
			proxies = append(proxies, p)
		}
		if len(proxies) == 0 {
			return "" // no working proxies in group → direct
		}
		// Round-robin selection from group
		counter := getProviderRRCounter(providerID + "_proxy")
		idx := int(counter.Add(1)-1) % len(proxies)
		p := proxies[idx]
		scheme := "socks5"
		if p.proxyType == "http" || p.proxyType == "https" {
			scheme = p.proxyType
		}
		return fmt.Sprintf("%s://%s:%d", scheme, p.addr, p.port)
	}

	// 3) Auto-pick fastest proxy from global pool (fallback for all cases)
	rows, err := db.DB.Query(`
		SELECT address, port, proxy_type FROM proxy_pools
		WHERE is_active = 1 AND test_status = 'ok'
		ORDER BY last_latency_ms ASC
		LIMIT 10
	`)
	if err == nil {
		defer rows.Close()
		var proxies []struct {
			addr, proxyType string
			port            int
		}
		for rows.Next() {
			var p struct {
				addr, proxyType string
				port            int
			}
			rows.Scan(&p.addr, &p.port, &p.proxyType)
			proxies = append(proxies, p)
		}
		if len(proxies) > 0 {
			counter := getProviderRRCounter("global_proxy")
			idx := int(counter.Add(1)-1) % len(proxies)
			p := proxies[idx]
			scheme := "socks5"
			if p.proxyType == "http" || p.proxyType == "https" {
				scheme = p.proxyType
			}
			return fmt.Sprintf("%s://%s:%d", scheme, p.addr, p.port)
		}
	}

	return ""
}

func singleProxyURL(proxyID string) string {
	var addr, proxyType string
	var port int
	err := db.DB.QueryRow(
		"SELECT address, port, proxy_type FROM proxy_pools WHERE id=? AND is_active=1",
		proxyID,
	).Scan(&addr, &port, &proxyType)
	if err != nil {
		return ""
	}
	scheme := "socks5"
	if proxyType == "http" || proxyType == "https" {
		scheme = proxyType
	}
	return fmt.Sprintf("%s://%s:%d", scheme, addr, port)
}

func makeProxyTransport(proxyURL string) (*http.Transport, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	return &http.Transport{
		Proxy: http.ProxyURL(u),
	}, nil
}

// ── Providers CRUD ──────────────────────────────────────────

func providerList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(`SELECT p.id, p.name, p.base_url, p.icon, p.is_active, p.round_robin,
		COALESCE(p.proxy_id,''), COALESCE(p.proxy_enabled,0), p.created_at,
		COALESCE(p.provider_type,'custom'), COALESCE(p.auth_type,'apikey'), COALESCE(p.builtin_id,''),
		COALESCE(p.round_robin_enabled,0), COALESCE(p.supports_anthropic,0)
		FROM providers p ORDER BY p.name`)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, name, baseURL, icon, proxyID, createdAt, providerType, authType, builtinID string
		var isActive, roundRobin, proxyEnabled, roundRobinEnabled, supportsAnthropic int
		rows.Scan(&id, &name, &baseURL, &icon, &isActive, &roundRobin, &proxyID, &proxyEnabled, &createdAt,
			&providerType, &authType, &builtinID, &roundRobinEnabled, &supportsAnthropic)

		// Get key counts (api_keys + provider_connections for connection-type providers)
		var totalKeys, activeKeys int
		db.DB.QueryRow("SELECT COUNT(*) FROM api_keys WHERE provider_id=?", id).Scan(&totalKeys)
		db.DB.QueryRow("SELECT COUNT(*) FROM api_keys WHERE provider_id=? AND is_active=1", id).Scan(&activeKeys)

		// Also count connections for connection-type providers
		if authType == "connection" {
			var connTotal, connActive int
			db.DB.QueryRow("SELECT COUNT(*) FROM provider_connections WHERE provider_id=?", id).Scan(&connTotal)
			db.DB.QueryRow("SELECT COUNT(*) FROM provider_connections WHERE provider_id=? AND is_active=1", id).Scan(&connActive)
			totalKeys += connTotal
			activeKeys += connActive
		}

		// Get model count
		var modelCount int
		db.DB.QueryRow("SELECT COUNT(*) FROM models WHERE provider_id=? AND is_selected=1", id).Scan(&modelCount)

		list = append(list, map[string]interface{}{
			"id": id, "name": name, "base_url": baseURL, "icon": icon,
			"is_active": isActive == 1, "round_robin": roundRobin == 1,
			"proxy_id": proxyID, "proxy_enabled": proxyEnabled == 1,
			"created_at":    createdAt,
			"provider_type": providerType, "auth_type": authType,
			"builtin_id":          builtinID,
			"round_robin_enabled": roundRobinEnabled == 1,
			"supports_anthropic":  supportsAnthropic == 1,
			"key_count":           totalKeys,
			"active_key_count":    activeKeys,
			"model_count":         modelCount,
			"status": func() string {
				if isActive == 1 && activeKeys > 0 {
					return "online"
				}
				return "offline"
			}(),
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

func providerCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string `json:"name"`
		BaseURL        string `json:"base_url"`
		Icon           string `json:"icon"`
		CustomHeaders  string `json:"custom_headers"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.Name == "" || body.BaseURL == "" {
		writeError(w, 400, "name and base_url required")
		return
	}

	// New providers always go through translator (supports_anthropic=0)
	// Existing builtin providers with native Anthropic support keep their flag
	id := genID()
	customHeaders := body.CustomHeaders
	if customHeaders == "" {
		customHeaders = "{}"
	}
	_, err := db.DB.Exec(
		"INSERT INTO providers (id, name, base_url, icon, supports_anthropic, custom_headers) VALUES (?, ?, ?, ?, 0, ?)",
		id, body.Name, body.BaseURL, body.Icon, customHeaders,
	)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	writeJSON(w, map[string]interface{}{
		"id": id, "name": body.Name, "base_url": body.BaseURL,
		"icon": body.Icon, "is_active": true, "round_robin": false,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func providerRoutes(w http.ResponseWriter, r *http.Request) {
	// Strip /api/providers/ prefix, split remaining
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/providers/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, 400, "missing provider id")
		return
	}
	id := parts[0]

	// /api/providers/:id/test-prompt
	if len(parts) == 2 && parts[1] == "test-prompt" && r.Method == "POST" {
		providerTestPrompt(w, r, id)
		return
	}

	// /api/providers/:id/toggle-active
	if len(parts) == 2 && parts[1] == "toggle-active" && r.Method == "POST" {
		providerToggleActive(w, r, id)
		return
	}

	// /api/providers/:id/test-prompt-stream
	if len(parts) == 2 && parts[1] == "test-prompt-stream" && r.Method == "POST" {
		providerTestPromptStream(w, r, id)
		return
	}

	// /api/providers/:id/keys
	if len(parts) == 2 && parts[1] == "keys" {
		switch r.Method {
		case "GET":
			providerKeyList(w, r, id)
		case "POST":
			// Detect bulk from body: { keys: [...] }
			raw, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				writeError(w, 400, "invalid body")
				return
			}
			var rawBody map[string]json.RawMessage
			if err := json.Unmarshal(raw, &rawBody); err != nil {
				writeError(w, 400, "invalid json")
				return
			}
			if _, ok := rawBody["keys"]; ok {
				// Bulk add: { keys: ["key1", "key2", ...], names?: [...] }
				var keysBody struct {
					Keys  []string `json:"keys"`
					Names []string `json:"names"`
				}
				if err := json.Unmarshal(raw, &keysBody); err != nil {
					writeError(w, 400, "invalid keys array")
					return
				}
				providerKeyBulkAdd(w, r, id, keysBody.Keys, keysBody.Names)
			} else {
				// Single add: { key: "..." } — restore body, downstream re-parses it
				r.Body = io.NopCloser(bytes.NewReader(raw))
				providerKeyCreate(w, r, id)
			}
		default:
			writeError(w, 405, "method not allowed")
		}
		return
	}

	// /api/providers/:id/keys/bulk
	if len(parts) == 3 && parts[1] == "keys" && parts[2] == "bulk" {
		if r.Method == "POST" {
			providerKeyBulkCreate(w, r, id)
			return
		}
		writeError(w, 405, "method not allowed")
		return
	}

	// /api/providers/:id/keys/enable-all
	if len(parts) == 3 && parts[1] == "keys" && parts[2] == "enable-all" {
		if r.Method == "POST" {
			result, err := db.DB.Exec("UPDATE api_keys SET is_active=1 WHERE provider_id=?", id)
			if err != nil {
				writeError(w, 500, err.Error())
				return
			}
			n, _ := result.RowsAffected()
			writeJSON(w, map[string]interface{}{"enabled": n})
			return
		}
		writeError(w, 405, "method not allowed")
		return
	}

	// /api/providers/:id/keys/disabled — bulk delete disabled keys
	if len(parts) == 3 && parts[1] == "keys" && parts[2] == "disabled" && r.Method == "POST" {
		providerKeysDeleteDisabled(w, r, id)
		return
	}
	// /api/providers/:id/keys/:key_id
	if len(parts) == 3 && parts[1] == "keys" {
		switch r.Method {
		case "DELETE":
			providerKeyDelete(w, r, id, parts[2])
		case "PATCH":
			providerKeyToggleActive(w, r, id, parts[2])
		default:
			writeError(w, 405, "method not allowed")
		}
		return
	}

	// /api/providers/:id/keys/:key_id/test
	if len(parts) == 4 && parts[1] == "keys" && parts[3] == "test" {
		if r.Method == "POST" {
			providerKeyTest(w, r, id, parts[2])
			return
		}
		writeError(w, 405, "method not allowed")
		return
	}

	// /api/providers/:id/models
	if len(parts) == 2 && parts[1] == "models" {
		switch r.Method {
		case "GET":
			providerModelList(w, r, id)
		case "POST":
			providerModelAdd(w, r, id)
		case "PUT":
			providerModelUpdateSelection(w, r, id)
		default:
			writeError(w, 405, "method not allowed")
		}
		return
	}

	// /api/providers/:id/models/detect
	if len(parts) == 3 && parts[1] == "models" && parts[2] == "detect" && r.Method == "POST" {
		providerDetectModels(w, r, id)
		return
	}

	// /api/providers/:id/models/:model_id[/select|/test] (model_id may contain slashes like @cf/meta/...)
	if len(parts) >= 3 && parts[1] == "models" {
		// Model ID = everything after "models" — last part may be action ("select", "test")
		// or it may be the end of URL for DELETE
		modelID := ""
		action := ""
		afterModels := parts[2:]

		if len(afterModels) > 1 {
			last := afterModels[len(afterModels)-1]
			if last == "select" || last == "test" {
				action = last
				modelID = strings.Join(afterModels[:len(afterModels)-1], "/")
			} else {
				modelID = strings.Join(afterModels, "/")
			}
		} else if len(afterModels) == 1 {
			modelID = afterModels[0]
		}

		modelID, _ = url.PathUnescape(modelID)

		if action == "select" && r.Method == "PATCH" {
			providerModelToggleSelect(w, r, id, modelID)
			return
		}
		if action == "test" && r.Method == "POST" {
			providerModelTest(w, r, id, modelID)
			return
		}
		if action == "" && r.Method == "DELETE" {
			providerModelDelete(w, r, id, modelID)
			return
		}
		writeError(w, 405, "method not allowed")
		return
	}

	// /api/providers/:id/proxy
	if len(parts) == 2 && parts[1] == "proxy" && r.Method == "PATCH" {
		providerPatchProxy(w, r, id)
		return
	}

	// /api/providers/:id/connections
	if len(parts) == 2 && parts[1] == "connections" {
		switch r.Method {
		case "GET":
			connectionList(w, r, id)
		case "POST":
			connectionCreate(w, r, id)
		default:
			writeError(w, 405, "method not allowed")
		}
		return
	}

	// /api/providers/:id/connections/:connId
	if len(parts) == 3 && parts[1] == "connections" {
		if r.Method == "DELETE" {
			connectionDelete(w, r, id, parts[2])
			return
		}
		if r.Method == "POST" {
			connectionToggle(w, r, id, parts[2])
			return
		}
		writeError(w, 405, "method not allowed")
		return
	}

	// /api/providers/:id/playground (alias for test-prompt)
	if len(parts) == 2 && parts[1] == "playground" && r.Method == "POST" {
		providerTestPrompt(w, r, id)
		return
	}

	// /api/providers/:id/round-robin (toggle round_robin_enabled)
	if len(parts) == 2 && parts[1] == "round-robin" {
		if r.Method == "PUT" {
			var body struct {
				Enabled *bool `json:"enabled"`
			}
			if err := parseBody(r, &body); err != nil {
				writeError(w, 400, "invalid json")
				return
			}
			if body.Enabled == nil {
				writeError(w, 400, "enabled required")
				return
			}
			v := 0
			if *body.Enabled {
				v = 1
			}
			_, err := db.DB.Exec("UPDATE providers SET round_robin_enabled=?, updated_at=? WHERE id=?",
				v, time.Now().UTC().Format(time.RFC3339), id)
			if err != nil {
				writeError(w, 500, err.Error())
				return
			}
			providerGet(w, r, id)
			return
		}
		writeError(w, 405, "method not allowed")
		return
	}

	// /api/providers/:id
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			providerGet(w, r, id)
		case "PUT":
			providerUpdate(w, r, id)
		case "PATCH":
			providerPatch(w, r, id)
		case "DELETE":
			providerDelete(w, r, id)
		default:
			writeError(w, 405, "method not allowed")
		}
		return
	}

	writeError(w, 404, "not found")
}

func providerGet(w http.ResponseWriter, r *http.Request, id string) {
	var name, baseURL, icon, proxyID, createdAt, providerType, authType, builtinID, customHeaders string
	var isActive, roundRobin, proxyEnabled, roundRobinEnabled int
	err := db.DB.QueryRow(
		`SELECT name, base_url, icon, is_active, round_robin, COALESCE(proxy_id,''), COALESCE(proxy_enabled,0), created_at,
		COALESCE(provider_type,'custom'), COALESCE(auth_type,'apikey'), COALESCE(builtin_id,''), COALESCE(round_robin_enabled,0),
		COALESCE(custom_headers,'{}')
		FROM providers WHERE id=?`, id,
	).Scan(&name, &baseURL, &icon, &isActive, &roundRobin, &proxyID, &proxyEnabled, &createdAt,
		&providerType, &authType, &builtinID, &roundRobinEnabled, &customHeaders)
	if err != nil {
		writeError(w, 404, "provider not found")
		return
	}

	// Get key counts (api_keys + provider_connections for connection-type providers)
	var totalKeys, activeKeys int
	db.DB.QueryRow("SELECT COUNT(*) FROM api_keys WHERE provider_id=?", id).Scan(&totalKeys)
	db.DB.QueryRow("SELECT COUNT(*) FROM api_keys WHERE provider_id=? AND is_active=1", id).Scan(&activeKeys)

	// Also count connections for connection-type providers
	if authType == "connection" {
		var connTotal, connActive int
		db.DB.QueryRow("SELECT COUNT(*) FROM provider_connections WHERE provider_id=?", id).Scan(&connTotal)
		db.DB.QueryRow("SELECT COUNT(*) FROM provider_connections WHERE provider_id=? AND is_active=1", id).Scan(&connActive)
		totalKeys += connTotal
		activeKeys += connActive
	}

	writeJSON(w, map[string]interface{}{
		"id": id, "name": name, "base_url": baseURL, "icon": icon,
		"is_active": isActive == 1, "round_robin": roundRobin == 1,
		"proxy_id": proxyID, "proxy_enabled": proxyEnabled == 1,
		"created_at":    createdAt,
		"provider_type": providerType, "auth_type": authType,
		"builtin_id":          builtinID,
		"round_robin_enabled": roundRobinEnabled == 1,
		"custom_headers":      customHeaders,
		"key_count":           totalKeys,
		"active_key_count":    activeKeys,
		"status": func() string {
			if isActive == 1 && activeKeys > 0 {
				return "online"
			}
			return "offline"
		}(),
	})
}

func providerUpdate(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Name           *string `json:"name"`
		BaseURL        *string `json:"base_url"`
		Icon           *string `json:"icon"`
		IsActive       *bool   `json:"is_active"`
		RoundRobin     *bool   `json:"round_robin"`
		CustomHeaders  *string `json:"custom_headers"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}

	sets := []string{}
	args := []interface{}{}
	if body.Name != nil {
		sets = append(sets, "name=?")
		args = append(args, *body.Name)
	}
	if body.BaseURL != nil {
		sets = append(sets, "base_url=?")
		args = append(args, *body.BaseURL)
	}
	if body.Icon != nil {
		sets = append(sets, "icon=?")
		args = append(args, *body.Icon)
	}
	if body.IsActive != nil {
		v := 0
		if *body.IsActive {
			v = 1
		}
		sets = append(sets, "is_active=?")
		args = append(args, v)
	}
	if body.RoundRobin != nil {
		v := 0
		if *body.RoundRobin {
			v = 1
		}
		sets = append(sets, "round_robin=?")
		args = append(args, v)
	}
	if body.CustomHeaders != nil {
		sets = append(sets, "custom_headers=?")
		args = append(args, *body.CustomHeaders)
	}
	if len(sets) == 0 {
		writeError(w, 400, "nothing to update")
		return
	}

	sets = append(sets, "updated_at=?")
	args = append(args, time.Now().UTC().Format(time.RFC3339))
	args = append(args, id)

	_, err := db.DB.Exec("UPDATE providers SET "+strings.Join(sets, ", ")+" WHERE id=?", args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	providerGet(w, r, id)
}

func providerDelete(w http.ResponseWriter, r *http.Request, id string) {
	// Guard: builtin providers cannot be deleted
	var providerType string
	db.DB.QueryRow("SELECT COALESCE(provider_type,'custom') FROM providers WHERE id=?", id).Scan(&providerType)
	if providerType == "builtin" {
		writeError(w, 403, "builtin providers cannot be deleted")
		return
	}
	result, err := db.DB.Exec("DELETE FROM providers WHERE id=?", id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, 404, "provider not found")
		return
	}
	writeJSON(w, map[string]string{"status": "deleted", "id": id})
}

func providerPatch(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		RoundRobin *bool `json:"round_robin"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.RoundRobin != nil {
		v := 0
		if *body.RoundRobin {
			v = 1
		}
		_, err := db.DB.Exec("UPDATE providers SET round_robin=?, updated_at=? WHERE id=?", v, time.Now().UTC().Format(time.RFC3339), id)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
	}
	providerGet(w, r, id)
}

func providerPatchProxy(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ProxyID      *string `json:"proxy_id"`
		ProxyEnabled *bool   `json:"proxy_enabled"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}

	sets := []string{}
	args := []interface{}{}
	if body.ProxyID != nil {
		sets = append(sets, "proxy_id=?")
		args = append(args, *body.ProxyID)
	}
	if body.ProxyEnabled != nil {
		v := 0
		if *body.ProxyEnabled {
			v = 1
		}
		sets = append(sets, "proxy_enabled=?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		writeError(w, 400, "nothing to update")
		return
	}
	sets = append(sets, "updated_at=?")
	args = append(args, time.Now().UTC().Format(time.RFC3339))
	args = append(args, id)

	_, err := db.DB.Exec("UPDATE providers SET "+strings.Join(sets, ", ")+" WHERE id=?", args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	providerGet(w, r, id)
}

func providerTestPrompt(w http.ResponseWriter, r *http.Request, providerID string) {
	var body struct {
		ModelID string      `json:"model_id"`
		Prompt  string      `json:"prompt"`
		KeyID   interface{} `json:"key_id"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.ModelID == "" || body.Prompt == "" {
		writeError(w, 400, "model_id and prompt required")
		return
	}

	// Normalize key_id to string
	keyIDStr := ""
	if body.KeyID != nil {
		switch v := body.KeyID.(type) {
		case string:
			keyIDStr = v
		case float64:
			keyIDStr = fmt.Sprintf("%.0f", v)
		default:
			keyIDStr = fmt.Sprintf("%v", v)
		}
	}

	// Get provider
	var baseURL, name, authType string
	err := db.DB.QueryRow("SELECT base_url, name, COALESCE(auth_type,'apikey') FROM providers WHERE id=?", providerID).Scan(&baseURL, &name, &authType)
	if err != nil {
		writeError(w, 404, "provider not found")
		return
	}

	// Get keys: specific key_id or all active keys
	// For connection-type providers, also check api_keys with key_type='oauth'
	var rows *sql.Rows
	if authType == "connection" && keyIDStr != "" {
		// User selected a specific key — look it up directly (works for both apikey and oauth)
		rows, err = db.DB.Query("SELECT id, name, key_encrypted, COALESCE(account_id,'') FROM api_keys WHERE id=? AND provider_id=?", keyIDStr, providerID)
	} else if authType == "connection" {
		// No specific key selected — get all active keys (including oauth-type)
		rows, err = db.DB.Query("SELECT id, name, key_encrypted, COALESCE(account_id,'') FROM api_keys WHERE provider_id=? AND is_active=1", providerID)
	} else if keyIDStr != "" {
		rows, err = db.DB.Query("SELECT id, name, key_encrypted, COALESCE(account_id,'') FROM api_keys WHERE id=? AND provider_id=?", keyIDStr, providerID)
	} else {
		rows, err = db.DB.Query("SELECT id, name, key_encrypted, COALESCE(account_id,'') FROM api_keys WHERE provider_id=? AND is_active=1", providerID)
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if rows != nil {
		defer rows.Close()
	}

	// Check if we got any keys, if not fallback to connections

	var results []map[string]interface{}
	if rows != nil {
		for rows.Next() {
			var keyID, keyName, keyVal, accountID string
			rows.Scan(&keyID, &keyName, &keyVal, &accountID)

			// Auto-refresh OAuth key if expired
			var keyType string
			db.DB.QueryRow("SELECT COALESCE(key_type,'apikey') FROM api_keys WHERE id=?", keyID).Scan(&keyType)
			if keyType == "oauth" {
				var expiresAt string
				db.DB.QueryRow("SELECT COALESCE(oauth_expires_at,'') FROM api_keys WHERE id=?", keyID).Scan(&expiresAt)
				if refreshed, err := GetOAuthKeyValue(keyID, keyVal, expiresAt); err == nil {
					keyVal = refreshed
				}
			}

			start := time.Now()
			// Build request — use resolveUpstreamURL for Cloudflare + account_id support
			upstreamURL := resolveUpstreamURL(baseURL, accountID)
			isMerlin := strings.Contains(strings.ToLower(baseURL), "getmerlin")

			var reqBody []byte
			if isMerlin {
				reqBody, _ = json.Marshal(convertToMerlinBody(map[string]interface{}{
					"messages": []map[string]string{{"role": "user", "content": body.Prompt}},
				}, body.ModelID))
			} else {
				reqBody, _ = json.Marshal(map[string]interface{}{
					"model":      body.ModelID,
					"messages":   []map[string]string{{"role": "user", "content": body.Prompt}},
					"max_tokens": 1000,
				})
			}

			req, _ := http.NewRequest("POST", upstreamURL, strings.NewReader(string(reqBody)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+keyVal)
			if strings.Contains(baseURL, "kimchi") {
				req.Header.Set("User-Agent", "kimchi/0.1.50")
			}
			if isMerlin {
				req.Header.Set("x-merlin-version", "web-merlin")
				req.Header.Set("x-request-timestamp", time.Now().Format("2006-01-02T15:04:05.000-07:00"))
				req.Header.Set("Accept", "text/event-stream")
			}

			client := &http.Client{Timeout: 30 * time.Second}
			proxyUsed := ""
			if proxyURL := getProviderProxy(providerID); proxyURL != "" {
				if transport, err := makeProxyTransport(proxyURL); err == nil {
					client.Transport = transport
					proxyUsed = proxyURL
				}
			}
			resp, err := client.Do(req)
			latency := time.Since(start).Milliseconds()

			result := map[string]interface{}{
				"key_id":     keyID,
				"key_name":   keyName,
				"latency_ms": latency,
				"proxy":      proxyUsed,
			}
			if err != nil {
				result["error"] = err.Error()
				result["status"] = 0
			} else {
				defer resp.Body.Close()
				buf := new(strings.Builder)
				io.Copy(buf, resp.Body)
				raw := buf.String()
				result["status"] = resp.StatusCode

				if isMerlin {
					// Parse Merlin SSE response
					var textParts []string
					currentEvent := ""
					for _, line := range strings.Split(raw, "\n") {
						line = strings.TrimSpace(line)
						if strings.HasPrefix(line, "event:") {
							currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
							continue
						}
						if !strings.HasPrefix(line, "data:") || currentEvent != "message" {
							continue
						}
						dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
						var data map[string]interface{}
						if json.Unmarshal([]byte(dataStr), &data) == nil {
							if msgData, ok := data["data"].(map[string]interface{}); ok {
								if text, ok := msgData["text"].(string); ok && text != "" {
									textParts = append(textParts, text)
								}
							}
						}
					}
					result["content"] = strings.Join(textParts, "")
					result["response"] = raw[:min(len(raw), 500)]
				} else {
					// Parse OpenAI JSON response
					var parsed map[string]interface{}
					if json.Unmarshal([]byte(raw), &parsed) == nil {
						if choices, ok := parsed["choices"].([]interface{}); ok && len(choices) > 0 {
							if msg, ok := choices[0].(map[string]interface{}); ok {
								if m, ok := msg["message"].(map[string]interface{}); ok {
									result["content"] = m["content"]
									result["reasoning_content"] = m["reasoning_content"]
								}
							}
						}
						if usage, ok := parsed["usage"].(map[string]interface{}); ok {
							result["usage"] = usage
						}
					}
					result["response"] = raw[:min(len(raw), 500)]
				}

				// Auto-disable key on auth failure or payment required
				if resp.StatusCode != 200 {
					autoDisableKey(keyID, keyName, resp.StatusCode, "")
				}
			}
			results = append(results, result)
		}
	} // end if rows != nil
	if results == nil {
		results = []map[string]interface{}{}
	}

	// Format results for frontend
	var formatted []map[string]interface{}
	for _, r := range results {
		content := ""
		if c, ok := r["content"]; ok && c != nil && fmt.Sprintf("%v", c) != "" {
			content = fmt.Sprintf("%v", c)
		} else if c, ok := r["reasoning_content"]; ok && c != nil && fmt.Sprintf("%v", c) != "" {
			content = fmt.Sprintf("%v", c)
		} else if c, ok := r["response"]; ok && c != nil {
			content = fmt.Sprintf("%v", c)
		} else if c, ok := r["error"]; ok && c != nil {
			content = fmt.Sprintf("Error: %v", c)
		}
		status := 0
		if s, ok := r["status"]; ok && s != nil {
			status = s.(int)
		}
		latency := int64(0)
		if l, ok := r["latency_ms"]; ok && l != nil {
			latency = l.(int64)
		}
		keyName := ""
		if k, ok := r["key_name"]; ok && k != nil {
			keyName = k.(string)
		}
		formatted = append(formatted, map[string]interface{}{
			"status":     status,
			"latency_ms": latency,
			"res":        content,
			"key":        keyName,
		})
	}

	if len(formatted) == 0 {
		// Fallback: check provider_connections for OAuth tokens
		// If a specific connection was selected (key_id matches a connection), use that one
		var connID, connEmail, connToken string
		var connErr error
		if keyIDStr != "" {
			connErr = db.DB.QueryRow(`SELECT id, COALESCE(email,''), COALESCE(access_token, COALESCE(api_key,''))
				FROM provider_connections WHERE id=? AND provider_id=? AND is_active=1`, keyIDStr, providerID).Scan(&connID, &connEmail, &connToken)
		} else {
			connErr = db.DB.QueryRow(`SELECT id, COALESCE(email,''), COALESCE(access_token, COALESCE(api_key,''))
				FROM provider_connections WHERE provider_id=? AND is_active=1 ORDER BY created_at DESC LIMIT 1`, providerID).Scan(&connID, &connEmail, &connToken)
		}
		if connErr == nil && connToken != "" {
			// Use connection token to make request
			startTime := time.Now()
			reqBody := map[string]interface{}{
				"model":    body.ModelID,
				"messages": []map[string]interface{}{{"role": "user", "content": body.Prompt}},
				"stream":   false,
			}
			bodyBytes, _ := json.Marshal(reqBody)

			// Check if Anigravity (special handling)
			if providerID == "builtin-anigravity" {
				// Use Anigravity translator
				content, latency, err := testAnigravityRequest(body.ModelID, body.Prompt, connToken)
				if err != nil {
					formatted = []map[string]interface{}{{"status": 500, "latency_ms": 0, "res": err.Error(), "key": connEmail}}
				} else {
					formatted = []map[string]interface{}{{"status": 200, "latency_ms": latency, "res": content, "key": connEmail}}
				}
			} else {
				// Standard request with connection token
				var reqURL string
				db.DB.QueryRow("SELECT base_url FROM providers WHERE id=?", providerID).Scan(&reqURL)
				reqURL = strings.TrimRight(reqURL, "/") + "/chat/completions"
				req, _ := http.NewRequest("POST", reqURL, bytes.NewReader(bodyBytes))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+connToken)
				client := sharedHTTPClient
				resp, err := client.Do(req)
				latency := time.Since(startTime).Milliseconds()
				if err != nil {
					formatted = []map[string]interface{}{{"status": 502, "latency_ms": latency, "res": err.Error(), "key": connEmail}}
				} else {
					defer resp.Body.Close()
					respBody, _ := io.ReadAll(resp.Body)
					var parsed map[string]interface{}
					json.Unmarshal(respBody, &parsed)
					content := ""
					if choices, ok := parsed["choices"].([]interface{}); ok && len(choices) > 0 {
						if msg, ok := choices[0].(map[string]interface{}); ok {
							if m, ok := msg["message"].(map[string]interface{}); ok {
								content, _ = m["content"].(string)
							}
						}
					}
					if content == "" {
						content = string(respBody)
						if len(content) > 500 {
							content = content[:500] + "..."
						}
					}
					formatted = []map[string]interface{}{{"status": resp.StatusCode, "latency_ms": latency, "res": content, "key": connEmail}}
				}
			}
		} else {
			formatted = []map[string]interface{}{{
				"status":     0,
				"latency_ms": 0,
				"res":        "No keys available",
				"key":        "",
			}}
		}
	}

	writeJSON(w, map[string]interface{}{
		"results": formatted,
	})
}

// providerTestPromptStream — SSE streaming test prompt for a single key
func providerTestPromptStream(w http.ResponseWriter, r *http.Request, providerID string) {
	var body struct {
		ModelID string      `json:"model_id"`
		Prompt  string      `json:"prompt"`
		KeyID   interface{} `json:"key_id"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.ModelID == "" || body.Prompt == "" {
		writeError(w, 400, "model_id and prompt required")
		return
	}

	// Normalize key_id to string
	keyIDStr := ""
	if body.KeyID != nil {
		switch v := body.KeyID.(type) {
		case string:
			keyIDStr = v
		case float64:
			keyIDStr = fmt.Sprintf("%.0f", v)
		default:
			keyIDStr = fmt.Sprintf("%v", v)
		}
	}

	// Get provider
	var baseURL, providerName string
	err := db.DB.QueryRow("SELECT base_url, name FROM providers WHERE id=?", providerID).Scan(&baseURL, &providerName)
	if err != nil {
		writeError(w, 404, "provider not found")
		return
	}

	// Get keys: specific or all active (includes OAuth keys)
	type keyInfo struct {
		id, name, value, accountID string
	}
	var keys []keyInfo
	if keyIDStr != "" {
		var k keyInfo
		err = db.DB.QueryRow("SELECT id, name, key_encrypted, COALESCE(account_id,'') FROM api_keys WHERE id=? AND provider_id=?", keyIDStr, providerID).Scan(&k.id, &k.name, &k.value, &k.accountID)
		if err != nil {
			writeError(w, 404, "key not found")
			return
		}
		keys = append(keys, k)
	} else {
		rows, err := db.DB.Query("SELECT id, name, key_encrypted, COALESCE(account_id,'') FROM api_keys WHERE provider_id=? AND is_active=1", providerID)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		defer rows.Close()
		for rows.Next() {
			var k keyInfo
			rows.Scan(&k.id, &k.name, &k.value, &k.accountID)
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		writeError(w, 400, "no active keys found")
		return
	}

	// Auto-refresh OAuth keys if expired
	for i := range keys {
		var keyType string
		db.DB.QueryRow("SELECT COALESCE(key_type,'apikey') FROM api_keys WHERE id=?", keys[i].id).Scan(&keyType)
		if keyType == "oauth" {
			var expiresAt string
			db.DB.QueryRow("SELECT COALESCE(oauth_expires_at,'') FROM api_keys WHERE id=?", keys[i].id).Scan(&expiresAt)
			if refreshed, err := GetOAuthKeyValue(keys[i].id, keys[i].value, expiresAt); err == nil {
				keys[i].value = refreshed
			}
		}
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	reqBodyBytes, _ := json.Marshal(map[string]interface{}{
		"model":    body.ModelID,
		"messages": []map[string]string{{"role": "user", "content": body.Prompt}},
		"stream":   true,
	})

	// Stream each key sequentially
	for ki, k := range keys {
		start := time.Now()
		upstreamURL := resolveUpstreamURL(baseURL, k.accountID)
		req, _ := http.NewRequest("POST", upstreamURL, bytes.NewReader(reqBodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+k.value)
		if strings.Contains(baseURL, "kimchi") {
			req.Header.Set("User-Agent", "kimchi/0.1.50")
		}

		client := sharedHTTPClient
		proxyUsed := ""
		if proxyURL := getProviderProxy(providerID); proxyURL != "" {
			if transport, perr := makeProxyTransport(proxyURL); perr == nil {
				client.Transport = transport
				proxyUsed = proxyURL
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			errData, _ := json.Marshal(map[string]interface{}{
				"type": "error", "key_name": k.name, "key_id": k.id, "error": err.Error(),
			})
			fmt.Fprintf(w, "data: %s\n\n", errData)
			flusher.Flush()
			continue
		}

		if resp.StatusCode != 200 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			// Auto-disable on auth failure or payment required
			if resp.StatusCode == 401 || resp.StatusCode == 402 || resp.StatusCode == 403 {
				autoDisableKey(k.id, k.name, resp.StatusCode, string(errBody))
			}
			errData, _ := json.Marshal(map[string]interface{}{
				"type": "error", "key_name": k.name, "key_id": k.id,
				"error": fmt.Sprintf("status %d: %s", resp.StatusCode, string(errBody[:min(len(errBody), 200)])),
			})
			fmt.Fprintf(w, "data: %s\n\n", errData)
			flusher.Flush()
			continue
		}

		// Send meta for this key
		meta, _ := json.Marshal(map[string]interface{}{
			"type":      "meta",
			"key_name":  k.name,
			"key_id":    k.id,
			"model":     body.ModelID,
			"key_index": ki,
			"key_total": len(keys),
			"proxy":     proxyUsed,
		})
		fmt.Fprintf(w, "data: %s\n\n", meta)
		flusher.Flush()

		var fullContent strings.Builder
		totalTokens := 0
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)
			if data == "[DONE]" {
				break
			}

			var chunk map[string]interface{}
			if json.Unmarshal([]byte(data), &chunk) != nil {
				continue
			}

			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if c, ok := choices[0].(map[string]interface{}); ok {
					if delta, ok := c["delta"].(map[string]interface{}); ok {
						if content, ok := delta["content"].(string); ok && content != "" {
							fullContent.WriteString(content)
							chunkData, _ := json.Marshal(map[string]interface{}{
								"type":    "content",
								"content": content,
							})
							fmt.Fprintf(w, "data: %s\n\n", chunkData)
							flusher.Flush()
						}
					}
				}
			}

			if usage, ok := chunk["usage"].(map[string]interface{}); ok {
				if pt, ok := usage["total_tokens"].(float64); ok {
					totalTokens = int(pt)
				}
			}
		}
		resp.Body.Close()

		latency := time.Since(start).Milliseconds()
		if totalTokens == 0 {
			totalTokens = len(fullContent.String()) / 4
		}

		// Send done for this key
		resultData, _ := json.Marshal(map[string]interface{}{
			"type":         "done",
			"content":      fullContent.String(),
			"latency_ms":   latency,
			"total_tokens": totalTokens,
			"key_name":     k.name,
			"key_id":       k.id,
			"key_index":    ki,
			"key_total":    len(keys),
		})
		fmt.Fprintf(w, "data: %s\n\n", resultData)
		flusher.Flush()
	}

	// Final all-done event
	finalData, _ := json.Marshal(map[string]interface{}{
		"type":      "all-done",
		"key_count": len(keys),
	})
	fmt.Fprintf(w, "data: %s\n\n", finalData)
	flusher.Flush()
}

func providerKeyBulkCreate(w http.ResponseWriter, r *http.Request, providerID string) {
	var body struct {
		Count      int    `json:"count"`
		NamePrefix string `json:"name_prefix"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.Count <= 0 {
		writeError(w, 400, "count must be > 0")
		return
	}
	if body.NamePrefix == "" {
		body.NamePrefix = "key"
	}

	// Find max key number
	var maxNum int
	db.DB.QueryRow(`SELECT COALESCE(MAX(CAST(SUBSTR(name, 5) AS INTEGER)), 0) FROM api_keys WHERE provider_id=? AND name LIKE '`+body.NamePrefix+`-%'`, providerID).Scan(&maxNum)

	var created []map[string]interface{}
	for i := 1; i <= body.Count; i++ {
		id := genID()
		name := fmt.Sprintf("%s-%d", body.NamePrefix, maxNum+i)
		raw := make([]byte, 24)
		rand.Read(raw)
		keyVal := "sk-" + hex.EncodeToString(raw)
		_, err := db.DB.Exec("INSERT INTO api_keys (id, provider_id, name, key_encrypted, is_active) VALUES (?, ?, ?, ?, 1)", id, providerID, name, keyVal)
		if err != nil {
			continue
		}
		created = append(created, map[string]interface{}{"id": id, "name": name, "key": keyVal})
	}
	writeJSON(w, map[string]interface{}{"created": len(created), "keys": created})
}

func providerKeyDelete(w http.ResponseWriter, r *http.Request, providerID, keyID string) {
	result, err := db.DB.Exec("DELETE FROM api_keys WHERE id=? AND provider_id=?", keyID, providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, 404, "key not found")
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

func providerKeyToggleActive(w http.ResponseWriter, r *http.Request, providerID, keyID string) {
	var body struct {
		IsActive *bool `json:"is_active"`
	}
	parseBody(r, &body) // ignore error — body is optional for toggle

	var current int
	err := db.DB.QueryRow("SELECT is_active FROM api_keys WHERE id=? AND provider_id=?", keyID, providerID).Scan(&current)
	if err != nil {
		writeError(w, 404, "key not found")
		return
	}

	// If body provides is_active, use it; otherwise toggle
	newVal := 1 - current // flip
	if body.IsActive != nil {
		if *body.IsActive {
			newVal = 1
		} else {
			newVal = 0
		}
	}

	if newVal == 1 {
		db.DB.Exec("UPDATE api_keys SET fail_count=0, last_error='' WHERE id=? AND provider_id=?", keyID, providerID)
	}
	_, err = db.DB.Exec("UPDATE api_keys SET is_active=? WHERE id=? AND provider_id=?", newVal, keyID, providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"id": keyID, "is_active": newVal == 1})
}

func providerKeyTest(w http.ResponseWriter, r *http.Request, providerID, keyID string) {
	var keyVal, name string
	err := db.DB.QueryRow("SELECT key_encrypted, name FROM api_keys WHERE id=? AND provider_id=?", keyID, providerID).Scan(&keyVal, &name)
	if err != nil {
		writeError(w, 404, "key not found")
		return
	}

	var providerName, baseURL string
	db.DB.QueryRow("SELECT name, base_url FROM providers WHERE id=?", providerID).Scan(&providerName, &baseURL)

	// Step 1: Format validation (fast, no network)
	formatValid := true
	formatReason := "ok"
	switch providerName {
	case "DeepSeek":
		if !strings.HasPrefix(keyVal, "sk-") {
			formatValid = false
			formatReason = "DeepSeek key must start with sk-"
		} else if strings.HasPrefix(keyVal, "sk-or-") {
			formatValid = false
			formatReason = "This looks like an OpenRouter key, not DeepSeek"
		}
	case "Xiaomi MiMo":
		if !strings.HasPrefix(keyVal, "sk-") {
			formatValid = false
			formatReason = "MiMo key must start with sk-"
		} else if strings.HasPrefix(keyVal, "sk-or-") {
			formatValid = false
			formatReason = "This looks like an OpenRouter key, not MiMo"
		} else if strings.HasPrefix(keyVal, "castai_") {
			formatValid = false
			formatReason = "This looks like a Kimchi key, not MiMo"
		} else if strings.HasPrefix(keyVal, "AIzaSy") {
			formatValid = false
			formatReason = "This looks like an AI Studio key, not MiMo"
		}
	case "OpenRouter":
		if !strings.HasPrefix(keyVal, "sk-or-") {
			formatValid = false
			formatReason = "OpenRouter key must start with sk-or-"
		}
	case "Google AI Studio":
		if !strings.HasPrefix(keyVal, "AIzaSy") {
			formatValid = false
			formatReason = "AI Studio key must start with AIzaSy"
		}
	case "Kimchi":
		if !strings.HasPrefix(keyVal, "castai_") {
			formatValid = false
			formatReason = "Kimchi key must start with castai_"
		}
	case "Meta":
		if !strings.HasPrefix(keyVal, "LLM|") {
			formatValid = false
			formatReason = "Meta key must start with LLM| (format: LLM|<id>|<secret>)"
		} else {
			parts := strings.Split(keyVal, "|")
			if len(parts) != 3 {
				formatValid = false
				formatReason = "Meta key format invalid — expected LLM|<numeric_id>|<secret>"
			}
		}
	default:
		if keyVal == "" {
			formatValid = false
			formatReason = "empty key"
		}
		// Allow unknown provider types if they look like standard formats
		if formatValid && !strings.HasPrefix(keyVal, "sk-") && !strings.HasPrefix(keyVal, "sk-or-") && !strings.HasPrefix(keyVal, "AIzaSy") && !strings.HasPrefix(keyVal, "castai_") && !strings.HasPrefix(keyVal, "LLM|") {
			if len(keyVal) < 8 {
				formatValid = false
				formatReason = "key too short"
			}
		}
	}

	if !formatValid {
		writeJSON(w, map[string]interface{}{"id": keyID, "name": name, "valid": false, "reason": formatReason, "api_status": "skipped"})
		return
	}

	// Step 2: Live API test (hit models endpoint)
	upstreamURL := resolveUpstreamURL(baseURL, "")
	modelsURL := strings.TrimRight(upstreamURL, "/chat/completions") + "/models"

	req, _ := http.NewRequest("GET", modelsURL, nil)
	req.Header.Set("Authorization", "Bearer "+keyVal)
	if strings.Contains(baseURL, "kimchi") {
		req.Header.Set("User-Agent", "kimchi/0.1.50")
	}
	if strings.Contains(baseURL, "getmerlin") {
		req.Header.Set("x-merlin-version", "web-merlin")
		req.Header.Set("Content-Type", "application/json")
		// Merlin doesn't have /models endpoint, use chat completions with minimal prompt
		modelsURL = strings.TrimRight(baseURL, "/") + "/arcane/api/v2/thread/unified"
		req, _ = http.NewRequest("POST", modelsURL, strings.NewReader(`{"message":{"content":"hi"},"model":"gemini-2.5-flash-lite","mode":"UNIFIED_CHAT","attachments":[],"language":"AUTO"}`))
		req.Header.Set("Authorization", "Bearer "+keyVal)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
		req.Header.Set("x-merlin-version", "web-merlin")
		req.Header.Set("x-request-timestamp", time.Now().Format("2006-01-02T15:04:05.000-07:00"))
	}

	client := &http.Client{Timeout: 10 * time.Second}
	if proxyURL := getProviderProxy(providerID); proxyURL != "" {
		if transport, terr := makeProxyTransport(proxyURL); terr == nil {
			client.Transport = transport
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, map[string]interface{}{"id": keyID, "name": name, "valid": false, "reason": "API unreachable: " + err.Error(), "api_status": "error"})
		return
	}
	defer resp.Body.Close()
	resp.Body.Close()

	if resp.StatusCode == 200 {
		writeJSON(w, map[string]interface{}{"id": keyID, "name": name, "valid": true, "reason": "ok", "api_status": resp.StatusCode})
	} else if resp.StatusCode == 401 || resp.StatusCode == 403 {
		writeJSON(w, map[string]interface{}{"id": keyID, "name": name, "valid": false, "reason": fmt.Sprintf("API rejected key — HTTP %d (unauthorized)", resp.StatusCode), "api_status": resp.StatusCode})
	} else if resp.StatusCode == 402 {
		writeJSON(w, map[string]interface{}{"id": keyID, "name": name, "valid": false, "reason": fmt.Sprintf("HTTP 402 — no credits/quota remaining"), "api_status": resp.StatusCode})
	} else {
		writeJSON(w, map[string]interface{}{"id": keyID, "name": name, "valid": true, "reason": fmt.Sprintf("Key accepted — HTTP %d", resp.StatusCode), "api_status": resp.StatusCode})
	}
}

// providerModelUpdateSelection updates which models are selected
func providerModelUpdateSelection(w http.ResponseWriter, r *http.Request, providerID string) {
	var body struct {
		Models []string `json:"models"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}

	// Deselect all models for this provider
	db.DB.Exec("UPDATE models SET is_selected=0 WHERE provider_id=?", providerID)

	// Select the specified models
	for _, modelID := range body.Models {
		db.DB.Exec("UPDATE models SET is_selected=1 WHERE provider_id=? AND model_id=?", providerID, modelID)
	}

	// Return updated list
	providerModelList(w, r, providerID)
}

func providerModelAdd(w http.ResponseWriter, r *http.Request, providerID string) {
	var body struct {
		ModelID string `json:"model_id"`
		IsFree  *bool  `json:"is_free"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.ModelID == "" {
		writeError(w, 400, "model_id required")
		return
	}
	isFree := 0
	if body.IsFree != nil && *body.IsFree {
		isFree = 1
	}
	id := genID()
	_, err := db.DB.Exec("INSERT OR IGNORE INTO models (id, provider_id, model_id, is_free, is_selected) VALUES (?, ?, ?, ?, 1)", id, providerID, body.ModelID, isFree)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"id": id, "provider_id": providerID, "model_id": body.ModelID, "is_selected": true})
}

// providerDetectModels fetches models from the provider's /v1/models endpoint
func providerDetectModels(w http.ResponseWriter, r *http.Request, providerID string) {
	var baseURL, builtinID, authType string
	var oauthToken string
	err := db.DB.QueryRow("SELECT base_url, COALESCE(builtin_id,''), COALESCE(auth_type,'apikey'), COALESCE(oauth_access_token,'') FROM providers WHERE id=?", providerID).Scan(&baseURL, &builtinID, &authType, &oauthToken)
	if err != nil {
		writeError(w, 404, "provider not found")
		return
	}

	// Provider-specific detect URLs and headers
	modelsURL := strings.TrimRight(baseURL, "/") + "/models"
	extraHeaders := map[string]string{}

	switch builtinID {
	case "anigravity":
		// Anigravity uses hardcoded models (API requires OAuth, no /v1/models endpoint)
		anigravityModels := []string{
			"gemini-3-flash-agent",
			"gemini-3.5-flash-low",
			"gemini-3.5-flash-extra-low",
			"gemini-pro-agent",
			"gemini-3.1-pro-low",
			"claude-sonnet-4-6",
			"claude-opus-4-6-thinking",
			"gpt-oss-120b-medium",
			"gemini-3-flash",
			"gemini-3.1-flash-image",
		}
		added := 0
		for _, m := range anigravityModels {
			var exists int
			db.DB.QueryRow("SELECT COUNT(*) FROM models WHERE provider_id=? AND model_id=?", providerID, m).Scan(&exists)
			if exists == 0 {
				id := genID()
				db.DB.Exec("INSERT OR IGNORE INTO models (id, provider_id, model_id, is_free, is_selected, created_at) VALUES (?, ?, ?, 0, 0, ?)", id, providerID, m, time.Now().Unix())
				added++
			}
		}
		writeJSON(w, map[string]interface{}{"detected": len(anigravityModels), "added": added})
		return
	case "kimchi":
		modelsURL = "https://llm.kimchi.dev/v1/models/metadata?include_in_cli=true"
		extraHeaders["User-Agent"] = "kimchi/0.1.50"
	case "openrouter":
		modelsURL = "https://openrouter.ai/api/v1/models"
	case "grok-cli":
		modelsURL = "https://cli-chat-proxy.grok.com/v1/models"
		extraHeaders["User-Agent"] = "grok-pager/0.2.93 grok-shell/0.2.93 (linux; x86_64)"
		extraHeaders["x-xai-token-auth"] = "xai-grok-cli"
		extraHeaders["x-grok-client-identifier"] = "grok-pager"
		extraHeaders["x-grok-client-version"] = "0.2.93"
	}

	req, _ := http.NewRequest("GET", modelsURL, nil)

	if authType == "oauth" && oauthToken != "" {
		setProviderAuth(req, baseURL, oauthToken)
	} else {
		// Try to get first active API key
		var keyValue string
		err = db.DB.QueryRow("SELECT key_encrypted FROM api_keys WHERE provider_id=? AND is_active=1 LIMIT 1", providerID).Scan(&keyValue)
		if err == nil && keyValue != "" {
			req.Header.Set("Authorization", "Bearer "+keyValue)
		}
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, 502, "failed to fetch models: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		writeError(w, resp.StatusCode, fmt.Sprintf("upstream returned %d: %s", resp.StatusCode, string(body)))
		return
	}

	bodyBytes, _ := io.ReadAll(resp.Body)

	// Provider-specific response parsing
	type modelEntry struct {
		ID string `json:"id"`
	}

	var detected []modelEntry

	switch builtinID {
	case "kimchi":
		// Kimchi returns {models: [{slug, display_name, ...}]}
		var kimchiResp struct {
			Models []struct {
				Slug        string `json:"slug"`
				DisplayName string `json:"display_name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(bodyBytes, &kimchiResp); err != nil {
			writeError(w, 502, "invalid kimchi models response")
			return
		}
		for _, m := range kimchiResp.Models {
			if m.Slug != "" {
				detected = append(detected, modelEntry{ID: m.Slug})
			}
		}
	case "openrouter":
		// OpenRouter returns {data: [{id, ...}]} — filter to free models only
		var orResp struct {
			Data []struct {
				ID      string `json:"id"`
				Pricing struct {
					Prompt string `json:"prompt"`
				} `json:"pricing"`
			} `json:"data"`
		}
		if err := json.Unmarshal(bodyBytes, &orResp); err != nil {
			writeError(w, 502, "invalid openrouter models response")
			return
		}
		allowed := map[string]bool{
			"inclusionai/ling-3.0-flash:free":                    true,
			"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free": true,
			"nvidia/nemotron-3-super-120b-a12b:free":             true,
			"nvidia/nemotron-3-ultra-550b-a55b:free":             true,
		}
		for _, m := range orResp.Data {
			if allowed[m.ID] {
				detected = append(detected, modelEntry{ID: m.ID})
			}
		}
	case "anigravity":
		// Anigravity uses hardcoded models (API requires OAuth)
		anigravityModels := []string{
			"gemini-3-flash-agent",
			"gemini-3.5-flash-low",
			"gemini-3.5-flash-extra-low",
			"gemini-pro-agent",
			"gemini-3.1-pro-low",
			"claude-sonnet-4-6",
			"claude-opus-4-6-thinking",
			"gpt-oss-120b-medium",
			"gemini-3-flash",
			"gemini-3.1-flash-image",
		}
		for _, m := range anigravityModels {
			detected = append(detected, modelEntry{ID: m})
		}
	default:
		// Standard OpenAI format: {data: [{id, ...}]}
		var modelsResp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(bodyBytes, &modelsResp); err != nil {
			writeError(w, 502, "invalid models response")
			return
		}
		for _, m := range modelsResp.Data {
			detected = append(detected, modelEntry{ID: m.ID})
		}
	}

	// Upsert models into DB
	added := 0
	for _, m := range detected {
		if m.ID == "" {
			continue
		}
		id := genID()
		result, _ := db.DB.Exec("INSERT OR IGNORE INTO models (id, provider_id, model_id, is_free, is_selected) VALUES (?, ?, ?, 0, 1)", id, providerID, m.ID)
		if result != nil {
			n, _ := result.RowsAffected()
			if n > 0 {
				added++
			}
		}
	}

	writeJSON(w, map[string]interface{}{
		"detected": len(detected),
		"added":    added,
		"models":   detected,
	})
}

func providerModelToggleSelect(w http.ResponseWriter, r *http.Request, providerID, modelID string) {
	var body struct {
		IsSelected *bool `json:"is_selected"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.IsSelected == nil {
		writeError(w, 400, "is_selected required")
		return
	}
	v := 0
	if *body.IsSelected {
		v = 1
	}
	_, err := db.DB.Exec("UPDATE models SET is_selected=? WHERE (id=? OR model_id=?) AND provider_id=?", v, modelID, modelID, providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"model_id": modelID, "is_selected": *body.IsSelected})
}

func providerModelTest(w http.ResponseWriter, r *http.Request, providerID, modelID string) {
	var baseURL string
	err := db.DB.QueryRow("SELECT base_url FROM providers WHERE id=?", providerID).Scan(&baseURL)
	if err != nil {
		writeError(w, 404, "provider not found")
		return
	}

	url := strings.TrimRight(baseURL, "/") + "/models"
	req, _ := http.NewRequest("GET", url, nil)

	var keyVal string
	db.DB.QueryRow("SELECT key_encrypted FROM api_keys WHERE provider_id=? AND is_active=1 LIMIT 1", providerID).Scan(&keyVal)
	if keyVal != "" {
		req.Header.Set("Authorization", "Bearer "+keyVal)
	}
	if strings.Contains(baseURL, "kimchi") {
		req.Header.Set("User-Agent", "kimchi/0.1.50")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	if proxyURL := getProviderProxy(providerID); proxyURL != "" {
		if transport, err := makeProxyTransport(proxyURL); err == nil {
			client.Transport = transport
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, map[string]interface{}{"model_id": modelID, "exists": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()

	exists := resp.StatusCode == 200
	if exists {
		// Check if model is in the list
		buf := new(strings.Builder)
		io.Copy(buf, resp.Body)
		exists = strings.Contains(buf.String(), modelID)
	}
	writeJSON(w, map[string]interface{}{"model_id": modelID, "exists": exists, "status": resp.StatusCode})
}

func providerModelDelete(w http.ResponseWriter, r *http.Request, providerID, modelID string) {
	result, err := db.DB.Exec("DELETE FROM models WHERE (id=? OR model_id=?) AND provider_id=?", modelID, modelID, providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, 404, "model not found")
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

func providerKeyList(w http.ResponseWriter, r *http.Request, providerID string) {
	rows, err := db.DB.Query(`SELECT id, name, key_encrypted, COALESCE(account_id,''), is_active, last_used, created_at,
		COALESCE(fail_count,0), COALESCE(last_error,'')
		FROM api_keys WHERE provider_id=?`, providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, name, keyVal, accountID, createdAt, lastError string
		var isActive, failCount int
		var lastUsed *string
		rows.Scan(&id, &name, &keyVal, &accountID, &isActive, &lastUsed, &createdAt, &failCount, &lastError)
		item := map[string]interface{}{
			"id": id, "name": name, "key": keyVal, "is_active": isActive == 1,
			"last_used": lastUsed, "created_at": createdAt,
			"fail_count": failCount, "last_error": lastError,
			"source": "apikey",
		}
		if accountID != "" {
			item["account_id"] = accountID
		}
		list = append(list, item)
	}

	// Also return provider_connections (for connection-type providers like Grok CLI OAuth)
	connRows, connErr := db.DB.Query(`SELECT id, COALESCE(name,''), COALESCE(email,''),
		CASE WHEN access_token != '' THEN '***' || SUBSTR(access_token, -4) ELSE '' END as token_preview,
		is_active, created_at, COALESCE(fail_count,0), COALESCE(test_status,'')
		FROM provider_connections WHERE provider_id=?`, providerID)
	if connErr == nil {
		defer connRows.Close()
		for connRows.Next() {
			var id, name, email, tokenPreview, testStatus string
			var isActive, failCount int
			var createdAt int64
			connRows.Scan(&id, &name, &email, &tokenPreview, &isActive, &createdAt, &failCount, &testStatus)
			displayName := name
			if displayName == "" {
				displayName = email
			}
			if displayName == "" {
				displayName = "connection-" + id[:8]
			}
			item := map[string]interface{}{
				"id": id, "name": displayName, "key": tokenPreview, "is_active": isActive == 1,
				"created_at": fmt.Sprintf("%d", createdAt),
				"fail_count": failCount, "last_error": testStatus,
				"source": "connection",
			}
			if email != "" {
				item["email"] = email
			}
			list = append(list, item)
		}
	}

	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

// maskKey masks an API key for display
func maskKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// providerKeyBulkAdd adds multiple user-provided keys at once
func providerKeyBulkAdd(w http.ResponseWriter, r *http.Request, providerID string, keys []string, optNames []string) {
	if len(keys) == 0 {
		writeError(w, 400, "keys array is empty")
		return
	}

	// find max existing key number
	maxNum := 0
	if rows, err := db.DB.Query(`SELECT name FROM api_keys WHERE provider_id=?`, providerID); err == nil {
		for rows.Next() {
			var n string
			rows.Scan(&n)
			// try key-<num> or trailing number
			if idx := strings.LastIndex(n, "-"); idx >= 0 {
				if v, err := strconv.Atoi(n[idx+1:]); err == nil && v > maxNum {
					maxNum = v
				}
			}
		}
		rows.Close()
	}

	var created []map[string]interface{}
	for i, keyVal := range keys {
		keyVal = strings.TrimSpace(keyVal)
		if keyVal == "" {
			continue
		}
		id := genID()
		var name string
		if i < len(optNames) && strings.TrimSpace(optNames[i]) != "" {
			name = strings.TrimSpace(optNames[i])
		} else {
			name = fmt.Sprintf("key-%d", maxNum+i+1)
		}
		_, err := db.DB.Exec(
			"INSERT INTO api_keys (id, provider_id, name, key_encrypted, is_active) VALUES (?, ?, ?, ?, 1)",
			id, providerID, name, keyVal,
		)
		if err != nil {
			continue
		}
		created = append(created, map[string]interface{}{"id": id, "name": name, "key_masked": maskKey(keyVal), "is_active": true})
	}
	writeJSON(w, created)
}

func providerKeyCreate(w http.ResponseWriter, r *http.Request, providerID string) {
	var body struct {
		Name      string `json:"name"`
		Key       string `json:"key"`
		AccountID string `json:"account_id"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.Key == "" {
		writeError(w, 400, "key required")
		return
	}
	// auto-name if not provided: continue from existing max
	if strings.TrimSpace(body.Name) == "" {
		maxNum := 0
		if rows, err := db.DB.Query(`SELECT name FROM api_keys WHERE provider_id=?`, providerID); err == nil {
			for rows.Next() {
				var n string
				rows.Scan(&n)
				if idx := strings.LastIndex(n, "-"); idx >= 0 {
					if v, err := strconv.Atoi(n[idx+1:]); err == nil && v > maxNum {
						maxNum = v
					}
				}
			}
			rows.Close()
		}
		body.Name = fmt.Sprintf("key-%d", maxNum+1)
	}

	id := genID()
	_, err := db.DB.Exec(
		"INSERT INTO api_keys (id, provider_id, name, key_encrypted, account_id) VALUES (?, ?, ?, ?, ?)",
		id, providerID, body.Name, body.Key, body.AccountID,
	)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{
		"id": id, "provider_id": providerID, "name": body.Name, "account_id": body.AccountID, "is_active": true,
	})
}

// ── Provider Models ─────────────────────────────────────────

func providerModelList(w http.ResponseWriter, r *http.Request, providerID string) {
	rows, err := db.DB.Query("SELECT id, model_id, is_free, is_selected FROM models WHERE provider_id=? ORDER BY model_id", providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, modelID string
		var isFree, isSelected int
		rows.Scan(&id, &modelID, &isFree, &isSelected)
		list = append(list, map[string]interface{}{
			"id":       id,
			"name":     modelID,
			"selected": isSelected == 1,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

func providerToggleActive(w http.ResponseWriter, r *http.Request, providerID string) {
	var current int
	err := db.DB.QueryRow("SELECT COALESCE(is_active,0) FROM providers WHERE id=?", providerID).Scan(&current)
	if err != nil {
		writeError(w, 404, "provider not found")
		return
	}
	newVal := 1
	if current == 1 {
		newVal = 0
	}
	db.DB.Exec("UPDATE providers SET is_active=?, updated_at=? WHERE id=?", newVal, time.Now().Unix(), providerID)
	writeJSON(w, map[string]interface{}{"id": providerID, "is_active": newVal == 1})
}

// providerKeysDeleteDisabled deletes all disabled keys for a provider
func providerKeysDeleteDisabled(w http.ResponseWriter, r *http.Request, providerID string) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}

	result, err := db.DB.Exec("DELETE FROM api_keys WHERE provider_id=? AND is_active=0", providerID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	affected, _ := result.RowsAffected()
	writeJSON(w, map[string]interface{}{
		"status":   "ok",
		"deleted":  affected,
		"provider": providerID,
	})
}
