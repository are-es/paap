#!/bin/bash
# PAAP Full Build Script
# Usage: bash /mnt/hdd/ares-workspace/paap/scripts/build.sh

set -e
cd /mnt/hdd/ares-workspace/paap

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
echo "Run: paap restart"
