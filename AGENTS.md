# PAAP — Pluggable AI API Proxy

A local AI API proxy/router that sits between clients (Cline, Continue, Open WebUI, custom apps) and dozens of AI providers (OpenAI, Anthropic, Google, xAI, Xiaomi, etc.). Frontend is a Next.js dashboard. Backend is Go, listening on `:9090`.

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

## Dev commands

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
- **Smart Compressor**: unified compression replacing old RTK/Caveman/Headroom systems
- Vision Gateway: loads providers from SQLite keys, forwards multi-model requests
- Xiaomi detection: `segment=think` + empty delta + `finish_reason` means thinking, not done

## Smart Compressor

Unified token compression. Compresses old messages in history before sending to provider. Content-aware routing detects content type and dispatches to optimal compressor.

### Levels

| Level | Batch | Roles | Strategies |
|---|---|---|---|
| **Off** | — | — | No compression. Auto-injects prompt for cache mode |
| **Lite** | 25 messages | tool outputs | ANSI strip, blank collapse, line dedup |
| **Medium** | 50 messages | tool + user + system | +content-type detection, +Headroom reformat (JSON minify, log dedup, diff strip), +bloat offload |
| **High** | 100 messages | all except assistant | +SmartCrusher (statistical JSON), +cache stability, +pattern collapse, +code block dedup, +list compaction, +BM25 extractive, +cross-msg field dedup |

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

### Rules

- **Assistant messages** are NEVER compressed — agent replies stay pure
- **Recent messages** (last 6) are skipped — preserve active context
- **Volatile content** is skipped — preserve provider cache prefix stability
- **Compression modes**: Off = auto-inject prompt (cache mode), Lite/Med/High = compress, skip injection
- Token savings logged to DB with before/after tracking
- Dashboard: `/compression` page with level selector + live compression logs

### Settings

```
compress_level: "off" | "lite" | "medium" | "high"
```

## Pitfalls

- **NVM required**: system Node 12 can't run Next.js 16. Use NVM Node 22+.
- **PII redaction**: Provider logs auto-redact SSN, credit cards, auth tokens, emails.
- **Xiaomi mode**: Client tools stripped from request (provider limitation). MCP still works via proxy-level injection.
- **DB**: SQLite WAL mode.
- **Workflow**: `go build -o bin/paap-server ./cmd/server && npm run build && sudo systemctl restart paap`

## Port map

- `:9090` — PAAP proxy
- `:3000` — Next.js dev
- `:9222` — Chrome CDP
