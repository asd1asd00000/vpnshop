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
