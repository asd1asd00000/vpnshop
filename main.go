package main

import (
	"log"
	"net/http"

	"github.com/asd1asd00000/vpnshop/api"
	"github.com/asd1asd00000/vpnshop/db"
)

func main() {
	// راه‌اندازی دیتابیس
	db.InitDB("./vpnshop.db")
	defer db.DB.Close()

	// تعریف مسیر وب‌هوک
	http.HandleFunc("/api/webhook/sms", api.WebhookHandler)

	// اجرای وب‌سرور روی پورت 8080
	port := ":8080"
	log.Printf("سرور VPNShop روی پورت %s در حال اجرا است...", port)
	
	err := http.ListenAndServe(port, nil)
	if err != nil {
		log.Fatalf("خطا در اجرای سرور: %v", err)
	}
}
