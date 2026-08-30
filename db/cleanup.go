package db

import (
	"fmt"
	"log"
	"time"
)

var TehranTZ *time.Location

func init() {
	var err error
	TehranTZ, err = time.LoadLocation("Asia/Tehran")
	if err != nil {
		TehranTZ = time.FixedZone("Tehran", 12600)
	}
}

func TehranNow() time.Time {
	return time.Now().In(TehranTZ)
}

// 🎯 مهلت نگهداری فاکتور پرداخت‌شده بعد از انقضا (ثابت)
const PaidGraceDays = 10

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

// parseUTC برای محاسبات انقضا
func parseUTC(s string) (time.Time, bool) {
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func gregorianToJalali(gy, gm, gd int) (int, int, int) {
	gdm := []int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	gy2 := gy
	if gm > 2 {
		gy2 = gy + 1
	}
	days := 355666 + 365*gy + (gy2+3)/4 - (gy2+99)/100 + (gy2+399)/400 + gd + gdm[gm-1]
	jy := -1595 + 33 * (days / 12053)
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

// MigrateOrders ستون‌های جدید رو اگه نباشن اضافه می‌کنه
func MigrateOrders() {
	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN created_at DATETIME DEFAULT CURRENT_TIMESTAMP`); err == nil {
		log.Println("✅ ستون created_at به جدول orders اضافه شد")
	}
	DB.Exec(`UPDATE orders SET created_at = datetime('now') WHERE created_at IS NULL`)

	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN payment_method TEXT DEFAULT ''`); err == nil {
		log.Println("✅ ستون payment_method به جدول orders اضافه شد")
	}

	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN paid_at DATETIME`); err == nil {
		log.Println("✅ ستون paid_at به جدول orders اضافه شد")
	}
	DB.Exec(`UPDATE orders SET paid_at = created_at WHERE status = 'paid' AND paid_at IS NULL`)

	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN admin_note TEXT DEFAULT ''`); err == nil {
		log.Println("✅ ستون admin_note به جدول orders اضافه شد")
	}

	// 🎯 ستون‌های آرشیو
	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN plan_days INTEGER DEFAULT 0`); err == nil {
		log.Println("✅ ستون plan_days به جدول orders اضافه شد")
	}
	if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN archived INTEGER DEFAULT 0`); err == nil {
		log.Println("✅ ستون archived به جدول orders اضافه شد")
	}
}

// StartOrderCleanup مایگریشن + پاکسازی‌ها
func StartOrderCleanup() {
	MigrateOrders()
	cleanOldOrders()
	cleanExpiredPaidOrders()

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleanOldOrders()
			cleanExpiredPaidOrders()
		}
	}()
}

// cleanOldOrders حذف سفارش‌های پرداخت‌نشده قدیمی
func cleanOldOrders() {
	hours := GetConfig().Cleanup.OrderExpireHours
	if hours <= 0 {
		hours = 48
	}

	query := fmt.Sprintf(`DELETE FROM orders WHERE status != 'paid' AND created_at IS NOT NULL AND created_at < datetime('now', '-%d hours')`, hours)
	res, err := DB.Exec(query)
	if err != nil {
		log.Printf("❌ خطا در پاکسازی: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("🗑️ %d سفارش پرداخت‌نشده قدیمی‌تر از %d ساعت حذف شد", n, hours)
		LogEventf("general", "warning", "🗑️ %d سفارش پرداخت‌نشده قدیمی به صورت خودکار حذف شد", n)
	}
}

// 🎯 cleanExpiredPaidOrders آرشیو فاکتورهای پرداخت‌شده‌ی منقضی
func cleanExpiredPaidOrders() {
	rows, err := DB.Query(`SELECT id, tracking_code, plan_days, paid_at FROM orders WHERE status = 'paid' AND IFNULL(archived, 0) = 0 AND paid_at IS NOT NULL`)
	if err != nil {
		log.Printf("❌ خطا در خواندن فاکتورهای پرداخت‌شده: %v", err)
		return
	}
	defer rows.Close()

	type exp struct {
		id     int
		code   string
	}
	var toArchive []exp

	for rows.Next() {
		var id, planDays int
		var code, paidAt string
		if err := rows.Scan(&id, &code, &planDays, &paidAt); err != nil {
			continue
		}

		t, ok := parseUTC(paidAt)
		if !ok {
			continue // احتیاط: اگه تاریخ نامعتبر بود، دست نزن
		}

		days := planDays
		if days <= 0 {
			days = 30 // fallback برای فاکتورهای قدیمی
		}

		expiry := t.AddDate(0, 0, days+PaidGraceDays)
		if time.Now().After(expiry) {
			toArchive = append(toArchive, exp{id: id, code: code})
		}
	}

	for _, o := range toArchive {
		if _, err := DB.Exec(`UPDATE orders SET archived = 1 WHERE id = ?`, o.id); err == nil {
			log.Printf("📦 فاکتور #%d (%s) منقضی شد و آرشیو گردید", o.id, o.code)
			LogEventf("general", "info", "📦 فاکتور #%d پس از انقضا آرشیو شد (برای آمار درآمد نگه داشته شد)", o.id)
		}
	}

	if len(toArchive) > 0 {
		log.Printf("📦 %d فاکتور پرداخت‌شده آرشیو شد", len(toArchive))
	}
}
