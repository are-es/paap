# PAAP — Pangkalan API

OpenAI-compatible API gateway yang me-routing request ke 30+ provider (Xiaomi, StepFun, Google AI Studio, OpenRouter, custom endpoint). Mengelola API key, model discovery, load balancing, failover, dan kompresi token.

## Fitur

- **Multi-Provider Routing** — Route ke 30+ provider dengan failover otomatis
- **API Key Management** — Multiple keys per provider, round-robin, auto-disable on error
- **Model Discovery** — Auto-detect model dari provider API
- **Vision Auto-Route** — Auto-route request berisi gambar ke model vision
- **MCP Server** — JSON-RPC 2.0 server dengan tools (Image Gen, TTS, Vision Analysis)
- **Smart Compressor** — Unified token compression (lite/medium/high)
- **System Prompt Injection** — Custom instructions di setiap request
- **Groups & Race Routing** — Kirim ke beberapa provider, gunakan response tercepat
- **Dashboard** — Stats real-time, provider topology, gateway key management
- **Multi-Language UI** — 6 bahasa (EN, ID, ZH, JA, KO, AR)

## Quick Start

### Prerequisites

- Go 1.24+
- Node.js 24+ (via nvm)
- SQLite (included in Go binary)

### Install

```bash
# Clone
git clone <repo-url> paap
cd paap

# Build backend
CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server/

# Build frontend
cd web && npm install && npm run build && cd ..

# Run
export PAAP_DATA=~/.paap
export PAAP_PORT=9090
./bin/paap-server
```

### Systemd (Production)

```bash
sudo cp paap.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable paap
sudo systemctl start paap

# Check status
sudo systemctl status paap
sudo journalctl -u paap -f
```

## API Usage

### Chat Completion (OpenAI-compatible)

```bash
curl -X POST http://localhost:9090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ***" \
  -d '{
    "model": "mimo-v2.5-pro",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Dashboard

Akses dashboard di `http://localhost:9090` setelah server berjalan.

- **Stats** — Request, token, biaya per 24 jam
- **Providers** — API key management, model discovery
- **Tools** — Vision auto-route, MCP tools config
- **Compression** — Smart Compressor level selector + live logs
- **Logs** — Request log dengan filter dan export
- **Groups** — Race routing, round-robin
- **Proxy** — Proxy pool management
- **Settings** — Bahasa, backup/restore, server control

## Smart Compressor

Unified token compression. Replaces old RTK/Caveman/Headroom systems with one pipeline. Content-aware routing detects content type (JSON, code, logs, diffs, text) and dispatches to the optimal compressor.

### How It Works

1. Request enters with messages array
2. Compressor detects message roles (tool/user/system/assistant)
3. Resolves compression level and eligible messages
4. Skips assistant messages (never compress agent replies)
5. Skips recent messages (last 6) to preserve active context
6. For volatile content (timestamps, UUIDs, request IDs): skips compression to preserve provider cache prefix stability

### Levels

| Level | Batch | Target | Strategies |
|---|---|---|---|
| **Off** | — | — | No compression. Auto-injects prompt for cache mode |
| **Lite** | 25 msg | tool outputs | ANSI strip, blank collapse, line dedup |
| **Medium** | 50 msg | tool + user + system | +content-type detection, +Headroom reformat (JSON minify, log dedup, diff strip), +bloat offload |
| **High** | 100 msg | all except assistant | +SmartCrusher (statistical JSON), +cache stability detection, +pattern collapse, +code block dedup, +list compaction, +BM25 extractive, +cross-msg field dedup |

### High Mode Pipeline (15 steps)

1. **Threshold gate** — skip if < 50 bytes
2. **Cache stability** — skip volatile content (timestamps, UUIDs, request IDs)
3. **Safe transforms** — ANSI strip, blank collapse
4. **Content-type detection** — JSON, code, logs, diffs, search, HTML, text
5. **Headroom reformat** — lossless (JSON minify, log dedup, diff strip)
6. **SmartCrusher** — statistical JSON compression (field importance, array truncation)
7. **Bloat offload** — score-based lossy compression
8. **Pattern collapse** — detect repeated tool-call sequences
9. **Code block dedup** — keep only latest version of duplicate code blocks
10. **List compaction** — merge similar list items
11. **FlintChipper** — head+tail line budget truncation
12. **Reasoning trim** — keep conclusion only (assistant messages)
13. **BM25 extractive** — score segments by relevance, keep top ones
14. **Cross-message field dedup** — deduplicate repeated fields across messages
15. **Final cleanup** — blank collapse, trim

### Compression Modes

| Mode | Behavior |
|---|---|
| **Off** | Auto-injects prompt for cache mode (prompt_injection_enabled + compression_mode) |
| **Lite/Medium/High** | Compress messages, skip prompt injection (cache-friendly, prefix stays stable) |

### Monitoring

- Dashboard `/compression` page: live compression logs with before/after tokens
- Logs show: timestamp, content type, level, original tokens, compressed tokens, saved %
- Clear logs via UI button

## MCP Client Setup

### Hermes Agent

```yaml
# ~/.hermes/config.yaml
mcp_servers:
  paap-mcp:
    url: http://127.0.0.1:9090/mcp/message
    enabled: true
```

## Troubleshooting

### Provider offline
```bash
sqlite3 ~/.paap/paap.db "SELECT * FROM api_keys WHERE provider_id='xxx' AND is_active=1"
sudo systemctl restart paap
```

### Compression tidak jalan
```bash
# Cek level setting
sqlite3 ~/.paap/paap.db "SELECT key, value FROM system_settings WHERE key='compress_level'"

# Cek logs
sudo journalctl -u paap | grep compression
```

## Tech Stack

- **Backend**: Go 1.24, SQLite (modernc.org/sqlite)
- **Frontend**: Next.js 15, React 19, TypeScript, Tailwind CSS, shadcn/ui
- **Database**: SQLite
- **Auth**: Gateway keys (Bearer token)
- **MCP**: JSON-RPC 2.0

## License

Private — internal use only.
