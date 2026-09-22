#!/bin/bash
set -e

# ========================================
# 🚀 اسکریپت نصب VPNShop - نسخه کامل
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

# ───────────── حذف Go قدیمی ─────────────
echo "🗑️ حذف نسخه قدیمی Go..."
rm -rf /usr/local/go

# ───────────── نصب آخرین نسخه Go (پایدار) ─────────────
echo "🔍 دریافت آخرین نسخه پایدار Go..."

# روش ۱: استفاده از VERSION endpoint رسمی (مطمئن‌ترین)
LATEST_VERSION=$(curl -fsSL "https://go.dev/VERSION?m=text" 2>/dev/null | head -1 || true)

# روش ۲: اگه بالا کار نکرد، از API JSON استفاده کن (با فیلتر stable)
if [ -z "$LATEST_VERSION" ]; then
    echo "⚠️  تلاش دوم با API JSON..."
    LATEST_VERSION=$(curl -fsSL "https://go.dev/dl/?mode=json" 2>/dev/null | grep -E '"version".*"stable":true' | head -1 | grep -oE '"version": "[^"]+"' | cut -d'"' -f4 || true)
fi

# روش ۳: اگه هیچکدوم کار نکرد، نسخه fallback
if [ -z "$LATEST_VERSION" ]; then
    echo "⚠️  دریافت نسخه ناموفق، استفاده از نسخه پایدار fallback..."
    LATEST_VERSION="go1.23.4"
fi

# اعتبارسنجی نام نسخه (باید با "go" شروع بشه)
if [[ ! "$LATEST_VERSION" =~ ^go[0-9] ]]; then
    echo "⚠️  نام نسخه نامعتبر: $LATEST_VERSION، استفاده از fallback..."
    LATEST_VERSION="go1.23.4"
fi

echo "📥 دانلود $LATEST_VERSION برای linux-$GOARCH..."
cd /tmp
DOWNLOAD_URL="https://go.dev/dl/${LATEST_VERSION}.linux-${GOARCH}.tar.gz"

# تلاش برای دانلود
if ! curl -fsSLO --retry 3 "$DOWNLOAD_URL"; then
    echo "❌ دانلود $LATEST_VERSION ناموفق بود"
    echo "🔄 تلاش با نسخه جایگزین go1.23.4..."
    LATEST_VERSION="go1.23.4"
    DOWNLOAD_URL="https://go.dev/dl/${LATEST_VERSION}.linux-${GOARCH}.tar.gz"
    if ! curl -fsSLO --retry 3 "$DOWNLOAD_URL"; then
        echo "❌ دانلود هر دو نسخه ناموفق بود"
        echo "💡 احتمالاً مشکل اینترنت یا تحریم هست"
        exit 1
    fi
fi

echo "📦 نصب Go در /usr/local..."
tar -C /usr/local -xzf "${LATEST_VERSION}.linux-${GOARCH}.tar.gz"
rm -f "${LATEST_VERSION}.linux-${GOARCH}.tar.gz"

# ───────────── تنظیم PATH و GOPROXY ─────────────
echo "⚙️ تنظیم environment..."
export PATH="/usr/local/go/bin:$PATH"
export GOPATH="/root/go"

# تنظیمات دائمی
if ! grep -q "/usr/local/go/bin" /root/.bashrc; then
    echo 'export PATH="/usr/local/go/bin:$PATH"' >> /root/.bashrc
fi
if ! grep -q "GOPATH=" /root/.bashrc; then
    echo 'export GOPATH="/root/go"' >> /root/.bashrc
fi

# 🇮🇷 تنظیم mirror چینی برای ایران (حیاتی!)
/usr/local/go/bin/go env -w GOPROXY=https://goproxy.cn,direct
/usr/local/go/bin/go env -w GOSUMDB=sum.golang.org
/usr/local/go/bin/go env -w GOPATH=/root/go
/usr/local/go/bin/go env -w GOTOOLCHAIN=local

echo "✅ Go نصب شد: $(/usr/local/go/bin/go version)"
echo "✅ Mirror: $(/usr/local/go/bin/go env GOPROXY)"

# ───────────── ساخت پوشه پروژه ─────────────
PROJECT_DIR="/root/vpnshop"
if [ ! -d "$PROJECT_DIR" ]; then
    echo "📁 ساخت پوشه پروژه..."
    mkdir -p "$PROJECT_DIR"
    cd "$PROJECT_DIR"
    echo "✅ پوشه پروژه آماده شد"
    echo "⚠️  کد منبع (شامل go.mod) رو در $PROJECT_DIR قرار بده"
    echo "⚠️  سپس دوباره این اسکریپت رو اجرا کن"
    exit 0
fi

cd "$PROJECT_DIR"

# ───────────── بررسی وجود کد ─────────────
if [ ! -f "go.mod" ]; then
    echo "❌ فایل go.mod یافت نشد"
    echo "💡 ابتدا کد رو در $PROJECT_DIR قرار بده:"
    echo "   cd /root/vpnshop"
    echo "   git clone https://github.com/asd1asd00000/vpnshop.git ."
    echo "   سپس دوباره اسکریپت رو اجرا کن"
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
echo "✅ پوشه بکاپ آماده"

# ───────────── ساخت فایل‌های پیش‌فرض ─────────────
if [ ! -f "config.json" ]; then
    cat > config.json << 'EOF'
{
  "admin": {
    "username": "admin",
    "password": "admin123"
  },
  "panels": [],
  "cards": [],
  "cleanup": {
    "order_expire_hours": 24
  }
}
EOF
    echo "⚠️  config.json پیش‌فرض ساخته شد (کاربر: admin, رمز: admin123)"
fi

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

[Install]
WantedBy=multi-user.target
EOF

# ───────────── فعال‌سازی و شروع سرویس ─────────────
echo "🚀 فعال‌سازی سرویس..."
systemctl daemon-reload
systemctl enable vpnshop
systemctl restart vpnshop

# ───────────── بررسی وضعیت ─────────────
sleep 3
if systemctl is-active --quiet vpnshop; then
    IP=$(curl -fsSL https://ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')
    echo ""
    echo "═══════════════════════════════════════"
    echo "✅ VPNShop با موفقیت نصب و راه‌اندازی شد!"
    echo "═══════════════════════════════════════"
    echo "📂 پوشه پروژه: $PROJECT_DIR"
    echo "🌐 آدرس پنل ادمین: http://$IP:8080/admin"
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
