package db

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

// InitDB دیتابیس را مقداردهی و جدول را ایجاد می‌کند
func InitDB(dataSourceName string) {
	var err error
	DB, err = sql.Open("sqlite3", dataSourceName)
	if err != nil {
		log.Fatalf("خطا در اتصال به دیتابیس: %v", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatalf("دیتابیس پاسخ نمی‌دهد: %v", err)
	}

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS orders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tracking_code TEXT UNIQUE NOT NULL,
		plan_name TEXT NOT NULL,
		base_price INTEGER NOT NULL,
		unique_amount INTEGER UNIQUE NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		config_link TEXT,
		admin_confirmed INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = DB.Exec(createTableQuery)
	if err != nil {
		log.Fatalf("خطا در ساخت جدول سفارشات: %v", err)
	}

	// ✅ مایگریشن: اگه ستون admin_confirmed روی دیتابیس قدیمی نیست، اضافه‌ش کن
	var colCount int
	err = DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('orders') WHERE name = 'admin_confirmed'`).Scan(&colCount)
	if err == nil && colCount == 0 {
		if _, err := DB.Exec(`ALTER TABLE orders ADD COLUMN admin_confirmed INTEGER NOT NULL DEFAULT 0`); err != nil {
			log.Printf("❌ خطا در افزودن ستون admin_confirmed: %v", err)
		} else {
			log.Println("✅ ستون admin_confirmed به جدول orders اضافه شد.")
		}
	}

	log.Println("دیتابیس متصل شد و جدول orders بررسی/ایجاد گردید.")
}
