# PAAP — Agent Context

PAAP (Pangkalan API) is an LLM gateway/proxy: `Client → PAAP → Provider`. It is not a provider itself. It fans requests out across multiple upstream providers, rotates API keys, optionally routes through SOCKS5/HTTP proxies, and compresses prompts before forwarding.

Repo: `/mnt/hdd/ares-workspace/paap` — version 0.1.0, branch `master`.

## Stack

- Backend: Go, `CGO_ENABLED=1` (SQLite via cgo), stdlib `net/http` + `http.ServeMux`
- Storage: SQLite, WAL mode
- Frontend: Next.js 16 static export (`output: "export"`), Tailwind v4, shadcn/ui, TanStack Query
- Served by the Go binary from `web/out/`

## Commands

```bash
# backend
CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server/

# frontend
cd web && npm run build

# deploy
sudo systemctl stop paap && mv bin/paap bin/paap-server && sudo systemctl start paap

# stop
sudo systemctl stop paap

# health
curl -s http://localhost:9090/api/health
```

Run via systemd, not `paap start`. The unit sets `WorkingDirectory=/mnt/hdd/ares-workspace/paap`, which is required: `handleIndex` resolves `web/out` relative to CWD, so a wrong CWD serves a 404 dashboard.

## Layout

```
cmd/server/
  main.go          entry point, mux routing, static file serving, SPA fallback
  routing.go       /v1/chat/completions proxy, group routing, auth middleware, key selection
  providers.go     provider / key / model CRUD
  groups.go        model groups + prompt injection
  gateway.go       gateway keys + system settings (+ 5s settings cache)
  logs.go          request logging + usage stats + /v1/models discovery
  proxy.go         proxy pool CRUD
  proxy_tester.go  background proxy latency/country tester
  anigravity.go    Anigravity provider (uses streamingHTTPClient, no timeout)
  anthropic.go     /v1/messages handler + Anthropic→OpenAI translation integration
  streaming.go     SSE passthrough + token extraction
  compression.go   compression middleware
internal/
  translator/
    translator.go      Anthropic↔OpenAI request/response conversion
    streaming.go        OpenAI SSE → Anthropic SSE stream translator
    translator_test.go  unit tests
  db/
    db.go            schema + migrations + seed
web/src/
  app/             Next.js routes
  lib/api.ts       ALL frontend HTTP calls live here
  lib/provider-logos.ts  provider logo mapping (builtin_id → asset path)
  components/      UI
.agent/            project context (memory.md, status.md, notes.md, roadmap.md, prd/)
graphify-out/      knowledge graph
```

`.agent/memory.md` holds design decisions and hard-won pitfalls. `.agent/status.md` holds current counts and the blocked list. Read both before non-trivial work.

## Request flow

1. Client hits `/v1/chat/completions` (OpenAI) or `/v1/messages` (Anthropic) with a gateway key
2. Auth middleware validates the gateway key (Bearer token for OpenAI, x-api-key for Anthropic)
3. Model string `provider/model` resolves to a provider, or a group name resolves to a model set
4. Model routing: `claude-` prefix is stripped (Claude Code compatibility), then match by provider ID, builtin_id, or slugified name
5. Compression pipeline runs on the request — three independent stages:
   - **instruction injection** (`compression_mode`, e.g. `caveman:ultra,ponytail:ultra`) prepends terseness rules to the system message, text loaded from `config/<mode>.md`
   - **RTK** (`rtk_enabled` + `rtk_level`) shells out to the `rtk` binary to shrink tool results
   - **caveman regex** (`caveman_compress_enabled` + `compression_level`) squeezes tool/assistant prose
6. **Translation** (Anthropic path): if `supports_anthropic=0`, translator converts Anthropic request → OpenAI format before forwarding. Response is converted back to Anthropic format. All providers default to translator path.
7. Provider dispatch: forward to upstream `/chat/completions` (OpenAI format)
8. Key selection: round-robin, or race N active keys and take the first response. Keys with `fail_count >= 3` are auto-disabled.
9. Optional proxy: if `proxy_enabled`, pick the fastest active proxy from the pool
10. Response: passthrough for OpenAI path, translated for Anthropic path

## Translator

`internal/translator/` converts between Anthropic Messages API and OpenAI Chat Completions API.

- **Request**: messages (text, image, tool_use, tool_result), tools (input_schema → parameters), tool_choice, system prompt (top-level → system message)
- **Response**: content blocks, tool_calls, finish_reason mapping (stop→end_turn, length→max_tokens, tool_calls→tool_use)
- **Streaming**: stateful `StreamTranslator` converts OpenAI SSE chunks to Anthropic SSE events (message_start, content_block_delta, message_delta, message_stop)
- **Tool names**: auto-truncated to 64 chars (provider limit)
- All providers use `supports_anthropic=0` (translator path) by default

## Compression

`rtk` is a **CLI tool** (Rust binary, `~/.local/bin/rtk`), not an instruction mode. Never put `rtk` in `compression_mode` — there is no `config/rtk.md`, so `GetCompressionPrompt("rtk", …)` returns `""` with no warning. It is controlled solely by `rtk_enabled` / `rtk_level`.

`config/caveman.md` and `config/ponytail.md` are parsed by `compression.go`, which splits on `## Levels` and `## Shared Rules` H2 headers and reads `###` subsections beneath them. They resemble Hermes skill files but are **not interchangeable** — overwriting one with a `SKILL.md` drops those headers and silently disables that mode's injection. After editing either file, verify:

