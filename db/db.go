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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = DB.Exec(createTableQuery)
	if err != nil {
		log.Fatalf("خطا در ساخت جدول سفارشات: %v", err)
	}

	log.Println("دیتابیس متصل شد و جدول orders بررسی/ایجاد گردید.")
}
