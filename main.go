package main

import (
	"log"
	"net/http"

	"github.com/asd1asd00000/vpnshop/api"
	"github.com/asd1asd00000/vpnshop/db"
)

func main() {
	// ۱. راه‌اندازی دیتابیس
	db.InitDB("./vpnshop.db")
	defer db.DB.Close()

	// ۲. ساخت یک فاکتور تستی (فقط برای الان)
	// مبلغ خرد شده در اینجا 200045 است
	_, err := db.DB.Exec(`INSERT OR IGNORE INTO orders (tracking_code, plan_name, base_price, unique_amount, status) 
		VALUES ('TEST-123', '20GB-1Month', 200000, 200045, 'pending')`)
	if err != nil {
		log.Printf("خطا در ساخت فاکتور تستی: %v", err)
	} else {
		log.Println("فاکتور تستی (TEST-123) با مبلغ 200045 به دیتابیس اضافه شد.")
	}

	// ۳. تعریف مسیر وب‌هوک
	http.HandleFunc("/api/webhook/sms", api.WebhookHandler)

	// ۴. اجرای وب‌سرور
	port := ":8080"
	log.Printf("سرور VPNShop روی پورت %s در حال اجرا است...", port)
	
	err = http.ListenAndServe(port, nil)
	if err != nil {
		log.Fatalf("خطا در اجرای سرور: %v", err)
	}
}
