# PAAP — Pangkalan API

LLM gateway/proxy. Client sends requests to PAAP, PAAP translates & routes to providers.

**Stack:** Go + SQLite + Next.js static export
**Port:** 9090
**Data:** `~/.paap/`

## Build & Run

```bash
# Build backend
CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server/

# Build frontend (Node 20+)
cd web && npm run build

# Deploy
sudo systemctl stop paap && mv bin/paap bin/paap-server && sudo systemctl start paap

# Health check
curl -s http://localhost:9090/api/health
```

## How It Works

1. Client sends to `/v1/messages` (Anthropic) or `/v1/chat/completions` (OpenAI)
2. PAAP validates gateway key
3. **Tools check** — if content triggers a tool (e.g., images → Vision), model is auto-overridden
4. Routes to provider based on `provider/model` format
5. Translates Anthropic → OpenAI (all providers go through translator)
6. Compresses request (caveman, RTK, headroom)
7. Sends to provider, returns response

## Model Format

Format: `provider/model` (e.g., `hcnsec/DeepSeek-V4-Flash`)

Claude Code adds `claude-` prefix → PAAP strips it automatically.
Claude Code adds `[1m]` suffix (1M context) → PAAP strips it automatically.

## Tools System

Tools auto-route requests based on content detection. When a tool triggers, PAAP overrides the model to the tool's configured route model.

### Vision Tool

Detects images in request (Anthropic `image` blocks or OpenAI `image_url` blocks). Routes to configured vision model. Images are sent intact — no conversion to text descriptions.

**Config:** Tools → Vision → Select route model

**Logs:** Tool usage tracked in logs with `tool_used` and `original_model` columns.

**API:** `GET /api/tools`, `POST /api/tools`, `PUT /api/tools/{id}`, `DELETE /api/tools/{id}`

### Adding New Tools

1. Add tool type to `cmd/server/tools.go` (const + detection logic in `ProcessTools()`)
2. Add UI card in `web/src/app/tools/page.tsx`
3. Add setup page in `web/src/app/tools/<type>/page.tsx`

## Structure

```
cmd/server/           Go backend (routing, handlers, providers, tools)
internal/translator/  Anthropic ↔ OpenAI conversion
internal/db/          SQLite database
web/src/              Next.js frontend
web/src/app/tools/    Tools UI pages
config/               Compression configs (caveman.md, ponytail.md)
```

## Common Errors

**"model not found"**
- Check model format: `provider/model`
- Check model exists: `curl http://localhost:9090/v1/models`

**"all keys exhausted"**
- All keys failed 3x → auto-disabled
- Reset: `sqlite3 ~/.paap/paap.db "UPDATE api_keys SET fail_count=0, is_active=1 WHERE provider_id='...'"`

**"invalid or inactive API key"**
- Gateway key wrong or not configured
- Check: Settings → Gateway Keys

**"upstream timeout"**
- Provider slow/down
- Try different key or provider

**"Failed to parse JSON"**
- Invalid provider response
- Check provider URL

**Empty output (0 tokens)**
- Provider warm-up / rate limit
- Auto-retry, usually succeeds

## Database

```bash
sqlite3 ~/.paap/paap.db

# Check key status
SELECT id, name, is_active, fail_count FROM api_keys WHERE provider_id='...';

# Reset key
UPDATE api_keys SET fail_count=0, is_active=1 WHERE id='...';

# Check logs (includes tool tracking)
SELECT timestamp, provider_name, model_id, tool_used, original_model FROM logs ORDER BY timestamp DESC LIMIT 10;

# Check tools
SELECT id, name, type, enabled, route_model FROM tools;
```

## Add Provider

1. Add via UI or `POST /api/providers`
2. Add API key via UI or `POST /api/keys`
3. Detect models: `POST /api/providers/{id}/models/detect`
4. Logo: place in `web/public/assets/`, update `web/src/lib/provider-logos.ts`

## Built-in Providers

xiaomi, meta, google, kimchi, openrouter, grok-cli, anigravity, ollamacloud, runapi, stepfun, hcnsec

## Important Notes

- Frontend requires rebuild: `cd web && npm run build`
- Deploy: `stop → mv binary → start` (binary is locked while running)
- Keys with `fail_count >= 3` are auto-disabled
- Tool names >64 chars are auto-truncated
- `config/*.md` are compression configs, not skill files
- Tools system: Vision auto-routes image requests to configured model
- `[1m]` suffix from Claude Code is stripped automatically

## Roadmap

See `.agent/roadmap.md` for current task breakdown and progress.
