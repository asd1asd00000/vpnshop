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

	// افزودن فاکتور تستی (فقط برای تست)
	_, err := db.DB.Exec(`INSERT OR IGNORE INTO orders (tracking_code, plan_name, base_price, unique_amount, status, config_link) 
		VALUES ('TEST-BALE', '20GB-1Month', 2000000, 2000000, 'paid', 'https://versub.bertly.top/guards/d9e06b2ca7f7942532d28059a5d086a2')`)
	if err == nil {
		log.Println("فاکتور تستی با وضعیت پرداخت‌شده به دیتابیس اضافه شد.")
	}

	// 🌐 روت‌های برنامه (مسیرهای سرور)
	
	// ۱. صفحه کاربری مشتری (ظاهر سایت) روی مسیر اصلی
	http.HandleFunc("/", api.TrackHandler)
	
	// ۲. وب‌هوک دریافت پیامک
	http.HandleFunc("/api/webhook/sms", api.WebhookHandler)

	port := ":8080"
	log.Printf("سرور VPNShop روی پورت %s در حال اجرا است...", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("خطای سرور: %v", err)
	}
}
