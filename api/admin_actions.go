package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
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

	_, err := db.DB.Exec(
		`UPDATE orders SET admin_confirmed = ? WHERE id = ?`,
		val,
		req.ID,
	)
	if err != nil {
		http.Error(w, "خطا در بروزرسانی", http.StatusInternalServerError)
		return
	}

	db.LogEventf(
		"general",
		"info",
		"🖱️ تایید ادمین برای فاکتور #%d: %v",
		req.ID,
		req.Confirmed,
	)

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

	_, err := db.DB.Exec(
		`UPDATE orders SET admin_note = ? WHERE id = ?`,
		req.Note,
		req.ID,
	)
	if err != nil {
		http.Error(w, "خطا در ذخیره", http.StatusInternalServerError)
		return
	}

	db.LogEventf(
		"general",
		"info",
		"📝 یادداشت ادمین برای فاکتور #%d بروزرسانی شد",
		req.ID,
	)

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
		FROM orders 
		WHERE id = ?`,
		req.ID,
	).Scan(
		&order.ID,
		&order.TrackingCode,
		&order.PlanName,
		&order.Status,
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
		db.LogEventf(
			"config",
			"error",
			"❌ خطا در ساخت کانفیگ برای سفارش #%d: %v",
			req.ID,
			err,
		)

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
		WHERE id = ?`,
		configLink,
		req.ID,
	)

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "خطا در بروزرسانی دیتابیس",
		})
		return
	}

	db.LogEventf(
		"config",
		"success",
		"✅ تایید دستی پرداخت سفارش #%d توسط ادمین + ساخت کانفیگ",
		req.ID,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "پرداخت تایید و کانفیگ ساخته شد",
	})
}

// ───────────── 📦 بکاپ کامل ZIP ─────────────

const backupDir = "./backups"
const latestBackupName = "vpnshop_backup_latest.zip"

// جلوگیری از اجرای همزمان عملیات ساخت/آپدیت بکاپ
var backupMu sync.Mutex

func BackupHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	// جلوگیری از اینکه دو درخواست همزمان فایل بکاپ را تغییر دهند
	backupMu.Lock()
	defer backupMu.Unlock()

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		http.Error(
			w,
			"خطا در ساخت پوشه بکاپ",
			http.StatusInternalServerError,
		)
		return
	}

	timestamp := time.Now().Format("20060102_150405")

	// فایل دیتابیس موقت
	tmpDB := fmt.Sprintf(
		"%s/.vpnshop_backup_%s.db",
		backupDir,
		timestamp,
	)

	// ZIP موقت
	tmpZip := fmt.Sprintf(
		"%s/.vpnshop_backup_%s.tmp.zip",
		backupDir,
		timestamp,
	)

	// ZIP نهایی timestampدار
	finalZip := fmt.Sprintf(
		"%s/vpnshop_backup_%s.zip",
		backupDir,
		timestamp,
	)

	// latest
	latestZip := fmt.Sprintf(
		"%s/%s",
		backupDir,
		latestBackupName,
	)

	// پاک‌سازی فایل‌های موقت در صورت خروج
	defer os.Remove(tmpDB)
	defer os.Remove(tmpZip)

	// ───────────── 1. ساخت Snapshot از دیتابیس ─────────────

	// مسیر را برای استفاده داخل SQL ایمن می‌کنیم
	safeTmpDB := strings.ReplaceAll(tmpDB, "'", "''")

	_, err := db.DB.Exec(
		fmt.Sprintf("VACUUM INTO '%s'", safeTmpDB),
	)

	if err != nil {
		db.LogEventf(
			"general",
			"error",
			"❌ خطا در گرفتن بکاپ دیتابیس: %v",
			err,
		)

		http.Error(
			w,
			"خطا در گرفتن بکاپ",
			http.StatusInternalServerError,
		)
		return
	}

	// اطمینان از وجود فایل دیتابیس
	dbStat, err := os.Stat(tmpDB)
	if err != nil || dbStat.Size() == 0 {
		db.LogEvent(
			"general",
			"error",
			"❌ فایل دیتابیس بکاپ ساخته نشد یا خالی است",
		)

		http.Error(
			w,
			"فایل بکاپ دیتابیس معتبر نیست",
			http.StatusInternalServerError,
		)
		return
	}

	// ───────────── 2. ساخت ZIP در فایل موقت ─────────────

	if err := createBackupZip(tmpZip, tmpDB); err != nil {
		db.LogEventf(
			"general",
			"error",
			"❌ خطا در ساخت ZIP بکاپ: %v",
			err,
		)

		http.Error(
			w,
			"خطا در ساخت فایل زیپ",
			http.StatusInternalServerError,
		)
		return
	}

	// ───────────── 3. بررسی ZIP ساخته‌شده ─────────────

	zipStat, err := os.Stat(tmpZip)
	if err != nil {
		db.LogEventf(
			"general",
			"error",
			"❌ فایل ZIP ساخته نشد: %v",
			err,
		)

		http.Error(
			w,
			"فایل ZIP معتبر نیست",
			http.StatusInternalServerError,
		)
		return
	}

	if zipStat.Size() == 0 {
		db.LogEvent(
			"general",
			"error",
			"❌ فایل ZIP خالی است",
		)

		http.Error(
			w,
			"فایل ZIP خالی است",
			http.StatusInternalServerError,
		)
		return
	}

	// ───────────── 4. نهایی کردن ZIP ─────────────
	//
	// تا اینجا فایل ZIP کاملاً بسته شده.
	// Rename اتمیک باعث می‌شود فایل نهایی هیچ‌وقت
	// در حالت نیمه‌ساخته دیده نشود.

	if err := os.Rename(tmpZip, finalZip); err != nil {
		db.LogEventf(
			"general",
			"error",
			"❌ خطا در نهایی کردن ZIP: %v",
			err,
		)

		http.Error(
			w,
			"خطا در نهایی کردن فایل بکاپ",
			http.StatusInternalServerError,
		)
		return
	}

	// tmpZip دیگر وجود ندارد
	// پس defer os.Remove(tmpZip) مشکلی ایجاد نمی‌کند.

	// ───────────── 5. بروزرسانی latest ─────────────

	// ابتدا یک کپی موقت برای latest می‌سازیم.
	// سپس با Rename آن را جایگزین می‌کنیم تا
	// latest هیچ‌وقت فایل ناقص نباشد.

	latestTmp := fmt.Sprintf(
		"%s/.%s.tmp",
		backupDir,
		latestBackupName,
	)

	defer os.Remove(latestTmp)

	if err := copyFile(finalZip, latestTmp); err != nil {
		db.LogEventf(
			"general",
			"error",
			"❌ خطا در ساخت latest backup: %v",
			err,
		)

		http.Error(
			w,
			"خطا در ساخت آخرین بکاپ",
			http.StatusInternalServerError,
		)
		return
	}

	if err := os.Rename(latestTmp, latestZip); err != nil {
		db.LogEventf(
			"general",
			"error",
			"❌ خطا در نهایی کردن latest backup: %v",
			err,
		)

		http.Error(
			w,
			"خطا در نهایی کردن آخرین بکاپ",
			http.StatusInternalServerError,
		)
		return
	}

	// ───────────── 6. پاک‌سازی بکاپ‌های قدیمی ─────────────

	cleanupOldBackups(3)

	// ───────────── 7. باز کردن فایل نهایی برای دانلود ─────────────

	file, err := os.Open(finalZip)
	if err != nil {
		db.LogEventf(
			"general",
			"error",
			"❌ خطا در باز کردن فایل بکاپ: %v",
			err,
		)

		http.Error(
			w,
			"خطا در باز کردن فایل",
			http.StatusInternalServerError,
		)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.Error(
			w,
			"خطا در خواندن اطلاعات فایل",
			http.StatusInternalServerError,
		)
		return
	}

	fileSize := stat.Size()

	if fileSize <= 0 {
		http.Error(
			w,
			"فایل بکاپ خالی است",
			http.StatusInternalServerError,
		)
		return
	}

	// ───────────── 8. Headers دانلود ─────────────

	w.Header().Set(
		"Content-Type",
		"application/zip",
	)

	w.Header().Set(
		"Content-Length",
		fmt.Sprintf("%d", fileSize),
	)

	w.Header().Set(
		"Content-Disposition",
		fmt.Sprintf(
			`attachment; filename="vpnshop_backup_%s.zip"`,
			timestamp,
		),
	)

	w.Header().Set(
		"Cache-Control",
		"no-cache, no-store, must-revalidate",
	)

	w.Header().Set(
		"Pragma",
		"no-cache",
	)

	w.Header().Set(
		"Expires",
		"0",
	)

	// Accept-Ranges را عمداً حذف کردیم.
	//
	// چون برای فایل بکاپ کوچک/متوسط نیازی به Range نداریم
	// و حذف آن احتمال مشکلات دانلود ناقص یا تغییر سایز
	// در بعضی Proxyها را کمتر می‌کند.

	// ───────────── 9. ارسال فایل ─────────────

	written, err := io.Copy(w, file)

	if err != nil {
		db.LogEventf(
			"general",
			"error",
			"❌ خطا در ارسال فایل بکاپ: %v",
			err,
		)
		return
	}

	// بررسی اینکه دقیقاً همان تعداد بایت ارسال شده
	if written != fileSize {
		db.LogEventf(
			"general",
			"error",
			"❌ دانلود ناقص بکاپ: expected=%d written=%d",
			fileSize,
			written,
		)
		return
	}

	db.LogEventf(
		"general",
		"info",
		"📥 بکاپ کامل دانلود شد - حجم: %d bytes",
		fileSize,
	)
}

