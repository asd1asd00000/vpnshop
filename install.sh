#!/bin/bash
set -e

# ========================================
# 🚀 اسکریپت نصب VPNShop - نسخه v2
# - تشخیص هوشمند نسخه Go (با HEAD check)
# - 5 mirror چینی به ترتیب تست می‌شن
# - swap خودکار برای سرورهای ضعیف
# - بیلد تک‌هسته‌ای برای RAM کم
# ========================================

echo "🚀 شروع نصب VPNShop..."

# ───────────── بررسی root ─────────────
if [ "$EUID" -ne 0 ]; then
    echo "❌ لطفاً با sudo یا root اجرا کنید"
    exit 1
fi

# ───────────── تشخیص معماری ─────────────
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then
    GOARCH="amd64"
elif [ "$ARCH" = "aarch64" ]; then
    GOARCH="arm64"
else
    echo "❌ معماری پشتیبانی نمی‌شود: $ARCH"
    exit 1
fi
echo "📦 معماری: $ARCH → $GOARCH"

# ───────────── نصب پیش‌نیازها ─────────────
echo "📦 نصب پیش‌نیازها..."
apt-get update -qq
apt-get install -y -qq git gcc build-essential curl wget ca-certificates > /dev/null 2>&1
echo "✅ پیش‌نیازها نصب شدند"

# ───────────── swap برای سرورهای ضعیف ─────────────
RAM_MB=$(free -m | awk '/^Mem:/{print $2}')
SWAP_MB=$(free -m | awk '/^Swap:/{print $2}')
if [ "$RAM_MB" -le 2048 ] && [ "$SWAP_MB" -le 100 ]; then
    echo "💾 RAM کم (${RAM_MB}MB) — ساخت swap 2GB..."
    fallocate -l 2G /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count=2048 status=none
    chmod 600 /swapfile
    mkswap /swapfile > /dev/null
    swapon /swapfile
    grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
    echo "✅ swap فعال شد"
else
    echo "💾 RAM: ${RAM_MB}MB | Swap: ${SWAP_MB}MB — نیازی به swap جدید نیست"
fi

# ───────────── حذف Go قدیمی ─────────────
echo "🗑️ حذف نسخه قدیمی Go..."
rm -rf /usr/local/go

# ═══════════════════════════════════════════════════════════════
# بخش ۱: پیدا کردن نسخه Go واقعاً قابل دانلود
# ═══════════════════════════════════════════════════════════════

echo "🔍 تشخیص آخرین نسخه پایدار Go..."

LATEST_VERSION=""

# مرحله ۱: تست نسخه از VERSION endpoint
VERSION_CANDIDATE=$(curl -fsSL --max-time 20 "https://go.dev/VERSION?m=text" 2>/dev/null | head -n1 | tr -d '\r\n ')
if [ -n "$VERSION_CANDIDATE" ]; then
    echo "   🧪 تست نسخه پیشنهادی: $VERSION_CANDIDATE"
    if curl -fsSI --max-time 20 "https://go.dev/dl/${VERSION_CANDIDATE}.linux-${GOARCH}.tar.gz" > /dev/null 2>&1; then
        LATEST_VERSION=$VERSION_CANDIDATE
        echo "   ✅ نسخه معتبر: $LATEST_VERSION"
    else
        echo "   ⚠️ نسخه $VERSION_CANDIDATE در دسترس نیست (احتمالاً هنوز منتشر نشده)"
    fi
fi

