package api

import (
	"database/sql"
	"html/template"
	"net/http"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

// TrackHandler مدیریت صفحه مشتری برای دریافت کانفیگ
func TrackHandler(w http.ResponseWriter, r *http.Request) {
	// پارس کردن فایل HTML (قالب)
	tmpl, err := template.ParseFiles("templates/track.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب صفحه", http.StatusInternalServerError)
		return
	}

	// اگر درخواست GET بود (کاربر تازه وارد سایت شده)، فقط فرم خالی را نشان بده
	if r.Method == http.MethodGet {
		tmpl.Execute(w, nil)
		return
	}

	// اگر درخواست POST بود (کاربر فرم جستجو را زده است)
	if r.Method == http.MethodPost {
		trackingCode := r.FormValue("tracking_code")
		var order models.Order

		// جستجوی کد پیگیری در دیتابیس
		query := `SELECT id, tracking_code, plan_name, status, IFNULL(config_link, '') 
		          FROM orders WHERE tracking_code = ?`
		
		err := db.DB.QueryRow(query, trackingCode).Scan(
			&order.ID, &order.TrackingCode, &order.PlanName, &order.Status, &order.ConfigLink,
		)

		// اگر فاکتور پیدا نشد
		if err == sql.ErrNoRows {
			tmpl.Execute(w, map[string]interface{}{
				"Error": "فاکتوری با این کد یافت نشد. لطفاً کد را بررسی کنید.",
			})
			return
		} else if err != nil {
			tmpl.Execute(w, map[string]interface{}{
				"Error": "خطای سیستمی در ارتباط با پایگاه داده رخ داده است.",
			})
			return
		}

		// اگر فاکتور پیدا شد، اطلاعات را به قالب ارسال کن
		tmpl.Execute(w, map[string]interface{}{
			"Order": order,
		})
	}
}
