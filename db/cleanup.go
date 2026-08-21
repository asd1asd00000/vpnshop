package db

import (
	"log"
	"time"
	"fmt"
)

var TehranTZ *time.Location

func init() {
	var err error
	TehranTZ, err = time.LoadLocation("Asia/Tehran")
	if err != nil {
		// fallback: +3:30 fixed
		TehranTZ = time.FixedZone("Tehran", 12600)
	}
}

// TehranNow زمان فعلی به وقت تهران
func TehranNow() time.Time {
	return time.Now().In(TehranTZ)
}

// FormatTehranUTC زمان UTC ذخیره‌شده در SQLite رو به وقت تهران و فرمت شمسی/میلادی تبدیل می‌کنه
// FormatTehranUTC زمان UTC رو به وقت تهران + تاریخ شمسی تبدیل می‌کنه
func FormatTehranUTC(s string) string {
	if s == "" {
		return ""
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	}
	var t time.Time
	var err error
	for _, l := range layouts {
		t, err = time.Parse(l, s)
		if err == nil {
			break
		}
	}
	if err != nil {
		return s
	}
	tt := t.In(TehranTZ)
	jy, jm, jd := gregorianToJalali(tt.Year(), int(tt.Month()), tt.Day())
	return fmt.Sprintf("%d/%02d/%02d - %02d:%02d", jy, jm, jd, tt.Hour(), tt.Minute())
}

// gregorianToJalali تبدیل میلادی به شمسی (الگوریتم استاندارد jalaali)
func gregorianToJalali(gy, gm, gd int) (int, int, int) {
	gdm := []int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	gy2 := gy
	if gm > 2 {
		gy2 = gy + 1
	}
	days := 355666 + 365*gy + (gy2+3)/4 - (gy2+99)/100 + (gy2+399)/400 + gd + gdm[gm-1]
	jy := -1595 + 33*(days/12053)
	days %= 12053
	jy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		jy += (days - 1) / 365
		days = (days - 1) % 365
	}
	var jm, jd int
	if days < 186 {
		jm = 1 + days/31
		jd = 1 + days%31
	} else {
		jm = 7 + (days-186)/30
		jd = 1 + (days-186)%30
	}
	return jy, jm, jd
}

// MigrateOrders ستون‌های جدید رو اضافه می‌کنه
func MigrateOrders() {
	// created_at
	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN created_at DATETIME DEFAULT CURRENT_TIMESTAMP`); err == nil {
		log.Println("✅ ستون created_at به جدول orders اضافه شد")
	}
	DB.Exec(`UPDATE orders SET created_at = datetime('now') WHERE created_at IS NULL`)

	// payment_method
	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN payment_method TEXT DEFAULT ''`); err == nil {
		log.Println("✅ ستون payment_method به جدول orders اضافه شد")
	}

	// paid_at (جدید)
	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN paid_at DATETIME`); err == nil {
		log.Println("✅ ستون paid_at به جدول orders اضافه شد")
	}
	// برای سفارش‌های paid قبلی، paid_at رو از created_at پر می‌کنیم (تخمین)
	DB.Exec(`UPDATE orders SET paid_at = created_at WHERE status = 'paid' AND paid_at IS NULL`)
}

// StartOrderCleanup حذف خودکار + مایگریشن
func StartOrderCleanup() {
	MigrateOrders()
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
		log.Printf("❌ خطا در پاکسازی: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("🗑️ %d سفارش پرداخت‌نشده قدیمی‌تر از 48 ساعت حذف شد", n)
		LogEventf("general", "warning", "🗑️ %d سفارش پرداخت‌نشده قدیمی به صورت خودکار حذف شد", n)
	}
}
