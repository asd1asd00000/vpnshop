package api

import (
	"database/sql"
	"html/template"
	"net/http"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

// TrackHandler مدیریت صفحه مشتری
func TrackHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/track.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب صفحه", http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodGet {
		tmpl.Execute(w, nil)
		return
	}

	if r.Method == http.MethodPost {
		trackingCode := r.FormValue("tracking_code")
		var order models.Order

		query := `SELECT id, tracking_code, plan_name, status, IFNULL(config_link, '') 
		          FROM orders WHERE tracking_code = ?`
		
		err := db.DB.QueryRow(query, trackingCode).Scan(
			&order.ID, &order.TrackingCode, &order.PlanName, &order.Status, &order.ConfigLink,
		)

		if err == sql.ErrNoRows {
			tmpl.Execute(w, map[string]interface{}{"Error": "فاکتوری با این کد یافت نشد."})
			return
		} else if err != nil {
			tmpl.Execute(w, map[string]interface{}{"Error": "خطای سیستمی رخ داده است."})
			return
		}

		tmpl.Execute(w, map[string]interface{}{"Order": order})
	}
}

// AdminHandler مدیریت داشبورد ادمین
func AdminHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/admin.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب ادمین", http.StatusInternalServerError)
		return
	}

	// خواندن تمام فاکتورها از جدیدترین به قدیمی‌ترین
	rows, err := db.DB.Query(`SELECT id, tracking_code, plan_name, unique_amount, status, IFNULL(config_link, '') FROM orders ORDER BY id DESC`)
	if err != nil {
		http.Error(w, "خطا در خواندن دیتابیس", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var orders []models.Order
	for rows.Next() {
		var o models.Order
		err := rows.Scan(&o.ID, &o.TrackingCode, &o.PlanName, &o.UniqueAmount, &o.Status, &o.ConfigLink)
		if err == nil {
			orders = append(orders, o)
		}
	}

	tmpl.Execute(w, map[string]interface{}{
		"Orders": orders,
	})
}
