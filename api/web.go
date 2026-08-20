package api

import (
	crand "crypto/rand"
	"database/sql"
	"encoding/json"
	"html/template"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"sort"
	"strconv"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

// ───────────── 🔒 محدودیت نرخ ─────────────

var (
	rateMu   sync.Mutex
	attempts = make(map[string][]time.Time)
)

const (
	rateLimitWindow = 1 * time.Minute
	rateLimitMax    = 3
)

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func allowTrackAttempt(ip string) bool {
	rateMu.Lock()
	defer rateMu.Unlock()

	now := time.Now()
	windowStart := now.Add(-rateLimitWindow)

	list := attempts[ip]
	filtered := make([]time.Time, 0, len(list))
	for _, t := range list {
		if t.After(windowStart) {
			filtered = append(filtered, t)
		}
	}

	if len(filtered) >= rateLimitMax {
		attempts[ip] = filtered
		return false
	}

	attempts[ip] = append(filtered, now)
	return true
}

// ───────────── 🎫 کد پیگیری امن ─────────────

func generateTrackingCode() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 15)
	if _, err := crand.Read(b); err != nil {
		return "VP000000000000000"
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return "VP" + string(b)
}

// ───────────── 🔐 مسیر مخفی ادمین (از متغیر محیطی) ─────────────

func AdminBasePath() string {
	secret := os.Getenv("ADMIN_SECRET_PATH")
	if secret == "" {
		return "/admin"
	}
	return "/" + secret + "/admin"
}

// ───────────── 🛒 فروشگاه ─────────────

func ShopHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/shop.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب فروشگاه", http.StatusInternalServerError)
		return
	}

	plans, _ := models.LoadPlans()

	if r.Method == http.MethodGet {
		// 🎯 اسم پنل‌ها بر اساس نقش برای نمایش در صفحه خرید
		cfg := db.GetConfig()
		panelNames := map[string]string{}
		for _, p := range cfg.Panels {
			panelNames[p.Role] = p.Name
		}
		tmpl.Execute(w, map[string]interface{}{"Plans": plans, "PanelNames": panelNames})
		return
	}

	if r.Method == http.MethodPost {
		planID := r.FormValue("plan_id")

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

		basePrice := selectedPlan.Price

		rand.Seed(time.Now().UnixNano())
		uniqueAmount := basePrice + rand.Intn(999) + 1
		trackingCode := generateTrackingCode()

		_, err = db.DB.Exec(`INSERT INTO orders (tracking_code, plan_name, base_price, unique_amount, status) 
			VALUES (?, ?, ?, ?, 'pending')`, trackingCode, selectedPlan.ID, basePrice, uniqueAmount)

		if err != nil {
			http.Error(w, "خطا در ثبت سفارش", http.StatusInternalServerError)
			return
		}

		order := models.Order{
			TrackingCode: trackingCode,
			PlanName:     selectedPlan.Title,
			UniqueAmount: uniqueAmount,
		}

		tmpl.Execute(w, map[string]interface{}{"CheckoutOrder": order, "Plans": plans})
	}
}

