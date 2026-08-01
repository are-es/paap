package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// ── Proxy Pools ─────────────────────────────────────────────

func proxyList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query("SELECT id, name, address, port, proxy_type, is_active, test_status, test_ip, test_region, last_latency_ms, success_count, fail_count FROM proxy_pools ORDER BY created_at DESC")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, name, address, proxyType, testStatus, testIP, testRegion string
		var port, isActive, lastLatency, successCount, failCount int
		rows.Scan(&id, &name, &address, &port, &proxyType, &isActive, &testStatus, &testIP, &testRegion, &lastLatency, &successCount, &failCount)
		list = append(list, map[string]interface{}{
			"id": id, "name": name, "address": address, "port": port,
			"type": proxyType,
			"status": func() string {
				if isActive == 1 && (testStatus == "ok" || (lastLatency > 0 && lastLatency <= 2000)) {
					return "active"
				}
				return "inactive"
			}(),
			"latency_ms": func() interface{} {
				if lastLatency > 0 {
					return lastLatency
				}
				return nil
			}(),
			"country":     testRegion,
			"test_status": testStatus, "test_ip": testIP,
			"is_active":     isActive == 1,
			"success_count": successCount, "fail_count": failCount,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

func proxyCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string `json:"name"`
		Address   string `json:"address"`
		Port      int    `json:"port"`
		ProxyType string `json:"proxy_type"`
		Username  string `json:"username"`
		Password  string `json:"password"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.Address == "" || body.Port == 0 {
		writeError(w, 400, "address and port required")
		return
	}
	if body.ProxyType == "" {
		body.ProxyType = "socks5"
	}
	id := genID()
	_, err := db.DB.Exec("INSERT INTO proxy_pools (id, name, address, port, proxy_type, username, password) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id, body.Name, body.Address, body.Port, body.ProxyType, body.Username, body.Password)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	// Auto-test immediately in background
	go testSingleProxy(id, body.Address, body.Port, body.ProxyType)
	writeJSON(w, map[string]interface{}{
		"id": id, "name": body.Name, "address": body.Address,
		"port": body.Port, "proxy_type": body.ProxyType,
		"is_active": true, "test_status": "testing",
	})
}

func proxyBulkCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Proxies []struct {
			Name      string `json:"name"`
			Address   string `json:"address"`
			Port      int    `json:"port"`
			ProxyType string `json:"proxy_type"`
			Username  string `json:"username"`
			Password  string `json:"password"`
		} `json:"proxies"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}

	type bulkResult struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Error string `json:"error,omitempty"`
	}
	var results []bulkResult
	created := 0

	for _, p := range body.Proxies {
		if p.Address == "" || p.Port == 0 {
			results = append(results, bulkResult{Name: p.Name, Error: "address and port required"})
			continue
		}
		if p.ProxyType == "" {
			p.ProxyType = "socks5"
		}
		id := genID()
		_, err := db.DB.Exec("INSERT INTO proxy_pools (id, name, address, port, proxy_type, username, password) VALUES (?, ?, ?, ?, ?, ?, ?)",
			id, p.Name, p.Address, p.Port, p.ProxyType, p.Username, p.Password)
		if err != nil {
			results = append(results, bulkResult{Name: p.Name, Error: err.Error()})
			continue
		}
		go testSingleProxy(id, p.Address, p.Port, p.ProxyType)
		results = append(results, bulkResult{ID: id, Name: p.Name})
		created++
	}

	writeJSON(w, map[string]interface{}{"created": created, "total": len(body.Proxies), "results": results})
}

func proxyRoutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/proxies/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, 400, "missing proxy id")
		return
	}
	id := parts[0]

	// /api/proxies/failed (bulk delete failed proxies)
	if id == "failed" && r.Method == "DELETE" {
		result, err := db.DB.Exec("DELETE FROM proxy_pools WHERE test_status='failed'")
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		n, _ := result.RowsAffected()
		// Also clean up group members for deleted proxies
		db.DB.Exec("DELETE FROM proxy_group_members WHERE proxy_id NOT IN (SELECT id FROM proxy_pools)")
		writeJSON(w, map[string]interface{}{"deleted": n})
		return
	}

	// /api/proxies/select — returns fastest active proxy
	if id == "select" && r.Method == "GET" {
		proxySelect(w, r)
		return
	}

	if len(parts) == 2 && parts[1] == "test" {
		if r.Method == "POST" {
			proxyTest(w, r, id)
			return
		}
		writeError(w, 405, "method not allowed")
		return
	}
	switch r.Method {
	case "DELETE":
		result, err := db.DB.Exec("DELETE FROM proxy_pools WHERE id=?", id)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		// Also remove from proxy group members
		db.DB.Exec("DELETE FROM proxy_group_members WHERE proxy_id=?", id)
		n, _ := result.RowsAffected()
		if n == 0 {
			writeError(w, 404, "not found")
			return
		}
		writeJSON(w, map[string]string{"status": "deleted", "id": id})
	default:
		writeError(w, 405, "method not allowed")
	}
}

// proxySelect returns the fastest active proxy
func proxySelect(w http.ResponseWriter, r *http.Request) {
	var id, name, address, proxyType string
	var port, latency int
	err := db.DB.QueryRow(`SELECT id, name, address, port, proxy_type, last_latency_ms
		FROM proxy_pools WHERE is_active=1 AND test_status='ok'
		ORDER BY last_latency_ms ASC LIMIT 1`).Scan(&id, &name, &address, &port, &proxyType, &latency)
	if err != nil {
		writeError(w, 404, "no active proxy available")
		return
	}
	scheme := "socks5"
	if proxyType == "http" || proxyType == "https" {
		scheme = proxyType
	}
	proxyURL := fmt.Sprintf("%s://%s:%d", scheme, address, port)
	writeJSON(w, map[string]interface{}{
		"id": id, "name": name, "address": address, "port": port,
		"proxy_type": proxyType, "proxy_url": proxyURL,
		"last_latency_ms": latency,
	})
}

func proxyTestAll(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query("SELECT id FROM proxy_pools WHERE is_active=1")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}

	// Parallel test with 10s timeout per proxy
	type result struct {
		data map[string]interface{}
	}
	ch := make(chan result, len(ids))
	sem := make(chan struct{}, 10) // max 10 concurrent

	for _, id := range ids {
		go func(pid string) {
			sem <- struct{}{}
			defer func() { <-sem }()
			// testProxyByID already has 10s timeout built in
			res := testProxyByID(pid)
			ch <- result{data: res}
		}(id)
	}

	var results []map[string]interface{}
	for i := 0; i < len(ids); i++ {
		r := <-ch
		results = append(results, r.data)
	}

	writeJSON(w, map[string]interface{}{"tested": len(results), "results": results})
}

func proxyTest(w http.ResponseWriter, r *http.Request, id string) {
	result := testProxyByID(id)
	writeJSON(w, result)
}

