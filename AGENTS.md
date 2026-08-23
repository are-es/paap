# AGENTS.md — paap

*Machine-generated architecture handbook for AI agents. Regenerate with `cds_update` after any structural change.*

## 0. Read This First — Codemaps-First Navigation

**Do NOT scan this codebase to find where something lives.** The codemaps already answer that:

| Artifact | Role | When to read |
|---|---|---|
| `.ares/codemaps/index.json` | Slim overview: counts, entry points, domain rollup, top hubs | **Always first** |
| `.ares/codemaps/detail.json` | Full per-file map: purpose, exports, functions, types, routes, components, deps | Grep by symbol / route / component to pinpoint the file to edit |
| `.ares/codemaps/modules.md` | Human-readable, grouped by domain | Review and PR context |
| `.ares/codemaps/graph.html` | Interactive dependency graph | Visual architecture inspection |

Open raw source **only after** codemaps narrow the target to specific files. If `generatedAt` predates recent edits, run `cds_update` before trusting the map.

**Ingestion order when updating this project**: `.ares/codemaps/index.json` → `.ares/memory.md` → `.ares/codemaps/detail.json` → `.ares/roadmap.md` / `.ares/arsitektur.md` → target source files.

## 1. Project Overview
- **Project Name**: paap
- **Primary Entrypoint**: `cmd/server/main.go`
- **Codemap Generated At**: 2026-08-23T01:36:53.537Z
- **Subsystem Standard**:
  - `.ares/` — internal specs, memory state, codemaps (git-ignored)
  - `.trash/` — pre-modification safety backups (git-ignored)

### Codemap Summary
- **Total Files Mapped**: 131
- **Code Files**: 106
- **Config Files**: 10
- **Docs**: 6
- **Scripts**: 5
- **Total Lines**: 48,034
- **Internal Dependency Edges**: 55
- **Languages**: `.go`, `.tsx`, `.ts`, `.mjs`

### Entry Points
- `cmd/server/main.go`

## 2. Tech Stack
- **Backend**: Go `1.25.0` — module `github.com/dolvin/paap`

## 3. Domain Boundaries
| Domain | Files | Representative Directories |
|---|---|---|
| **CORE** | 34 | `cmd/server`, `web/src/components/providers`, `web/src/lib`, `internal/translator`, `web`, `web/src/app/groups/[id]` |
| **AI** | 22 | `cmd/server`, `cmd/server/compression`, `web/src/app/compression`, `web/src/app/tools/mcp`, `web/src/app/tools/vision` |
| **UI** | 22 | `web/src/app/groups/detail`, `web/src/app/groups`, `web/src/app`, `web/src/app/logs`, `web/src/app/providers`, `web/src/app/providers/setup` |
| **TEST** | 20 | `cmd/server`, `cmd/server/compression`, `internal/db`, `internal/tokens`, `internal/translator` |
| **CONFIG** | 10 | `web`, `web/src/i18n` |
| **API** | 8 | `cmd/server`, `internal/translator`, `web/src/app/proxy`, `web/src/lib` |
| **DOCS** | 6 | `.`, `web` |
| **BUILD** | 5 | `.`, `scripts`, `web/scripts` |
| **AUTH** | 2 | `cmd/server`, `internal/tokens` |
| **BILLING** | 1 | `cmd/server` |
| **DB** | 1 | `internal/db` |

## 4. Dependency Hubs — Widest Blast Radius
Changing these affects the most dependents. Verify every dependent before shipping.

| Module | Dependents |
|---|---|
| `internal/db/db.go` | 27 |
| `web/src/lib/provider-logos.ts` | 12 |
| `internal/tokens/estimate.go` | 4 |
| `cmd/server/compression/bm25.go` | 3 |
| `web/src/lib/language-context.tsx` | 3 |
| `internal/translator/streaming.go` | 2 |
| `web/src/app/groups/[id]/client.tsx` | 2 |
| `web/src/app/globals.css` | 1 |
| `web/src/lib/api.ts` | 1 |