// ───────────── پاک‌سازی بکاپ‌های قدیمی ─────────────

func cleanupOldBackups(keep int) {
	files, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	type backupFile struct {
		name    string
		modTime time.Time
	}

	var backups []backupFile

	for _, f := range files {
		name := f.Name()

		if f.IsDir() {
			continue
		}

		// latest هیچ‌وقت حذف نشود
		if name == latestBackupName {
			continue
		}

		// فایل‌های موقت را حذف کن
		if strings.HasPrefix(name, ".vpnshop_backup_") {
			os.Remove(fmt.Sprintf("%s/%s", backupDir, name))
			continue
		}

		if strings.HasPrefix(name, "vpnshop_backup_") &&
			strings.HasSuffix(name, ".zip") {

			info, err := f.Info()
			if err != nil {
				continue
			}

			backups = append(backups, backupFile{
				name:    name,
				modTime: info.ModTime(),
			})
		}
	}

	// جدیدترین‌ها اول
	for i := 0; i < len(backups); i++ {
		for j := i + 1; j < len(backups); j++ {
			if backups[j].modTime.After(backups[i].modTime) {
				backups[i], backups[j] = backups[j], backups[i]
			}
		}
	}

	if len(backups) <= keep {
		return
	}

	for i := keep; i < len(backups); i++ {
		path := fmt.Sprintf(
			"%s/%s",
			backupDir,
			backups[i].name,
		)

		if err := os.Remove(path); err != nil {
			db.LogEventf(
				"general",
				"warning",
				"⚠️ خطا در حذف بکاپ قدیمی %s: %v",
				backups[i].name,
				err,
			)
		}
	}
}

// ───────────── ساخت ZIP ─────────────

func createBackupZip(zipPath, dbPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}

	// اگر هر اتفاقی افتاد، فایل ZIP ناقص پاک شود.
	success := false

	defer func() {
		if !success {
			os.Remove(zipPath)
		}
	}()

	zw := zip.NewWriter(zipFile)

	// اضافه کردن دیتابیس
	if err := addFileToZip(
		zw,
		dbPath,
		"vpnshop.db",
	); err != nil {
		zw.Close()
		zipFile.Close()
		return err
	}

	// اضافه کردن config.json در صورت وجود
	if _, err := os.Stat("./config.json"); err == nil {
		if err := addFileToZip(
			zw,
			"./config.json",
			"config.json",
		); err != nil {
			zw.Close()
			zipFile.Close()
			return err
		}
	}

	// ───────────── بسیار مهم ─────────────
	//
	// ابتدا ZIP Writer را Close می‌کنیم تا
	// Central Directory داخل فایل نوشته شود.

	if err := zw.Close(); err != nil {
		zipFile.Close()
		return fmt.Errorf("خطا در بستن zip writer: %w", err)
	}

	// سپس خود فایل را Close می‌کنیم
	if err := zipFile.Close(); err != nil {
		return fmt.Errorf("خطا در بستن فایل zip: %w", err)
	}

	success = true

	return nil
}

