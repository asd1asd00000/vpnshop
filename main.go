package main

import (
	"log"
	"net/http"

	"github.com/asd1asd00000/vpnshop/api"
	"github.com/asd1asd00000/vpnshop/db"
)

func main() {
	db.InitDB("./vpnshop.db")
	defer db.DB.Close()

	// 🌐 روت‌های برنامه

	// ۱. صفحه اصلی (فروشگاه)
	http.HandleFunc("/", api.ShopHandler)

	// ۲. صفحه پیگیری سفارش
	http.HandleFunc("/track", api.TrackHandler)

	// ۳. داشبورد مدیریت
	http.HandleFunc("/admin", api.AdminHandler)

	// ۴. بکاپ دیتابیس (جدید)
	http.HandleFunc("/admin/backup", api.BackupHandler)

	// ۵. تایید دستی ادمین (جدید)
	http.HandleFunc("/admin/confirm", api.AdminConfirmHandler)

	// ۶. وب‌هوک دریافت پیامک
	http.HandleFunc("/api/webhook/sms", api.WebhookHandler)

	port := ":8080"
	log.Printf("سرور VPNShop روی پورت %s در حال اجرا است...", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("خطای سرور: %v", err)
	}
}
