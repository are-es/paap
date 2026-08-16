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

check_go_version() {
  local version major minor
  version=$(go env GOVERSION 2>/dev/null | sed -n 's/^go\([0-9]\+\)\.\([0-9]\+\).*/\1 \2/p')
  if [ -z "$version" ]; then
    echo "❌ Could not determine Go version. Go 1.25+ required."
    exit 1
  fi
  read -r major minor <<< "$version"
  if [ "$major" -lt 1 ] || { [ "$major" -eq 1 ] && [ "$minor" -lt 25 ]; }; then
    echo "❌ Go 1.25+ required; found $(go version)."
    exit 1
  fi
  echo "✅ Go $(go env GOVERSION) found"
}

echo ""
echo "Checking dependencies..."
check_dep go
check_go_version
check_dep node
check_dep npm
check_dep git
check_dep cc

# Clone or update
SOURCE_CHANGED=true
if [ -d "$INSTALL_DIR/paap/.git" ]; then
  echo ""
  echo "📁 PAAP already exists. Updating..."
  cd "$INSTALL_DIR/paap"
  OLD_COMMIT=$(git rev-parse HEAD)
  git pull
  NEW_COMMIT=$(git rev-parse HEAD)
  if [ "$OLD_COMMIT" = "$NEW_COMMIT" ]; then
    SOURCE_CHANGED=false
  fi
else
  echo ""
  echo "📥 Cloning PAAP..."
  mkdir -p "$INSTALL_DIR"
  git clone "$REPO" "$INSTALL_DIR/paap"
  cd "$INSTALL_DIR/paap"
fi

# Build backend when source changed or binary is missing.
echo ""
if [ "$SOURCE_CHANGED" = true ] || [ ! -f "bin/paap-server" ]; then
  echo "🔨 Building backend..."
  CGO_ENABLED=1 go build -o bin/paap-server ./cmd/server/
  echo "✅ Backend built"
else
  echo "✅ Backend matches current source — skip build"
fi

# Build frontend when source changed or export is missing.
echo ""
if [ "$SOURCE_CHANGED" = true ] || [ ! -f "web/out/index.html" ]; then
  echo "🔨 Building frontend..."
  cd web
  if [ ! -d "node_modules" ]; then
    npm install --production=false
  fi
  npm run build
  cd ..
  echo "✅ Frontend built"
else
  echo "✅ Frontend matches current source — skip build"
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
