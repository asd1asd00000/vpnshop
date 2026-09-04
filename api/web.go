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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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

// ───────────── 🔐 مسیر مخفی ادمین ─────────────

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
	cfg := db.GetConfig()
	cards := cfg.Cards

	if r.Method == http.MethodGet {
		panelNames := map[string]string{}
		for _, p := range cfg.Panels {
			panelNames[p.Role] = p.Name
		}
		tmpl.Execute(w, map[string]interface{}{"Plans": plans, "PanelNames": panelNames, "Cards": cards})
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

		_, err = db.DB.Exec(`INSERT INTO orders (tracking_code, plan_name, base_price, unique_amount, plan_days, status) 
			VALUES (?, ?, ?, ?, ?, 'pending')`, trackingCode, selectedPlan.ID, basePrice, uniqueAmount, selectedPlan.Days)

		if err != nil {
			http.Error(w, "خطا در ثبت سفارش", http.StatusInternalServerError)
			return
		}

		order := models.Order{
			TrackingCode: trackingCode,
			PlanName:     selectedPlan.Title,
			UniqueAmount: uniqueAmount,
		}

		tmpl.Execute(w, map[string]interface{}{"CheckoutOrder": order, "Plans": plans, "Cards": cards})
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
		          FROM orders WHERE tracking_code = ? AND IFNULL(archived, 0) = 0`

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

// CheckOrderStatus وضعیت سفارش رو برمی‌گردونه (برای polling)
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
	          FROM orders WHERE tracking_code = ? AND IFNULL(archived, 0) = 0`

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
			"status":      "paid",
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
	CreatedAt      string
	PaidAt         string
	CreatedAtFmt   string
	PaidAtFmt      string
	Configs        []ConfigItem
	TelegramText   string
	AdminNote      string
	Username       string
}

type pageItem struct {
	Page    int
	Current bool
	Dots    bool
}

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