```bash
grep -E '^## ' config/caveman.md    # must show '## Levels' and '## Shared Rules'
# then check the log line:  [PAAP] Compression mode=… text_len=2847
```

`detectFilter()` in `rtk.go` must return a name that exists in rtk's filter set (`rtk pipe --filter bogus` prints the list). An invalid name makes rtk exit non-zero and the code fails open — compression silently stops. Returning `""` means "no filter fits", and the caller skips compression, because bare `rtk pipe` with no `--filter` is a passthrough that costs a subprocess and saves nothing.

Error tool results (`is_error: true` / `status: "error"`) are never compressed — the model needs traces verbatim.

## Built-in Providers

Providers with `provider_type='builtin'` are seeded in `db.go`. Each has a `builtin_id` (used for logo lookup) and `icon` field (filename in `web/public/assets/`).

Current builtins: xiaomi, meta, google, kimchi, openrouter, grok-cli, anigravity, ollamacloud, runapi, stepfun, hcnsec.

Logo mapping: `web/src/lib/provider-logos.ts` — maps `builtin_id` to asset path. To add a new builtin provider logo, add the file to `web/public/assets/` and add a mapping entry.

## Conventions

- Every mutating frontend call goes through `fetchApi()` in `web/src/lib/api.ts`, never raw `fetch()`. `fetchApi` throws on non-2xx; raw `fetch` swallows failures and produces buttons that silently do nothing.
- HTTP methods must match the Go handler exactly. Handlers use explicit `switch r.Method` with a `405` default — a mismatched method fails silently on the frontend.
- Provider and group IDs are hex hash strings, not integers. Type them `string` (or `number | string`).
- Settings writes go through `setSetting()`, which invalidates the 5s settings cache. Raw `db.DB.Exec` on `system_settings` leaves the routing layer reading stale values.
- UI colors use semantic tokens (`bg-primary`, `text-foreground`). Hardcoded hex breaks dark mode. Live Orbit is the one sanctioned exception.
- All agent-generated docs go in `.agent/`, never the repo root. `AGENTS.md` is the exception.

## Pitfalls

- **`parseBody(r, v)` closes `r.Body`.** A handler that peeks at the body to route (bulk-vs-single, discriminated unions) must `io.ReadAll` first and restore with `r.Body = io.NopCloser(bytes.NewReader(raw))` before delegating. Otherwise the downstream handler sees an empty closed body and returns `invalid json` 400 on a valid request.
- **Never let an HTTP handler recurse into itself** to return the updated resource. `r.Method` is still the write method, so it re-enters the write branch with a consumed body. Factor the read path into its own function.
- **`useSearchParams` needs a `<Suspense>` boundary** under static export.
- **Do not add a rebuild-triggering UI action.** A build takes 20-40s, needs `node_modules` (absent for binary installs), and restarting the server kills in-flight streams.
- **`db.go:287` seeds `claude-*` groups only when none exist.** Deleting one is durable; deleting all of them re-seeds every one on next boot.
- **`DELETE /api/logs` really does clear all logs and cost history.** Do not probe it to check routing.
- **`config/caveman.md` / `config/ponytail.md` are NOT Hermes skill files.** They are parsed for `## Levels` / `## Shared Rules` headers. Overwriting one with a `SKILL.md` silently disables that compression mode — no error, the only symptom is a lower `text_len=` in the log.
- **An invalid rtk filter name fails open.** `rtk` exits non-zero, PAAP logs one line and returns uncompressed content. Compression appears to work while doing nothing.
- **`resp2.Body` must be closed in fallback loops.** Both `routing.go` and `anthropic.go` had resource leaks where streaming success paths didn't close the response body.
- **`autoDisableKey` disables on 500+ when `fail_count >= 3`.** Keys that keep getting server errors get disabled, not just auth failures.

## Cleanup queue

- Qoder is removed from the product but 950 lines remain: `qoder.go`, `qoder_cosy.go`, `qoder_oauth.go`, plus the dispatch at `routing.go:409-413`. Safe to delete — no DB rows reference it.
- **Port RTK filters to native Go** — drop `exec.Command` entirely. Reference implementation: 9router `open-sse/rtk/` at `~/Downloads/9router-master/`. Gains: no subprocess per tool output, no `rtk` binary dependency, and support for the message shapes PAAP currently misses — Claude `tool_result` (string + array), OpenAI Responses `function_call_output`, Kiro `toolResults`, and `{role:"tool", content:[{type:"text"}]}. Today only `{role:"tool", content:string}` is handled, so `/v1/messages` requests get zero RTK. ~800-1000 lines; delegate to OpenCode.
- `ClientUsesRTK()` becomes unnecessary once the native port lands — compressed output no longer matches raw-format patterns, so autodetect rejects it and idempotence is structural. Today's version is coarse: a single `"rtk ` match disables compression for every tool output in the request.
- `handleDeleteOffline` (`web/src/app/proxy/page.tsx:69`) awaits deletes serially with no error handling.
- 7 raw `fetch()` calls left in `api.ts`: `addConnection`, `deleteConnection`, `updateModels`, `clearLogs`, `deleteGroup`, `shutdown`, `restart`. Methods are correct; failures are silent.

## References

- `.agent/memory.md` — design decisions, Live Orbit spec, backend pitfalls
- `.agent/status.md` — current counts, file sizes, blocked list
- `.agent/notes.md` — session history and rationale for past decisions
- `.agent/roadmap.md` — build breakdown
- `.agent/prd/` — product requirement docs
