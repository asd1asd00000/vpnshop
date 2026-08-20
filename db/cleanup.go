package db

import (
	"log"
	"time"
)

// StartOrderCleanup مایگریشن ستون‌های جدید + حذف خودکار سفارش‌های قدیمی
func StartOrderCleanup() {
	// مایگریشن: ستون created_at (اگه نباشه)
	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN created_at DATETIME DEFAULT CURRENT_TIMESTAMP`); err == nil {
		log.Println("✅ ستون created_at به جدول orders اضافه شد")
	}
	DB.Exec(`UPDATE orders SET created_at = datetime('now') WHERE created_at IS NULL`)

	// مایگریشن: ستون payment_method (اگه نباشه)
	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN payment_method TEXT DEFAULT ''`); err == nil {
		log.Println("✅ ستون payment_method به جدول orders اضافه شد")
	}

	cleanOldOrders()

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleanOldOrders()
		}
	}()
}

// cleanOldOrders حذف سفارش‌های پرداخت‌نشده قدیمی‌تر از 48 ساعت
func cleanOldOrders() {
	res, err := DB.Exec(`DELETE FROM orders WHERE status != 'paid' AND created_at IS NOT NULL AND created_at < datetime('now', '-48 hours')`)
	if err != nil {
		log.Printf("❌ خطا در پاکسازی سفارش‌های قدیمی: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("🗑️ %d سفارش پرداخت‌نشده قدیمی‌تر از 48 ساعت حذف شد", n)
		LogEventf("general", "warning", "🗑️ %d سفارش پرداخت‌نشده قدیمی به صورت خودکار حذف شد", n)
	}
}
