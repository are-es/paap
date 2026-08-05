#!/bin/bash
# PAAP Full Build Script
# Usage: bash scripts/build.sh

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

echo "=== PAAP Build ==="

# 1. Generate static params from DB
echo "[1/3] Generating static params..."
bash web/scripts/gen-static-params.sh

# 2. Build Next.js
echo "[2/3] Building Next.js..."
cd web && npm run build && cd ..

# 3. Build Go binary
echo "[3/3] Building Go binary..."
go build -o bin/paap-server ./cmd/server

echo ""
echo "=== Build Complete ==="
echo "Run: ./bin/paap-server"