// ───────────── اضافه کردن فایل به ZIP ─────────────

func addFileToZip(
	zw *zip.Writer,
	srcPath string,
	nameInZip string,
) error {

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return err
	}

	if info.IsDir() {
		return fmt.Errorf("%s یک فایل نیست", srcPath)
	}

	w, err := zw.Create(nameInZip)
	if err != nil {
		return err
	}

	if _, err := io.Copy(w, src); err != nil {
		return err
	}

	return nil
}

// ───────────── کپی امن فایل ─────────────

func copyFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	srcInfo, err := src.Stat()
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return fmt.Errorf("%s یک فایل نیست", srcPath)
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}

	success := false

	defer func() {
		if !success {
			dst.Close()
			os.Remove(dstPath)
		}
	}()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	if err := dst.Close(); err != nil {
		return err
	}

	success = true

	return nil
}

// ───────────── 📤 ریستور (ZIP یا DB) ─────────────

func RestoreHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(
			w,
			"خطا در پردازش فرم",
			http.StatusBadRequest,
		)
		return
	}

	file, _, err := r.FormFile("backup_file")
	if err != nil {
		http.Error(
			w,
			"فایلی دریافت نشد",
			http.StatusBadRequest,
		)
		return
	}
	defer file.Close()

	tmpPath := "./restore_tmp_upload"

	dst, err := os.Create(tmpPath)
	if err != nil {
		http.Error(
			w,
			"خطا در ذخیره فایل موقت",
			http.StatusInternalServerError,
		)
		return
	}

	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.Remove(tmpPath)

		http.Error(
			w,
			"خطا در کپی فایل",
			http.StatusInternalServerError,
		)
		return
	}

	if err := dst.Close(); err != nil {
		os.Remove(tmpPath)

		http.Error(
			w,
			"خطا در بستن فایل",
			http.StatusInternalServerError,
		)
		return
	}

	defer os.Remove(tmpPath)

	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) < 4 {
		http.Error(
			w,
			"فایل معتبر نیست",
			http.StatusBadRequest,
		)
		return
	}

	switch {
	case bytes.HasPrefix(
		data,
		[]byte("SQLite format 3"),
	):

		if err := restoreFromDB(tmpPath); err != nil {
			http.Error(
				w,
				err.Error(),
				http.StatusInternalServerError,
			)
			return
		}

	case bytes.HasPrefix(
		data,
		[]byte{0x50, 0x4B, 0x03, 0x04},
	):

		if err := restoreFromZip(tmpPath); err != nil {
			http.Error(
				w,
				err.Error(),
				http.StatusInternalServerError,
			)
			return
		}

	default:

		http.Error(
			w,
			"فایل معتبر نیست (باید .zip یا .db باشد)",
			http.StatusBadRequest,
		)
		return
	}

	http.Redirect(
		w,
		r,
		AdminBasePath(),
		http.StatusSeeOther,
	)
}

// ───────────── Restore DB ─────────────

func restoreFromDB(tmpPath string) error {
	if cur, err := os.ReadFile("./vpnshop.db"); err == nil {
		if err := os.WriteFile(
			"./vpnshop.db.before_restore",
			cur,
			0644,
		); err != nil {
			return fmt.Errorf(
				"خطا در ساخت بکاپ قبل از restore: %w",
				err,
			)
		}
	}

	db.DB.Close()

	if err := os.Rename(
		tmpPath,
		"./vpnshop.db",
	); err != nil {
		return err
	}

	db.InitDB("./vpnshop.db")
	db.MigrateOrders()

	db.LogEvent(
		"general",
		"success",
		"♻️ دیتابیس از بکاپ بازگردانی شد",
	)

	return nil
}

// ───────────── Restore ZIP ─────────────

