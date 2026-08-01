package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// ── Round-robin counter for group model rotation ────────────
var groupModelRRCounters = &sync.Map{}

func getGroupModelRRCounter(groupID string) *atomic.Int64 {
	val, ok := groupModelRRCounters.Load(groupID)
	if ok {
		return val.(*atomic.Int64)
	}
	counter := &atomic.Int64{}
	actual, _ := groupModelRRCounters.LoadOrStore(groupID, counter)
	return actual.(*atomic.Int64)
}

// ── Groups CRUD ────────────────────────────────────────────

func groupList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(`SELECT id, name, icon, round_robin, parallel, race_mode,
		selected_keys, selected_models, race_count, max_keys, created_at
		FROM groups ORDER BY name`)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, name, icon, raceMode, selectedKeys, selectedModels, createdAt string
		var roundRobin, parallel, raceCount, maxKeys int
		rows.Scan(&id, &name, &icon, &roundRobin, &parallel, &raceMode,
			&selectedKeys, &selectedModels, &raceCount, &maxKeys, &createdAt)
		list = append(list, map[string]interface{}{
			"id": id, "name": name, "icon": icon,
			"round_robin": roundRobin == 1, "parallel": parallel,
			"race_mode":       raceMode,
			"selected_keys":   jsonParse(selectedKeys, "[]"),
			"selected_models": jsonParse(selectedModels, "[]"),
			"race_count":      raceCount,
			"max_keys":        maxKeys,
			"created_at":      createdAt,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

func groupCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string      `json:"name"`
		Icon           string      `json:"icon"`
		RaceMode       string      `json:"race_mode"`
		SelectedKeys   interface{} `json:"selected_keys"`
		SelectedModels interface{} `json:"selected_models"`
		RaceCount      *int        `json:"race_count"`
		MaxKeys        *int        `json:"max_keys"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.Name == "" {
		writeError(w, 400, "name required")
		return
	}
	if body.Icon == "" {
		body.Icon = "grup.png"
	}
	if body.RaceMode == "" {
		body.RaceMode = "round_robin"
	}
	raceCount := 3
	if body.RaceCount != nil {
		raceCount = *body.RaceCount
	}
	maxKeys := 10
	if body.MaxKeys != nil {
		maxKeys = *body.MaxKeys
	}
	selectedKeysJSON := toJSON(body.SelectedKeys, "[]")
	selectedModelsJSON := toJSON(body.SelectedModels, "[]")

	id := genID()
	_, err := db.DB.Exec(`INSERT INTO groups (id, name, icon, race_mode, selected_keys, selected_models, race_count, max_keys)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, body.Name, body.Icon, body.RaceMode, selectedKeysJSON, selectedModelsJSON, raceCount, maxKeys)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	// Auto-create claude-* variant for Claude Code compatibility
	claudeID := "claude-" + id
	db.DB.Exec(`INSERT OR IGNORE INTO groups (id, name, icon, race_mode, selected_keys, selected_models, race_count, max_keys)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, claudeID, "claude-"+body.Name, body.Icon, body.RaceMode, selectedKeysJSON, selectedModelsJSON, raceCount, maxKeys)
	writeJSON(w, map[string]interface{}{
		"id": id, "name": body.Name, "icon": body.Icon,
		"round_robin": true, "race_mode": body.RaceMode,
		"selected_keys":   jsonParse(selectedKeysJSON, "[]"),
		"selected_models": jsonParse(selectedModelsJSON, "[]"),
		"race_count":      raceCount,
		"max_keys":        maxKeys,
	})
}

func groupRoutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/groups/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, 400, "missing group id")
		return
	}
	id := parts[0]

	// /api/groups/:id/models
	if len(parts) == 2 && parts[1] == "models" {
		switch r.Method {
		case "GET":
			groupModelList(w, r, id)
		case "POST":
			groupModelAdd(w, r, id)
		default:
			writeError(w, 405, "method not allowed")
		}
		return
	}

	// /api/groups/:id/models/:model_id (model_id may contain slashes like @cf/meta/...)
	if len(parts) >= 3 && parts[1] == "models" {
		if r.Method == "DELETE" {
			modelID := strings.Join(parts[2:], "/")
			modelID, _ = url.PathUnescape(modelID) // handle URL-encoded slashes
			groupModelRemove(w, r, id, modelID)
			return
		}
		writeError(w, 405, "method not allowed")
		return
	}

	// /api/groups/:id
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			groupGet(w, r, id)
		case "PUT":
			groupUpdate(w, r, id)
		case "DELETE":
			groupDelete(w, r, id)
		default:
			writeError(w, 405, "method not allowed")
		}
		return
	}
	writeError(w, 404, "not found")
}

func groupGet(w http.ResponseWriter, r *http.Request, id string) {
	var name, icon, raceMode, selectedKeys, selectedModels, createdAt string
	var roundRobin, parallel, raceCount, maxKeys int
	err := db.DB.QueryRow(`SELECT name, icon, round_robin, parallel, race_mode,
		selected_keys, selected_models, race_count, max_keys, created_at
		FROM groups WHERE id=?`, id).
		Scan(&name, &icon, &roundRobin, &parallel, &raceMode,
			&selectedKeys, &selectedModels, &raceCount, &maxKeys, &createdAt)
	if err != nil {
		writeError(w, 404, "group not found")
		return
	}
	writeJSON(w, map[string]interface{}{
		"id": id, "name": name, "icon": icon,
		"round_robin":     roundRobin == 1,
		"parallel":        parallel,
		"race_mode":       raceMode,
		"selected_keys":   jsonParse(selectedKeys, "[]"),
		"selected_models": jsonParse(selectedModels, "[]"),
		"race_count":      raceCount,
		"max_keys":        maxKeys,
		"created_at":      createdAt,
	})
}

func groupUpdate(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Name           *string     `json:"name"`
		Icon           *string     `json:"icon"`
		RoundRobin     *bool       `json:"round_robin"`
		Parallel       *int        `json:"parallel"`
		RaceMode       *string     `json:"race_mode"`
		SelectedKeys   interface{} `json:"selected_keys"`
		SelectedModels interface{} `json:"selected_models"`
		RaceCount      *int        `json:"race_count"`
		MaxKeys        *int        `json:"max_keys"`
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
	if body.Icon != nil {
		sets = append(sets, "icon=?")
		args = append(args, *body.Icon)
	}
	if body.RoundRobin != nil {
		v := 0
		if *body.RoundRobin {
			v = 1
		}
		sets = append(sets, "round_robin=?")
		args = append(args, v)
	}
	if body.Parallel != nil {
		sets = append(sets, "parallel=?")
		args = append(args, *body.Parallel)
	}
	if body.RaceMode != nil {
		sets = append(sets, "race_mode=?")
		args = append(args, *body.RaceMode)
	}
	if body.SelectedKeys != nil {
		sets = append(sets, "selected_keys=?")
		args = append(args, toJSON(body.SelectedKeys, "[]"))
	}
	if body.SelectedModels != nil {
		sets = append(sets, "selected_models=?")
		args = append(args, toJSON(body.SelectedModels, "[]"))
	}
	if body.RaceCount != nil {
		sets = append(sets, "race_count=?")
		args = append(args, *body.RaceCount)
	}
	if body.MaxKeys != nil {
		sets = append(sets, "max_keys=?")
		args = append(args, *body.MaxKeys)
	}
	if len(sets) == 0 {
		writeError(w, 400, "nothing to update")
		return
	}
	sets = append(sets, "updated_at=?")
	args = append(args, time.Now().UTC().Format(time.RFC3339))
	args = append(args, id)
	_, err := db.DB.Exec("UPDATE groups SET "+strings.Join(sets, ", ")+" WHERE id=?", args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	groupGet(w, r, id)
}

func groupDelete(w http.ResponseWriter, r *http.Request, id string) {
	// Delete group models first
	db.DB.Exec("DELETE FROM group_models WHERE group_id=?", id)
	result, err := db.DB.Exec("DELETE FROM groups WHERE id=?", id)
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

func groupModelList(w http.ResponseWriter, r *http.Request, groupID string) {
	rows, err := db.DB.Query(
		"SELECT gm.id, gm.provider_id, gm.model_id, p.name as provider_name, p.icon as provider_icon FROM group_models gm LEFT JOIN providers p ON gm.provider_id=p.id WHERE gm.group_id=? ORDER BY gm.position",
		groupID,
	)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	var list []map[string]interface{}
	for rows.Next() {
		var id, providerID, modelID string
		var providerName, providerIcon *string
		rows.Scan(&id, &providerID, &modelID, &providerName, &providerIcon)
		list = append(list, map[string]interface{}{
			"id": id, "provider_id": providerID, "model_id": modelID,
			"provider_name": providerName, "provider_icon": providerIcon,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

func groupModelAdd(w http.ResponseWriter, r *http.Request, groupID string) {
	var body struct {
		ProviderID string `json:"provider_id"`
		ModelID    string `json:"model_id"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.ProviderID == "" || body.ModelID == "" {
		writeError(w, 400, "provider_id and model_id required")
		return
	}
	id := genID()
	_, err := db.DB.Exec("INSERT INTO group_models (id, group_id, provider_id, model_id) VALUES (?, ?, ?, ?)",
		id, groupID, body.ProviderID, body.ModelID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{
		"id": id, "group_id": groupID, "provider_id": body.ProviderID, "model_id": body.ModelID,
	})
}

func groupModelRemove(w http.ResponseWriter, r *http.Request, groupID, modelID string) {
	result, err := db.DB.Exec("DELETE FROM group_models WHERE group_id=? AND (id=? OR model_id=?)", groupID, modelID, modelID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, 404, "not found")
		return
	}
	writeJSON(w, map[string]string{"status": "removed"})
}

