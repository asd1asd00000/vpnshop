#!/bin/bash
set -e

# ========================================
# VPNShop Installation Script v4
# - Byte-range download test (more reliable than HEAD)
# - 5 proxy mirrors tested in sequence
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
    echo "💾 RAM: ${RAM_MB}MB | Swap: ${SWAP_MB}MB — no new swap needed"
fi

# ───────────── Remove old Go ─────────────
echo "🗑️ Removing old Go installation..."
rm -rf /usr/local/go

# ═══════════════════════════════════════════════════════════════
# Section 1: Find an actually downloadable Go version
# Uses byte-range download (1KB) instead of HEAD for reliability
# Falls back to hardcoded list if go.dev is unreachable
# ═══════════════════════════════════════════════════════════════

echo "🔍 Detecting latest stable Go version..."

# Helper: check if Go version is actually downloadable using byte-range
is_go_version_available() {
    local version=$1
    local arch=$2
    local url="https://go.dev/dl/${version}.linux-${arch}.tar.gz"
    local status

    # Download just 1KB (byte range 0-1023) to test if file exists
    # This is faster and more reliable than HEAD with redirects
    status=$(curl -sL --max-time 30 -o /dev/null -w "%{http_code}" -r 0-1023 "$url" 2>/dev/null)
    [ "$status" = "200" ] || [ "$status" = "206" ]
}

LATEST_VERSION=""

# Step 1: Try version from VERSION endpoint
VERSION_CANDIDATE=$(curl -fsSL --max-time 20 "https://go.dev/VERSION?m=text" 2>/dev/null | head -n1 | tr -d '\r\n ')
if [ -n "$VERSION_CANDIDATE" ]; then
    echo "   🧪 Testing suggested version: $VERSION_CANDIDATE"
    if is_go_version_available "$VERSION_CANDIDATE" "$GOARCH"; then
        LATEST_VERSION=$VERSION_CANDIDATE
        echo "   ✅ Valid version: $LATEST_VERSION"
    else
        echo "   ⚠️ Version $VERSION_CANDIDATE has no downloadable file yet"
    fi
fi

# Step 2: If Step 1 failed, try JSON list
if [ -z "$LATEST_VERSION" ]; then
    echo "   🧪 Trying JSON list from go.dev..."
    VERSIONS_JSON=$(curl -fsSL --max-time 30 "https://go.dev/dl/?mode=json" 2>/dev/null)

    if [ -n "$VERSIONS_JSON" ]; then
        for v in $(echo "$VERSIONS_JSON" | grep -o '"version":"go[^"]*"' | cut -d'"' -f4); do
            if is_go_version_available "$v" "$GOARCH"; then
                LATEST_VERSION=$v
                echo "   ✅ First available from JSON: $LATEST_VERSION"
                break
            fi
        done
    else
        echo "   ⚠️ Cannot fetch JSON list from go.dev"
    fi
fi

# Step 3: Fallback to hardcoded list of known good versions
# This guarantees installation even if go.dev is unreachable or JSON parsing fails
if [ -z "$LATEST_VERSION" ]; then
    echo "   🧪 Falling back to hardcoded version list..."

    # Hardcoded list of known stable Go versions (newest first)
    HARDCODED_VERSIONS=(
        "go1.23.4"
        "go1.23.3"
        "go1.23.2"
        "go1.23.1"
        "go1.23.0"
        "go1.22.10"
        "go1.22.9"
        "go1.22.8"
        "go1.22.7"
        "go1.22.6"
        "go1.22.5"
        "go1.22.4"
        "go1.22.3"
        "go1.22.2"
        "go1.22.1"
        "go1.22.0"
        "go1.21.13"
        "go1.21.12"
        "go1.21.11"
        "go1.21.10"
    )

    for v in "${HARDCODED_VERSIONS[@]}"; do
        echo "   🧪 Testing $v..."
        if is_go_version_available "$v" "$GOARCH"; then
            LATEST_VERSION=$v
            echo "   ✅ First available from hardcoded list: $LATEST_VERSION"
            break
        fi
    done
fi

if [ -z "$LATEST_VERSION" ]; then
    echo "❌ No valid Go version found from any source"
    echo "💡 This might be due to network restrictions. Trying direct Google mirror..."
    
    # Last resort: try direct download from dl.google.com
    LATEST_VERSION="go1.23.4"
    echo "   🧪 Forcing $LATEST_VERSION from dl.google.com..."
fi

# ═══════════════════════════════════════════════════════════════
# Section 2: Download and install Go
# Try multiple download sources
# ═══════════════════════════════════════════════════════════════

