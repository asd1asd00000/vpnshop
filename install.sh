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
# ==========================================
# 🌐 تنظیمات Nginx و دامنه
# ==========================================
echo ""
echo "🌐 تنظیمات دامنه (پروکسی معکوس Nginx)"
read -p "لطفاً نام دامنه یا ساب‌دامین فروشگاه خود را وارد کنید (مثلاً shop.erfjab.com) [اگر نمی‌خواهید اینتر بزنید]: " domain_name </dev/tty

if [ ! -z "$domain_name" ]; then
    echo "⚙️ در حال ساخت کانفیگ Nginx برای دامنه $domain_name ..."
    
    cat <<EOF> /etc/nginx/sites-available/vpnshop
server {
    listen 80;
    server_name $domain_name;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF

    # ایجاد اتصال (سیم‌لینک) و ری‌استارت انجینکس
    ln -sf /etc/nginx/sites-available/vpnshop /etc/nginx/sites-enabled/
    nginx -t && systemctl restart nginx
    echo "✅ دامنه $domain_name با موفقیت تنظیم شد و به پورت 8080 متصل گردید!"
else
    echo "⚠️ نام دامنه‌ای وارد نشد. تنظیمات Nginx نادیده گرفته شد."
fi

echo ""
echo "🎉 نصب و راه‌اندازی با موفقیت به پایان رسید!"
echo "🛒 آدرس فروشگاه شما: http://$domain_name (یا http://IP:8080)"
echo "🔒 آدرس داشبورد مدیریت: http://$domain_name/admin"