func testProxyByID(id string) map[string]interface{} {
	var address string
	var port int
	var proxyType string
	err := db.DB.QueryRow("SELECT address, port, proxy_type FROM proxy_pools WHERE id=?", id).
		Scan(&address, &port, &proxyType)
	if err != nil {
		return map[string]interface{}{"id": id, "test_status": "failed", "error": "not found"}
	}

	start := time.Now()
	scheme := "socks5"
	if proxyType == "http" || proxyType == "https" {
		scheme = proxyType
	}
	proxyURL := fmt.Sprintf("%s://%s:%d", scheme, address, port)

	transport, err := makeProxyTransport(proxyURL)
	if err != nil {
		db.DB.Exec("UPDATE proxy_pools SET test_status='failed', fail_count=fail_count+1, updated_at=? WHERE id=?",
			time.Now().UTC().Format(time.RFC3339), id)
		return map[string]interface{}{"id": id, "test_status": "failed", "error": err.Error()}
	}

	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	resp, err := client.Get("https://ifconfig.co/json")
	latency := time.Since(start).Milliseconds()

	if err != nil {
		db.DB.Exec("UPDATE proxy_pools SET test_status='failed', last_latency_ms=?, fail_count=fail_count+1, updated_at=? WHERE id=?",
			latency, time.Now().UTC().Format(time.RFC3339), id)
		return map[string]interface{}{"id": id, "test_status": "failed", "latency_ms": latency, "error": err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var info struct {
		IP      string `json:"ip"`
		Country string `json:"country_iso"`
		City    string `json:"city"`
		ASN     string `json:"asn_org"`
	}
	json.Unmarshal(body, &info)

	ip := info.IP
	country := info.Country
	if country == "" {
		country = "XX"
	}

	db.DB.Exec("UPDATE proxy_pools SET test_status='ok', test_ip=?, test_region=?, last_latency_ms=?, success_count=success_count+1, fail_count=0, updated_at=? WHERE id=?",
		ip, country, latency, time.Now().UTC().Format(time.RFC3339), id)

	return map[string]interface{}{
		"id": id, "test_status": "ok", "test_ip": ip,
		"country": country, "city": info.City, "asn": info.ASN,
		"latency_ms": latency, "status_code": resp.StatusCode,
	}
}

// ── Proxy Groups ──────────────────────────────────────────

func proxyGroupList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query("SELECT id, name, is_active, created_at FROM proxy_groups ORDER BY created_at DESC")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, name, createdAt string
		var isActive int
		rows.Scan(&id, &name, &isActive, &createdAt)

		// Get member count and stats
		var memberCount, activeCount int
		db.DB.QueryRow("SELECT COUNT(*) FROM proxy_group_members WHERE group_id=?", id).Scan(&memberCount)
		db.DB.QueryRow(`SELECT COUNT(*) FROM proxy_group_members pgm
			JOIN proxy_pools p ON pgm.proxy_id = p.id
			WHERE pgm.group_id=? AND p.is_active=1`, id).Scan(&activeCount)

		list = append(list, map[string]interface{}{
			"id": id, "name": name, "is_active": isActive == 1, "created_at": createdAt,
			"member_count": memberCount, "active_count": activeCount,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

func proxyGroupCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.Name == "" {
		writeError(w, 400, "name required")
		return
	}
	id := genID()
	_, err := db.DB.Exec("INSERT INTO proxy_groups (id, name) VALUES (?, ?)", id, body.Name)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"id": id, "name": body.Name, "is_active": true})
}

func proxyGroupRoutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/proxy-groups/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, 400, "missing group id")
		return
	}
	id := parts[0]

	// /api/proxy-groups/:id/members
	if len(parts) == 2 && parts[1] == "members" {
		switch r.Method {
		case "POST":
			proxyGroupAddMember(w, r, id)
		default:
			writeError(w, 405, "method not allowed")
		}
		return
	}

	// /api/proxy-groups/:id/members/:proxy_id
	if len(parts) == 3 && parts[1] == "members" {
		if r.Method == "DELETE" {
			proxyGroupRemoveMember(w, r, id, parts[2])
			return
		}
		writeError(w, 405, "method not allowed")
		return
	}

	// /api/proxy-groups/:id/test
	if len(parts) == 2 && parts[1] == "test" {
		if r.Method == "POST" {
			proxyGroupTest(w, r, id)
			return
		}
		writeError(w, 405, "method not allowed")
		return
	}

	// /api/proxy-groups/:id
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			proxyGroupGet(w, r, id)
		case "DELETE":
			proxyGroupDelete(w, r, id)
		default:
			writeError(w, 405, "method not allowed")
		}
		return
	}

	writeError(w, 404, "not found")
}

