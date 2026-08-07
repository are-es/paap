package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// Shared HTTP client with connection pooling + keepalive
var sharedHTTPClient = &http.Client{
	Timeout: 180 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     180 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

// Streaming HTTP client — no timeout for long-running streams
var streamingHTTPClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     300 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

func main() {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".paap")
	port := "9090"

	if p := os.Getenv("PAAP_PORT"); p != "" {
		port = p
	}
	if d := os.Getenv("PAAP_DATA"); d != "" {
		dataDir = d
	}

	log.Printf("PAAP initializing — data: %s", dataDir)
	if err := db.Init(dataDir); err != nil {
		log.Fatalf("db init: %v", err)
	}
	defer db.Close()

	// Initialize compression engine
	assetsDir := filepath.Join(filepath.Dir(os.Args[0]), "..", "assets")
	if _, err := os.Stat(assetsDir); os.IsNotExist(err) {
		assetsDir = "assets"
	}

	mux := http.NewServeMux()

	// ── Health (public, no auth) ─────────────────────────────
	mux.HandleFunc("/api/health", handleHealth)

	// ── Providers ───────────────────────────────────────────
	mux.HandleFunc("/api/providers", methodRouter(map[string]http.HandlerFunc{
		"GET":  providerList,
		"POST": providerCreate,
	}))
	mux.HandleFunc("/api/providers/", providerRoutes)

	// ── API Keys ────────────────────────────────────────────
	mux.HandleFunc("/api/keys", methodRouter(map[string]http.HandlerFunc{
		"GET":  keyList,
		"POST": keyCreate,
	}))
	mux.HandleFunc("/api/keys/", keyRoutes)

	// ── Groups ──────────────────────────────────────────────
	mux.HandleFunc("/api/groups", methodRouter(map[string]http.HandlerFunc{
		"GET":  groupList,
		"POST": groupCreate,
	}))
	mux.HandleFunc("/api/groups/", groupRoutes)

	// ── Logs ────────────────────────────────────────────────
	mux.HandleFunc("/api/logs", methodRouter(map[string]http.HandlerFunc{
		"GET":    logList,
		"DELETE": logClear,
	}))
	mux.HandleFunc("/api/logs/cost", logCostSummary)
	mux.HandleFunc("/api/logs/export", logExport)
	mux.HandleFunc("/api/proxies/test-all", proxyTestAll)

	// ── Models (all models from all providers) ─────────────
	mux.HandleFunc("/api/models", modelListDB)

	// ── Proxy Pools ─────────────────────────────────────────
	mux.HandleFunc("/api/proxies", methodRouter(map[string]http.HandlerFunc{
		"GET":  proxyList,
		"POST": proxyCreate,
	}))
	mux.HandleFunc("/api/proxies/bulk", methodRouter(map[string]http.HandlerFunc{
		"POST": proxyBulkCreate,
	}))
	mux.HandleFunc("/api/proxies/", proxyRoutes)

	// ── Proxy Groups ────────────────────────────────────────
	mux.HandleFunc("/api/proxy-groups", methodRouter(map[string]http.HandlerFunc{
		"GET":  proxyGroupList,
		"POST": proxyGroupCreate,
	}))
	mux.HandleFunc("/api/proxy-groups/", proxyGroupRoutes)

	// ── Gateway Keys ────────────────────────────────────────
	mux.HandleFunc("/api/gateway/keys", methodRouter(map[string]http.HandlerFunc{
		"GET":  gatewayKeyList,
		"POST": gatewayKeyCreate,
	}))
	mux.HandleFunc("/api/gateway/keys/", gatewayKeyRoutes)

	// ── System ──────────────────────────────────────────────
	mux.HandleFunc("/api/settings", settingsHandler)
	mux.HandleFunc("/api/headroom/status", headroomStatus)
	mux.HandleFunc("/api/compression/logs", compressionLogsHandler)
	mux.HandleFunc("/api/compression/logs/clear", clearCompressionLogsHandler)
	mux.HandleFunc("/api/system/shutdown", systemShutdown)
	mux.HandleFunc("/api/system/restart", systemRestart)
	mux.HandleFunc("/api/backup", backupDatabase)
	mux.HandleFunc("/api/restore", restoreDatabase)
	mux.HandleFunc("/api/clear-all", clearAllData)
	mux.HandleFunc("/api/usage/summary", usageSummary)

	// ── Compression routes removed (engine still used by routing) ──

	// ── Tools (auto-routing: vision, websearch, etc.) ──────
	mux.HandleFunc("/api/tools", methodRouter(map[string]http.HandlerFunc{
		"GET":  toolListHandler,
		"POST": toolCreateHandler,
	}))
	mux.HandleFunc("/api/tools/", toolRoutes)

	// ── Merlin Auth ────────────────────────────────────────
	mux.HandleFunc("/api/merlin/auth", merlinAuth)
	mux.HandleFunc("/api/merlin/capture", merlinCapture)

	// ── OAuth (grok-cli device code flow) ─────────────────
	mux.HandleFunc("/api/oauth/", oauthRoutes)

	// ── MCP (Model Context Protocol) ──────────────────────
	mux.HandleFunc("/mcp/message", authMiddleware(mcpMessageHandler))
	mux.HandleFunc("/mcp/status", mcpStatusHandler)

	// ── OpenAI-compatible proxy (/v1/...) ───────────────────
	mux.HandleFunc("/v1/chat/completions", authMiddleware(chatCompletionsHandler))
	mux.HandleFunc("/v1/models", authMiddleware(modelList))

	// ── Anthropic-compatible proxy (/v1/messages) ──────────
	mux.HandleFunc("/v1/messages", authMiddlewareAnthropic(anthropicMessagesHandler))

	mux.HandleFunc("/v1/", catchV1)

	// ── Static ──────────────────────────────────────────────
	// Serve assets from Next.js export first, fallback to legacy
	nextAssetsDir := filepath.Join("web", "out", "assets")
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try Next.js export assets first
		nextPath := filepath.Join(nextAssetsDir, r.URL.Path)
		if _, err := os.Stat(nextPath); err == nil {
			http.ServeFile(w, r, nextPath)
			return
		}
		// Fallback to legacy assets dir
		http.FileServer(http.Dir(assetsDir)).ServeHTTP(w, r)
	})))
	mux.HandleFunc("/", handleIndex)

	// Start background proxy tester
	go backgroundProxyTest()

	// Auto-start headroom if enabled
	initHeadroomOnStartup()

	addr := ":" + port
	log.Printf("PAAP listening on http://localhost%s", addr)
	log.Printf("  Dashboard : http://localhost%s", addr)
	log.Printf("  API       : http://localhost%s/v1/chat/completions", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func methodRouter(routes map[string]http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h, ok := routes[r.Method]; ok {
			h(w, r)
			return
		}
		writeError(w, 405, "method not allowed")
	}
}

