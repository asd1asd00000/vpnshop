package db

import (
	"fmt"
	"log"
	"time"
)

// LogEvent لاگ رو هم در کنسول و هم در دیتابیس ثبت می‌کنه
func LogEvent(category, level, message string) {
	log.Printf("%s", message)

	if DB == nil {
		return
	}
	_, err := DB.Exec(`INSERT INTO logs (category, level, message) VALUES (?, ?, ?)`, category, level, message)
	if err != nil {
		log.Printf("خطا در ثبت لاگ: %v", err)
	}
}

// LogEventf نسخه فرمت‌دار LogEvent
func LogEventf(category, level, format string, args ...interface{}) {
	LogEvent(category, level, fmt.Sprintf(format, args...))
}

// CleanupLogs پاکسازی بر اساس سیاست نگهداری
func CleanupLogs() {
	if DB == nil {
		return
	}
	// لاگ‌های عمومی: فقط ۷۲ ساعت
	DB.Exec(`DELETE FROM logs WHERE category = 'general' AND created_at < datetime('now', '-72 hours')`)
	// لاگ‌های مهم (تراکنش/کانفیگ/محدودیت نرخ): ۳۰ روز
	DB.Exec(`DELETE FROM logs WHERE category != 'general' AND created_at < datetime('now', '-30 days')`)
}

// StartLogCleanup پاکسازی خودکار هر ساعت
func StartLogCleanup() {
	CleanupLogs()
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			CleanupLogs()
		}
	}()
}
