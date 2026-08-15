package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
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

// newGroupClient creates an isolated HTTP client for group routing attempts.
// Each call gets its own Transport (connection pool) to prevent HTTP/2
// connection reuse across different providers (421 Misdirected Request).
func newGroupClient() *http.Client {
	return &http.Client{
		Timeout: 180 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
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
	dataDir := dataDirPath()
	port := "9090"
	// Bind loopback by default — PAAP holds provider credentials and a control
	// plane. Set PAAP_HOST=0.0.0.0 to expose it deliberately.
	host := defaultHost

	if p := os.Getenv("PAAP_PORT"); p != "" {
		port = p
	}
	if h := os.Getenv("PAAP_HOST"); h != "" {
		host = h
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
	mux.HandleFunc("/api/providers/favicon", handleFaviconFetch)
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
	mux.HandleFunc("/api/compression/logs", compressionLogsHandler)
	mux.HandleFunc("/api/compression/summary", compressionSummaryHandler)
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

	handler := apiGuard(mux)

	// Default: both loopback families. "localhost" resolves to 127.0.0.1 for some
	// clients and ::1 for others (browsers usually pick ::1), so binding only one
	// silently breaks the other half.
	addrs := []string{net.JoinHostPort(host, port)}
	if host == defaultHost {
		addrs = append(addrs, net.JoinHostPort("::1", port))
	} else {
		log.Printf("WARNING: bound to %s — /api requires a gateway key from non-local addresses", host)
	}

	var listeners []net.Listener
	for _, a := range addrs {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			// A host without IPv6 loopback is fine as long as something bound.
			log.Printf("listen %s: %v", a, err)
			continue
		}
		listeners = append(listeners, ln)
	}
	if len(listeners) == 0 {
		log.Fatalf("could not bind any address on port %s", port)
	}

	log.Printf("PAAP listening on %s", strings.Join(addrs, ", "))
	log.Printf("  Dashboard : http://localhost:%s", port)
	log.Printf("  API       : http://localhost:%s/v1/chat/completions", port)

	errc := make(chan error, len(listeners))
	for _, ln := range listeners {
		go func(l net.Listener) { errc <- http.Serve(l, handler) }(ln)
	}
	log.Fatal(<-errc)
}

// dataDirPath resolves the PAAP data directory: $PAAP_DATA, else ~/.paap.
// Single source of truth — backup, request logs and the database must all land
// in the same place or a restore silently writes to a database nobody is using.
func dataDirPath() string {
	if d := os.Getenv("PAAP_DATA"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".paap")
}

// defaultHost is the IPv4 loopback; ::1 is added alongside it at listen time.
const defaultHost = "127.0.0.1"

// isLoopback reports whether the request came from the local machine.
func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// apiGuard protects the /api/* control plane. Those endpoints read gateway keys,
// dump and overwrite the database, and stop the process — none of it should be
// reachable by an unauthenticated caller.
//
// Loopback callers pass without a key so the bundled dashboard keeps working;
// everyone else must present a gateway key. A cross-origin Origin header is
// rejected outright: without that check, any website the user visits could drive
// their browser into this server and inherit the loopback exemption.
func apiGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}

		// CSRF: a browser sends Origin on cross-site requests. Same-origin and
		// non-browser clients (curl, scripts) either match Host or send nothing.
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				writeError(w, 403, "cross-origin request rejected")
				return
			}
		}

		if isLoopback(r) {
			next.ServeHTTP(w, r)
			return
		}

		if !validGatewayKey(r.Header.Get("Authorization")) {
			writeError(w, 401, "unauthorized: /api requires a gateway key from a non-local address")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validGatewayKey reports whether an Authorization header carries an active gateway key.
func validGatewayKey(authHeader string) bool {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	var keyID string
	err := db.DB.QueryRow("SELECT id FROM gateway_keys WHERE key=? AND is_active=1", parts[1]).Scan(&keyID)
	return err == nil
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
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}
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
