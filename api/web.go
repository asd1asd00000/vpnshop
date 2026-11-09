package api

import (
	crand "crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
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
	dbInitMu sync.Once
)

const (
	rateLimitWindow = 1 * time.Minute
	rateLimitMax    = 3
)

func ensureDBPragmas() {
	dbInitMu.Do(func() {
		if db.DB != nil {
			db.DB.Exec("PRAGMA busy_timeout = 5000;")
			db.DB.Exec("PRAGMA journal_mode = WAL;")
		}
	})
}

// insertOrderWithRetry ثبت مقاوم فاکتور در دیتابیس با تلاش مجدد در صورت قفل بودن لحظه‌ای SQLite
func insertOrderWithRetry(planID string, basePrice int, days int, renewUsername string, carryGB int) (string, int, error) {
	ensureDBPragmas()

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		uniqueAmount := basePrice + rand.Intn(999) + 1
		trackingCode := generateTrackingCode()

		var err error
		if renewUsername != "" {
			_, err = db.DB.Exec(`INSERT INTO orders (tracking_code, plan_name, base_price, unique_amount, plan_days, renew_username, carry_gb, status) 
				VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')`,
				trackingCode, planID, basePrice, uniqueAmount, days, renewUsername, carryGB)
		} else {
			_, err = db.DB.Exec(`INSERT INTO orders (tracking_code, plan_name, base_price, unique_amount, plan_days, status) 
				VALUES (?, ?, ?, ?, ?, 'pending')`,
				trackingCode, planID, basePrice, uniqueAmount, days)
		}

		if err == nil {
			return trackingCode, uniqueAmount, nil
		}

		lastErr = err
		log.Printf("⚠️ [DB-Retry %d/5] خطا در ثبت فاکتور: %v", attempt+1, err)
		time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
	}
	return "", 0, lastErr
}

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
	ensureDBPragmas()

	tmpl, err := template.ParseFiles("templates/shop.html")
	if err != nil {
		log.Printf("❌ خطا در پارس قالب shop.html: %v", err)
		renderErrorPage(w, "خطا در بارگذاری صفحه فروشگاه",
			"سرور در آماده‌سازی صفحه فروشگاه به مشکل خورد. نگران نباشید؛ صفحه به‌صورت خودکار دوباره تلاش می‌کند.",
			"/", 5)
		return
	}

	plans, _ := models.LoadPlans()
	cfg := db.GetConfig()
	cards := cfg.Cards

	// 🎯 مرتب‌سازی: به‌صرفه‌ترین اول
	sort.SliceStable(plans, func(i, j int) bool {
		si := float64(plans[i].VolumeGB*plans[i].Days) / float64(plans[i].Price)
		sj := float64(plans[j].VolumeGB*plans[j].Days) / float64(plans[j].Price)
		return si > sj
	})

	// نقش → نوع پنل (برای تشخیص conf)
	panelTypes := make(map[string]string)
	for _, pn := range cfg.Panels {
		panelTypes[pn.Role] = pn.Type
	}

	type shopPlanView struct {
		models.Plan
		MonthLabel     string
		PriceFormatted string
		BackupDisplay  string
		GiftDisplay    string
	}
	var enrichedPlans []shopPlanView
	for _, p := range plans {
		ml := fmt.Sprintf("%d روز", p.Days)
		if p.Days >= 30 && p.Days%30 == 0 {
			ml = fmt.Sprintf("%d ماهه", p.Days/30)
		}
		enrichedPlans = append(enrichedPlans, shopPlanView{
			Plan:           p,
			MonthLabel:     ml,
			PriceFormatted: formatPrice(p.Price),
			BackupDisplay:  formatVolumeForPanel(panelTypes["backup"], p.BackupGB),
			GiftDisplay:    formatVolumeForPanel(panelTypes["gift"], p.GiftGB),
		})
	}

	// ── ساخت panelNames ──
	panelNames := make(map[string]string)
	for _, panel := range cfg.Panels {
		if panel.Role == "backup" || panel.Role == "gift" {
			panelNames[panel.Role] = panel.Name
		}
	}

	// ── بخش POST (checkout خرید جدید) ──
	if r.Method == http.MethodPost {
		planID := strings.TrimSpace(r.FormValue("plan_id"))
		var selectedPlan *models.Plan
		for _, p := range plans {
			if p.ID == planID {
				selectedPlan = &p
				break
			}
		}

		if selectedPlan == nil {
			renderErrorPage(w, "پلن انتخاب‌شده معتبر نیست",
				"پلنی که انتخاب کرده‌اید در سیستم یافت نشد. لطفاً یک پلن دیگر را انتخاب کنید.",
				"/", 0)
			return
		}

		trackingCode, _, err := insertOrderWithRetry(selectedPlan.ID, selectedPlan.Price, selectedPlan.Days, "", 0)
		if err != nil {
			renderErrorPage(w, "خطا در ایجاد فاکتور",
				"سرور نتوانست فاکتور شما را ثبت کند. این مشکل معمولاً موقتی است. لطفاً دوباره تلاش کنید.",
				"/", 5)
			return
		}

		// هدایت به آدرس GET فاکتور تا در صورت رفرش صفحه در موبایل خطایی رخ ندهد
		http.Redirect(w, r, "/?order="+trackingCode, http.StatusSeeOther)
		return
	}

	// ── بررسی نمایش فاکتور ثبت‌شده با پارامتر ?order=VPxxxx ──
	if orderCode := strings.TrimSpace(r.URL.Query().Get("order")); orderCode != "" {
		var planID, renewUser string
		var uniqueAmount int
		err := db.DB.QueryRow(`SELECT plan_name, unique_amount, IFNULL(renew_username, '') FROM orders WHERE tracking_code = ?`, orderCode).
			Scan(&planID, &uniqueAmount, &renewUser)
		if err == nil {
			planTitle := planID
			for _, p := range plans {
				if p.ID == planID {
					planTitle = p.Title
					break
				}
			}
			order := models.Order{
				TrackingCode: orderCode,
				PlanName:     planTitle,
				UniqueAmount: uniqueAmount,
			}
			data := map[string]interface{}{
				"CheckoutOrder": order,
				"Plans":         enrichedPlans,
				"Cards":         cards,
				"PanelNames":    panelNames,
			}
			if renewUser != "" {
				data["IsRenewal"] = true
				data["RenewUsername"] = renewUser
			}
			if terr := tmpl.Execute(w, data); terr != nil {
				log.Printf("❌ خطا در اجرای قالب فاکتور: %v", terr)
			}
			return
		}
	}

	// ── بخش GET (نمایش پلن‌ها) ──
	if terr := tmpl.Execute(w, map[string]interface{}{
		"Plans":      enrichedPlans,
		"Cards":      cards,
		"PanelNames": panelNames,
	}); terr != nil {
		log.Printf("❌ خطا در اجرای قالب فروشگاه: %v", terr)
	}
}

