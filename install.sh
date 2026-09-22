#!/bin/bash
set -e

# ========================================
# 🚀 اسکریپت نصب VPNShop - نسخه کامل
# - آخرین نسخه Go
# - Mirror چینی برای ایران
# - راه‌اندازی خودکار systemd
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
apt-get install -y -qq git gcc build-essential curl wget > /dev/null 2>&1
echo "✅ پیش‌نیازها نصب شدند"

# ───────────── حذف Go قدیمی ─────────────
echo "🗑️ حذف نسخه قدیمی Go..."
rm -rf /usr/local/go

# ───────────── نصب آخرین نسخه Go ─────────────
echo "🔍 دریافت آخرین نسخه Go..."
# استفاده از API رسمی Go برای گرفتن آخرین نسخه
LATEST_VERSION=$(curl -fsSL "https://go.dev/dl/?mode=json" | grep -o '"version": "[^"]*"' | head -1 | cut -d'"' -f4)

if [ -z "$LATEST_VERSION" ]; then
    echo "⚠️ دریافت نسخه ناموفق، استفاده از نسخه پایدار..."
    LATEST_VERSION="go1.23.4"
fi

echo "📥 دانلود $LATEST_VERSION برای linux-$GOARCH..."
cd /tmp
curl -fsSLO "https://go.dev/dl/${LATEST_VERSION}.linux-${GOARCH}.tar.gz"

echo "📦 نصب Go در /usr/local..."
tar -C /usr/local -xzf "${LATEST_VERSION}.linux-${GOARCH}.tar.gz"
rm -f "${LATEST_VERSION}.linux-${GOARCH}.tar.gz"

# ───────────── تنظیم PATH و GOPROXY ─────────────
echo "⚙️ تنظیم environment..."
export PATH="/usr/local/go/bin:$PATH"
export GOPATH="/root/go"

# تنظیمات دائمی
echo 'export PATH="/usr/local/go/bin:$PATH"' >> /root/.bashrc
echo 'export GOPATH="/root/go"' >> /root/.bashrc

# 🇮🇷 تنظیم mirror چینی برای ایران
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GOSUMDB=sum.golang.org
go env -w GOPATH=/root/go
go env -w GOTOOLCHAIN=local

echo "✅ Go نصب شد: $(go version)"
echo "✅ Mirror: $(go env GOPROXY)"

# ───────────── ساخت پوشه پروژه ─────────────
PROJECT_DIR="/root/vpnshop"
if [ ! -d "$PROJECT_DIR" ]; then
    echo "📁 ساخت پوشه پروژه..."
    mkdir -p "$PROJECT_DIR"
    cd "$PROJECT_DIR"
    git init
    echo "✅ پوشه پروژه آماده شد"
    echo "⚠️  کد منبع رو در $PROJECT_DIR قرار بده و دوباره اسکریپت رو اجرا کن"
    exit 0
fi

cd "$PROJECT_DIR"

# ───────────── بررسی وجود کد ─────────────
if [ ! -f "go.mod" ]; then
    echo "❌ فایل go.mod یافت نشد. ابتدا کد رو در $PROJECT_DIR قرار بده"
    exit 1
fi

# ───────────── دانلود وابستگی‌ها ─────────────
echo "📥 دانلود وابستگی‌ها (با mirror چینی)..."
go mod download
go mod tidy
echo "✅ وابستگی‌ها دانلود شدند"

# ───────────── بیلد پروژه ─────────────
echo "🔨 بیلد پروژه..."
CGO_ENABLED=1 go build -ldflags="-s -w" -o vpnshop-app .
echo "✅ بیلد موفق: $(ls -lh vpnshop-app | awk '{print $5}')"

# ───────────── ساخت پوشه‌های لازم ─────────────
mkdir -p "$PROJECT_DIR/backups"
mkdir -p "$PROJECT_DIR/templates"
echo "✅ پوشه‌های پشتیبان و قالب آماده"

# ───────────── ساخت فایل config.json اگه نیست ─────────────
if [ ! -f "config.json" ]; then
    echo '{"admin":{"username":"admin","password":"admin123"},"panels":[],"cards":[]}' > config.json
    echo "⚠️  config.json پیش‌فرض ساخته شد (کاربر: admin, رمز: admin123)"
fi

# ───────────── ساخت plans.json اگه نیست ─────────────
if [ ! -f "plans.json" ]; then
    echo '[]' > plans.json
    echo "⚠️  plans.json خالی ساخته شد"
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
Environment=ADMIN_SECRET_PATH=

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
    sudo journalctl -u vpnshop -n 20 --no-pager
fi