# مرحله ۲: اگه مرحله ۱ شکست خورد، لیست JSON رو چک کن
if [ -z "$LATEST_VERSION" ]; then
    echo "   🧪 جستجو در لیست نسخه‌های منتشر شده..."
    VERSIONS_JSON=$(curl -fsSL --max-time 30 "https://go.dev/dl/?mode=json" 2>/dev/null)
    
    if [ -z "$VERSIONS_JSON" ]; then
        echo "❌ نتونستم به go.dev وصل بشم. اتصال اینترنت رو چک کن."
        exit 1
    fi
    
    # استخراج نسخه‌ها از JSON
    for v in $(echo "$VERSIONS_JSON" | grep -o '"version":"go[^"]*"' | cut -d'"' -f4); do
        if curl -fsSI --max-time 15 "https://go.dev/dl/${v}.linux-${GOARCH}.tar.gz" > /dev/null 2>&1; then
            LATEST_VERSION=$v
            echo "   ✅ اولین نسخه موجود: $LATEST_VERSION"
            break
        fi
    done
fi

if [ -z "$LATEST_VERSION" ]; then
    echo "❌ هیچ نسخه معتبر Go پیدا نشد"
    exit 1
fi

# ═══════════════════════════════════════════════════════════════
# بخش ۲: دانلود و نصب Go
# ═══════════════════════════════════════════════════════════════

echo "📥 دانلود ${LATEST_VERSION} برای linux-${GOARCH}..."
cd /tmp
TARBALL="${LATEST_VERSION}.linux-${GOARCH}.tar.gz"
if ! curl -fSL --retry 3 --retry-delay 5 -o "$TARBALL" "https://go.dev/dl/${TARBALL}"; then
    echo "❌ دانلود Go ناموفق بود"
    exit 1
fi

echo "📦 نصب Go در /usr/local..."
tar -C /usr/local -xzf "$TARBALL"
rm -f "$TARBALL"

# ───────────── تنظیم PATH ─────────────
export PATH="/usr/local/go/bin:$PATH"
export GOPATH="/root/go"

grep -q '/usr/local/go/bin' /root/.bashrc || echo 'export PATH="/usr/local/go/bin:$PATH"' >> /root/.bashrc
grep -q 'GOPATH=' /root/.bashrc || echo 'export GOPATH="/root/go"' >> /root/.bashrc

echo "✅ Go نصب شد: $(go version)"

# ═══════════════════════════════════════════════════════════════
# بخش ۳: پیدا کردن proxy کارآمد با تست واقعی
# ═══════════════════════════════════════════════════════════════

echo "🔍 تست mirror ها برای دانلود ماژول‌های Go..."

# لیست mirror ها به ترتیب اولویت
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
    echo "   🧪 تست: $proxy"
    TEST_URL="${proxy}/${TEST_PKG}"
    
    # HEAD request برای چک کردن سریع
    if curl -fsSI --max-time 15 "$TEST_URL" > /dev/null 2>&1; then
        WORKING_PROXY=$proxy
        echo "   ✅ mirror کارآمد: $proxy"
        break
    else
        echo "   ⚠️ پاسخ نداد، بعدی..."
    fi
done

# اگر هیچ mirror کار نکرد
if [ -z "$WORKING_PROXY" ]; then
    echo "   ⚠️ هیچ mirror چینی کار نکرد. تست مستقیم (proxy.golang.org)..."
    if curl -fsSI --max-time 20 "https://proxy.golang.org/${TEST_PKG}" > /dev/null 2>&1; then
        WORKING_PROXY="https://proxy.golang.org"
        echo "   ✅ proxy رسمی کار می‌کنه"
    else
        WORKING_PROXY="direct"
        echo "   ⚠️ از حالت direct استفاده می‌کنم (شاید سرور خارجیه)"
    fi
fi

# تنظیم محیطی
go env -w GOPROXY="${WORKING_PROXY},direct"
go env -w GOSUMDB=sum.golang.org
go env -w GOPATH=/root/go
go env -w GOTOOLCHAIN=local

echo "✅ GOPROXY تنظیم شد: $(go env GOPROXY)"

# ═══════════════════════════════════════════════════════════════
# بخش ۴: آماده‌سازی پروژه
# ═══════════════════════════════════════════════════════════════