## 5. HTTP Routes
| File | Routes |
|---|---|
| `cmd/server/main.go` | `GET /api/providers`, `POST /api/providers`, `GET /api/keys`, `POST /api/keys`, `GET /api/groups`, `POST /api/groups`, `GET /api/logs`, `DELETE /api/logs`, `GET /api/proxies`, `POST /api/proxies`, `POST /api/proxies/bulk`, `GET /api/proxy-groups`, `POST /api/proxy-groups`, `GET /api/gateway/keys`, `POST /api/gateway/keys`, `GET /api/tools`, `POST /api/tools`, `ANY /api/health`, `ANY /api/providers/favicon`, `ANY /api/providers/`, `ANY /api/keys/`, `ANY /api/groups/`, `ANY /api/logs/cost`, `ANY /api/logs/export`, `ANY /api/logs/reconcile`, `ANY /api/logs/stream`, `ANY /api/proxies/test-all`, `ANY /api/models`, `ANY /api/proxies/`, `ANY /api/proxy-groups/`, `ANY /api/gateway/keys/`, `ANY /api/settings`, `ANY /api/compression/logs`, `ANY /api/compression/summary`, `ANY /api/compression/logs/clear`, `ANY /api/system/shutdown`, `ANY /api/system/restart`, `ANY /api/backup`, `ANY /api/restore`, `ANY /api/clear-all`, `ANY /api/usage/summary`, `ANY /api/tools/`, `ANY /api/merlin/auth`, `ANY /api/merlin/capture`, `ANY /api/oauth/`, `ANY /mcp/message`, `ANY /mcp/status`, `ANY /v1/chat/completions`, `ANY /v1/models`, `ANY /v1/messages`, `ANY /v1/`, `ANY /assets/`, `ANY /` |

