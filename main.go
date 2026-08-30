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

	// بارگذاری تنظیمات
	db.LoadConfig()

	db.StartLogCleanup()
    db.StartOrderCleanup()
		api.StartGroupCache()

	// 🌐 مسیرهای عمومی
	http.HandleFunc("/", api.ShopHandler)
	http.HandleFunc("/track", api.TrackHandler)
	http.HandleFunc("/api/webhook/sms", api.WebhookHandler)
	http.HandleFunc("/api/order-status", api.CheckOrderStatus)

	// 🔐 مسیرهای ادمین
	adminBase := api.AdminBasePath()
	http.HandleFunc(adminBase, api.AdminHandler)
	http.HandleFunc(adminBase+"/backup", api.BackupHandler)
	http.HandleFunc(adminBase+"/confirm", api.AdminConfirmHandler)
	http.HandleFunc(adminBase+"/restore", api.RestoreHandler)
	http.HandleFunc(adminBase+"/logs", api.AdminLogsHandler)
	http.HandleFunc(adminBase+"/logs/clear", api.AdminLogsClearHandler)
	http.HandleFunc(adminBase+"/manual-confirm", api.ManualConfirmHandler)
	http.HandleFunc(adminBase+"/note", api.AdminNoteHandler)
	http.HandleFunc(adminBase+"/login", api.LoginHandler)
	http.HandleFunc(adminBase+"/logout", api.LogoutHandler)

	// ⚙️ مسیرهای تنظیمات
	http.HandleFunc(adminBase+"/settings", api.SettingsHandler)
	http.HandleFunc(adminBase+"/settings/update-admin", api.UpdateAdminHandler)
	http.HandleFunc(adminBase+"/settings/add-panel", api.AddPanelHandler)
	http.HandleFunc(adminBase+"/settings/delete-panel", api.DeletePanelHandler)
	http.HandleFunc(adminBase+"/settings/email-backup", api.EmailBackupHandler)
	http.HandleFunc(adminBase+"/settings/add-card", api.AddCardHandler)
	http.HandleFunc(adminBase+"/settings/delete-card", api.DeleteCardHandler)
	http.HandleFunc(adminBase+"/settings/update-cleanup", api.UpdateCleanupHandler)

	port := ":8080"
	log.Printf("سرور VPNShop روی پورت %s در حال اجرا است...", port)
	log.Printf("🔐 مسیر پنل ادمین: %s", adminBase)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("خطای سرور: %v", err)
	}
}
