#!/bin/bash

echo "==> در حال نصب پیش‌نیازهای لینوکس..."
sudo apt update
sudo apt install -y golang-go git build-essential

echo "==> در حال دریافت سورس‌کد از گیت‌هاب..."
cd /root
rm -rf vpnshop
git clone https://github.com/asd1asd00000/vpnshop.git
cd vpnshop

echo "==> در حال دانلود پکیج‌ها و کامپایل پروژه..."
go mod tidy
CGO_ENABLED=1 go build -o vpnshop-app main.go

echo "==> نصب با موفقیت انجام شد! ✅"
echo "برای اجرای سرور دستور زیر را وارد کنید:"
echo "cd /root/vpnshop && ./vpnshop-app"
echo "⏳ در حال ساخت و تنظیم سرویس دائمی (Systemd)..."

# ساخت فایل سرویس به صورت خودکار
cat <<EOF > /etc/systemd/system/vpnshop.service
[Unit]
Description=VPNShop Golang Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root/vpnshop
ExecStart=/root/vpnshop/vpnshop-app
Restart=always
RestartSec=5

# متغیرهای پنل گارد
Environment="PANEL_URL=https://core.erfjab.com"
Environment="PANEL_USER=Javatava"
Environment="PANEL_PASS=3cet2&xhsA&X"

# متغیرهای ورود به داشبورد ادمین (میتوانید یوزر و پسورد را اینجا تغییر دهید)
Environment="ADMIN_USER=admin"
Environment="ADMIN_PASS=123456"

[Install]
WantedBy=multi-user.target
EOF

# فعال‌سازی و راه‌اندازی سرویس
systemctl daemon-reload
systemctl enable vpnshop
systemctl restart vpnshop

echo "✅ سرویس دائمی VPNShop با موفقیت نصب و روشن شد!"