## 6. Directory Tree
```text
paap/
├── cmd/
│   └── server/
│       ├── compression/
│       │   ├── bm25.go
│       │   ├── cache_stability.go
│       │   ├── caveman_pipeline_test.go
│       │   ├── caveman_pipeline.go
│       │   ├── compressor_test.go
│       │   ├── compressor.go
│       │   ├── content_detector.go
│       │   ├── headroom.go
│       │   ├── levels.go
│       │   ├── smart_crusher.go
│       │   ├── strategies_high.go
│       │   ├── strategies_log.go
│       │   ├── strategies_text.go
│       │   └── strip.go
│       ├── anigravity_usage_test.go
│       ├── anigravity.go
│       ├── anthropic_compress_test.go
│       ├── anthropic_stream_test.go
│       ├── anthropic.go
│       ├── backup.go
│       ├── caveman_compress.go
│       ├── codex_test.go
│       ├── codex.go
│       ├── compression_logs.go
│       ├── connections.go
│       ├── datadir_test.go
│       ├── gateway.go
│       ├── groups.go
│       ├── keys.go
│       ├── logs.go
│       ├── logstream_test.go
│       ├── logstream.go
│       ├── main.go
│       ├── mcp_adapters.go
│       ├── mcp_tools.go
│       ├── mcp.go
│       ├── merlin_usage_test.go
│       ├── merlin.go
│       ├── oauth_test.go
│       ├── oauth.go
│       ├── pricing_test.go
│       ├── pricing.go
│       ├── providers.go
│       ├── proxy.go
│       ├── reconcile_test.go
│       ├── reconcile.go
│       ├── reqdump_test.go
│       ├── reqdump.go
│       ├── reqlog.go
│       ├── router_split_test.go
│       ├── routing.go
│       ├── rtk_detect_test.go
│       ├── rtk.go
│       ├── streaming.go
│       ├── t03_verify_test.go
│       ├── tools.go
│       ├── traffic_logger.go
│       ├── usage_extract_test.go
│       └── vision.go
├── data/
│   ├── paap.db
│   ├── paap.db-shm
│   └── paap.db-wal
├── internal/
│   ├── db/
│   │   ├── db.go
│   │   └── migration_test.go
│   ├── tokens/
│   │   ├── estimate_test.go
│   │   └── estimate.go
│   └── translator/
│       ├── streaming.go
│       ├── translator_test.go
│       └── translator.go
├── logs/
├── previews/
│   ├── provider-topology-5layouts.html
│   ├── topology-layout-preview.html
│   └── topology-v2-preview.html
├── scripts/
│   ├── build.sh
│   └── paap
├── web/
│   ├── public/
│   │   ├── assets/
│   │   │   ├── fonts/
│   │   │   │   ├── BricolageGrotesque-Bold.ttf
│   │   │   │   └── Syne-Bold.ttf
│   │   │   ├── icons/
│   │   │   │   ├── activity.svg
│   │   │   │   ├── add.svg
│   │   │   │   ├── alert-triangle.svg
│   │   │   │   ├── check.svg
│   │   │   │   ├── chevron-down.svg
│   │   │   │   ├── chevron-right.svg
│   │   │   │   ├── circle.svg
│   │   │   │   ├── close.svg
│   │   │   │   ├── copy.svg
│   │   │   │   ├── delete.svg
│   │   │   │   ├── external-link.svg
│   │   │   │   ├── eye-off.svg
│   │   │   │   ├── eye.svg
│   │   │   │   ├── globe.svg
│   │   │   │   ├── info.svg
│   │   │   │   ├── key.svg
│   │   │   │   ├── layers.svg
│   │   │   │   ├── layout-dashboard.svg
│   │   │   │   ├── minimize-2.svg
│   │   │   │   ├── moon.svg
│   │   │   │   ├── network.svg
│   │   │   │   ├── play.svg
│   │   │   │   ├── plus.svg
│   │   │   │   ├── refresh-cw.svg
│   │   │   │   ├── refresh.svg
│   │   │   │   ├── scroll-text.svg
│   │   │   │   ├── server.svg
│   │   │   │   ├── settings.svg
│   │   │   │   ├── shield.svg
│   │   │   │   ├── sun.svg
│   │   │   │   ├── terminal.svg
│   │   │   │   ├── test.svg
│   │   │   │   ├── trash-2.svg
│   │   │   │   ├── user.svg
│   │   │   │   ├── x.svg
│   │   │   │   └── zap.svg
│   │   │   ├── agentrouter.svg
│   │   │   ├── antigravtiy.ico
│   │   │   ├── cloudflare.svg
│   │   │   ├── deepseek.ico
│   │   │   ├── fatherlesai.svg
│   │   │   ├── favicon.svg
│   │   │   ├── grok.ico
│   │   │   ├── grok.png
│   │   │   ├── grup.png
│   │   │   ├── hcnsec.png
│   │   │   ├── kimchi.png
│   │   │   ├── logo_ai_studio_color_1x_web_512dp.png
│   │   │   ├── logo.svg
│   │   │   ├── meta.svg
│   │   │   ├── ollama.avif
│   │   │   ├── openai.svg
│   │   │   ├── openrouter.png
│   │   │   ├── runapi.png
│   │   │   ├── runapi.svg
│   │   │   ├── setings.png
│   │   │   ├── show-hide.png
│   │   │   ├── stepfun.svg
│   │   │   ├── test-prompt.png
│   │   │   ├── tokenharbor.png
│   │   │   └── xiaomi-mimo.png
│   │   ├── file.svg
│   │   ├── globe.svg
│   │   ├── next.svg
│   │   ├── vercel.svg
│   │   └── window.svg
│   ├── scripts/
│   │   ├── dev.sh
│   │   ├── gen-static-params.sh
│   │   └── setup-vault.sh
│   ├── src/
│   │   ├── app/
│   │   │   ├── compression/
│   │   │   │   └── page.tsx
│   │   │   ├── groups/
│   │   │   │   ├── [id]/
│   │   │   │   ├── detail/
│   │   │   │   └── page.tsx
│   │   │   ├── logs/
│   │   │   │   └── page.tsx
│   │   │   ├── providers/
│   │   │   │   ├── setup/
│   │   │   │   └── page.tsx
│   │   │   ├── proxy/
│   │   │   │   └── page.tsx
│   │   │   ├── settings/
│   │   │   │   └── page.tsx
│   │   │   ├── tools/
│   │   │   │   ├── mcp/
│   │   │   │   ├── vision/
│   │   │   │   └── page.tsx
│   │   │   ├── favicon.ico
│   │   │   ├── globals.css
│   │   │   ├── layout.tsx
│   │   │   └── page.tsx
│   │   ├── components/
│   │   │   ├── dashboard/
│   │   │   │   ├── config-card.tsx
│   │   │   │   ├── stats-bar.tsx
│   │   │   │   └── topology.tsx
│   │   │   ├── providers/
│   │   │   │   ├── add-provider-modal.tsx
│   │   │   │   └── provider-helpers.tsx
│   │   │   ├── ui/
│   │   │   │   ├── button.tsx
│   │   │   │   ├── collapse.tsx
│   │   │   │   ├── confirm-modal.tsx
│   │   │   │   ├── docs-modal.tsx
│   │   │   │   ├── modal.tsx
│   │   │   │   └── toggle.tsx
│   │   │   ├── providers.tsx
│   │   │   ├── sidebar.tsx
│   │   │   └── theme-provider.tsx
│   │   ├── i18n/
│   │   │   ├── ar.json
│   │   │   ├── en.json
│   │   │   ├── id.json
│   │   │   ├── ja.json
│   │   │   ├── ko.json
│   │   │   └── zh.json
│   │   └── lib/
│   │       ├── api.ts
│   │       ├── language-context.tsx
│   │       ├── provider-logos.ts
│   │       ├── providers.ts
│   │       ├── use-log-stream.ts
│   │       └── utils.ts
│   ├── AGENTS.md
│   ├── CLAUDE.md
│   ├── components.json
│   ├── eslint.config.mjs
│   ├── next-env.d.ts
│   ├── next.config.ts
│   ├── package-lock.json
│   ├── package.json
│   ├── postcss.config.mjs
│   ├── README.md
│   ├── tsconfig.json
│   └── tsconfig.tsbuildinfo
├── AGENTS.md
├── ARES.md
├── go.mod
├── go.sum
├── install.sh
├── LICENSE
├── README.md
└── server
```

