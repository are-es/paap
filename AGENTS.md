# PAAP — Pluggable AI API Proxy

A local AI API proxy/router that sits between clients (Cline, Continue, Open WebUI, custom apps) and dozens of AI providers (OpenAI, Anthropic, Google, xAI, Xiaomi, etc.). Frontend is a Next.js dashboard. Backend is Go, listening on `:9090`.

## Build & Run

```bash
# Backend (produces ./bin/paap)
go build -o bin/paap ./cmd/server
# or
./build.sh

# Frontend (Next.js, needs NVM Node 22+ — see Pitfalls)
cd web && npm install && npm run build

# Run
./bin/paap                    # defaults to :9090
./bin/paap -addr :8080        # custom port
```

Config lives at `~/.paap/serviceFile` (auto-created). Regedit UI at `http://localhost:9090/regedit`.

## Dev commands

```bash
npm run dev           # Next.js dev server
npm run build         # production build
npm run start         # serve production build
npm run lint          # Next.js lint
go test ./...         # Go tests
go vet ./...          # Go vet
gofmt -w .            # format Go code
```

## Project layout

```
cmd/server/          Go HTTP server (main, api, routing, headers, anthropic adapter, mcp, etc.)
internal/
  circuitbreaker/    Per-provider circuit breaker
  config/            App config
  db/                SQLite (WAL mode)
  middleware/        Auth guard
  models/            Provider key management
  policy/            Tool filtering, model routing, compression
  proxy/             Request lifecycle, streaming, provider routing
  tracking/          Token/cost/accounting
  policy/agent_policy.go  MCP server exposure mode config
  policy/mcp.go            MCP server loop
web/src/
  app/               Next.js pages: /providers, /gateway, /policy, /dashboard, /docs, /tools, /settings, /topology, /vision, /main, /security
  components/        Sidebar, flow diagrams, sidebar UI
  lib/               API helpers, providers
  utils/             AI SDK, speech, system-reminder, mcp
config/              Caveman, Ponytail skill configs
```

## Architecture

- Go server on `:9090` with two routing modes:
  - **Prefix mode** (`/openai/`, `/anthropic/`, `/google/`) — same key, dynamic provider
  - **Suffix mode** (`/v1`, `/chat/completions`) — auto-detect provider, dynamic key
- Gemini adapter: converts OpenAI format to Google AI format
- Anthropic adapter: converts OpenAI format to Anthropic Messages format
- MCP server: SSE transport (GET `/mcp/sse`), tools as MCP endpoints, `mcp__chrome_devtools__*` toolset, `/mcp/api` REST fallback
- Compression: Caveman (preserve content, drop filler), Ponytail (structural LLM compress)
- Vision Gateway: loads providers from SQLite keys, forwards multi-model requests
- Xiaomi detection: `segment=think` + empty delta + `finish_reason` means thinking, not done. Don't drop `reasoning_content` — clients wait for it.
- Xiaomi strips `tools` field — regenerate request without tools when validation error (resolves automatically in routing)

## Config

```json
{
  "api_keys": {"openai": "sk-...", "anthropic": "sk-ant-..."},
  "urls": {"openai": "https://api.openai.com/v1"},
  "compress": {"enabled": false},
  "secret": "jwt-signing-key"
}
```

Write via `curl -X PUT http://localhost:9090/regedit/api/config -d '{"key":"openai","value":"sk-..."}'`.

## Pitfalls

- **NVM required**: system Node 12 can't run Next.js 16. Use NVM Node 22+ (`nvm use 22`). Build scripts auto-fallback but global `node` won't work.
- **PII redaction**: Provider logs auto-redact SSN, credit cards, auth tokens, emails. Don't log raw provider responses.
- **Tool encoding**: Forward compatible — unknown fields preserved, no schema enforcement.
- **Xiaomi mode**: Client tools stripped from request (provider limitation). MCP still works via proxy-level injection.
- **venv**: Create a dedicated venv (`python -m venv venv`) — system site-packages break installs (`externally-managed-environment`).
- **DB**: SQLite WAL mode. Path in `serviceFile`.
- **Agent mode**: Claude Code runs with `--dangerously-skip-permissions` (auto-accept all edits). NOT safe for production.
- **Workflow**: Typical: `go build -o bin/paap ./cmd/server && npm run build && ./bin/paap`

## Port map

- `:9090` — PAAP proxy
- `:3000` — Next.js dev
- `:9222` — Chrome CDP
