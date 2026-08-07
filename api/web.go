package api

import (
	"database/sql"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

// ShopHandler نمایش فروشگاه و ساخت فاکتور جدید
func ShopHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/shop.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب فروشگاه", http.StatusInternalServerError)
		return
	}

	plans, _ := models.LoadPlans()

	if r.Method == http.MethodGet {
		tmpl.Execute(w, map[string]interface{}{"Plans": plans})
		return
	}

	if r.Method == http.MethodPost {
		planID := r.FormValue("plan_id")
		
		// پیدا کردن پلن در دیتابیس متنی ما برای امنیت
		var selectedPlan *models.Plan
		for _, p := range plans {
			if p.ID == planID {
				selectedPlan = &p
				break
			}
		}

		if selectedPlan == nil {
			http.Error(w, "پلن انتخاب شده نامعتبر است", http.StatusBadRequest)
			return
		}

		// قیمت از فایل JSON خوانده می‌شود نه از HTML (امنیت مطلق)
		basePrice := selectedPlan.Price

		rand.Seed(time.Now().UnixNano())
		uniqueAmount := basePrice + rand.Intn(999) + 1
		trackingCode := fmt.Sprintf("VP%d", rand.Intn(900000)+100000)

		// آیدی پلن (مثل 20GB-1M) را در دیتابیس ذخیره می‌کنیم
		_, err = db.DB.Exec(`INSERT INTO orders (tracking_code, plan_name, base_price, unique_amount, status) 
			VALUES (?, ?, ?, ?, 'pending')`, trackingCode, selectedPlan.ID, basePrice, uniqueAmount)
		
		if err != nil {
			http.Error(w, "خطا در ثبت سفارش", http.StatusInternalServerError)
			return
		}

		order := models.Order{
			TrackingCode: trackingCode,
			PlanName:     selectedPlan.Title, // برای نمایش زیبا به کاربر
			UniqueAmount: uniqueAmount,
		}

		tmpl.Execute(w, map[string]interface{}{"CheckoutOrder": order, "Plans": plans})
	}
}

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

	tmpl.Execute(w, map[string]interface{}{"Orders": orders})
}
