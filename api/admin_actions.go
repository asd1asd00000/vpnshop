package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

// AdminConfirmHandler تایید دستی ادمین رو ثبت می‌کنه
func AdminConfirmHandler(w http.ResponseWriter, r *http.Request) {
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
	if !checkAdminAuth(w, r) {
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("/tmp/vpnshop_backup_%s.db", timestamp)

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

// RestoreHandler بکاپ آپلود شده رو جایگزین دیتابیس فعلی می‌کنه
func RestoreHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "خطا در پردازش فرم", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("backup_file")
	if err != nil {
		http.Error(w, "فایلی دریافت نشد", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// ذخیره موقت در همون دایرکتوری (برای rename امن)
	tmpPath := "./restore_tmp.db"
	dst, err := os.Create(tmpPath)
	if err != nil {
		http.Error(w, "خطا در ذخیره فایل موقت", http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		http.Error(w, "خطا در کپی فایل", http.StatusInternalServerError)
		return
	}
	dst.Close()

	// ✅ اعتبارسنجی: فایل باید SQLite باشه
	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) < 16 || string(data[:15]) != "SQLite format 3" {
		os.Remove(tmpPath)
		http.Error(w, "فایل بکاپ معتبر نیست (باید دیتابیس SQLite باشد)", http.StatusBadRequest)
		return
	}

	// ✅ بکاپ ایمنی از دیتابیس فعلی (قبل از جایگزینی)
	if cur, err := os.ReadFile("./vpnshop.db"); err == nil {
		os.WriteFile("./vpnshop.db.before_restore", cur, 0644)
		log.Println("🛡️ بکاپ ایمنی قبل از restore ساخته شد")
	}

	// بستن اتصال فعلی و جایگزینی فایل
	db.DB.Close()

	if err := os.Rename(tmpPath, "./vpnshop.db"); err != nil {
		log.Printf("❌ خطا در جایگزینی دیتابیس: %v", err)
		http.Error(w, "خطا در جایگزینی دیتابیس", http.StatusInternalServerError)
		return
	}

	// باز کردن مجدد دیتابیس
	db.InitDB("./vpnshop.db")

	log.Println("♻️ دیتابیس با موفقیت از بکاپ بازگردانی شد")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
