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

	// تغییر مبلغ یکتا به 2000000 تا با تست پیامک «بله» شما هماهنگ شود
	_, err := db.DB.Exec(`INSERT OR IGNORE INTO orders (tracking_code, plan_name, base_price, unique_amount, status) 
		VALUES ('TEST-BALE', '20GB-1Month', 2000000, 2000000, 'pending')`)
	if err != nil {
		log.Printf("خطا در ساخت فاکتور تستی: %v", err)
	} else {
		log.Println("فاکتور تستی (TEST-BALE) با مبلغ 2000000 به دیتابیس اضافه شد.")
	}

	http.HandleFunc("/api/webhook/sms", api.WebhookHandler)

	port := ":8080"
	log.Printf("سرور VPNShop روی پورت %s در حال اجرا است...", port)
	
	err = http.ListenAndServe(port, nil)
	if err != nil {
		log.Fatalf("خطا در اجرای سرور: %v", err)
	}
}