func genID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func parseBody(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func splitPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	var gwKeyCount int
	db.DB.QueryRow("SELECT COUNT(*) FROM gateway_keys WHERE is_active=1").Scan(&gwKeyCount)
	writeJSON(w, map[string]interface{}{
		"name":              "PAAP — Pangkalan API",
		"status":            "ok",
		"version":           "1.0.0",
		"gateway_key_count": gwKeyCount,
	})
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	// Serve Next.js static export
	outDir := filepath.Join("web", "out")
	urlPath := r.URL.Path

	// Try exact file first
	filePath := filepath.Join(outDir, urlPath)
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		http.ServeFile(w, r, filePath)
		return
	}

	// Try index.html in directory
	if info, err := os.Stat(filePath); err == nil && info.IsDir() {
		indexPath := filepath.Join(filePath, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
			return
		}
	}

	// Try .html file at root (e.g., /providers → providers.html)
	htmlPath := filepath.Join(outDir, strings.TrimPrefix(urlPath, "/")+".html")
	if _, err := os.Stat(htmlPath); err == nil {
		http.ServeFile(w, r, htmlPath)
		return
	}

	// SPA fallback — serve root index.html for client-side routing
	http.ServeFile(w, r, filepath.Join(outDir, "index.html"))
}

func catchV1(w http.ResponseWriter, r *http.Request) {
	writeError(w, 404, "unknown v1 endpoint: "+r.URL.Path)
}

func systemShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}
	writeJSON(w, map[string]string{"status": "shutting_down", "message": "Server is shutting down..."})
	log.Println("[PAAP] Shutdown requested via API")
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}()
}

func systemRestart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "restarting", "message": "Server is restarting..."})
	log.Println("[PAAP] Restart requested via API")
	go func() {
		time.Sleep(100 * time.Millisecond)
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Start(); err != nil {
			log.Printf("[PAAP] Restart failed: %v", err)
			return
		}
		os.Exit(0)
	}()
}
