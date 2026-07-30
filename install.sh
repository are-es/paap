#!/bin/bash
set -e

# PAAP — Pangkalan API Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/are-es/paap/master/install.sh | sh

REPO="https://github.com/are-es/paap.git"
INSTALL_DIR="$HOME/.paap"
PORT="${PAAP_PORT:-9090}"

echo "╔══════════════════════════════════════╗"
echo "║  PAAP — Pangkalan API Installer      ║"
echo "╚══════════════════════════════════════╝"

# Check dependencies (auto-skip if exists)
check_dep() {
  if command -v "$1" &> /dev/null; then
    echo "✅ $1 found — skip"
    return 0
  fi
  echo "❌ $1 not found. Install it first."
  exit 1
}

echo ""
echo "Checking dependencies..."
check_dep go
check_dep node
check_dep npm
check_dep git

# Clone or update
if [ -d "$INSTALL_DIR/paap/.git" ]; then
  echo ""
  echo "📁 PAAP already exists. Updating..."
  cd "$INSTALL_DIR/paap"
  git pull
else
  echo ""
  echo "📥 Cloning PAAP..."
  mkdir -p "$INSTALL_DIR"
  git clone "$REPO" "$INSTALL_DIR/paap"
  cd "$INSTALL_DIR/paap"
fi

# Build backend (skip if binary exists and recent)
echo ""
if [ -f "bin/paap-server" ]; then
  BINARY_AGE=$(( $(date +%s) - $(stat -c %Y bin/paap-server 2>/dev/null || echo 0) ))
  if [ $BINARY_AGE -lt 3600 ]; then
    echo "✅ Backend binary recent ($(($BINARY_AGE / 60))m old) — skip build"
  else
    echo "🔨 Building backend..."
    CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server/
    echo "✅ Backend built"
  fi
else
  echo "🔨 Building backend..."
  CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server/
  echo "✅ Backend built"
fi

# Build frontend (skip if out/ exists and recent)
echo ""
if [ -d "web/out" ] && [ -f "web/out/index.html" ]; then
  FRONTEND_AGE=$(( $(date +%s) - $(stat -c %Y web/out/index.html 2>/dev/null || echo 0) ))
  if [ $FRONTEND_AGE -lt 3600 ]; then
    echo "✅ Frontend build recent ($(($FRONTEND_AGE / 60))m old) — skip build"
  else
    echo "🔨 Building frontend..."
    cd web && npm run build && cd ..
    echo "✅ Frontend built"
  fi
else
  echo "🔨 Building frontend..."
  cd web && npm install --production=false && npm run build && cd ..
  echo "✅ Frontend built"
fi

# Create data directory (skip if exists)
echo ""
if [ -d "$INSTALL_DIR/config" ]; then
  echo "✅ Data directory exists — skip"
else
  echo "📁 Setting up data directory..."
  mkdir -p "$INSTALL_DIR"
  mkdir -p "$INSTALL_DIR/config"
  cp -n config/caveman.md "$INSTALL_DIR/config/" 2>/dev/null || true
  cp -n config/ponytail.md "$INSTALL_DIR/config/" 2>/dev/null || true
  echo "✅ Data directory ready"
fi

# Create systemd service (skip if exists and unchanged)
echo ""
if [ -f "/etc/systemd/system/paap.service" ]; then
  echo "✅ Systemd service exists — skip"
else
  echo "⚙️  Creating systemd service..."
  sudo tee /etc/systemd/system/paap.service > /dev/null << EOF
[Unit]
Description=PAAP - Pangkalan API Gateway
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=$INSTALL_DIR/paap
ExecStart=$INSTALL_DIR/paap/bin/paap-server
Environment=PAAP_PORT=$PORT
Environment=PAAP_DATA=$INSTALL_DIR
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  sudo systemctl daemon-reload
  sudo systemctl enable paap
  echo "✅ Systemd service created"
fi

# Install CLI (skip if exists)
echo ""
if [ -f "/usr/local/bin/paap" ]; then
  echo "✅ CLI already installed — skip"
else
  echo "⚙️  Installing CLI..."
  chmod +x "$INSTALL_DIR/paap/scripts/paap"
  sudo ln -sf "$INSTALL_DIR/paap/scripts/paap" /usr/local/bin/paap
  echo "✅ CLI installed"
fi

# Start/restart service
echo ""
if systemctl is-active --quiet paap 2>/dev/null; then
  echo "♻️  Restarting PAAP..."
  sudo systemctl restart paap
else
  echo "🚀 Starting PAAP..."
  sudo systemctl start paap
fi

sleep 2

echo ""
echo "╔══════════════════════════════════════╗"
echo "║  ✅ PAAP Installed Successfully!      ║"
echo "╚══════════════════════════════════════╝"
echo ""
echo "   URL:      http://localhost:$PORT"
echo "   Health:   curl http://localhost:$PORT/api/health"
echo "   Data:     $INSTALL_DIR"
echo ""
echo "   CLI Commands:"
echo "     paap start        Start server"
echo "     paap stop         Stop server"
echo "     paap status       Show status"
echo "     paap update       Pull + rebuild + restart"
echo "     paap version      Show version"
echo "     paap help         All commands"
echo ""