// checkAdminAuth بررسی session کوکی
func checkAdminAuth(w http.ResponseWriter, r *http.Request) bool {
	if c, err := r.Cookie("admin_session"); err == nil && validSession(c.Value) {
		return true
	}
	http.Redirect(w, r, AdminBasePath()+"/login", http.StatusSeeOther)
	return false
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

	// ── صفحه‌بندی ──
	pageSize := 10
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	var totalOrders int
		if err := db.DB.QueryRow(`SELECT COUNT(*) FROM orders WHERE IFNULL(archived, 0) = 0`).Scan(&totalOrders); err != nil {
		totalOrders = 0
	}
	totalPages := (totalOrders + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * pageSize

	rows, err := db.DB.Query(`
		SELECT id, tracking_code, plan_name, unique_amount, status, 
		       IFNULL(config_link, ''), IFNULL(admin_confirmed, 0), 
		       IFNULL(payment_method, ''),
		       IFNULL(created_at, ''), IFNULL(paid_at, ''),
		       IFNULL(admin_note, '')
		FROM orders WHERE IFNULL(archived, 0) = 0 ORDER BY id DESC LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		http.Error(w, "خطا در خواندن دیتابیس", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var orders []adminOrder
	for rows.Next() {
		var o adminOrder
		var confirmed int
		if err := rows.Scan(
			&o.ID, &o.TrackingCode, &o.PlanName, &o.UniqueAmount, &o.Status,
			&o.ConfigLink, &confirmed, &o.PaymentMethod, &o.CreatedAt, &o.PaidAt,
			&o.AdminNote,
		); err != nil {
			continue
		}
		o.AdminConfirmed = confirmed == 1

		if o.ConfigLink != "" {
			var items []ConfigItem
			if jerr := json.Unmarshal([]byte(o.ConfigLink), &items); jerr == nil && len(items) > 0 {
				o.Configs = items
			}
		}

		if len(o.Configs) > 0 {
			o.TelegramText = buildTelegramText(o.Configs)
			// 🎯 نام کاربری از اولین کانفیگ
			o.Username = o.Configs[0].Username
		}

		o.CreatedAtFmt = db.FormatTehranUTC(o.CreatedAt)
		o.PaidAtFmt = db.FormatTehranUTC(o.PaidAt)

		orders = append(orders, o)
	}

	tmpl.Execute(w, map[string]interface{}{
		"Orders":     orders,
		"AdminBase":  AdminBasePath(),
		"Page":       page,
		"TotalPages": totalPages,
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
		"Pagination": buildPagination(page, totalPages),
	})
}
// CheckRenewalHandler بررسی وجود نام کاربری و محاسبه carry-over
func CheckRenewalHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	if username == "" {
		http.Error(w, "نام کاربری لازم است", http.StatusBadRequest)
		return
	}

	// پیدا کردن آخرین فاکتور پرداخت‌شده با این username
	var orderID int
	var configLink string
	err := db.DB.QueryRow(`SELECT id, IFNULL(config_link, '') FROM orders 
	                       WHERE status = 'paid' AND config_link LIKE ?
	                       ORDER BY id DESC LIMIT 1`, "%\"username\":\""+username+"\"%").
		Scan(&orderID, &configLink)

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"found": false,
			"error": "اشتراکی با این نام کاربری یافت نشد",
		})
		return
	}

	// پیدا کردن پنل اصلی
	cfg := db.GetConfig()
	var mainPanel *db.PanelConfig
	for i := range cfg.Panels {
		if cfg.Panels[i].Role == "main" {
			mainPanel = &cfg.Panels[i]
			break
		}
	}

	if mainPanel == nil {
		http.Error(w, "پنل اصلی یافت نشد", http.StatusInternalServerError)
		return
	}

	// خواندن حجم و روز باقیمانده از پنل اصلی
	var limitUsage, totalUsage, limitExpire int64
	switch mainPanel.Type {
	case "guards":
		limitUsage, totalUsage, limitExpire, err = GetGuardsUserUsage(*mainPanel, username)
	case "marzban":
		// برای Marzban، می‌تونیم از GetMarzbanUserUsage استفاده کنیم
		// ولی فعلاً فقط Guards پشتیبانی میشه
		http.Error(w, "تمدید فعلاً فقط برای پنل Guards پشتیبانی می‌شود", http.StatusBadRequest)
		return
	default:
		http.Error(w, "نوع پنل پشتیبانی نمی‌شود", http.StatusBadRequest)
		return
	}

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"found": false,
			"error": "خطا در خواندن اطلاعات اشتراک: " + err.Error(),
		})
		return
	}

	// محاسبه carry-over
	now := time.Now().Unix()
	remainingVolume := limitUsage - totalUsage
	remainingDays := (limitExpire - now) / 86400

	if remainingDays <= 0 {
		remainingDays = 0
	}

	// نرخ روزانه = حجم کل / 30 روز (فرض)
	dailyRate := limitUsage / 30
	carryOverBytes := remainingVolume
	if remainingDays*dailyRate < carryOverBytes {
		carryOverBytes = remainingDays * dailyRate
	}

	carryOverGB := int(carryOverBytes / 1073741824)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"found":            true,
		"username":         username,
		"remaining_volume": remainingVolume / 1073741824,
		"remaining_days":   remainingDays,
		"carry_gb":         carryOverGB,
	})
}

// RenewalHandler ساخت فاکتور تمدید
func RenewalHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/shop.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب", http.StatusInternalServerError)
		return
	}

	plans, _ := models.LoadPlans()
	cfg := db.GetConfig()
	cards := cfg.Cards

	if r.Method == http.MethodPost {
		planID := r.FormValue("plan_id")
		renewUsername := r.FormValue("renew_username")
		carryGBStr := r.FormValue("carry_gb")

		var selectedPlan *models.Plan
		for _, p := range plans {
			if p.ID == planID {
				selectedPlan = &p
				break
			}
		}

		if selectedPlan == nil {
			http.Error(w, "پلن نامعتبر", http.StatusBadRequest)
			return
		}

		carryGB := 0
		if carryGBStr != "" {
			fmt.Sscanf(carryGBStr, "%d", &carryGB)
		}

		basePrice := selectedPlan.Price
		rand.Seed(time.Now().UnixNano())
		uniqueAmount := basePrice + rand.Intn(999) + 1
		trackingCode := generateTrackingCode()

		// ساخت فاکتور تمدید
		_, err = db.DB.Exec(`INSERT INTO orders (tracking_code, plan_name, base_price, unique_amount, renew_username, carry_gb, status) 
			VALUES (?, ?, ?, ?, ?, ?, 'pending')`, trackingCode, selectedPlan.ID, basePrice, uniqueAmount, renewUsername, carryGB)

		if err != nil {
			http.Error(w, "خطا در ثبت فاکتور تمدید", http.StatusInternalServerError)
			return
		}

		order := models.Order{
			TrackingCode: trackingCode,
			PlanName:     selectedPlan.Title,
			UniqueAmount: uniqueAmount,
		}

		tmpl.Execute(w, map[string]interface{}{"CheckoutOrder": order, "Plans": plans, "Cards": cards, "IsRenewal": true, "RenewUsername": renewUsername})
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
