package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

// AdminConfirmHandler تایید دستی ادمین
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

	db.LogEventf("general", "info", "🖱️ تایید ادمین برای فاکتور #%d: %v", req.ID, req.Confirmed)
	w.WriteHeader(http.StatusOK)
}

// BackupHandler بکاپ دیتابیس
func BackupHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("/tmp/vpnshop_backup_%s.db", timestamp)

	if _, err := db.DB.Exec(fmt.Sprintf("VACUUM INTO '%s'", backupPath)); err != nil {
		db.LogEventf("general", "error", "❌ خطا در گرفتن بکاپ: %v", err)
		http.Error(w, "خطا در گرفتن بکاپ", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="vpnshop_backup_%s.db"`, timestamp))
	http.ServeFile(w, r, backupPath)

	os.Remove(backupPath)
	db.LogEvent("general", "info", "📥 بکاپ دیتابیس دانلود شد")
}

// RestoreHandler بازگردانی بکاپ
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

	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) < 16 || string(data[:15]) != "SQLite format 3" {
		os.Remove(tmpPath)
		http.Error(w, "فایل بکاپ معتبر نیست (باید دیتابیس SQLite باشد)", http.StatusBadRequest)
		return
	}

	if cur, err := os.ReadFile("./vpnshop.db"); err == nil {
		os.WriteFile("./vpnshop.db.before_restore", cur, 0644)
	}

	db.DB.Close()

	if err := os.Rename(tmpPath, "./vpnshop.db"); err != nil {
		http.Error(w, "خطا در جایگزینی دیتابیس", http.StatusInternalServerError)
		return
	}

	db.InitDB("./vpnshop.db")

	db.LogEvent("general", "success", "♻️ دیتابیس با موفقیت از بکاپ بازگردانی شد")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// ─────────────────────────────────────────────
// 📜 لاگ‌بین
// ─────────────────────────────────────────────

type logEntry struct {
	ID        int    `json:"id"`
	Category  string `json:"category"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

// AdminLogsHandler لاگ‌ها رو به صورت JSON برمی‌گردونه
func AdminLogsHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	rows, err := db.DB.Query(`SELECT id, category, level, message, created_at FROM logs ORDER BY id DESC LIMIT 500`)
	if err != nil {
		http.Error(w, "خطا در خواندن لاگ‌ها", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	logs := make([]logEntry, 0)
	for rows.Next() {
		var l logEntry
		if err := rows.Scan(&l.ID, &l.Category, &l.Level, &l.Message, &l.CreatedAt); err == nil {
			logs = append(logs, l)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

// AdminLogsClearHandler کل لاگ‌ها رو پاک می‌کنه
func AdminLogsClearHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, err := db.DB.Exec(`DELETE FROM logs`); err != nil {
		http.Error(w, "خطا در پاک کردن لاگ‌ها", http.StatusInternalServerError)
		return
	}

	db.LogEvent("general", "warning", "🗑️ لاگ‌ها توسط ادمین پاک شدند")
	w.WriteHeader(http.StatusOK)
}
