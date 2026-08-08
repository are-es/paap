# PAAP — Pluggable AI API Proxy

A local AI API proxy/router that sits between clients (Cline, Continue, Open WebUI, custom apps) and AI providers (OpenAI, Anthropic, Google, xAI, Xiaomi, etc.). Frontend is a Next.js dashboard. Backend is Go, listening on `:9090`.

## Build & Run

```bash
# Backend
go build -o bin/paap-server ./cmd/server

# Frontend
cd web && npm install && npm run build

# Run
./bin/paap-server              # defaults to :9090
./bin/paap-server -addr :8080  # custom port
```

## Dev Commands

```bash
npm run dev           # Next.js dev server
npm run build         # production build
npm run start         # serve production build
go test ./...         # Go tests
go vet ./...          # Go vet
gofmt -w .            # format Go code
```

## Architecture

- Go server on `:9090` with two routing modes:
  - **Prefix mode** (`/openai/`, `/anthropic/`, `/google/`) — same key, dynamic provider
  - **Suffix mode** (`/v1`, `/chat/completions`) — auto-detect provider, dynamic key
- Gemini adapter: converts OpenAI format to Google AI format
- Anthropic adapter: converts OpenAI format to Anthropic Messages format
- MCP server: SSE transport (GET `/mcp/sse`), tools as MCP endpoints
- **Smart Compressor**: unified compression with content-aware routing
- Vision Gateway: loads providers from SQLite keys, forwards multi-model requests
- Xiaomi detection: `segment=think` + empty delta + `finish_reason` means thinking, not done

## Code Structure

### Backend (`cmd/server/`)

| File | Purpose |
|---|---|
| `main.go` | HTTP server setup, routes, middleware, shared HTTP clients |
| `routing.go` | Core request router — auth, provider selection, compression wiring, upstream forwarding |
| `anthropic.go` | Anthropic Messages format adapter (OpenAI ↔ Anthropic translation) |
| `providers.go` | Provider CRUD, builtin detection, model discovery |
| `groups.go` | Proxy groups, race routing, round-robin across providers |
| `keys.go` | API key management per provider |
| `proxy.go` | Proxy pool management |
| `streaming.go` | SSE streaming proxy + token extraction from final chunk |
| `vision.go` | Vision gateway — auto-route image requests to vision models |
| `mcp.go` | MCP server (JSON-RPC 2.0) over SSE transport |
| `mcp_tools.go` | MCP tool definitions (Image Gen, TTS, Vision Analysis) |
| `mcp_adapters.go` | MCP protocol adapters |
| `oauth.go` | OAuth flow handlers |
| `backup.go` | Database backup/restore/clear |
| `logs.go` | Request logging API |
| `reqlog.go` | Request log storage |
| `traffic_logger.go` | Traffic logging for analytics |
| `gateway.go` | Gateway key management, settings CRUD |
| `connections.go` | Provider connection management |
| `tools.go` | Tool system — auto-route based on content detection |
| `rtk.go` | RTK (Request Token Knobs) detection |
| `caveman_compress.go` | Legacy caveman compressor (kept for backward compat) |
| `compression.go` | Compression config loader (markdown-based configs) |
| `compression_logs.go` | Compression log API + stats aggregation |
| `anigravity.go` | Gemini/Vertex AI adapter |
| `merlin.go` | Merlin provider adapter |

### Compression Package (`cmd/server/compression/`)

| File | Purpose |
|---|---|
| `levels.go` | Level definitions (Off/Lite/Medium/High) + per-level config |
| `compressor.go` | Main entry point — `CompressRawMessages()` dispatches to level-specific compressors |
| `content_detector.go` | Detects content type (JSON, code, logs, diffs, search, HTML, text) |
| `headroom.go` | Headroom-style 2-phase compression (reformat + bloat offload) |
| `smart_crusher.go` | SmartCrusher-lite — statistical JSON compression (field importance, array truncation) |
| `cache_stability.go` | Volatile content detection (timestamps, UUIDs, request IDs) — skip compression for cache safety |
| `bm25.go` | BM25 extractive scoring — score text segments by relevance, keep top ones |
| `strategies_high.go` | High-mode strategies — pattern collapse, code block dedup, list compaction |
| `strategies_text.go` | Prose filter — filler word removal (EN + ID), word-boundary safe regex |
| `strategies_log.go` | Log-specific — dedup repeated lines, head/tail truncation |
| `caveman_pipeline.go` | FlintChipper (head+tail line budget), ANSI strip, blank collapse |
| `strip.go` | ANSI escape removal, blank line collapse |

### Internal Packages (`internal/`)

| File | Purpose |
|---|---|
| `db/db.go` | SQLite database init, migrations, schema (WAL mode, busy_timeout=5000) |
| `translator/translator.go` | OpenAI ↔ Anthropic message format translation |
| `translator/streaming.go` | Streaming SSE translation (delta events) |

### Frontend (`web/src/`)

| Path | Purpose |
|---|---|
| `app/page.tsx` | Dashboard home — stats, topology |
| `app/providers/page.tsx` | Provider management |
| `app/providers/setup/` | Provider setup wizard |
| `app/compression/page.tsx` | Compression level selector + live logs |
| `app/tools/page.tsx` | Tool configuration |
| `app/tools/vision/page.tsx` | Vision auto-route config |
| `app/tools/mcp/page.tsx` | MCP server config |
| `app/groups/page.tsx` | Proxy groups |
| `app/logs/page.tsx` | Request logs |
| `app/proxy/page.tsx` | Proxy pool |
| `app/settings/page.tsx` | Settings (language, backup, server) |
| `components/sidebar.tsx` | Navigation sidebar |
| `components/providers/` | Provider components |
| `components/ui/` | Reusable UI components |
| `lib/api.ts` | API client helpers |