// formatPrice جداسازی سه‌رقمی از سمت راست با کاما
func formatPrice(n int) string {
	s := strconv.Itoa(n)
	var result strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(r)
	}
	return result.String()
}

// ───────────── 🔍 پیگیری سفارش ─────────────

func TrackHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/track.html")
	if err != nil {
		renderErrorPage(w, "خطا در بارگذاری صفحه پیگیری",
			"سرور در آماده‌سازی صفحه پیگیری به مشکل خورد. صفحه به‌صورت خودکار دوباره تلاش می‌کند.",
			"/track", 5)
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

		trackingCode := strings.TrimSpace(r.FormValue("tracking_code"))
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

	trackingCode := strings.TrimSpace(r.URL.Query().Get("code"))
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
	RenewUsername  string
	StatsFixed     bool
	IsNew          bool
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
		renderErrorPage(w, "خطا در بارگذاری پنل ادمین",
			"قالب پنل ادمین در دسترس نیست. صفحه به‌صورت خودکار دوباره تلاش می‌کند.",
			AdminBasePath(), 5)
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
		       IFNULL(admin_note, ''), IFNULL(renew_username, ''), IFNULL(stats_fixed, 0)
		FROM orders WHERE IFNULL(archived, 0) = 0 ORDER BY id DESC LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		renderErrorPage(w, "خطا در خواندن دیتابیس",
			"سرور در خواندن لیست سفارش‌ها به مشکل خورد. معمولاً با تلاش دوباره حل می‌شود.",
			AdminBasePath(), 5)
		return
	}
	defer rows.Close()

	var orders []adminOrder
	for rows.Next() {
		var o adminOrder
		var confirmed, statsFixed int
		var renewUsername string
		if err := rows.Scan(
			&o.ID, &o.TrackingCode, &o.PlanName, &o.UniqueAmount, &o.Status,
			&o.ConfigLink, &confirmed, &o.PaymentMethod, &o.CreatedAt, &o.PaidAt,
			&o.AdminNote, &renewUsername, &statsFixed,
		); err != nil {
			continue
		}
		o.AdminConfirmed = confirmed == 1
		o.RenewUsername = renewUsername
		o.StatsFixed = statsFixed == 1

		// 🎯 لیبل «جدید»: ساخته‌شده در ۲۴ ساعت اخیر
		if o.CreatedAt != "" {
			if t, perr := time.Parse("2006-01-02 15:04:05", o.CreatedAt); perr == nil {
				o.IsNew = time.Now().UTC().Sub(t) < 24*time.Hour
			}
		}

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

// ───────────── 🔄 منطق محاسبه حجم انتقالی (Carry-Over) ─────────────

// calculateUserCarryOver حجم باقیمانده، روز باقیمانده و حجم قابل انتقال به دوره بعد را محاسبه می‌کند.
func calculateUserCarryOver(username string) (remainingVolumeGB int, remainingDays int, carryOverGB int, err error) {
	// پیدا کردن آخرین فاکتور پرداخت‌شده با این username
	var orderID int
	var configLink string
	err = db.DB.QueryRow(`SELECT id, IFNULL(config_link, '') FROM orders 
	                      WHERE status = 'paid' AND config_link LIKE ?
	                      ORDER BY id DESC LIMIT 1`, "%\"username\":\""+username+"\"%").
		Scan(&orderID, &configLink)

	if err != nil {
		return 0, 0, 0, fmt.Errorf("اشتراکی با این نام کاربری یافت نشد")
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
	if mainPanel == nil && len(cfg.Panels) > 0 {
		mainPanel = &cfg.Panels[0]
	}
	if mainPanel == nil {
		return 0, 0, 0, fmt.Errorf("پنل اصلی در تنظیمات یافت نشد")
	}

	// خواندن حجم و روز باقیمانده از پنل اصلی
	var limitUsage, totalUsage, limitExpire int64
	switch mainPanel.Type {
	case "guards":
		limitUsage, totalUsage, limitExpire, err = GetGuardsUserUsage(*mainPanel, username)
	default:
		return 0, 0, 0, fmt.Errorf("تمدید فعلاً فقط برای پنل Guards پشتیبانی می‌شود")
	}

	if err != nil {
		// 🎯 اگر کاربر در دیتابیس فروشگاه بود ولی از پنل Guards (به خاطر انقضای ۱۴ روزه) حذف شده بود:
		if strings.Contains(err.Error(), "یافت نشد") {
			log.Printf("ℹ️ [تمدید] کاربر %s در دیتابیس هست اما از پنل حذف شده -> انتقال=0GB", username)
			return 0, 0, 0, nil
		}
		return 0, 0, 0, fmt.Errorf("خطا در خواندن اطلاعات اشتراک: %v", err)
	}

	const oneGB = int64(1073741824)
	const oneDay = int64(86400)

	// ۱. محاسبه حجم باقیمانده و گرد کردن به سمت بالا (به نفع مشتری)
	remainingBytes := limitUsage - totalUsage
	if remainingBytes > 0 {
		remainingVolumeGB = int((remainingBytes + oneGB - 1) / oneGB)
	} else {
		remainingVolumeGB = 0
	}

	// ۲. محاسبه روزهای باقیمانده و گرد کردن به سمت بالا (به نفع مشتری)
	now := time.Now().Unix()
	remainingSeconds := limitExpire - now
	if remainingSeconds > 0 {
		remainingDays = int((remainingSeconds + oneDay - 1) / oneDay)
	} else {
		remainingDays = 0
	}

	// ۳. قانون انتقال: به ازای هر ۱ گیگ انتقال، حداقل ۱ روز زمان نیاز است
	if remainingVolumeGB > 0 && remainingDays > 0 {
		if remainingVolumeGB < remainingDays {
			carryOverGB = remainingVolumeGB
		} else {
			carryOverGB = remainingDays
		}
	} else {
		carryOverGB = 0
	}

	log.Printf("📊 [محاسبه تمدید] کاربر=%s | سقف=%d | مصرف=%d | مانده=%dGB | زمان‌مانده=%dروز (%dثانیه) | انتقالی=%dGB",
		username, limitUsage, totalUsage, remainingVolumeGB, remainingDays, remainingSeconds, carryOverGB)

	return remainingVolumeGB, remainingDays, carryOverGB, nil
}

// CheckRenewalHandler بررسی وجود نام کاربری و محاسبه carry-over
func CheckRenewalHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	if username == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"found": false,
			"error": "نام کاربری لازم است",
		})
		return
	}

	remainingVolumeGB, remainingDays, carryOverGB, err := calculateUserCarryOver(username)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"found": false,
			"error": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"found":            true,
		"username":         username,
		"remaining_volume": remainingVolumeGB,
		"remaining_days":   remainingDays,
		"carry_gb":         carryOverGB,
	})
}