## 7. File-by-File Functional Breakdown

#### CORE — 34 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `cmd/server/main.go` | Shared HTTP client with connection pooling + keepalive | - | 459 |
| `web/src/components/providers/provider-helpers.tsx` | Source module: provider-helpers in web/src/components/providers | `ProviderIcon`, `AuthTypeBadge`, `ProviderTypeBadge`, `StatusPill` | 72 |
| `web/src/lib/utils.ts` | Source module: utils in web/src/lib | `cn` | 7 |
| `cmd/server/anigravity.go` | extractReasoningEffort extracts reasoning effort level (lowmediumhighmax) from various client parameter formats | - | 1173 |
| `cmd/server/backup.go` | ── Backup & Restore ───────────────────────────────────────── | - | 145 |
| `cmd/server/codex.go` | codexDefaultModels is the curated fallback list when live discovery fails. | - | 884 |
| `cmd/server/connections.go` | ── Connection Management ────────────────────────────────── | - | 167 |
| `cmd/server/groups.go` | ── Roundrobin counter for group model rotation ──────────── | - | 1022 |
| `cmd/server/keys.go` | Source module: keys in cmd/server | - | 45 |
| `cmd/server/logs.go` | ── Subrouter for apilogs ────────────────────────────── | - | 847 |
| `cmd/server/merlin.go` | ── Merlin Auth ───────────────────────────────────────── | - | 589 |
| `cmd/server/providers.go` | ── Proxy helpers for provider ─────────────────────────── | - | 2321 |
| `cmd/server/reconcile.go` | ── Historical cost reconciliation ─────────────────────────── | - | 258 |
| `cmd/server/reqdump.go` | ── Request dump handle ──────────────────────────────────────────────────── | `RequestDump`, `BeginRequestDump` | 376 |
| `cmd/server/reqlog.go` | ── Singlefile request log with smart rolling ───────────────────────────── | `LogRequestDump` | 249 |
| `cmd/server/routing.go` | ── Roundrobin counters for each provider ───────────────── | - | 2134 |
| `cmd/server/rtk.go` | RTK tuning constants. Values mirror rtk's own defaults so PAAPside | `IsRTKAvailable`, `CompressToolOutput`, `ClientUsesRTK`, `CompressToolOutputs` | 354 |
| `cmd/server/tools.go` | ── Tool Types ────────────────────────────────────────── | `Tool`, `ToolRow`, `ToolMatch`, `LoadTools` | 366 |
| `cmd/server/traffic_logger.go` | TrafficLog logs a full requestresponse cycle with compression info | `TrafficLog`, `TrafficEntry` | 114 |
| `internal/translator/translator.go` | Package translator converts between AI provider API formats. | `CleanToolIDForAnthropic`, `Format`, `DetectFormat`, `DetectFormatFromHeaders` | 829 |
| `web/eslint.config.mjs` | Source module: eslint.config in web | `default` | 19 |
| `web/next-env.d.ts` | <reference types="next" > | - | 7 |
| `web/next.config.ts` | Source module: next.config in web | `default` | 19 |
| `web/postcss.config.mjs` | Source module: postcss.config in web | `default` | 8 |
| `web/src/app/groups/[id]/client.tsx` | Source module: client in web/src/app/groups/[id] | `GroupSetupClient` | 502 |
| `web/src/app/providers/setup/client.tsx` | parseKeyError extracts a humanreadable message from a stored upstream error body. | `ProviderSetupClient` | 962 |
| `web/src/components/dashboard/config-card.tsx` | Source module: config-card in web/src/components/dashboard | `ConfigCard` | 57 |
| `web/src/components/dashboard/stats-bar.tsx` | Source module: stats-bar in web/src/components/dashboard | `StatsBar` | 110 |
| `web/src/components/dashboard/topology.tsx` | ─── Provider Logo + Name Card ────────────────────────────── | `ProviderTopology` | 574 |
| `web/src/components/ui/collapse.tsx` | Source module: collapse in web/src/components/ui | `Collapse` | 44 |
| `web/src/components/ui/toggle.tsx` | Source module: toggle in web/src/components/ui | `ToggleProps` | 71 |
| `web/src/lib/language-context.tsx` | Source module: language-context in web/src/lib | `LANGUAGES`, `LangCode`, `LanguageProvider`, `useLanguage` | 78 |
| `web/src/lib/provider-logos.ts` | Provider logo mapping: maps builtin_id or name to actual asset path | `getProviderLogo`, `getProviderInitials` | 58 |
| `web/src/lib/providers.ts` | Reexport provider utilities for serverside static generation | - | 9 |

