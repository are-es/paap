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

Unified token compression. Compresses old messages in history before sending to provider.

### Levels

| Level | Batch | Roles | Strategies |
|---|---|---|---|
| **Off** | — | — | No compression |
| **Lite** | 10 messages | tool outputs | ANSI strip, blank collapse |
| **Medium** | 20 messages | tool + user | +line budget, +prose filter, +log dedup |
| **High** | 30 messages | tool + user + system | +JSON/XML compress, +aggressive trunc |

### Rules

- **Assistant messages** are NEVER compressed — agent replies stay pure
- **Recent messages** (last 6) are skipped — preserve active context
- Compression runs in parallel (goroutines) for speed
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