echo "📥 Downloading ${LATEST_VERSION} for linux-${GOARCH}..."
cd /tmp
TARBALL="${LATEST_VERSION}.linux-${GOARCH}.tar.gz"

# Try go.dev first, then dl.google.com, then Chinese mirror
DOWNLOAD_SUCCESS=false

echo "   🧪 Trying go.dev..."
if curl -fSL --retry 3 --retry-delay 5 -o "$TARBALL" "https://go.dev/dl/${TARBALL}" 2>/dev/null; then
    DOWNLOAD_SUCCESS=true
    echo "   ✅ Downloaded from go.dev"
fi

if [ "$DOWNLOAD_SUCCESS" = false ]; then
    echo "   🧪 Trying dl.google.com..."
    if curl -fSL --retry 3 --retry-delay 5 -o "$TARBALL" "https://dl.google.com/go/${TARBALL}" 2>/dev/null; then
        DOWNLOAD_SUCCESS=true
        echo "   ✅ Downloaded from dl.google.com"
    fi
fi

if [ "$DOWNLOAD_SUCCESS" = false ]; then
    echo "   🧪 Trying Chinese mirror (golang.google.cn)..."
    if curl -fSL --retry 3 --retry-delay 5 -o "$TARBALL" "https://golang.google.cn/dl/${TARBALL}" 2>/dev/null; then
        DOWNLOAD_SUCCESS=true
        echo "   ✅ Downloaded from Chinese mirror"
    fi
fi

if [ "$DOWNLOAD_SUCCESS" = false ]; then
    echo "❌ Go download failed from all sources"
    exit 1
fi

echo "📦 Installing Go to /usr/local..."
tar -C /usr/local -xzf "$TARBALL"
rm -f "$TARBALL"

# ───────────── Set PATH ─────────────
export PATH="/usr/local/go/bin:$PATH"
export GOPATH="/root/go"

grep -q '/usr/local/go/bin' /root/.bashrc || echo 'export PATH="/usr/local/go/bin:$PATH"' >> /root/.bashrc
grep -q 'GOPATH=' /root/.bashrc || echo 'export GOPATH="/root/go"' >> /root/.bashrc

echo "✅ Go installed: $(go version)"

# ═══════════════════════════════════════════════════════════════
# Section 3: Find a working proxy with real HEAD check
# ═══════════════════════════════════════════════════════════════

echo "🔍 Testing proxy mirrors for Go modules..."

# Mirrors in priority order
MIRRORS=(
    "https://goproxy.cn"
    "https://goproxy.io"
    "https://proxy.go.kr"
    "https://mirrors.aliyun.com/goproxy"
    "https://goproxy.jp"
)

WORKING_PROXY=""
TEST_PKG="github.com/mattn/go-sqlite3/@v/list"

for proxy in "${MIRRORS[@]}"; do
    echo "   🧪 Testing: $proxy"
    TEST_URL="${proxy}/${TEST_PKG}"

    if curl -fsSI --max-time 15 "$TEST_URL" > /dev/null 2>&1; then
        WORKING_PROXY=$proxy
        echo "   ✅ Working mirror: $proxy"
        break
    else
        echo "   ⚠️ No response, trying next..."
    fi
done

# Fallback chain if no mirror worked
if [ -z "$WORKING_PROXY" ]; then
    echo "   ⚠️ No Chinese mirror worked. Testing official proxy..."
    if curl -fsSI --max-time 20 "https://proxy.golang.org/${TEST_PKG}" > /dev/null 2>&1; then
        WORKING_PROXY="https://proxy.golang.org"
        echo "   ✅ Official proxy works"
    else
        WORKING_PROXY="direct"
        echo "   ⚠️ Falling back to direct mode"
    fi
fi

# Set Go env variables
go env -w GOPROXY="${WORKING_PROXY},direct"
go env -w GOSUMDB=sum.golang.org
go env -w GOPATH=/root/go
go env -w GOTOOLCHAIN=local

echo "✅ GOPROXY set: $(go env GOPROXY)"

# ═══════════════════════════════════════════════════════════════
# Section 4: Prepare project
# ═══════════════════════════════════════════════════════════════

PROJECT_DIR="/root/vpnshop"
if [ ! -d "$PROJECT_DIR" ]; then
    echo "📁 Creating project directory..."
    mkdir -p "$PROJECT_DIR/templates"
    echo "✅ Project directory ready: $PROJECT_DIR"
    echo "⚠️  Place source code in $PROJECT_DIR and re-run the script"
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
    echo "❌ Dependencies download failed with $WORKING_PROXY"
    echo "🔄 Falling back to direct mode..."
    go env -w GOPROXY=direct
    go mod download || { echo "❌ Download failed"; exit 1; }
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
