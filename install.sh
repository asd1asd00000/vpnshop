#!/bin/bash
set -e

# ========================================
# VPNShop Installation Script
# - Latest stable Go from go.dev
# - goproxy.cn for fast module downloads
# - Automatic swap for weak servers
# - Single-core build for low RAM
# ========================================

echo "🚀 Starting VPNShop installation..."

# ───────────── Check root ─────────────
if [ "$EUID" -ne 0 ]; then
    echo "❌ Please run as root or with sudo"
    exit 1
fi

# ───────────── Detect architecture ─────────────
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then
    GOARCH="amd64"
elif [ "$ARCH" = "aarch64" ]; then
    GOARCH="arm64"
else
    echo "❌ Unsupported architecture: $ARCH"
    exit 1
fi
echo "📦 Architecture: $ARCH → $GOARCH"

# ───────────── Install prerequisites ─────────────
echo "📦 Installing prerequisites..."
apt-get update -qq
apt-get install -y -qq git gcc build-essential curl wget ca-certificates > /dev/null 2>&1
echo "✅ Prerequisites installed"

# ───────────── Swap for weak servers ─────────────
RAM_MB=$(free -m | awk '/^Mem:/{print $2}')
SWAP_MB=$(free -m | awk '/^Swap:/{print $2}')
if [ "$RAM_MB" -le 2048 ] && [ "$SWAP_MB" -le 100 ]; then
    echo "💾 Low RAM (${RAM_MB}MB) — creating 2GB swap..."
    fallocate -l 2G /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count=2048 status=none
    chmod 600 /swapfile
    mkswap /swapfile > /dev/null
    swapon /swapfile
    grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
    echo "✅ Swap enabled"
else
    echo "💾 RAM: ${RAM_MB}MB | Swap: ${SWAP_MB}MB"
fi

# ───────────── Remove old Go ─────────────
echo "🗑️ Removing old Go installation..."
rm -rf /usr/local/go

# ───────────── Get latest Go version ─────────────
echo "🔍 Fetching latest Go version..."
LATEST_VERSION=$(curl -fsSL "https://go.dev/VERSION?m=text" 2>/dev/null | head -n1 | tr -d '\r\n ')

if [ -z "$LATEST_VERSION" ]; then
    echo "❌ Failed to fetch Go version from go.dev"
    exit 1
fi

echo "📥 Downloading ${LATEST_VERSION} for linux-${GOARCH}..."
cd /tmp
TARBALL="${LATEST_VERSION}.linux-${GOARCH}.tar.gz"

if ! curl -fSL --retry 3 --retry-delay 5 -o "$TARBALL" "https://go.dev/dl/${TARBALL}"; then
    echo "❌ Go download failed"
    exit 1
fi

# ───────────── Install Go ─────────────
echo "📦 Installing Go to /usr/local..."
tar -C /usr/local -xzf "$TARBALL"
rm -f "$TARBALL"

export PATH="/usr/local/go/bin:$PATH"
export GOPATH="/root/go"

grep -q '/usr/local/go/bin' /root/.bashrc || echo 'export PATH="/usr/local/go/bin:$PATH"' >> /root/.bashrc
grep -q 'GOPATH=' /root/.bashrc || echo 'export GOPATH="/root/go"' >> /root/.bashrc

echo "✅ Go installed: $(go version)"

# ───────────── Set GOPROXY for faster downloads ─────────────
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GOSUMDB=sum.golang.org
go env -w GOPATH=/root/go
go env -w GOTOOLCHAIN=local

echo "✅ GOPROXY configured: $(go env GOPROXY)"

# ───────────── Prepare project ─────────────
PROJECT_DIR="/root/vpnshop"
if [ ! -d "$PROJECT_DIR" ]; then
    echo "📁 Creating project directory..."
    mkdir -p "$PROJECT_DIR/templates"
    echo "✅ Project directory ready: $PROJECT_DIR"
    echo "⚠️  Place source code in $PROJECT_DIR and re-run this script"
    exit 0
fi

cd "$PROJECT_DIR"

if [ ! -f "go.mod" ]; then
    echo "❌ go.mod not found. Place source code in $PROJECT_DIR first"
    exit 1
fi

# ───────────── Download dependencies ─────────────
echo "📥 Downloading dependencies..."
if ! go mod download; then
    echo "❌ Dependencies download failed"
    exit 1
fi
go mod tidy
echo "✅ Dependencies downloaded"

# ───────────── Build project ─────────────
echo "🔨 Building project..."
BUILD_FLAGS=""
if [ "$RAM_MB" -le 1024 ]; then
    echo "🐢 Low RAM (${RAM_MB}MB): using single-core build..."
    export GOMAXPROCS=1
    BUILD_FLAGS="-p=1"
fi

if ! CGO_ENABLED=1 go build $BUILD_FLAGS -ldflags="-s -w" -o vpnshop-app .; then
    echo "❌ Build failed"
    exit 1
fi
echo "✅ Build succeeded: $(ls -lh vpnshop-app | awk '{print $5}')"

# ───────────── Create required directories ─────────────
mkdir -p "$PROJECT_DIR/backups"
mkdir -p "$PROJECT_DIR/templates"

# ───────────── Create config.json if missing ─────────────
if [ ! -f "config.json" ]; then
    echo '{"admin":{"username":"admin","password":"admin123"},"panels":[],"cards":[]}' > config.json
    echo "⚠️  Default config.json created (user: admin, password: admin123)"
fi

# ───────────── Create plans.json if missing ─────────────
if [ ! -f "plans.json" ]; then
    echo '[]' > plans.json
    echo "⚠️  Empty plans.json created"
fi

# ───────────── Create systemd service ─────────────
echo "⚙️ Setting up systemd service..."
cat > /etc/systemd/system/vpnshop.service << 'EOF'
[Unit]
Description=VPNShop - VPN Config Store
After=network.target

[Service]
Type=simple
WorkingDirectory=/root/vpnshop
ExecStart=/root/vpnshop/vpnshop-app
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# ───────────── Enable and start service ─────────────
echo "🚀 Starting service..."
systemctl daemon-reload
systemctl enable vpnshop
systemctl restart vpnshop

# ───────────── Verify status ─────────────
sleep 2
if systemctl is-active --quiet vpnshop; then
    echo ""
    echo "═══════════════════════════════════════"
    echo "✅ VPNShop installed and running!"
    echo "═══════════════════════════════════════"
    echo "📂 Project directory: $PROJECT_DIR"
    echo "🌐 Admin panel: http://YOUR_IP:8080/admin"
    echo "👤 Default user: admin"
    echo "🔑 Default password: admin123"
    echo "═══════════════════════════════════════"
    echo ""
    echo "📋 Useful commands:"
    echo "  sudo systemctl status vpnshop     # status"
    echo "  sudo systemctl restart vpnshop    # restart"
    echo "  sudo journalctl -u vpnshop -f     # live logs"
    echo ""
else
    echo "❌ Service failed to start. Logs:"
    journalctl -u vpnshop -n 20 --no-pager
fi
