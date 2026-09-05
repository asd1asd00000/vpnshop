package api

import (
	"archive/zip"
	"bytes"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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

// ───────────── 📝 یادداشت ادمین ─────────────

func AdminNoteHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID   int    `json:"id"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	_, err := db.DB.Exec(`UPDATE orders SET admin_note = ? WHERE id = ?`, req.Note, req.ID)
	if err != nil {
		http.Error(w, "خطا در ذخیره", http.StatusInternalServerError)
		return
	}

	db.LogEventf("general", "info", "📝 یادداشت ادمین برای فاکتور #%d بروزرسانی شد", req.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "یادداشت ذخیره شد",
	})
}

// ───────────── تایید دستی پرداخت + ساخت کانفیگ ─────────────

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

	if order.Status == "paid" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "این سفارش قبلاً تایید شده",
		})
		return
	}

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

	_, err = db.DB.Exec(`
		UPDATE orders 
		SET status = 'paid', 
		    admin_confirmed = 1, 
		    payment_method = 'admin',
		    paid_at = datetime('now'),
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

// ───────────── 📥 بکاپ کامل (UUID-based) ─────────────

const backupDir = "./backups"

// backupUUID یه شناسه منحصربه‌فرد برای هر بکاپ می‌سازه
func backupUUID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 16)
	if _, err := crand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// BackupHandler ساخت بکاپ جدید با UUID و redirect به لینک دانلود
func BackupHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		http.Error(w, "خطا در ساخت پوشه بکاپ", http.StatusInternalServerError)
		return
	}

	uuid := backupUUID()
	timestamp := time.Now().Format("20060102_150405")
	tmpDB := fmt.Sprintf("%s/%s.db", backupDir, uuid)
	zipPath := fmt.Sprintf("%s/%s.zip", backupDir, uuid)

	// ۱. کپی یکپارچه دیتابیس
	if _, err := db.DB.Exec(fmt.Sprintf("VACUUM INTO '%s'", tmpDB)); err != nil {
		db.LogEventf("general", "error", "❌ خطا در گرفتن بکاپ: %v", err)
		http.Error(w, "خطا در گرفتن بکاپ", http.StatusInternalServerError)
		return
	}

	// ۲. ساخت ZIP
	if err := createBackupZip(zipPath, tmpDB); err != nil {
		os.Remove(tmpDB)
		http.Error(w, "خطا در ساخت فایل زیپ", http.StatusInternalServerError)
		return
	}
	os.Remove(tmpDB)

	// ۳. تمیزکاری بکاپ‌های قدیمی
	cleanupOldBackups()

	db.LogEvent("general", "info", "📥 بکاپ آماده شد، در حال ارسال به دانلود منیجر")

	// ۴. Redirect به لینک دانلود UUID-based
	downloadURL := fmt.Sprintf("%s/backup/download/%s?t=%s", AdminBasePath(), uuid, timestamp)
	http.Redirect(w, r, downloadURL, http.StatusSeeOther)
}

// BackupFileHandler سرو فایل بکاپ با UUID (ثابت و قابل Resume)
func BackupFileHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	// استخراج UUID از URL: /admin/backup/download/{uuid}
	path := strings.TrimPrefix(r.URL.Path, AdminBasePath()+"/backup/download/")
	uuid := strings.TrimRight(path, "/")

	if uuid == "" || strings.Contains(uuid, "/") || strings.Contains(uuid, "..") {
		http.Error(w, "شناسه بکاپ نامعتبر", http.StatusBadRequest)
		return
	}

	zipPath := fmt.Sprintf("%s/%s.zip", backupDir, uuid)

	file, err := os.Open(zipPath)
	if err != nil {
		http.Error(w, "فایل بکاپ یافت نشد", http.StatusNotFound)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "خطا در خواندن اطلاعات فایل", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="vpnshop_backup_%s.zip"`, uuid))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", fmt.Sprintf(`"%s-%d"`, uuid, stat.Size()))

	http.ServeContent(w, r, "backup.zip", stat.ModTime(), file)

	db.LogEvent("general", "info", "📥 بکاپ دانلود شد")
}

// cleanupOldBackups پاک کردن بکاپ‌های قدیمی‌تر از ۲ ساعت
func cleanupOldBackups() {
	files, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-2 * time.Hour)
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if !strings.HasSuffix(f.Name(), ".zip") {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(fmt.Sprintf("%s/%s", backupDir, f.Name()))
			db.LogEventf("general", "info", "🗑️ بکاپ قدیمی حذف شد: %s", f.Name())
		}
	}
}

// StartBackupCleanup تایمر پاکسازی خودکار بکاپ‌های قدیمی
func StartBackupCleanup() {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cleanupOldBackups()
		}
	}()
}

func createBackupZip(zipPath, dbPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zw := zip.NewWriter(zipFile)
	defer zw.Close()

	if err := addFileToZip(zw, dbPath, "vpnshop.db"); err != nil {
		return err
	}

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
		if err := restoreFromDB(tmpPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case bytes.HasPrefix(data, []byte{0x50, 0x4B, 0x03, 0x04}):
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
	db.MigrateOrders()
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

	if data, err := os.ReadFile(dbTmp); err != nil || len(data) < 16 || string(data[:15]) != "SQLite format 3" {
		os.Remove(dbTmp)
		os.Remove(cfgTmp)
		return fmt.Errorf("دیتابیس داخل بکاپ معتبر نیست")
	}

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
	db.MigrateOrders()

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
