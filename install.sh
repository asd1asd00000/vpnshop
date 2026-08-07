#!/bin/bash

echo "==> در حال نصب پیش‌نیازهای لینوکس (Golang و Git)..."
sudo apt update
sudo apt install -y golang-go git

echo "==> در حال دریافت سورس‌کد از گیت‌هاب..."
cd /root
rm -rf vpnshop
git clone https://github.com/asd1asd00000/vpnshop.git
cd vpnshop

echo "==> در حال دانلود پکیج‌ها و کامپایل پروژه..."
go mod tidy
go build -o vpnshop-app main.go

echo "==> نصب با موفقیت انجام شد! ✅"
echo "برای اجرای سرور دستور زیر را وارد کنید:"
echo "cd /root/vpnshop && ./vpnshop-app"