#### AI — 22 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `cmd/server/anthropic.go` | authMiddlewareAnthropic validates Anthropicstyle xapikey header | - | 910 |
| `cmd/server/caveman_compress.go` | CavemanCompressor applies caveman compression rules to text | `CavemanCompressor`, `CompressionRule`, `NewCavemanCompressor`, `EstimateSavings` | 186 |
| `cmd/server/compression/bm25.go` | BM25Extractive compresses text by scoring segments with BM25 and keeping top ones. | `BM25Extractive` | 204 |
| `cmd/server/compression/cache_stability.go` | Cache stability detection — inspired by Headroom's CacheAligner. | `IsVolatile`, `IsVolatileLight`, `ContainsTimestamps`, `ContainsUUIDs` | 118 |
| `cmd/server/compression/caveman_pipeline.go` | Package compression implements deterministic, tokensaving transforms | `ToolBudget`, `DefaultToolBudgets`, `FallbackBudget`, `GetToolBudget` | 83 |
| `cmd/server/compression/compressor.go` | ChatMessage mirrors the minimal proxy message structure. | `ChatMessage`, `ProcessedResult`, `CompressRawMessages`, `CompressInterfaceMessages` | 410 |
| `cmd/server/compression/content_detector.go` | ContentType represents the detected content type for Headroomstyle compression. | `ContentType`, `DetectContentType` | 149 |
| `cmd/server/compression/headroom.go` | HeadroomCompress applies Headroomstyle 2phase compression. | `HeadroomCompress` | 266 |
| `cmd/server/compression/levels.go` | Level defines the compression aggressiveness. | `Level`, `ParseLevel` | 123 |
| `cmd/server/compression/smart_crusher.go` | SmartCrusherLite — statistical JSON compression inspired by Headroom's SmartCrusher. | `SmartCrushJSON` | 396 |
| `cmd/server/compression/strategies_high.go` | ── Repeated ToolCall Pattern Collapse ────────────────────── | - | 292 |
| `cmd/server/compression/strategies_log.go` | DeduplicateLogLines removes consecutive repeated lines and appends a count. | `DeduplicateLogLines`, `TruncateHeadTail`, `HardCapLines`, `IsLogLike` | 116 |
| `cmd/server/compression/strategies_text.go` | proseRule is a compiled fillerremoval pattern with its replacement. | `ApplyProseFilter`, `IsStructuredOutput` | 282 |
| `cmd/server/compression/strip.go` | CollapseBlankLines replaces 3+ consecutive newlines with 2. | `CollapseBlankLines`, `StripANSI` | 21 |
| `cmd/server/compression_logs.go` | compressionLogEntry is a single compression event for the API response. | - | 235 |
| `cmd/server/mcp.go` | ── MCP Protocol Types ──────────────────────────────────────── | - | 220 |
| `cmd/server/mcp_adapters.go` | ── Provider Type Detection ───────────────────────────────── | - | 407 |
| `cmd/server/mcp_tools.go` | ── MCP Tool Helpers ────────────────────────────────────────── | `SearchResult` | 521 |
| `cmd/server/vision.go` | ── Vision Image Types ──────────────────────────────────── | - | 581 |
| `web/src/app/compression/page.tsx` | Source module: page in web/src/app/compression | `CompressionPage`, `default` | 241 |
| `web/src/app/tools/mcp/page.tsx` | Add Model Modal | `MCPToolsPage`, `default` | 333 |
| `web/src/app/tools/vision/page.tsx` | Add Model Modal  same pattern as Groups | `VisionSetupPage`, `default` | 320 |