func restoreFromZip(zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	var dbTmp string
	var cfgTmp string

	for _, f := range zr.File {

		switch f.Name {

		case "vpnshop.db":

			dbTmp = "./restore_db_tmp"

			if err := extractZipFile(
				f,
				dbTmp,
			); err != nil {
				return err
			}

		case "config.json":

			cfgTmp = "./restore_cfg_tmp"

			if err := extractZipFile(
				f,
				cfgTmp,
			); err != nil {
				return err
			}
		}
	}

	if dbTmp == "" {
		return fmt.Errorf(
			"فایل vpnshop.db در بکاپ یافت نشد",
		)
	}

	// بررسی معتبر بودن دیتابیس
	data, err := os.ReadFile(dbTmp)

	if err != nil {
		os.Remove(dbTmp)
		os.Remove(cfgTmp)

		return fmt.Errorf(
			"خطا در خواندن دیتابیس بکاپ: %w",
			err,
		)
	}

	if len(data) < 16 ||
		string(data[:15]) != "SQLite format 3" {

		os.Remove(dbTmp)
		os.Remove(cfgTmp)

		return fmt.Errorf(
			"دیتابیس داخل بکاپ معتبر نیست",
		)
	}

	// بکاپ قبل از restore
	if cur, err := os.ReadFile("./vpnshop.db"); err == nil {
		if err := os.WriteFile(
			"./vpnshop.db.before_restore",
			cur,
			0644,
		); err != nil {
			os.Remove(dbTmp)
			os.Remove(cfgTmp)

			return fmt.Errorf(
				"خطا در بکاپ دیتابیس فعلی: %w",
				err,
			)
		}
	}

	if cur, err := os.ReadFile("./config.json"); err == nil {
		if err := os.WriteFile(
			"./config.json.before_restore",
			cur,
			0644,
		); err != nil {
			os.Remove(dbTmp)
			os.Remove(cfgTmp)

			return fmt.Errorf(
				"خطا در بکاپ config فعلی: %w",
				err,
			)
		}
	}

	db.DB.Close()

	if err := os.Rename(
		dbTmp,
		"./vpnshop.db",
	); err != nil {
		os.Remove(dbTmp)
		os.Remove(cfgTmp)

		return err
	}

	if cfgTmp != "" {
		if err := os.Rename(
			cfgTmp,
			"./config.json",
		); err != nil {
			return err
		}
	}

	db.InitDB("./vpnshop.db")
	db.LoadConfig()
	db.MigrateOrders()

	db.LogEvent(
		"general",
		"success",
		"♻️ بکاپ کامل (دیتابیس + تنظیمات) بازگردانی شد",
	)

	return nil
}

// ───────────── استخراج فایل ZIP ─────────────

func extractZipFile(
	f *zip.File,
	dest string,
) error {

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}

	success := false

	defer func() {
		if !success {
			out.Close()
			os.Remove(dest)
		}
	}()

	if _, err := io.Copy(out, rc); err != nil {
		return err
	}

	if err := out.Close(); err != nil {
		return err
	}

	success = true

	return nil
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

	rows, err := db.DB.Query(`
		SELECT id, category, level, message, created_at
		FROM logs
		ORDER BY id DESC
		LIMIT 500
	`)

	if err != nil {
		http.Error(
			w,
			"خطا در خواندن لاگ‌ها",
			http.StatusInternalServerError,
		)
		return
	}

	defer rows.Close()

	logs := make([]logEntry, 0)

	for rows.Next() {
		var l logEntry

		if err := rows.Scan(
			&l.ID,
			&l.Category,
			&l.Level,
			&l.Message,
			&l.CreatedAt,
		); err == nil {
			logs = append(logs, l)
		}
	}

	if err := rows.Err(); err != nil {
		http.Error(
			w,
			"خطا در خواندن لاگ‌ها",
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	json.NewEncoder(w).Encode(logs)
}

// ───────────── پاک کردن لاگ‌ها ─────────────

func AdminLogsClearHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	if _, err := db.DB.Exec(`DELETE FROM logs`); err != nil {
		http.Error(
			w,
			"خطا در پاک کردن لاگ‌ها",
			http.StatusInternalServerError,
		)
		return
	}

	db.LogEvent(
		"general",
		"warning",
		"🗑️ لاگ‌ها توسط ادمین پاک شدند",
	)

	w.WriteHeader(http.StatusOK)
}