PROJECT_DIR="/root/vpnshop"
if [ ! -d "$PROJECT_DIR" ]; then
    echo "📁 ساخت پوشه پروژه..."
    mkdir -p "$PROJECT_DIR/templates"
    echo "✅ پوشه پروژه آماده شد: $PROJECT_DIR"
    echo "⚠️  کد منبع رو در $PROJECT_DIR قرار بده و دوباره اسکریپت رو اجرا کن"
    exit 0
fi

cd "$PROJECT_DIR"

if [ ! -f "go.mod" ]; then
    echo "❌ فایل go.mod یافت نشد. ابتدا کد رو در $PROJECT_DIR قرار بده"
    exit 1
fi

# ───────────── دانلود وابستگی‌ها ─────────────
echo "📥 دانلود وابستگی‌ها..."
if ! go mod download; then
    echo "❌ دانلود وابستگی‌ها با $WORKING_PROXY شکست خورد"
    echo "🔄 تست حالت direct..."
    go env -w GOPROXY=direct
    go mod download || { echo "❌ دانلود ناموفق"; exit 1; }
fi
go mod tidy
echo "✅ وابستگی‌ها دانلود شدند"

# ───────────── بیلد پروژه ─────────────
echo "🔨 بیلد پروژه..."
BUILD_FLAGS=""
if [ "$RAM_MB" -le 1024 ]; then
    echo "🐢 سرور ضعیف (${RAM_MB}MB RAM): بیلد تک‌هسته‌ای..."
    export GOMAXPROCS=1
    BUILD_FLAGS="-p=1"
fi

if ! CGO_ENABLED=1 go build $BUILD_FLAGS -ldflags="-s -w" -o vpnshop-app .; then
    echo "❌ بیلد ناموفق"
    exit 1
fi
echo "✅ بیلد موفق: $(ls -lh vpnshop-app | awk '{print $5}')"

# ───────────── ساخت پوشه‌های لازم ─────────────
mkdir -p "$PROJECT_DIR/backups"
mkdir -p "$PROJECT_DIR/templates"

# ───────────── ساخت config.json اگه نیست ─────────────
if [ ! -f "config.json" ]; then
    echo '{"admin":{"username":"admin","password":"admin123"},"panels":[],"cards":[]}' > config.json
    echo "⚠️  config.json پیش‌فرض ساخته شد (کاربر: admin, رمز: admin123)"
fi

# ───────────── ساخت plans.json اگه نیست ─────────────
if [ ! -f "plans.json" ]; then
    echo '[]' > plans.json
fi

# ───────────── ساخت systemd service ─────────────
echo "⚙️ ساخت systemd service..."
cat > /etc/systemd/system/vpnshop.service << 'EOF'
[Unit]
Description=VPNShop - فروشگاه کانفیگ VPN
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

# ───────────── فعال‌سازی و شروع سرویس ─────────────
echo "🚀 فعال‌سازی سرویس..."
systemctl daemon-reload
systemctl enable vpnshop
systemctl restart vpnshop

# ───────────── بررسی وضعیت ─────────────
sleep 2
if systemctl is-active --quiet vpnshop; then
    echo ""
    echo "═══════════════════════════════════════"
    echo "✅ VPNShop با موفقیت نصب و راه‌اندازی شد!"
    echo "═══════════════════════════════════════"
    echo "📂 پوشه پروژه: $PROJECT_DIR"
    echo "🌐 آدرس پنل ادمین: http://YOUR_IP:8080/admin"
    echo "👤 کاربر پیش‌فرض: admin"
    echo "🔑 رمز پیش‌فرض: admin123"
    echo "═══════════════════════════════════════"
    echo ""
    echo "📋 دستورات مفید:"
    echo "  sudo systemctl status vpnshop     # وضعیت"
    echo "  sudo systemctl restart vpnshop    # ریستارت"
    echo "  sudo journalctl -u vpnshop -f     # لاگ زنده"
    echo ""
else
    echo "❌ سرویس شروع نشد. لاگ:"
    journalctl -u vpnshop -n 20 --no-pager
fi
