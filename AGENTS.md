# AGENTS.md — PAAP (Pangkalan API)

AI agent guidance for working on the PAAP codebase. Read this before making any changes.

## What is PAAP?

PAAP is a self-hosted API gateway that routes LLM requests across 40+ providers (OpenAI, Anthropic, Google, Xiaomi, Kimchi, Meta, Grok, OpenRouter, etc.) through a single OpenAI-compatible endpoint. It handles API key management, load balancing, logging, compression, and cost tracking.

**Core value:** One endpoint (`/v1/chat/completions`) → many providers. Swap providers without changing client code.

## Tech Stack

| Layer | Stack |
|-------|-------|
| Backend | Go 1.25, SQLite (go-sqlite3), net/http |
| Frontend | Next.js 16 (static export), React, Tailwind v4, shadcn/ui |
| Database | SQLite at `~/.paap/paap.db` |
| Build | `CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server/` |
| Frontend build | `cd web && npm run build` |
| Service | `sudo systemctl restart paap` |
| Theme | #FFFEF7 light / #0a0715 dark, 480px mobile |

## Project Structure

```
paap/
├── cmd/server/          # Go backend (all server code)
│   ├── main.go          # Entry point, routes, startup
│   ├── routing.go       # Main request handler (/v1/chat/completions)
│   ├── providers.go     # Provider CRUD, key management, model management
│   ├── groups.go        # Group routing (race, round-robin, fail-first)
│   ├── logs.go          # Request logging, cost tracking, export
│   ├── compression.go   # Markdown config loader (caveman/ponytail)
│   ├── caveman_compress.go  # Regex-based content compression
│   ├── rtk.go           # RTK integration (tool output compression)
│   ├── streaming.go     # SSE streaming handler
│   ├── gateway.go       # Gateway key management
│   ├── oauth.go         # OAuth flows (Grok, Anigravity, Google)
│   ├── anigravity.go    # Google Anigravity provider
│   ├── anthropic.go     # Anthropic Messages API
│   ├── merlin.go        # Merlin provider
│   ├── qoder.go         # Qoder provider
│   ├── qoder_cosy.go    # Qoder COSY signing
│   ├── qoder_oauth.go   # Qoder OAuth
│   ├── proxy.go         # Proxy pool management
│   ├── connections.go   # Provider connections
│   ├── keys.go          # API key helpers
│   ├── reqlog.go        # Request logging to file
│   └── t03_verify_test.go  # Tests
├── internal/
│   ├── db/db.go         # SQLite database layer
│   └── auth/            # Authentication
├── config/
│   ├── caveman.md       # Caveman compression instructions
│   └── ponytail.md      # Ponytail compression instructions
├── web/                 # Next.js frontend
│   ├── src/app/         # Pages (dashboard, providers, logs, groups, etc.)
│   ├── src/components/  # Shared components
│   └── src/lib/api.ts   # API client
└── .gitignore
```

## Key Architecture Decisions

### Request Flow
```
Client → /v1/chat/completions → authMiddleware → chatCompletionsHandler
  → Parse body (model, messages, stream)
  → Prompt injection (prepend/append)
  → Compression mode injection (caveman/ponytail)
  → RTK tool output compression
  → Caveman content compression (regex)
  → Route by model (direct or group)
  → Select API key (round-robin)
  → Forward to provider
  → Handle response (streaming or non-streaming)
  → Log request + update stats
```

### Provider Routing
- **Direct:** model name matches a provider's model → route directly
- **Group:** model name matches a group → race/round-robin/fail-first across group members
- **Round-robin:** cycle through API keys for load balancing
- **Race:** fire all keys simultaneously, take first winner

### Compression Pipeline
1. **Prompt Injection** — inject text into system message (prepend/append)
2. **Compression Mode** — caveman/ponytail instruction text from Markdown files
3. **RTK** — compress tool outputs (shell, git, test results)
4. **Caveman Content** — regex-based natural language compression

### Database Schema
Key tables:
- `providers` — provider config (name, base_url, auth_type, is_active)
- `api_keys` — API keys per provider (key_encrypted, is_active, fail_count)
- `groups` — model groups for routing
- `group_models` — models in each group
- `logs` — request logs (provider, model, tokens, latency, cost)
- `system_settings` — key-value settings (compression, injection, etc.)
- `gateway_keys` — client authentication keys

## Commands

```bash
# Build backend
cd /mnt/hdd/ares-workspace/paap
CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server/

# Build frontend
cd web && npm run build

# Restart service
sudo systemctl restart paap

# Check health
curl -s http://localhost:9090/api/health

# Check logs
journalctl -u paap -f

# Database
sqlite3 ~/.paap/paap.db "SELECT * FROM providers;"
sqlite3 ~/.paap/paap.db "SELECT * FROM api_keys WHERE is_active=1;"
```

## API Endpoints

