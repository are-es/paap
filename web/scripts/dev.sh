#!/bin/bash
# PAAP Web Interface Build & Run Script
# Handles Node.js version compatibility, dependency management, and process lifecycle
# Usage: ./dev.sh [port]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PORT="${1:-3000}"
PID_FILE="$WEB_DIR/.dev-server.pid"

echo "=== PAAP Web Interface ==="
echo "Working directory: $WEB_DIR"
echo "Port: $PORT"

# Cleanup function
cleanup() {
    echo ""
    echo "Shutting down..."
    if [ -f "$PID_FILE" ]; then
        kill $(cat "$PID_FILE") 2>/dev/null || true
        rm -f "$PID_FILE"
    fi
    # Kill any child processes
    jobs -p | xargs -r kill 2>/dev/null || true
    exit 0
}

trap cleanup SIGINT SIGTERM

# Check and setup Node.js
setup_node() {
    # Try system Node.js first
    if command -v node &> /dev/null; then
        NODE_VERSION=$(node -v | sed 's/v//' | cut -d. -f1)
        if [ "$NODE_VERSION" -ge 18 ]; then
            echo "✓ Node.js $(node -v) available"
            return 0
        fi
    fi

    # Try NVM
    if [ -f "$HOME/.nvm/nvm.sh" ]; then
        source "$HOME/.nvm/nvm.sh"
        if command -v nvm &> /dev/null; then
            echo "Found NVM, installing Node.js 22..."
            nvm install 22
            nvm use 22
            echo "✓ Node.js $(node -v) via NVM"
            return 0
        fi
    fi

    # Try volta
    if command -v volta &> /dev/null; then
        volta install node@22
        echo "✓ Node.js $(node -v) via Volta"
        return 0
    fi

    # Try fnm
    if command -v fnm &> /dev/null; then
        eval "$(fnm env)"
        fnm install 22
        fnm use 22
        echo "✓ Node.js $(node -v) via fnm"
        return 0
    fi

    echo "ERROR: Node.js 18+ required but not found"
    echo "Install options:"
    echo "  curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash - && sudo apt-get install -y nodejs"
    echo "  # or"
    echo "  curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash"
    echo "  nvm install 22"
    exit 1
}

# Install dependencies
install_deps() {
    cd "$WEB_DIR"
    if [ ! -d "node_modules" ] || [ ! -f "node_modules/.package-lock.json" ]; then
        echo "Installing dependencies..."
        npm install
    else
        echo "✓ Dependencies installed"
    fi
}

# Build the project
build_project() {
    cd "$WEB_DIR"
    echo "Building project..."
    npm run build
    echo "✓ Build complete"
}

# Main execution
main() {
    setup_node
    install_deps
    build_project

    echo ""
    echo "Starting production server..."
    cd "$WEB_DIR"
    PORT=$PORT npm run start &
    SERVER_PID=$!
    echo $SERVER_PID > "$PID_FILE"
    echo "✓ Server started (PID: $SERVER_PID)"
    echo "✓ Web interface: http://localhost:$PORT"
    echo ""
    echo "Press Ctrl+C to stop"
    wait $SERVER_PID
}

main "$@"