## Smart Compressor

Unified token compression. Content-aware routing detects content type and dispatches to optimal compressor.

### Levels

| Level | Batch | Roles | Strategies |
|---|---|---|---|
| **Off** | — | — | No compression. Auto-injects prompt for cache mode |
| **Lite** | 25 messages | tool outputs | ANSI strip, blank collapse, line dedup |
| **Medium** | 50 messages | tool + user + system | +content-type detection, +Headroom reformat, +bloat offload |
| **High** | 100 messages | all except assistant | +SmartCrusher, +cache stability, +pattern collapse, +code block dedup, +list compaction, +BM25 extractive, +cross-msg field dedup |

### High Mode Pipeline

1. Threshold gate (skip < 50 bytes)
2. Cache stability (skip volatile content: timestamps, UUIDs, request IDs)
3. Safe transforms (ANSI strip, blank collapse)
4. Content-type detection (JSON, code, logs, diffs, search, HTML, text)
5. Headroom reformat (lossless)
6. SmartCrusher (statistical JSON compression)
7. Bloat offload (score-based lossy)
8. Pattern collapse (repeated tool-call sequences)
9. Code block dedup
10. List compaction
11. FlintChipper (head+tail truncation)
12. Reasoning trim (assistant messages)
13. BM25 extractive
14. Cross-message field dedup
15. Final cleanup

### Compression Modes

| Mode | Behavior |
|---|---|
| **Off** | Auto-injects prompt for cache mode (prompt_injection_enabled + compression_mode) |
| **Lite/Medium/High** | Compress messages, skip prompt injection (cache-friendly, prefix stays stable) |

### Rules

- **Assistant messages** are NEVER compressed — agent replies stay pure
- **Recent messages** (last 6) are skipped — preserve active context
- **Volatile content** is skipped — preserve provider cache prefix stability
- Token savings logged to DB with before/after tracking
- Dashboard: `/compression` page with level selector + live compression logs

### Settings

```
compress_level: "off" | "lite" | "medium" | "high"
```

## Database

SQLite with WAL mode. Located at `~/.paap/paap.db`.

### Key Tables

| Table | Purpose |
|---|---|
| `providers` | Provider configs (name, base_url, type, round_robin, skip_compression) |
| `api_keys` | API keys per provider (key, fail_count, last_error) |
| `models` | Discovered models per provider |
| `logs` | Request logs (tokens_in, tokens_out, latency, compression stats) |
| `compression_logs` | Detailed compression events (content_type, level, before/after bytes) |
| `system_settings` | Key-value settings (compress_level, prompt_injection_enabled, etc.) |
| `groups` | Proxy groups for race routing |
| `group_models` | Model-to-group mapping |
| `proxy_groups` | Proxy pool configs |
| `proxy_group_members` | Proxy pool members |
| `gateway_keys` | Gateway authentication keys |
| `tools` | MCP tool configurations |
| `model_pricing` | Per-model pricing for cost tracking |
| `usage_stats` | Aggregated usage statistics |

### Useful Queries

```bash
# Check compression level
sqlite3 ~/.paap/paap.db "SELECT key, value FROM system_settings WHERE key='compress_level'"

# Check active providers
sqlite3 ~/.paap/paap.db "SELECT builtin_id, name, is_active FROM providers"

# Check compression savings
sqlite3 ~/.paap/paap.db "SELECT COUNT(*), SUM(tokens_saved) FROM logs WHERE tokens_saved > 0"

# Check compression logs
sqlite3 ~/.paap/paap.db "SELECT COUNT(*) FROM compression_logs"

# Check volatile skip count
sqlite3 ~/.paap/paap.db "SELECT COUNT(*) FROM logs WHERE notes LIKE '%volatile%'"
```

## Pitfalls

- **NVM required**: system Node 12 can't run Next.js 16. Use NVM Node 22+.
- **PII redaction**: Provider logs auto-redact SSN, credit cards, auth tokens, emails.
- **Xiaomi mode**: Client tools stripped from request (provider limitation). MCP still works via proxy-level injection.
- **DB**: SQLite WAL mode. Busy timeout 5000ms.
- **Workflow**: `go build -o bin/paap-server ./cmd/server && npm run build && sudo systemctl restart paap`
- **Compression**: `compress_level=Off` auto-injects prompt for cache mode. `Lite/Med/High` compress and skip injection.

## Troubleshooting

### Provider offline
```bash
sqlite3 ~/.paap/paap.db "SELECT * FROM api_keys WHERE provider_id='xxx' AND is_active=1"
sudo systemctl restart paap
```

### Compression not working
```bash
# Check level setting
sqlite3 ~/.paap/paap.db "SELECT key, value FROM system_settings WHERE key='compress_level'"

# Check logs
sudo journalctl -u paap | grep compression

# Check if provider has skip_compression
sqlite3 ~/.paap/paap.db "SELECT builtin_id, name, skip_compression FROM providers"
```

### Build fails
```bash
# Check Go version
go version  # needs 1.24+

# Check CGO (required for SQLite)
CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server

# Check Node version
node -v  # needs 22+ via nvm
```

### Dashboard not loading
```bash
# Rebuild frontend
cd web && npm run build

# Check if server is running
curl http://localhost:9090/health
```

## Port Map

- `:9090` — PAAP proxy
- `:3000` — Next.js dev
- `:9222` — Chrome CDP
