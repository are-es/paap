# paap

> Production project built with high precision and automated reasoning architecture.

## Overview

| Metric | Value |
|---|---|
| Files tracked | 131 |
| Code files | 106 |
| Total lines | 48,034 |
| Languages | `.go`, `.tsx`, `.ts`, `.mjs` |
| Entry point | `cmd/server/main.go` |

## Tech Stack
- **Backend**: Go `1.25.0` — module `github.com/dolvin/paap`

## Getting Started

```bash
git clone <repository-url>
cd paap
go build ./...
go test ./...
```

## Project Structure
```text
paap/
├── cmd/
│   └── server/
│       ├── compression/
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
│   │   ├── components/
│   │   ├── i18n/
│   │   └── lib/
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

## Contributing
- Architecture reference for AI agents and new contributors: [`AGENTS.md`](./AGENTS.md)
- Internal specs and state live in `.ares/` (git-ignored)

## License
See [LICENSE](./LICENSE).