#### UI — 22 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `web/src/app/groups/detail/page.tsx` | Source module: page in web/src/app/groups/detail | `GroupDetailPage`, `default` | 20 |
| `web/src/app/groups/page.tsx` | Source module: page in web/src/app/groups | `GroupsPage`, `default` | 220 |
| `web/src/app/layout.tsx` | Source module: layout in web/src/app | `metadata`, `RootLayout`, `default` | 65 |
| `web/src/app/logs/page.tsx` | Source module: page in web/src/app/logs | `LogsPage`, `default` | 471 |
| `web/src/app/page.tsx` | Source module: page in web/src/app | `DashboardPage`, `default` | 269 |
| `web/src/app/providers/page.tsx` | Source module: page in web/src/app/providers | `ProvidersPage`, `default` | 184 |
| `web/src/app/providers/setup/page.tsx` | Source module: page in web/src/app/providers/setup | `ProviderSetupPage`, `default` | 11 |
| `web/src/app/settings/page.tsx` | Source module: page in web/src/app/settings | `SettingsPage`, `default` | 204 |
| `web/src/app/tools/mcp/web-search/page.tsx` | Source module: page in web/src/app/tools/mcp/web-search | `WebSearchSetupPage`, `default` | 354 |
| `web/src/app/tools/page.tsx` | Source module: page in web/src/app/tools | `ToolsPage`, `default` | 97 |
| `web/src/components/providers/add-provider-modal.tsx` | Source module: add-provider-modal in web/src/components/providers | `AddProviderModal` | 209 |
| `web/src/components/providers.tsx` | Source module: providers in web/src/components | `Providers` | 20 |
| `web/src/components/sidebar.tsx` | Source module: sidebar in web/src/components | `Sidebar` | 143 |
| `web/src/components/theme-provider.tsx` | Source module: theme-provider in web/src/components | `ThemeProvider`, `useTheme` | 51 |
| `web/src/components/ui/button.tsx` | Source module: button in web/src/components/ui | `ButtonProps` | 78 |
| `web/src/components/ui/confirm-modal.tsx` | Source module: confirm-modal in web/src/components/ui | `ConfirmModal` | 65 |
| `web/src/components/ui/docs-modal.tsx` | Docs button  reusable | `DocsModal`, `DocsButton` | 113 |
| `web/src/components/ui/modal.tsx` | Source module: modal in web/src/components/ui | `Modal` | 60 |
| `previews/provider-topology-5layouts.html` | Template: provider-topology-5layouts | - | 423 |
| `previews/topology-layout-preview.html` | Template: topology-layout-preview | - | 490 |
| `previews/topology-v2-preview.html` | Template: topology-v2-preview | - | 366 |
| `web/src/app/globals.css` | Stylesheet: globals | - | 151 |

