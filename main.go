package main

import (
	"log"
	
	// ایمپورت داخلی با استفاده از آدرس ریپازیتوری شما
	"github.com/asd1asd00000/vpnshop/db" 
)

func main() {
	// راه‌اندازی دیتابیس محلی
	db.InitDB("./vpnshop.db")
	defer db.DB.Close()

	log.Println("سرور VPNShop در حال راه‌اندازی است...")
}