### Core
- `POST /v1/chat/completions` — Main LLM endpoint (OpenAI-compatible)
- `GET /v1/models` — List available models
- `POST /v1/messages` — Anthropic Messages API

### Providers
- `GET /api/providers` — List providers
- `GET /api/providers/:id` — Get provider details
- `POST /api/providers` — Create provider
- `PATCH /api/providers/:id` — Update provider
- `DELETE /api/providers/:id` — Delete provider

### API Keys
- `GET /api/providers/:id/keys` — List keys
- `POST /api/providers/:id/keys` — Add key
- `PATCH /api/providers/:id/keys/:key_id` — Toggle key active
- `DELETE /api/providers/:id/keys/:key_id` — Delete key
- `POST /api/providers/:id/keys/disabled` — Bulk delete disabled keys

### Groups
- `GET /api/groups` — List groups
- `POST /api/groups` — Create group
- `GET /api/groups/:id` — Get group details
- `PATCH /api/groups/:id` — Update group
- `DELETE /api/groups/:id` — Delete group

### Logs
- `GET /api/logs` — List logs (with filters)
- `DELETE /api/logs` — Clear logs
- `GET /api/logs/export` — Export CSV
- `GET /api/logs/cost` — Cost summary

### Settings
- `GET /api/settings` — Get all settings
- `PUT /api/settings` — Update settings

### System
- `GET /api/health` — Health check
- `POST /api/system/restart` — Restart server
- `POST /api/system/shutdown` — Shutdown server

## Coding Conventions

### Go
- Package: `main` (single binary)
- Error handling: always check errors, log and continue (don't crash)
- Database: use parameterized queries (no string concatenation)
- Logging: `log.Printf("[PAAP] ...")` format
- Naming: camelCase for functions, snake_case for DB columns

### Frontend (TypeScript/React)
- Components: PascalCase
- Hooks: camelCase with `use` prefix
- Styling: Tailwind CSS (avoid inline styles)
- State: React Query for server state, useState for local state
- API: all calls go through `src/lib/api.ts`

### File Naming
- Go: `snake_case.go`
- React: `PascalCase.tsx` or `camelCase.tsx`
- Config: `snake_case.json` or `kebab-case.md`

## Common Patterns

### Adding a New Provider
1. Add provider to `providers` table
2. Add API keys to `api_keys` table
3. Add models to `models` table
4. Test with `POST /v1/chat/completions`

### Adding a New API Endpoint
1. Add handler function in appropriate file
2. Register route in `main.go` (or provider routes)
3. Add frontend API function in `src/lib/api.ts`
4. Add UI component

### Adding Compression Mode
1. Create `config/mymode.md` with levels (lite/full/ultra)
2. Add mode to `COMPRESSION_MODES` in `skills/page.tsx`
3. No backend changes needed (auto-loads from config/)

## Known Issues

1. **providers.go** — 1900 lines, needs splitting (but works)
2. **routing.go** — 1349 lines, needs splitting (but works)
3. **OAuth secret** — hardcoded in oauth.go (should be env var)
4. **Frontend catch blocks** — some are empty (should log errors)

## Security

- All API keys stored encrypted in DB
- Gateway keys required for `/v1/*` endpoints
- Key values masked in logs (first 6 + last 4 chars)
- No secrets in source code (except oauth.go — needs fix)
- SQL queries parameterized (no injection risk)

## Testing

```bash
# Run Go tests
cd /mnt/hdd/ares-workspace/paap
go test ./cmd/server/...

# Test health
curl -s http://localhost:9090/api/health

# Test with gateway key
GKEY=$(curl -s http://localhost:9090/api/gateway/keys | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['key'])")
curl -X POST http://localhost:9090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GKEY" \
  -d '{"model":"mimo-v2.5","messages":[{"role":"user","content":"hi"}],"max_tokens":10}'
```

## Deployment

1. Build: `CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server/`
2. Frontend: `cd web && npm run build`
3. Restart: `sudo systemctl restart paap`
4. Verify: `curl -s http://localhost:9090/api/health`

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PAAP_PORT` | `9090` | Server port |
| `PAAP_DATA` | `~/.paap` | Data directory |

## File Permissions

- Config files: `600` (owner read/write only)
- Database: `600` (owner read/write only)
- Binary: `755` (executable)
- Logs: `644` (world readable)

## When Working on This Codebase

1. **Always read AGENTS.md first** before making changes
2. **Build and test** after every change
3. **Check hardcoded paths** — use env vars or dynamic paths
4. **Don't crash on errors** — log and continue
5. **Parameterized SQL** — never concatenate strings into queries
6. **Frontend: use Tailwind** — avoid inline styles
7. **Frontend: use api.ts** — all API calls go through the client
8. **Backend: follow existing patterns** — look at similar code before adding new
9. **Config files** — put in `config/` folder, not hardcoded in code
10. **Test with gateway key** — all `/v1/*` endpoints require auth