#### TEST — 20 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `cmd/server/router_split_test.go` | openaiSSEUpstream builds a mock OpenAIcompatible SSE upstream whose final | `TestOpenAIStreamSplitReachesDB`, `TestOpenAINonStreamSplitReachesDB`, `TestGoRouterZeroedAnthropicPairNotRegressed`, `TestRouterLogsCarrySplitForAllProviderPaths` | 292 |
| `cmd/server/anigravity_usage_test.go` | TestAnigravityGeminiTokenCounts covers the Gemini usage > tokenCounts mapping. | `TestAnigravityGeminiTokenCounts`, `TestAnigravityGeminiUsageMapThinking`, `TestAnigravityGeminiUsageMapOmitsEmptyDetails`, `TestAnigravityGeminiUsageReconciles` | 213 |
| `cmd/server/anthropic_compress_test.go` | bigToolText builds a tool result body well past anthropicMinCompressSize that | `TestResolveCompressionLevelUsesOnlyCompressLevel`, `TestCompressAnthropicToolResults`, `TestCompressAnthropicToolResults_Off`, `TestCompressAnthropicToolResults_ArrayShape` | 192 |
| `cmd/server/anthropic_stream_test.go` | countLogRows returns the number of rows currently in the logs table. | `TestAnthropicStreamSingleLogRow`, `TestAnthropicStreamCacheTokensRecorded`, `TestAnthropicStreamSubscriptionProviderZeroCost` | 195 |
| `cmd/server/codex_test.go` | ── JWT Account ID Extraction ────────────────────────────── | `TestExtractChatGPTAccountID_ValidJWT`, `TestExtractChatGPTAccountID_NoAuthClaim`, `TestExtractChatGPTAccountID_MalformedToken`, `TestExtractChatGPTAccountID_EmptyAccountID` | 1333 |
| `cmd/server/compression/caveman_pipeline_test.go` | Source module: caveman_pipeline_test in cmd/server/compression | `TestStripAnsi`, `TestCollapseBlanks`, `TestFlintChipper` | 55 |
| `cmd/server/compression/compressor_test.go` | Source module: compressor_test in cmd/server/compression | `TestCompressLiteConsecutiveDedup`, `TestCompressLiteSizeGuard`, `TestCompressSizeGuardRetainsOriginal`, `TestCompressShrinksWhenPossible` | 113 |
| `cmd/server/datadir_test.go` | TestDataDirPath locks the contract every write path depends on: one data | `TestDataDirPath`, `TestInternalAPIBaseURL` | 46 |
| `cmd/server/logstream_test.go` | drainHub removes any subscribers left behind by a failed test so cases do not | `TestPublishDoesNotBlockOnFullBuffer`, `TestSubscriberCapEnforced`, `TestUnsubscribeIsIdempotent`, `TestPublishConcurrentWithSubscribeChurn` | 341 |
| `cmd/server/merlin_usage_test.go` | merlinSSEBody builds a Merlinstyle SSE payload carrying text and reasoning | `TestMerlinEstimatedInput`, `TestMerlinStreamingEstimatesTokens`, `TestMerlinNonStreamingEstimatesTokens`, `TestMerlinLogRowMarkedEstimated` | 170 |
| `cmd/server/oauth_test.go` | Source module: oauth_test in cmd/server | `TestIsCodexOAuthProviderID`, `TestNewCodexTokenExchangeRequestMatchesCodexClientContract`, `TestMarshalCodexOAuthDataEscapesExternalValues` | 61 |
| `cmd/server/pricing_test.go` | setupPricingDB spins up a real SQLite DB with a known pricing table so the | `TestResolvePricingDeterministic`, `TestNoFabricatedPricing`, `TestProviderScopedPricingWins`, `TestSubscriptionAndFreeProvidersCostZero` | 299 |
| `cmd/server/reconcile_test.go` | insertLegacyLog writes a row shaped like one written by the premigration | `TestReconcileZeroesFabricatedCost`, `TestReconcileRepricesKnownModel`, `TestReconcileSubscriptionProviderZeroed`, `TestReconcileIsIdempotent` | 217 |
| `cmd/server/reqdump_test.go` | ── Singlefile smart rolling tests ──────────────────────────────────────── | `TestRequestDumpFullPrompt`, `TestRequestDumpHistoryMetadataOnly`, `TestRequestDumpParamDiff`, `TestRequestDumpSnakeCamelNotSpurious` | 390 |
| `cmd/server/rtk_detect_test.go` | TestDetectFilter locks in the filterdetection table. Every name returned | `TestDetectFilter`, `TestIsErrorToolResult` | 101 |
| `cmd/server/t03_verify_test.go` | testSharedDBDir is the data dir backing the packagewide test database opened | `TestMain`, `TestProviderRoutes_ModelDetect` | 71 |
| `cmd/server/usage_extract_test.go` | TestExtractUsageOpenAICachedAndReasoning covers the OpenAI chatcompletions | `TestExtractUsageOpenAICachedAndReasoning`, `TestExtractUsageGoRouterZeroedAnthropicPair`, `TestExtractUsageAnthropicNamesAreFallback`, `TestExtractUsageClampsNegativeSplits` | 217 |
| `internal/db/migration_test.go` | legacyPricingSchema mirrors the premigration model_pricing shape that exists | `TestMigrateModelPricingKeyFromLegacy`, `TestMigrationAddsTokenSplitColumns` | 170 |
| `internal/tokens/estimate_test.go` | TestEstimateByteClasses checks the estimator responds to character class | `TestEstimateByteClasses`, `TestEstimateDenseBlob`, `TestEstimateProseNotTreatedAsDense`, `TestEstimateEdgeCases` | 150 |
| `internal/translator/translator_test.go` | ── CleanToolIDForAnthropic Tests ────────────────────────────────────────── | `TestAnthropicToOpenAIRequest_BasicText`, `TestAnthropicToOpenAIRequest_ToolUse`, `TestAnthropicToOpenAIRequest_Tools`, `TestOpenAIToAnthropicResponse_BasicText` | 799 |

