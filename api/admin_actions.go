package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

// AdminConfirmHandler تایید دستی ادمین رو ثبت می‌کنه
func AdminConfirmHandler(w http.ResponseWriter, r *http.Request) {
	// 🔒 امنیت: فقط ادمین مجاز است
	if !checkAdminAuth(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID        int  `json:"id"`
		Confirmed bool `json:"confirmed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	val := 0
	if req.Confirmed {
		val = 1
	}

	_, err := db.DB.Exec(`UPDATE orders SET admin_confirmed = ? WHERE id = ?`, val, req.ID)
	if err != nil {
		http.Error(w, "خطا در بروزرسانی", http.StatusInternalServerError)
		return
	}

	log.Printf("🖱️ تایید ادمین برای فاکتور #%d: %v", req.ID, req.Confirmed)
	w.WriteHeader(http.StatusOK)
}

// BackupHandler بکاپ کامل از دیتابیس می‌گیره و دانلود می‌کنه
func BackupHandler(w http.ResponseWriter, r *http.Request) {
	// 🔒 امنیت: فقط ادمین مجاز است
	if !checkAdminAuth(w, r) {
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("/tmp/vpnshop_backup_%s.db", timestamp)

	// VACUUM INTO یه کپی یکپارچه و سالم می‌گیره
	if _, err := db.DB.Exec(fmt.Sprintf("VACUUM INTO '%s'", backupPath)); err != nil {
		log.Printf("❌ خطا در گرفتن بکاپ: %v", err)
		http.Error(w, "خطا در گرفتن بکاپ", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="vpnshop_backup_%s.db"`, timestamp))
	http.ServeFile(w, r, backupPath)

	os.Remove(backupPath)
	log.Printf("📥 بکاپ دیتابیس دانلود شد")
}
