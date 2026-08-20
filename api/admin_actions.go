package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

// ───────────── تایید ادمین (چک‌باکس) ─────────────

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

// ─────────────  تایید دستی پرداخت + ساخت کانفیگ ─────────────

func ManualConfirmHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "درخواست نامعتبر",
		})
		return
	}

	// ۱. خواندن سفارش
	var order models.Order
	err := db.DB.QueryRow(`
		SELECT id, tracking_code, plan_name, status 
		FROM orders WHERE id = ?`, req.ID).Scan(
		&order.ID, &order.TrackingCode, &order.PlanName, &order.Status,
	)

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "سفارش یافت نشد",
		})
		return
	}

	// ۲. چک کن قبلاً تایید نشده باشه
	if order.Status == "paid" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "این سفارش قبلاً تایید شده",
		})
		return
	}

	// ۳. ساخت کانفیگ
	configLink, err := GenerateConfigFromOrder(order)
	if err != nil {
		db.LogEventf("config", "error", "❌ خطا در ساخت کانفیگ برای سفارش #%d: %v", req.ID, err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("خطا در ساخت کانفیگ: %v", err),
		})
		return
	}

	// ۴. بروزرسانی دیتابیس (status = paid + admin_confirmed = 1 + config_link)
	_, err = db.DB.Exec(`
		UPDATE orders 
		SET status = 'paid', 
		    admin_confirmed = 1, 
		    config_link = ? 
		WHERE id = ?`, configLink, req.ID)

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "خطا در بروزرسانی دیتابیس",
		})
		return
	}

	db.LogEventf("config", "success", "✅ تایید دستی پرداخت سفارش #%d توسط ادمین + ساخت کانفیگ", req.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "پرداخت تایید و کانفیگ ساخته شد",
	})
}

// ─────────────  بکاپ کامل (ZIP) ─────────────

func BackupHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	tmpDB := fmt.Sprintf("/tmp/vpnshop_backup_%s.db", timestamp)
	zipPath := fmt.Sprintf("/tmp/vpnshop_backup_%s.zip", timestamp)

	// ۱. کپی یکپارچه دیتابیس
	if _, err := db.DB.Exec(fmt.Sprintf("VACUUM INTO '%s'", tmpDB)); err != nil {
		db.LogEventf("general", "error", "❌ خطا در گرفتن بکاپ: %v", err)
		http.Error(w, "خطا در گرفتن بکاپ", http.StatusInternalServerError)
		return
	}

	// ۲. ساخت ZIP شامل دیتابیس + تنظیمات
	if err := createBackupZip(zipPath, tmpDB); err != nil {
		os.Remove(tmpDB)
		http.Error(w, "خطا در ساخت فایل زیپ", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="vpnshop_backup_%s.zip"`, timestamp))
	http.ServeFile(w, r, zipPath)

	os.Remove(tmpDB)
	os.Remove(zipPath)
	db.LogEvent("general", "info", "📥 بکاپ کامل (دیتابیس + تنظیمات) دانلود شد")
}

func createBackupZip(zipPath, dbPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zw := zip.NewWriter(zipFile)
	defer zw.Close()

	// دیتابیس
	if err := addFileToZip(zw, dbPath, "vpnshop.db"); err != nil {
		return err
	}

	// تنظیمات (اگه وجود داشته باشه)
	if _, err := os.Stat("./config.json"); err == nil {
		if err := addFileToZip(zw, "./config.json", "config.json"); err != nil {
			return err
		}
	}

	return nil
}

func addFileToZip(zw *zip.Writer, srcPath, nameInZip string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	w, err := zw.Create(nameInZip)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, src)
	return err
}

// ───────────── 📤 ریستور (ZIP یا DB) ─────────────

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

	tmpPath := "./restore_tmp_upload"
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
	defer os.Remove(tmpPath)

	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) < 4 {
		http.Error(w, "فایل معتبر نیست", http.StatusBadRequest)
		return
	}

	switch {
	case bytes.HasPrefix(data, []byte("SQLite format 3")):
		// بکاپ قدیمی (.db)
		if err := restoreFromDB(tmpPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case bytes.HasPrefix(data, []byte{0x50, 0x4B, 0x03, 0x04}):
		// بکاپ جدید (.zip)
		if err := restoreFromZip(tmpPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "فایل معتبر نیست (باید .zip یا .db باشد)", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, AdminBasePath(), http.StatusSeeOther)
}

func restoreFromDB(tmpPath string) error {
	if cur, err := os.ReadFile("./vpnshop.db"); err == nil {
		os.WriteFile("./vpnshop.db.before_restore", cur, 0644)
	}
	db.DB.Close()
	if err := os.Rename(tmpPath, "./vpnshop.db"); err != nil {
		return err
	}
	db.InitDB("./vpnshop.db")
	db.LogEvent("general", "success", "♻️ دیتابیس از بکاپ بازگردانی شد")
	return nil
}

func restoreFromZip(zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	var dbTmp, cfgTmp string

	for _, f := range zr.File {
		switch f.Name {
		case "vpnshop.db":
			dbTmp = "./restore_db_tmp"
			if err := extractZipFile(f, dbTmp); err != nil {
				return err
			}
		case "config.json":
			cfgTmp = "./restore_cfg_tmp"
			if err := extractZipFile(f, cfgTmp); err != nil {
				return err
			}
		}
	}

	if dbTmp == "" {
		return fmt.Errorf("فایل vpnshop.db در بکاپ یافت نشد")
	}

	// اعتبارسنجی دیتابیس داخل زیپ
	if data, err := os.ReadFile(dbTmp); err != nil || len(data) < 16 || string(data[:15]) != "SQLite format 3" {
		os.Remove(dbTmp)
		os.Remove(cfgTmp)
		return fmt.Errorf("دیتابیس داخل بکاپ معتبر نیست")
	}

	// بکاپ ایمنی از فایل‌های فعلی
	if cur, err := os.ReadFile("./vpnshop.db"); err == nil {
		os.WriteFile("./vpnshop.db.before_restore", cur, 0644)
	}
	if cur, err := os.ReadFile("./config.json"); err == nil {
		os.WriteFile("./config.json.before_restore", cur, 0644)
	}

	db.DB.Close()

	if err := os.Rename(dbTmp, "./vpnshop.db"); err != nil {
		return err
	}
	if cfgTmp != "" {
		os.Rename(cfgTmp, "./config.json")
	}

	db.InitDB("./vpnshop.db")
	db.LoadConfig()

	db.LogEvent("general", "success", "♻️ بکاپ کامل (دیتابیس + تنظیمات) بازگردانی شد")
	return nil
}

func extractZipFile(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

// ───────────── 📜 لاگ‌ها ─────────────

type logEntry struct {
	ID        int    `json:"id"`
	Category  string `json:"category"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

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
