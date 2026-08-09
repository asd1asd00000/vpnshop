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

	// ✅ پاکسازی خودکار لاگ‌ها
	db.StartLogCleanup()

	http.HandleFunc("/", api.ShopHandler)
	http.HandleFunc("/track", api.TrackHandler)
	http.HandleFunc("/admin", api.AdminHandler)
	http.HandleFunc("/admin/backup", api.BackupHandler)
	http.HandleFunc("/admin/confirm", api.AdminConfirmHandler)
	http.HandleFunc("/admin/restore", api.RestoreHandler)
	http.HandleFunc("/admin/logs", api.AdminLogsHandler)             // ✅ جدید
	http.HandleFunc("/admin/logs/clear", api.AdminLogsClearHandler) // ✅ جدید
	http.HandleFunc("/api/webhook/sms", api.WebhookHandler)

	port := ":8080"
	log.Printf("سرور VPNShop روی پورت %s در حال اجرا است...", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("خطای سرور: %v", err)
	}
}
