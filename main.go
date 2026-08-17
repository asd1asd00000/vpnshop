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

	db.StartLogCleanup()
http.HandleFunc("/api/order-status", api.CheckOrderStatus)
	// 🌐 مسیرهای عمومی
	http.HandleFunc("/", api.ShopHandler)
	http.HandleFunc("/track", api.TrackHandler)
	http.HandleFunc("/api/webhook/sms", api.WebhookHandler)

	// 🔐 مسیرهای ادمین (با پیشوند مخفی از متغیر محیطی)
	adminBase := api.AdminBasePath()
	http.HandleFunc(adminBase, api.AdminHandler)
	http.HandleFunc(adminBase+"/backup", api.BackupHandler)
	http.HandleFunc(adminBase+"/confirm", api.AdminConfirmHandler)
	http.HandleFunc(adminBase+"/restore", api.RestoreHandler)
	http.HandleFunc(adminBase+"/logs", api.AdminLogsHandler)
	http.HandleFunc(adminBase+"/logs/clear", api.AdminLogsClearHandler)

	port := ":8080"
	log.Printf("سرور VPNShop روی پورت %s در حال اجرا است...", port)
	log.Printf("🔐 مسیر پنل ادمین: %s", adminBase)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("خطای سرور: %v", err)
	}
}
