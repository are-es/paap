# PAAP — Pangkalan API

Self-hosted API gateway for LLM requests. One endpoint, 40+ providers.

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/are-es/paap/main/install.sh | sh
```

## What it does

- **Single endpoint** — `/v1/chat/completions` (OpenAI-compatible)
- **40+ providers** — OpenAI, Anthropic, Google, Xiaomi, Kimchi, Meta, Grok, OpenRouter, etc.
- **API key management** — multiple keys per provider, round-robin, auto-disable on failure
- **Group routing** — race, round-robin, fail-first across multiple providers
- **Compression** — caveman/ponytail instruction injection, RTK tool output compression
- **Logging** — request logs, cost tracking, CSV export
- **Dashboard** — web UI for managing providers, keys, groups, logs

## Manual Install

```bash
# Clone
git clone https://github.com/are-es/paap.git ~/.paap/paap
cd ~/.paap/paap

# Build backend
CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server/

# Build frontend
cd web && npm install && npm run build && cd ..

# Run
./bin/paap-server
```

## Usage

```bash
# Health check
curl http://localhost:9090/api/health

# List models
curl http://localhost:9090/v1/models

# Chat completion
curl -X POST http://localhost:9090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_GATEWAY_KEY" \
  -d '{"model":"mimo-v2.5","messages":[{"role":"user","content":"hello"}]}'
```

## Configuration

| Env Variable | Default | Description |
|--------------|---------|-------------|
| `PAAP_PORT` | `9090` | Server port |
| `PAAP_DATA` | `~/.paap` | Data directory |

## Dashboard

Open `http://localhost:9090` in your browser.

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/chat/completions` | POST | Main LLM endpoint |
| `/v1/models` | GET | List models |
| `/api/providers` | GET/POST | Manage providers |
| `/api/groups` | GET/POST | Manage groups |
| `/api/logs` | GET | View logs |
| `/api/settings` | GET/PUT | Manage settings |
| `/api/health` | GET | Health check |

## License

MIT