func proxyGroupGet(w http.ResponseWriter, r *http.Request, id string) {
	var name, createdAt string
	var isActive int
	err := db.DB.QueryRow("SELECT name, is_active, created_at FROM proxy_groups WHERE id=?", id).Scan(&name, &isActive, &createdAt)
	if err != nil {
		writeError(w, 404, "proxy group not found")
		return
	}

	// Get members
	rows, err := db.DB.Query(`SELECT p.id, p.name, p.address, p.port, p.proxy_type, p.is_active, p.test_status, p.last_latency_ms
		FROM proxy_pools p
		JOIN proxy_group_members pgm ON pgm.proxy_id = p.id
		WHERE pgm.group_id=? ORDER BY p.last_latency_ms ASC`, id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var members []map[string]interface{}
	for rows.Next() {
		var pid, pname, paddr, ptype, pstatus string
		var pport, pisActive, platency int
		rows.Scan(&pid, &pname, &paddr, &pport, &ptype, &pisActive, &pstatus, &platency)
		members = append(members, map[string]interface{}{
			"id": pid, "name": pname, "address": paddr, "port": pport,
			"proxy_type": ptype, "is_active": pisActive == 1, "test_status": pstatus, "last_latency_ms": platency,
		})
	}
	if members == nil {
		members = []map[string]interface{}{}
	}

	writeJSON(w, map[string]interface{}{
		"id": id, "name": name, "is_active": isActive == 1, "created_at": createdAt,
		"members": members,
	})
}

func proxyGroupDelete(w http.ResponseWriter, r *http.Request, id string) {
	result, err := db.DB.Exec("DELETE FROM proxy_groups WHERE id=?", id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, 404, "not found")
		return
	}
	// Members are cascade-deleted via FK
	// Also clear proxy_group_id from any providers using this group
	db.DB.Exec("UPDATE providers SET proxy_group_id='' WHERE proxy_group_id=?", id)
	writeJSON(w, map[string]string{"status": "deleted", "id": id})
}

func proxyGroupAddMember(w http.ResponseWriter, r *http.Request, groupID string) {
	var body struct {
		ProxyID string `json:"proxy_id"`
	}
	if err := parseBody(r, &body); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if body.ProxyID == "" {
		writeError(w, 400, "proxy_id required")
		return
	}
	// Check if already member
	var existing string
	err := db.DB.QueryRow("SELECT id FROM proxy_group_members WHERE group_id=? AND proxy_id=?", groupID, body.ProxyID).Scan(&existing)
	if err == nil {
		writeJSON(w, map[string]interface{}{"id": existing, "group_id": groupID, "proxy_id": body.ProxyID, "duplicate": true})
		return
	}
	id := genID()
	_, err = db.DB.Exec("INSERT INTO proxy_group_members (id, group_id, proxy_id) VALUES (?, ?, ?)", id, groupID, body.ProxyID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"id": id, "group_id": groupID, "proxy_id": body.ProxyID})
}