#### CONFIG — 10 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `web/components.json` | Configuration file for components | - | 26 |
| `web/package-lock.json` | Configuration file for package-lock | - | 9743 |
| `web/package.json` | Configuration file for package | - | 35 |
| `web/src/i18n/ar.json` | Configuration file for ar | - | 123 |
| `web/src/i18n/en.json` | Configuration file for en | - | 204 |
| `web/src/i18n/id.json` | Configuration file for id | - | 204 |
| `web/src/i18n/ja.json` | Configuration file for ja | - | 123 |
| `web/src/i18n/ko.json` | Configuration file for ko | - | 123 |
| `web/src/i18n/zh.json` | Configuration file for zh | - | 123 |
| `web/tsconfig.json` | Configuration file for tsconfig | - | 44 |

#### API — 8 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `cmd/server/gateway.go` | ── System Settings ───────────────────────────────────── | `InvalidateSettingsCache` | 198 |
| `cmd/server/logstream.go` | ── Live log stream ────────────────────────────────────────── | - | 209 |
| `cmd/server/proxy.go` | ── Proxy Pools ───────────────────────────────────────────── | - | 668 |
| `cmd/server/streaming.go` | handleStreaming is the legacy call shape kept for callers that only need a | - | 289 |
| `internal/translator/streaming.go` | StreamTranslator converts OpenAI SSE streaming chunks to Anthropic SSE format. | `StreamTranslator`, `NewStreamTranslator`, `EstimateInputTokens` | 370 |
| `web/src/app/proxy/page.tsx` | Source module: page in web/src/app/proxy | `ProxyPage`, `default` | 354 |
| `web/src/lib/api.ts` | Named exports for serverside static generation | `Provider`, `ApiKeyItem`, `ConnectionItem`, `ModelItem` | 571 |
| `web/src/lib/use-log-stream.ts` | Source module: use-log-stream in web/src/lib | `LogStreamState`, `useLogStream` | 111 |

#### DOCS — 6 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `AGENTS.md` | AGENTS.md — paap | - | 522 |
| `ARES.md` | ARES.md — PAAP Session Guide | - | 235 |
| `README.md` | Documentation: README | - | 146 |
| `web/AGENTS.md` | This is NOT the Next.js you know | - | 6 |
| `web/CLAUDE.md` | Documentation: CLAUDE | - | 2 |
| `web/README.md` | Getting Started | - | 37 |

#### BUILD — 5 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `install.sh` | Script: install | - | 173 |
| `scripts/build.sh` | Script: build | - | 27 |
| `web/scripts/dev.sh` | Script: dev | - | 119 |
| `web/scripts/gen-static-params.sh` | Script: gen-static-params | - | 131 |
| `web/scripts/setup-vault.sh` | Script: setup-vault | - | 91 |

#### AUTH — 2 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `cmd/server/oauth.go` | grokcli OAuth constants | `RefreshOAuthKey`, `GetOAuthKeyValue`, `RefreshAnigravityToken` | 1077 |
| `internal/tokens/estimate.go` | Package tokens provides a single shared tokencount estimator. | `Estimate` | 124 |

#### BILLING — 1 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `cmd/server/pricing.go` | ── Token counters ─────────────────────────────────────────── | `InvalidatePricingCache`, `InvalidateBillingModeCache` | 410 |

#### DB — 1 files

| File | Purpose | Key Symbols | Lines |
|---|---|---|---|
| `internal/db/db.go` | getSettingRaw reads a system_settings value directly. Used by migrations that | `DB`, `Init`, `Close` | 584 |


## 8. Workflows
- **Build / Run**: inspect `package.json` scripts, `Makefile`, or the backend launcher.
- **Verification**: run tests and linters before claiming completion.
- **Codemap Sync**: run `cds_build` (codemaps only) or `cds_update` (codemaps + docs) after structural changes.

## 9. Safety & Portability Rules (MANDATORY)
1. **No hardcoded machine paths** in source or project config. Use relative paths, `process.cwd()`, or environment variables.
2. **Never commit** `.ares/`, `.trash/`, `.env*`, API keys, private tokens, or credentials. Keep them in `.gitignore`.
3. **Back up before overwrite**: copy the original into `.trash/` before editing an existing file.
4. **Never permanently delete** (`rm -rf`) without explicit user approval. Move to `.trash/` instead.