// ── Helper: safe JSON parse ─────────────────────────────────

func jsonParse(s, fallback string) interface{} {
	if s == "" {
		s = fallback
	}
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return fallback
	}
	return v
}

func toJSON(v interface{}, fallback string) string {
	if v == nil {
		return fallback
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fallback
	}
	return string(b)
}

// ── Group Routing Logic ─────────────────────────────────────

// getGroupInfo fetches group settings
func getGroupInfo(groupName string) (groupID, raceMode, selectedKeysJSON, selectedModelsJSON string, raceCount, maxKeys, roundRobin int, err error) {
	err = db.DB.QueryRow(`SELECT id, race_mode, selected_keys, selected_models, race_count, max_keys, round_robin
		FROM groups WHERE name=?`, groupName).Scan(
		&groupID, &raceMode, &selectedKeysJSON, &selectedModelsJSON, &raceCount, &maxKeys, &roundRobin)
	return
}

// handleGroupRaceKeys: fire N keys concurrently, take fastest, cancel rest
func handleGroupRaceKeys(w http.ResponseWriter, r *http.Request, modelName, groupName string, rawBody map[string]interface{}, raceCount int, selectedKeysJSON string) {
	startTime := time.Now()
	raceID := genID()

	// Parse selected_keys
	var selectedKeyIDs []string
	if selectedKeysJSON != "" && selectedKeysJSON != "[]" {
		json.Unmarshal([]byte(selectedKeysJSON), &selectedKeyIDs)
	}

	// Build tasks from selected keys (or all active keys if none selected)
	type raceTask struct {
		providerID   string
		providerName string
		baseURL      string
		modelID      string
		keyID        string
		keyName      string
		keyVal       string
		keyAccID     string
	}

	var tasks []raceTask

	if len(selectedKeyIDs) > 0 {
		// Use selected keys
		for _, keyID := range selectedKeyIDs {
			var kID, kName, kVal, kAccID, pID, pName, pBase string
			err := db.DB.QueryRow(`
				SELECT k.id, k.name, k.key_encrypted, COALESCE(k.account_id,''),
				       p.id, p.name, p.base_url
				FROM api_keys k
				JOIN providers p ON k.provider_id = p.id
				WHERE k.id=? AND k.is_active=1 AND p.is_active=1`, keyID).Scan(
				&kID, &kName, &kVal, &kAccID, &pID, &pName, &pBase)
			if err != nil {
				continue
			}
			// Get a model from this provider's group models
			var modelID string
			db.DB.QueryRow(`
				SELECT gm.model_id FROM group_models gm
				WHERE gm.group_id=(SELECT id FROM groups WHERE name=?)
				AND gm.provider_id=? LIMIT 1`, groupName, pID).Scan(&modelID)
			if modelID == "" {
				continue
			}
			tasks = append(tasks, raceTask{pID, pName, pBase, modelID, kID, kName, kVal, kAccID})
		}
	} else {
		// Fallback: use all models in group with all active keys
		groupID := ""
		db.DB.QueryRow("SELECT id FROM groups WHERE name=?", groupName).Scan(&groupID)
		if groupID == "" {
			writeError(w, 400, "group not found")
			return
		}
		rows, err := db.DB.Query(`
			SELECT gm.provider_id, gm.model_id, p.name, p.base_url
			FROM group_models gm
			JOIN providers p ON gm.provider_id = p.id
			WHERE gm.group_id=? AND p.is_active=1
			ORDER BY gm.position`, groupID)
		if err != nil {
			writeError(w, 500, "failed to query group models")
			return
		}
		defer rows.Close()

		type route struct {
			providerID, providerName, baseURL, modelID string
		}
		var routes []route
		for rows.Next() {
			var rt route
			rows.Scan(&rt.providerID, &rt.modelID, &rt.providerName, &rt.baseURL)
			routes = append(routes, rt)
		}
		for _, rt := range routes {
			keys := getAllActiveKeys(rt.providerID)
			for _, k := range keys {
				tasks = append(tasks, raceTask{rt.providerID, rt.providerName, rt.baseURL, rt.modelID, k.id, k.name, k.value, k.accountID})
			}
		}
	}

	if len(tasks) == 0 {
		writeError(w, 502, "no active keys available for race")
		return
	}

	// Limit to raceCount
	if raceCount > 0 && len(tasks) > raceCount {
		tasks = tasks[:raceCount]
	}

	totalTasks := len(tasks)
	log.Printf("[PAAP] Race Keys '%s': %d tasks", groupName, totalTasks)

	// Fire all in parallel
	type raceResult struct {
		task       raceTask
		statusCode int
		body       []byte
		latencyMs  int64
		proxyUsed  string
		err        error
	}
	resultCh := make(chan raceResult, totalTasks)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, t := range tasks {
		go func(t raceTask) {
			body := make(map[string]interface{})
			for k, v := range rawBody {
				body[k] = v
			}
			body["model"] = t.modelID
			delete(body, "stream_options")
			bodyBytes, _ := json.Marshal(body)

			upstreamURL := resolveUpstreamURL(t.baseURL, t.keyAccID)
			req, err := http.NewRequestWithContext(ctx, "POST", upstreamURL, bytes.NewReader(bodyBytes))
			if err != nil {
				resultCh <- raceResult{task: t, err: err}
				return
			}
			setProviderAuth(req, t.baseURL, t.keyVal)

			client := sharedHTTPClient
			var proxyUsed string
			if proxyURL := getProviderProxy(t.providerID); proxyURL != "" {
				proxyUsed = proxyURL
				if transport, perr := makeProxyTransport(proxyURL); perr == nil {
					client.Transport = transport
				}
			}

			start := time.Now()
			resp, err := client.Do(req)
			lat := time.Since(start).Milliseconds()

			if err != nil {
				resultCh <- raceResult{task: t, err: err, latencyMs: lat, proxyUsed: proxyUsed}
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				bodyBytes2, _ := io.ReadAll(resp.Body)
				resultCh <- raceResult{task: t, statusCode: 200, body: bodyBytes2, latencyMs: lat, proxyUsed: proxyUsed}
			} else {
				errBody, _ := io.ReadAll(resp.Body)
				if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 402 {
					db.DB.Exec("UPDATE api_keys SET is_active=0 WHERE id=?", t.keyID)
					log.Printf("[PAAP] Race auto-disabled key %s — status %d", t.keyName, resp.StatusCode)
				}
				resultCh <- raceResult{task: t, statusCode: resp.StatusCode, latencyMs: lat, proxyUsed: proxyUsed, err: fmt.Errorf("status %d: %s", resp.StatusCode, string(errBody))}
			}
		}(t)
	}

	// Wait for first success or all failures
	var failures []string
	responseSent := false
	for i := 0; i < totalTasks; i++ {
		res := <-resultCh
		if res.err != nil {
			failures = append(failures, fmt.Sprintf("[%s:%s] %v", res.task.providerName, res.task.modelID, res.err))
			logRaceTask(raceID, groupName, 0, totalTasks, res.task.providerName, res.task.modelID, res.task.keyName, "error", 0, 0, res.latencyMs, res.proxyUsed, res.err.Error())
			continue
		}
		if res.statusCode == 200 {
			totalMs := time.Since(startTime).Milliseconds()
			if !responseSent {
				responseSent = true
				cancel() // cancel remaining
				var tokensIn, tokensOut int
				parseUsageJSON(res.body, &tokensIn, &tokensOut)
				if tokensIn == 0 && tokensOut == 0 {
					parseUsageSSE(res.body, &tokensIn, &tokensOut)
				}
				log.Printf("[PAAP] Race winner body len=%d tokens_in=%d tokens_out=%d", len(res.body), tokensIn, tokensOut)
				logRaceTask(raceID, groupName, 0, totalTasks, res.task.providerName, res.task.modelID, res.task.keyName, "winner", tokensIn, tokensOut, totalMs, res.proxyUsed, "")
				log.Printf("[PAAP] Race Keys winner: %s/%s (%dms)", res.task.providerName, res.task.modelID, totalMs)

				// Forward response to client
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				w.WriteHeader(200)
				w.Write(res.body)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
			continue
		}
		failures = append(failures, fmt.Sprintf("[%s:%s] %v", res.task.providerName, res.task.modelID, res.err))
	}

	if responseSent {
		return
	}
	writeError(w, 502, fmt.Sprintf("all race keys failed: %s", strings.Join(failures, "; ")))
}

// handleGroupRoundRobinModel: rotate model per request
func handleGroupRoundRobinModel(w http.ResponseWriter, r *http.Request, groupName string, rawBody map[string]interface{}, selectedModelsJSON string) {
	startTime := time.Now()

	groupID := ""
	db.DB.QueryRow("SELECT id FROM groups WHERE name=?", groupName).Scan(&groupID)
	if groupID == "" {
		writeError(w, 400, "group not found")
		return
	}

	// Get group models
	rows, err := db.DB.Query(`
		SELECT gm.provider_id, gm.model_id, p.name, p.base_url
		FROM group_models gm
		JOIN providers p ON gm.provider_id = p.id
		WHERE gm.group_id=? AND p.is_active=1
		ORDER BY gm.position`, groupID)
	if err != nil {
		writeError(w, 500, "failed to query group models")
		return
	}
	defer rows.Close()

	type route struct {
		providerID, providerName, baseURL, modelID string
	}
	var routes []route
	for rows.Next() {
		var rt route
		rows.Scan(&rt.providerID, &rt.modelID, &rt.providerName, &rt.baseURL)
		routes = append(routes, rt)
	}

	if len(routes) == 0 {
		writeError(w, 400, fmt.Sprintf("group '%s' has no active models", groupName))
		return
	}

	// Rotate
	counter := getGroupModelRRCounter(groupID)
	idx := int(counter.Add(1)-1) % len(routes)
	selected := routes[idx]

	log.Printf("[PAAP] Round Robin Model '%s': selected %s/%s (index %d)", groupName, selected.providerName, selected.modelID, idx)

	// Get key
	keyID, keyName, keyValue, keyAccountID, err := getNextActiveKey(selected.providerID)
	if err != nil {
		writeError(w, 502, fmt.Sprintf("no active keys for provider '%s'", selected.providerName))
		return
	}

	// Build and send request
	upstreamBody := make(map[string]interface{})
	for k, v := range rawBody {
		upstreamBody[k] = v
	}
	upstreamBody["model"] = selected.modelID
	bodyBytes, _ := json.Marshal(upstreamBody)

	upstreamURL := resolveUpstreamURL(selected.baseURL, keyAccountID)
	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, 500, "failed to create request")
		return
	}
	setProviderAuth(req, selected.baseURL, keyValue)

	client := sharedHTTPClient
	var proxyUsed string
	if proxyURL := getProviderProxy(selected.providerID); proxyURL != "" {
		proxyUsed = proxyURL
		if transport, perr := makeProxyTransport(proxyURL); perr == nil {
			client.Transport = transport
		}
	}

	resp, err := client.Do(req)
	latencyMs := time.Since(startTime).Milliseconds()
	if err != nil {
		logProxyRequest(selected.providerID, selected.providerName, selected.modelID, keyID, keyName, groupName, proxyUsed, 0, 0, 0, latencyMs, err.Error())
		writeError(w, 502, fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		errStr := string(respBody)
		if len(errStr) > 500 {
			errStr = errStr[:500]
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 402 {
			db.DB.Exec("UPDATE api_keys SET is_active=0 WHERE id=?", keyID)
		}
		logProxyRequest(selected.providerID, selected.providerName, selected.modelID, keyID, keyName, groupName, proxyUsed, resp.StatusCode, 0, 0, latencyMs, errStr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	var tokensIn, tokensOut int
	parseUsageJSON(respBody, &tokensIn, &tokensOut)
	logProxyRequest(selected.providerID, selected.providerName, selected.modelID, keyID, keyName, groupName, proxyUsed, 200, tokensIn, tokensOut, latencyMs, "")

	w.Header().Set("Content-Type", "application/json")
	w.Write(respBody)
}

// handleGroupFailFirst: model A fails → fallback B → C (cascade)
func handleGroupFailFirst(w http.ResponseWriter, r *http.Request, groupName string, rawBody map[string]interface{}, selectedModelsJSON string) {
	startTime := time.Now()

	groupID := ""
	db.DB.QueryRow("SELECT id FROM groups WHERE name=?", groupName).Scan(&groupID)
	if groupID == "" {
		writeError(w, 400, "group not found")
		return
	}

	// Get group models in order
	rows, err := db.DB.Query(`
		SELECT gm.provider_id, gm.model_id, p.name, p.base_url
		FROM group_models gm
		JOIN providers p ON gm.provider_id = p.id
		WHERE gm.group_id=? AND p.is_active=1
		ORDER BY gm.position`, groupID)
	if err != nil {
		writeError(w, 500, "failed to query group models")
		return
	}
	defer rows.Close()

	type route struct {
		providerID, providerName, baseURL, modelID string
	}
	var routes []route
	for rows.Next() {
		var rt route
		rows.Scan(&rt.providerID, &rt.modelID, &rt.providerName, &rt.baseURL)
		routes = append(routes, rt)
	}

	if len(routes) == 0 {
		writeError(w, 400, fmt.Sprintf("group '%s' has no active models", groupName))
		return
	}

	// Try each model in order
	for i, rt := range routes {
		keyID, keyName, keyValue, keyAccountID, kerr := getNextActiveKey(rt.providerID)
		if kerr != nil {
			log.Printf("[PAAP] Fail First '%s': no keys for %s, trying next", groupName, rt.providerName)
			continue
		}

		log.Printf("[PAAP] Fail First '%s': trying %s/%s (attempt %d/%d)", groupName, rt.providerName, rt.modelID, i+1, len(routes))

		upstreamBody := make(map[string]interface{})
		for k, v := range rawBody {
			upstreamBody[k] = v
		}
		upstreamBody["model"] = rt.modelID
		bodyBytes, _ := json.Marshal(upstreamBody)

		upstreamURL := resolveUpstreamURL(rt.baseURL, keyAccountID)
		req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(bodyBytes))
		if err != nil {
			continue
		}
		setProviderAuth(req, rt.baseURL, keyValue)

		client := sharedHTTPClient
		var proxyUsed string
		if proxyURL := getProviderProxy(rt.providerID); proxyURL != "" {
			proxyUsed = proxyURL
			if transport, perr := makeProxyTransport(proxyURL); perr == nil {
				client.Transport = transport
			}
		}

		resp, err := client.Do(req)
		latencyMs := time.Since(startTime).Milliseconds()
		if err != nil {
			log.Printf("[PAAP] Fail First '%s': %s/%s error: %v", groupName, rt.providerName, rt.modelID, err)
			continue
		}

		if resp.StatusCode == 200 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var tokensIn, tokensOut int
			parseUsageJSON(respBody, &tokensIn, &tokensOut)
			logProxyRequest(rt.providerID, rt.providerName, rt.modelID, keyID, keyName, groupName, proxyUsed, 200, tokensIn, tokensOut, latencyMs, "")
			log.Printf("[PAAP] Fail First '%s': success with %s/%s (%dms)", groupName, rt.providerName, rt.modelID, latencyMs)
			w.Header().Set("Content-Type", "application/json")
			w.Write(respBody)
			return
		}

		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		errStr := string(errBody)
		if len(errStr) > 500 {
			errStr = errStr[:500]
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 402 {
			db.DB.Exec("UPDATE api_keys SET is_active=0 WHERE id=?", keyID)
		}
		log.Printf("[PAAP] Fail First '%s': %s/%s returned %d — %s", groupName, rt.providerName, rt.modelID, resp.StatusCode, errStr)
		logProxyRequest(rt.providerID, rt.providerName, rt.modelID, keyID, keyName, groupName, proxyUsed, resp.StatusCode, 0, 0, latencyMs, errStr)
	}

	writeError(w, 502, fmt.Sprintf("all models in group '%s' failed", groupName))
}

// handleGroupRRRaceKeys: COMBINED mode — Round Robin picks model, then Race N keys for that model
func handleGroupRRRaceKeys(w http.ResponseWriter, r *http.Request, groupName string, rawBody map[string]interface{}, maxKeys int) {
	startTime := time.Now()

	groupID := ""
	db.DB.QueryRow("SELECT id FROM groups WHERE name=?", groupName).Scan(&groupID)
	if groupID == "" {
		writeError(w, 400, "group not found")
		return
	}

	// Get group models
	rows, err := db.DB.Query(`
		SELECT gm.provider_id, gm.model_id, p.name, p.base_url
		FROM group_models gm
		JOIN providers p ON gm.provider_id = p.id
		WHERE gm.group_id=? AND p.is_active=1
		ORDER BY gm.position`, groupID)
	if err != nil {
		writeError(w, 500, "failed to query group models")
		return
	}
	defer rows.Close()

	type route struct {
		providerID, providerName, baseURL, modelID string
	}
	var routes []route
	for rows.Next() {
		var rt route
		rows.Scan(&rt.providerID, &rt.modelID, &rt.providerName, &rt.baseURL)
		routes = append(routes, rt)
	}

	if len(routes) == 0 {
		writeError(w, 400, fmt.Sprintf("group '%s' has no active models", groupName))
		return
	}

	// Step 1: Round Robin — pick next model
	counter := getGroupModelRRCounter(groupID)
	idx := int(counter.Add(1)-1) % len(routes)
	selected := routes[idx]

	log.Printf("[PAAP] RR+Race Keys '%s': RR selected %s/%s (index %d), racing max %d keys",
		groupName, selected.providerName, selected.modelID, idx, maxKeys)

	// Step 2: Get all active keys for selected model's provider
	allKeys := getAllActiveKeys(selected.providerID)
	if len(allKeys) == 0 {
		writeError(w, 502, fmt.Sprintf("no active keys for provider '%s'", selected.providerName))
		return
	}

	// Cap to maxKeys (but don't exceed available)
	keysToRace := allKeys
	if maxKeys > 0 && len(keysToRace) > maxKeys {
		keysToRace = keysToRace[:maxKeys]
	}

	totalTasks := len(keysToRace)
	raceID := genID()
	log.Printf("[PAAP] RR+Race Keys '%s': racing %d keys for %s/%s", groupName, totalTasks, selected.providerName, selected.modelID)

	// Step 3: Race all keys in parallel
	type raceTask struct {
		keyID, keyName, keyValue, keyAccID string
	}
	type raceResult struct {
		task       raceTask
		statusCode int
		body       []byte
		latencyMs  int64
		proxyUsed  string
		err        error
	}

	resultCh := make(chan raceResult, totalTasks)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, k := range keysToRace {
		t := raceTask{k.id, k.name, k.value, k.accountID}
		go func(t raceTask) {
			body := make(map[string]interface{})
			for k, v := range rawBody {
				body[k] = v
			}
			body["model"] = selected.modelID
			delete(body, "stream_options")
			bodyBytes, _ := json.Marshal(body)

			upstreamURL := resolveUpstreamURL(selected.baseURL, t.keyAccID)
			req, err := http.NewRequestWithContext(ctx, "POST", upstreamURL, bytes.NewReader(bodyBytes))
			if err != nil {
				resultCh <- raceResult{task: t, err: err}
				return
			}
			setProviderAuth(req, selected.baseURL, t.keyValue)

			client := sharedHTTPClient
			var proxyUsed string
			if proxyURL := getProviderProxy(selected.providerID); proxyURL != "" {
				proxyUsed = proxyURL
				if transport, perr := makeProxyTransport(proxyURL); perr == nil {
					client.Transport = transport
				}
			}

			start := time.Now()
			resp, err := client.Do(req)
			lat := time.Since(start).Milliseconds()

			if err != nil {
				resultCh <- raceResult{task: t, err: err, latencyMs: lat, proxyUsed: proxyUsed}
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				bodyBytes2, _ := io.ReadAll(resp.Body)
				resultCh <- raceResult{task: t, statusCode: 200, body: bodyBytes2, latencyMs: lat, proxyUsed: proxyUsed}
			} else {
				errBody, _ := io.ReadAll(resp.Body)
				if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 402 {
					db.DB.Exec("UPDATE api_keys SET is_active=0 WHERE id=?", t.keyID)
					log.Printf("[PAAP] RR+Race auto-disabled key %s — status %d", t.keyName, resp.StatusCode)
				}
				resultCh <- raceResult{task: t, statusCode: resp.StatusCode, latencyMs: lat, proxyUsed: proxyUsed, err: fmt.Errorf("status %d: %s", resp.StatusCode, string(errBody))}
			}
		}(t)
	}

	// Wait for first success or all failures
	var failures []string
	responseSent := false
	for i := 0; i < totalTasks; i++ {
		res := <-resultCh
		if res.err != nil {
			failures = append(failures, fmt.Sprintf("[%s] %v", res.task.keyName, res.err))
			logRaceTask(raceID, groupName, 0, totalTasks, selected.providerName, selected.modelID, res.task.keyName, "error", 0, 0, res.latencyMs, res.proxyUsed, res.err.Error())
			continue
		}
		if res.statusCode == 200 {
			totalMs := time.Since(startTime).Milliseconds()
			if !responseSent {
				responseSent = true
				cancel()
				var tokensIn, tokensOut int
				parseUsageJSON(res.body, &tokensIn, &tokensOut)
				if tokensIn == 0 && tokensOut == 0 {
					parseUsageSSE(res.body, &tokensIn, &tokensOut)
				}
				log.Printf("[PAAP] RR+Race winner body_len=%d tokens_in=%d tokens_out=%d", len(res.body), tokensIn, tokensOut)
				logRaceTask(raceID, groupName, 0, totalTasks, selected.providerName, selected.modelID, res.task.keyName, "winner", tokensIn, tokensOut, totalMs, res.proxyUsed, "")
				log.Printf("[PAAP] RR+Race Keys winner: %s/%s key=%s (%dms)", selected.providerName, selected.modelID, res.task.keyName, totalMs)

				// Forward response to client
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				w.WriteHeader(200)
				w.Write(res.body)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
			continue
		}
		failures = append(failures, fmt.Sprintf("[%s] %v", res.task.keyName, res.err))
	}

	if responseSent {
		return
	}
	writeError(w, 502, fmt.Sprintf("all race keys failed for %s/%s: %s", selected.providerName, selected.modelID, strings.Join(failures, "; ")))
}