// ───────────── 🔍 پیگیری سفارش ─────────────

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
		ip := clientIP(r)
		if !allowTrackAttempt(ip) {
			db.LogEventf("ratelimit", "warning", "⚠️ محدودیت نرخ پیگیری برای IP: %s", ip)
			tmpl.Execute(w, map[string]interface{}{
				"Error": "تعداد تلاش‌ها بیش از حد مجاز است. لطفاً چند دقیقه بعد دوباره تلاش کنید.",
			})
			return
		}

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
// CheckOrderStatus وضعیت سفارش رو برمی‌گردونه (برای polling صفحه فاکتور)
func CheckOrderStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	trackingCode := r.URL.Query().Get("code")
	if trackingCode == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	var order models.Order
	query := `SELECT id, tracking_code, plan_name, status, IFNULL(config_link, '') 
	          FROM orders WHERE tracking_code = ?`

	err := db.DB.QueryRow(query, trackingCode).Scan(
		&order.ID, &order.TrackingCode, &order.PlanName, &order.Status, &order.ConfigLink,
	)

	if err == sql.ErrNoRows {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "not_found"}`))
		return
	} else if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if order.Status == "paid" && order.ConfigLink != "" {
		json.NewEncoder(w).Encode(map[string]string{
			"status": "paid",
			"config_link": order.ConfigLink,
		})
	} else if order.Status == "paid" {
		json.NewEncoder(w).Encode(map[string]string{
			"status": "processing",
		})
	} else {
		json.NewEncoder(w).Encode(map[string]string{
			"status": "pending",
		})
	}
}

// ───────────── 👨‍💼 داشبورد ادمین ─────────────

type adminOrder struct {
	ID             int
	TrackingCode   string
	PlanName       string
	UniqueAmount   int
	Status         string
	ConfigLink     string
	AdminConfirmed bool
	PaymentMethod  string
	Configs        []ConfigItem
}
// pageItem یک آیتم از نوار صفحه‌بندی
type pageItem struct {
	Page    int
	Current bool
	Dots    bool
}

// buildPagination لیست شماره صفحات با ... (مثل تصویر)
func buildPagination(current, total int) []pageItem {
	var items []pageItem

	if total <= 7 {
		for i := 1; i <= total; i++ {
			items = append(items, pageItem{Page: i, Current: i == current})
		}
		return items
	}

	want := map[int]bool{1: true, total: true}
	for i := current - 1; i <= current + 1; i++ {
		if i >= 1 && i <= total {
			want[i] = true
		}
	}
	var pages []int
	for p := range want {
		pages = append(pages, p)
	}
	sort.Ints(pages)

	prev := 0
	for _, p := range pages {
		if prev != 0 && p-prev > 1 {
			items = append(items, pageItem{Dots: true})
		}
		items = append(items, pageItem{Page: p, Current: p == current})
		prev = p
	}
	return items
}

func checkAdminAuth(w http.ResponseWriter, r *http.Request) bool {
	// اولویت ۱: از config.json
	cfg := db.GetConfig()
	if cfg.Admin.Username != "" && cfg.Admin.Password != "" {
		user, pass, ok := r.BasicAuth()
		if !ok || user != cfg.Admin.Username || pass != cfg.Admin.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted Admin Dashboard"`)
			http.Error(w, "دسترسی غیرمجاز", http.StatusUnauthorized)
			return false
		}
		return true
	}

	// اولویت ۲: از متغیرهای محیطی (fallback)
	adminUser := os.Getenv("ADMIN_USER")
	adminPass := os.Getenv("ADMIN_PASS")

	if adminUser == "" {
		adminUser = "admin"
	}
	if adminPass == "" {
		adminPass = "123456"
	}

	user, pass, ok := r.BasicAuth()
	if !ok || user != adminUser || pass != adminPass {
		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted Admin Dashboard"`)
		http.Error(w, "دسترسی غیرمجاز", http.StatusUnauthorized)
		return false
	}
	return true
}

func AdminHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	tmpl, err := template.ParseFiles("templates/admin.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب ادمین", http.StatusInternalServerError)
		return
	}

	rows, err := db.DB.Query(`SELECT id, tracking_code, plan_name, unique_amount, status, IFNULL(config_link, ''), IFNULL(admin_confirmed, 0) FROM orders ORDER BY id DESC`)
	if err != nil {
		http.Error(w, "خطا در خواندن دیتابیس", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var orders []adminOrder
	for rows.Next() {
		var o adminOrder
		var confirmed int
		err := rows.Scan(&o.ID, &o.TrackingCode, &o.PlanName, &o.UniqueAmount, &o.Status, &o.ConfigLink, &confirmed)
		if err != nil {
			continue
		}

		o.AdminConfirmed = confirmed == 1

		// پارس JSON کانفیگ‌ها برای نمایش جداگانه
		if o.ConfigLink != "" {
			var items []ConfigItem
			if jerr := json.Unmarshal([]byte(o.ConfigLink), &items); jerr == nil && len(items) > 0 {
				o.Configs = items
			}
		}

		orders = append(orders, o)
	}

	tmpl.Execute(w, map[string]interface{}{"Orders": orders, "AdminBase": AdminBasePath()})
}