// RenewalHandler ساخت فاکتور تمدید
func RenewalHandler(w http.ResponseWriter, r *http.Request) {
	ensureDBPragmas()

	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	plans, _ := models.LoadPlans()
	planID := strings.TrimSpace(r.FormValue("plan_id"))
	renewUsername := strings.TrimSpace(r.FormValue("renew_username"))
	carryGBStr := strings.TrimSpace(r.FormValue("carry_gb"))

	if renewUsername == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	var selectedPlan *models.Plan
	for _, p := range plans {
		if p.ID == planID {
			selectedPlan = &p
			break
		}
	}

	// اگر کاربر فقط ۱ پلن فعال داشت یا plan_id ارسال نشده بود، اولین پلن موجود را بررسی کن
	if selectedPlan == nil {
		renderErrorPage(w, "پلن انتخاب‌شده معتبر نیست",
			"لطفاً ابتدا نام کاربری را جستجو کرده و سپس یکی از پلن‌ها را از لیست انتخاب نمایید.",
			"/", 0)
		return
	}

	// محاسبه مجدد سمت سرور برای اطمینان از صحت مقدار carry_gb
	carryGB := 0
	if _, _, serverCarryGB, cerr := calculateUserCarryOver(renewUsername); cerr == nil {
		carryGB = serverCarryGB
	} else if carryGBStr != "" {
		fmt.Sscanf(carryGBStr, "%d", &carryGB)
	}
	if carryGB < 0 {
		carryGB = 0
	}

	trackingCode, _, err := insertOrderWithRetry(selectedPlan.ID, selectedPlan.Price, selectedPlan.Days, renewUsername, carryGB)
	if err != nil {
		renderErrorPage(w, "خطا در ایجاد فاکتور تمدید",
			"سرور نتوانست فاکتور تمدید را ثبت کند. این مشکل معمولاً موقتی است.",
			"/", 5)
		return
	}

	// هدایت به صفحه نمایش فاکتور با کد پیگیری (جلوگیری از خطای رفرش صفحه در موبایل)
	http.Redirect(w, r, "/?order="+trackingCode, http.StatusSeeOther)
}
