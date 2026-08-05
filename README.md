# PAAP — Pangkalan API

OpenAI-compatible API gateway yang me-routing request ke 30+ provider (Xiaomi, StepFun, Google AI Studio, OpenRouter, custom endpoint). Mengelola API key, model discovery, load balancing, failover, dan kompresi token.

## Fitur

- **Multi-Provider Routing** — Route ke 30+ provider dengan failover otomatis
- **API Key Management** — Multiple keys per provider, round-robin, auto-disable on error
- **Model Discovery** — Auto-detect model dari provider API
- **Vision Auto-Route** — Auto-route request berisi gambar ke model vision
- **MCP Server** — JSON-RPC 2.0 server dengan tools (Image Gen, TTS, Vision Analysis)
- **Token Compression** — Caveman pipeline (tool output compression), RTK (Rust Token Killer)
- **System Prompt Injection** — Custom instructions di setiap request
- **Headroom Proxy** — External Python compression service
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
export GATEWAY_KEYS=your-secret-key
./bin/paap-server
```

### Systemd (Production)

```bash
# Install service
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
  -H "Authorization: Bearer YOUR_GATEWAY_KEY" \
  -d '{
    "model": "mimo-v2.5-pro",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Text-to-Speech (MCP)

```bash
curl -X POST http://localhost:9090/mcp/message \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_GATEWAY_KEY" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "text_to_speech",
      "arguments": {"text": "Hello world", "voice": "Mia"}
    }
  }'
```

### Image Generation (MCP)

```bash
curl -X POST http://localhost:9090/mcp/message \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_GATEWAY_KEY" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "generate_image",
      "arguments": {"prompt": "a cat in space"}
    }
  }'
```

## Dashboard

Akses dashboard di `http://localhost:9090` setelah server berjalan.

Fitur dashboard:
- **Stats** — Request, token, biaya per 24 jam
- **Provider Topology** — Visual map semua provider
- **Gateway Keys** — Generate, copy, revoke API keys
- **Provider Setup** — Tambah provider, API key, detect model
- **Tools** — Vision auto-route, MCP tools config
- **Compression** — Caveman, RTK, Headroom settings
- **Logs** — Request log dengan filter dan export
- **Groups** — Race routing, round-robin
- **Proxy** — Proxy pool management
- **Settings** — Bahasa, backup/restore, server control

## MCP Client Setup

### Hermes Agent

```yaml
# ~/.hermes/config.yaml
mcp_servers:
  paap-mcp:
    url: http://127.0.0.1:9090/mcp/message
    enabled: true
```

### Claude Code

```json
// ~/.claude/settings.json
{
  "mcpServers": {
    "paap": {
      "url": "http://127.0.0.1:9090/mcp/message",
      "headers": { "Authorization": "Bearer YOUR_GATEWAY_KEY" }
    }
  }
}
```

### OpenCode

```json
// ~/.config/opencode/config.json
{
  "mcp": {
    "servers": {
      "paap": {
        "url": "http://127.0.0.1:9090/mcp/message",
        "auth": "Bearer YOUR_GATEWAY_KEY"
      }
    }
  }
}
```

## Compression

### Caveman Mode

Kompresi tool output — hapus filler words, whitespace, HTML noise. Levels:
- **lite** — whitespace only
- **full** — filler + whitespace
- **ultra** — maximum compression

### RTK (Rust Token Killer)

Kompresi tool output (bash, grep, git). 60-90% pengurangan.

### Headroom

Proxy Python eksternal untuk kompresi tambahan. Berjalan di port 8787.

## Troubleshooting

### Provider offline
```bash
# Cek API key
sqlite3 ~/.paap/paap.db "SELECT * FROM api_keys WHERE provider_id='xxx' AND is_active=1"

# Restart
sudo systemctl restart paap
```

### Model tidak terdeteksi
```bash
# Manual detect
curl -X POST http://localhost:9090/api/providers/xxx/detect \
  -H "Authorization: Bearer YOUR_KEY"
```

### Compression tidak jalan
```bash
# Cek setting
sqlite3 ~/.paap/paap.db "SELECT key, value FROM system_settings WHERE key='compression_mode'"

# Cek logs
sudo journalctl -u paap | grep -i caveman
```

### MCP error
```bash
# Cek status
curl http://localhost:9090/mcp/status

# Cek logs
sudo journalctl -u paap | grep -i mcp
```

## Tech Stack

- **Backend**: Go 1.24, SQLite (modernc.org/sqlite)
- **Frontend**: Next.js 15, React 19, TypeScript, Tailwind CSS, shadcn/ui
- **Database**: SQLite (~/.paap/paap.db)
- **Auth**: Gateway keys (Bearer token)
- **MCP**: JSON-RPC 2.0

## License

Private — internal use only.