func proxyGroupRemoveMember(w http.ResponseWriter, r *http.Request, groupID, proxyID string) {
	result, err := db.DB.Exec("DELETE FROM proxy_group_members WHERE group_id=? AND proxy_id=?", groupID, proxyID)
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

func proxyGroupTest(w http.ResponseWriter, r *http.Request, groupID string) {
	// Get all proxies in this group
	rows, err := db.DB.Query(`SELECT p.id, p.address, p.port, p.proxy_type, p.username, p.password
		FROM proxy_pools p
		JOIN proxy_group_members pgm ON pgm.proxy_id = p.id
		WHERE pgm.group_id=? AND p.is_active=1`, groupID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id, address, proxyType, username, password string
		var port int
		rows.Scan(&id, &address, &port, &proxyType, &username, &password)

		result := testSingleProxy(id, address, port, proxyType)
		results = append(results, result)
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	writeJSON(w, map[string]interface{}{"group_id": groupID, "results": results})
}

// testSingleProxy tests a proxy and updates DB. Returns result map.
func testSingleProxy(id, address string, port int, proxyType string) map[string]interface{} {
	scheme := "socks5"
	if proxyType == "http" || proxyType == "https" {
		scheme = proxyType
	}
	proxyURL := fmt.Sprintf("%s://%s:%d", scheme, address, port)

	start := time.Now()
	transport, err := makeProxyTransport(proxyURL)
	if err != nil {
		db.DB.Exec("UPDATE proxy_pools SET test_status='failed', fail_count=fail_count+1, updated_at=? WHERE id=?",
			time.Now().UTC().Format(time.RFC3339), id)
		return map[string]interface{}{"id": id, "test_status": "failed", "error": err.Error()}
	}

	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	resp, err := client.Get("http://ifconfig.co/json")
	latency := time.Since(start).Milliseconds()

	if err != nil {
		db.DB.Exec("UPDATE proxy_pools SET test_status='failed', last_latency_ms=?, fail_count=fail_count+1, updated_at=? WHERE id=?",
			latency, time.Now().UTC().Format(time.RFC3339), id)
		return map[string]interface{}{"id": id, "test_status": "failed", "latency_ms": latency, "error": err.Error()}
	}
	defer resp.Body.Close()

	// Latency > 2000ms = offline
	if latency > 2000 {
		db.DB.Exec("UPDATE proxy_pools SET test_status='failed', last_latency_ms=?, fail_count=fail_count+1, updated_at=? WHERE id=?",
			latency, time.Now().UTC().Format(time.RFC3339), id)
		return map[string]interface{}{"id": id, "test_status": "failed", "latency_ms": latency, "error": "latency exceeded 2000ms"}
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	var result struct {
		Country string `json:"country"`
		IP      string `json:"ip"`
	}
	json.Unmarshal(bodyBytes, &result)

	country := result.Country
	if country == "" {
		country = "Unknown"
	}

	db.DB.Exec("UPDATE proxy_pools SET test_status='ok', test_ip=?, test_region=?, last_latency_ms=?, success_count=success_count+1, fail_count=0, updated_at=? WHERE id=?",
		result.IP, country, latency, time.Now().UTC().Format(time.RFC3339), id)

	return map[string]interface{}{"id": id, "test_status": "ok", "test_ip": result.IP, "country": country, "latency_ms": latency}
}

// backgroundProxyTest runs every N minutes to test all proxies (parallel)
func backgroundProxyTest() {
	intervalStr := getSettingStr("proxy_test_interval", "3")
	intervalMin, err := strconv.Atoi(intervalStr)
	if err != nil || intervalMin < 1 {
		intervalMin = 3
	}

	for {
		time.Sleep(time.Duration(intervalMin) * time.Minute)
		log.Printf("[PAAP] Background proxy test starting (interval: %d min)...", intervalMin)

		rows, err := db.DB.Query(`SELECT id, address, port, proxy_type
			FROM proxy_pools WHERE is_active=1`)
		if err != nil {
			log.Printf("[PAAP] Background proxy test error: %v", err)
			continue
		}

		type proxyJob struct {
			id, address, proxyType string
			port                   int
		}
		var jobs []proxyJob
		for rows.Next() {
			var j proxyJob
			rows.Scan(&j.id, &j.address, &j.port, &j.proxyType)
			jobs = append(jobs, j)
		}
		rows.Close()

		// Test all proxies in parallel
		var wg sync.WaitGroup
		for _, j := range jobs {
			wg.Add(1)
			go func(job proxyJob) {
				defer wg.Done()
				testSingleProxy(job.id, job.address, job.port, job.proxyType)

				var failCount int
				db.DB.QueryRow("SELECT fail_count FROM proxy_pools WHERE id=?", job.id).Scan(&failCount)
				if failCount >= 3 {
					db.DB.Exec("UPDATE proxy_pools SET is_active=0 WHERE id=?", job.id)
					log.Printf("[PAAP] Auto-disabled proxy %s — %d consecutive failures", job.id, failCount)
				}
			}(j)
		}
		wg.Wait()
		log.Printf("[PAAP] Background proxy test done — tested %d proxies", len(jobs))
	}
}
