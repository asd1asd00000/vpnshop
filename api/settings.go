package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

// SettingsHandler صفحه تنظیمات
func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	tmpl, err := template.New("settings.html").Funcs(template.FuncMap{
		"formatPrice": formatPrice,
	}).ParseFiles("templates/settings.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب تنظیمات", http.StatusInternalServerError)
		return
	}

	flash := ""
	flashError := false
	switch r.URL.Query().Get("msg") {
	case "email_saved":
		flash = "✅ تنظیمات بکاپ ایمیل ذخیره گردید"
	case "email_test":
		flash = "📧 ایمیل تست ارسال شد — وضعیت آن را در باکس تاریخچه پایین صفحه بررسی کنید"
	case "telegram_saved":
		flash = "✅ تنظیمات بکاپ تلگرام ذخیره گردید"
	case "telegram_test":
		flash = "✈️ فایل تست در حال ارسال به تلگرام است — وضعیت را در تاریخچه پایین صفحه بررسی کنید"
	case "groups_saved":
		flash = "✅ گروه‌های پنل ذخیره شد"
	case "plan_saved":
		flash = "✅ پلن با موفقیت ذخیره شد و بلافاصله در فروشگاه اعمال گردید"
	case "plan_deleted":
		flash = "🗑️ پلن با موفقیت حذف شد"
	case "plan_toggled":
		flash = "✅ وضعیت فعال/غیرفعال پلن تغییر کرد"
	case "plan_dup_id":
		flash = "❌ شناسه پلن (ID) تکراری است — لطفاً یک شناسه یکتا انتخاب کنید"
		flashError = true
	case "plan_invalid":
		flash = "❌ اطلاعات واردشده برای پلن نامعتبر است (شناسه، عنوان، قیمت، حجم و روز الزامی هستند)"
		flashError = true
	}

	// شمارش فاکتورهای در انتظار پرداخت برای هشدار مدیریت پلن‌ها
	var pendingCount int
	if db.DB != nil {
		db.DB.QueryRow(`SELECT COUNT(*) FROM orders WHERE status = 'pending' AND IFNULL(archived, 0) = 0`).Scan(&pendingCount)
	}

	cfg := db.GetConfig()
	if err := tmpl.Execute(w, map[string]interface{}{
		"Config":             cfg,
		"AdminBase":          AdminBasePath(),
		"Flash":              flash,
		"FlashError":         flashError,
		"PendingOrdersCount": pendingCount,
		"OpenSection":        r.URL.Query().Get("open"),
	}); err != nil {
		log.Printf("❌ خطای اجرای قالب settings: %v", err)
	}
}

// SavePlanHandler افزودن پلن جدید یا ویرایش پلن موجود
func SavePlanHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idx, err := strconv.Atoi(strings.TrimSpace(r.FormValue("plan_index")))
	if err != nil {
		idx = -1
	}

	planID := strings.TrimSpace(r.FormValue("plan_id"))
	title := strings.TrimSpace(r.FormValue("plan_title"))
	priceStr := strings.ReplaceAll(strings.TrimSpace(r.FormValue("plan_price")), ",", "")
	price, _ := strconv.Atoi(priceStr)
	volumeGB, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("plan_volume_gb")))
	days, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("plan_days")))
	backupGB, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("plan_backup_gb")))
	giftGB, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("plan_gift_gb")))
	giftNote := strings.TrimSpace(r.FormValue("plan_gift_note"))
	isSpecial := r.FormValue("plan_is_special") == "1" || r.FormValue("plan_is_special") == "on"
	enabled := r.FormValue("plan_enabled") == "1" || r.FormValue("plan_enabled") == "on"

	if planID == "" || title == "" || price <= 0 || volumeGB <= 0 || days <= 0 || backupGB < 0 || giftGB < 0 {
		http.Redirect(w, r, AdminBasePath()+"/settings?msg=plan_invalid&open=plans", http.StatusSeeOther)
		return
	}

	cfg := db.GetConfig()

	// بررسی یکتا بودن شناسه پلن
	for i, p := range cfg.Plans {
		if i != idx && strings.EqualFold(p.ID, planID) {
			http.Redirect(w, r, AdminBasePath()+"/settings?msg=plan_dup_id&open=plans", http.StatusSeeOther)
			return
		}
	}

	newPlan := db.PlanConfig{
		ID:        planID,
		Title:     title,
		Price:     price,
		VolumeGB:  volumeGB,
		Days:      days,
		IsSpecial: isSpecial,
		BackupGB:  backupGB,
		GiftGB:    giftGB,
		GiftNote:  giftNote,
		Disabled:  !enabled,
	}

	if idx >= 0 && idx < len(cfg.Plans) {
		cfg.Plans[idx] = newPlan
		db.LogEventf("general", "success", "✏️ پلن %s ویرایش شد (قیمت: %d | حجم: %dGB | روز: %d | فعال: %v)", planID, price, volumeGB, days, enabled)
	} else {
		cfg.Plans = append(cfg.Plans, newPlan)
		db.LogEventf("general", "success", "➕ پلن جدید اضافه شد: %s (قیمت: %d | حجم: %dGB | روز: %d)", planID, price, volumeGB, days)
	}

	if err := db.SaveConfig(cfg); err != nil {
		http.Error(w, "خطا در ذخیره پلن‌ها", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, AdminBasePath()+"/settings?msg=plan_saved&open=plans", http.StatusSeeOther)
}

// TogglePlanHandler فعال یا غیرفعال کردن سریع یک پلن
func TogglePlanHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idx, err := strconv.Atoi(strings.TrimSpace(r.FormValue("plan_index")))
	if err != nil {
		http.Error(w, "شناسه نامعتبر", http.StatusBadRequest)
		return
	}

	cfg := db.GetConfig()
	if idx >= 0 && idx < len(cfg.Plans) {
		cfg.Plans[idx].Disabled = !cfg.Plans[idx].Disabled
		if err := db.SaveConfig(cfg); err != nil {
			http.Error(w, "خطا در ذخیره", http.StatusInternalServerError)
			return
		}
		state := "فعال"
		if cfg.Plans[idx].Disabled {
			state = "غیرفعال"
		}
		db.LogEventf("general", "info", "🔄 وضعیت پلن %s به «%s» تغییر کرد", cfg.Plans[idx].ID, state)
	}

	http.Redirect(w, r, AdminBasePath()+"/settings?msg=plan_toggled&open=plans", http.StatusSeeOther)
}

// DeletePlanHandler حذف یک پلن
func DeletePlanHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idx, err := strconv.Atoi(strings.TrimSpace(r.FormValue("plan_index")))
	if err != nil {
		http.Error(w, "شناسه نامعتبر", http.StatusBadRequest)
		return
	}

	cfg := db.GetConfig()
	if idx >= 0 && idx < len(cfg.Plans) {
		planID := cfg.Plans[idx].ID
		cfg.Plans = append(cfg.Plans[:idx], cfg.Plans[idx+1:]...)
		if err := db.SaveConfig(cfg); err != nil {
			http.Error(w, "خطا در حذف پلن", http.StatusInternalServerError)
			return
		}
		db.LogEventf("general", "warning", "🗑️ پلن حذف شد: %s", planID)
	}

	http.Redirect(w, r, AdminBasePath()+"/settings?msg=plan_deleted&open=plans", http.StatusSeeOther)
}

// UpdateAdminHandler تغییر یوزرنیم و پسورد ادمین
func UpdateAdminHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("admin_username")
	password := r.FormValue("admin_password")
	confirm := r.FormValue("admin_password_confirm")

	if username == "" {
		http.Error(w, "نام کاربری نمی‌تواند خالی باشد", http.StatusBadRequest)
		return
	}

	if password != confirm {
		http.Error(w, "رمز عبور و تکرار آن یکسان نیست", http.StatusBadRequest)
		return
	}

	cfg := db.GetConfig()
	cfg.Admin.Username = username
	if password != "" {
		cfg.Admin.Password = password
	}

	if err := db.SaveConfig(cfg); err != nil {
		http.Error(w, "خطا در ذخیره تنظیمات", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ تنظیمات ادمین بروزرسانی شد (یوزر: %s)", username)
	db.LogEvent("general", "success", "✅ تنظیمات ادمین بروزرسانی شد")
	http.Redirect(w, r, AdminBasePath()+"/settings", http.StatusSeeOther)
}

// AddPanelHandler افزودن پنل جدید
func AddPanelHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.FormValue("panel_name")
	panelType := r.FormValue("panel_type")
	role := r.FormValue("panel_role")
	url := r.FormValue("panel_url")
	username := r.FormValue("panel_username")
	password := r.FormValue("panel_password")
	apiKey := r.FormValue("panel_api_key")

	if url == "" || panelType == "" {
		http.Error(w, "فیلدهای اجباری را پر کنید", http.StatusBadRequest)
		return
	}

	switch panelType {
	case "conf", "guards":
		if apiKey == "" && password == "" {
			http.Error(w, "برای پنل‌های Guards و Conf، API Key الزامی است", http.StatusBadRequest)
			return
		}
		if apiKey == "" {
			apiKey = password
		}
		username = ""
		password = ""
	case "marzban":
		if username == "" || password == "" {
			http.Error(w, "برای پنل Pasarguard، نام کاربری و رمز عبور الزامی است", http.StatusBadRequest)
			return
		}
		apiKey = ""
	default:
		if username == "" || password == "" {
			http.Error(w, "نام کاربری و رمز عبور برای این نوع پنل الزامی است", http.StatusBadRequest)
			return
		}
		apiKey = ""
	}

	if role == "" {
		role = "main"
	}

	cfg := db.GetConfig()
	cfg.Panels = append(cfg.Panels, db.PanelConfig{
		Name:     name,
		Type:     panelType,
		Role:     role,
		URL:      url,
		Username: username,
		Password: password,
		APIKey:   apiKey,
	})

	if err := db.SaveConfig(cfg); err != nil {
		http.Error(w, "خطا در ذخیره", http.StatusInternalServerError)
		return
	}

	db.LogEventf("general", "success", "✅ پنل جدید اضافه شد: %s (%s / %s)", name, panelType, role)
	http.Redirect(w, r, AdminBasePath()+"/settings", http.StatusSeeOther)
}

// DeletePanelHandler حذف پنل
func DeletePanelHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.FormValue("panel_id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id < 0 {
		http.Error(w, "شناسه نامعتبر", http.StatusBadRequest)
		return
	}

	cfg := db.GetConfig()
	if id >= len(cfg.Panels) {
		http.Error(w, "پنل یافت نشد", http.StatusNotFound)
		return
	}

	name := cfg.Panels[id].Name
	cfg.Panels = append(cfg.Panels[:id], cfg.Panels[id+1:]...)

	if err := db.SaveConfig(cfg); err != nil {
		http.Error(w, "خطا در حذف پنل", http.StatusInternalServerError)
		return
	}

	db.LogEventf("general", "warning", "🗑️ پنل حذف شد: %s", name)
	http.Redirect(w, r, AdminBasePath()+"/settings", http.StatusSeeOther)
}

// FetchPanelGroupsHandler لیست گروه‌های یک پنل Pasarguard رو از API می‌گیره
func FetchPanelGroupsHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PanelID int `json:"panel_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "درخواست نامعتبر",
		})
		return
	}

	cfg := db.GetConfig()
	if req.PanelID < 0 || req.PanelID >= len(cfg.Panels) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "پنل یافت نشد",
		})
		return
	}

	panel := cfg.Panels[req.PanelID]
	if panel.Type != "marzban" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "این قابلیت فقط برای پنل Pasarguard است",
		})
		return
	}

	groups, source, err := fetchPasarguardGroups(panel)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	log.Printf("✅ [گروه‌ها] %d گروه از %s (%s) بارگذاری شد", len(groups), panel.URL, source)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"groups":  groups,
		"source":  source,
	})
}

// SavePanelGroupsHandler ذخیره group_ids برای یک پنل
func SavePanelGroupsHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PanelID  int   `json:"panel_id"`
		GroupIDs []int `json:"group_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "درخواست نامعتبر",
		})
		return
	}

	cfg := db.GetConfig()
	if req.PanelID < 0 || req.PanelID >= len(cfg.Panels) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "پنل یافت نشد",
		})
		return
	}

	cfg.Panels[req.PanelID].GroupIDs = req.GroupIDs

	if err := db.SaveConfig(cfg); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "خطا در ذخیره تنظیمات",
		})
		return
	}

	panelName := cfg.Panels[req.PanelID].Name
	db.LogEventf("general", "success", "✅ گروه‌های پنل %s ذخیره شد (%d گروه)", panelName, len(req.GroupIDs))
	log.Printf("✅ [گروه‌ها] %d گروه برای پنل %s ذخیره شد", len(req.GroupIDs), panelName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("%d گروه ذخیره شد", len(req.GroupIDs)),
	})
}

// EmailBackupHandler ذخیره تنظیمات ایمیل بکاپ + تست ارسال
func EmailBackupHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	smtpServer := strings.TrimSpace(r.FormValue("smtp_server"))
	smtpPort, _ := strconv.Atoi(r.FormValue("smtp_port"))
	smtpUser := strings.TrimSpace(r.FormValue("smtp_user"))
	smtpPass := r.FormValue("smtp_pass")
	intervalHours, _ := strconv.Atoi(r.FormValue("interval_hours"))
	zipPassword := r.FormValue("zip_password")

	emailEnabledStr := r.FormValue("email_enabled")
	enabled := emailEnabledStr == "1"

	if smtpPort <= 0 {
		smtpPort = 587
	}
	if intervalHours <= 0 {
		intervalHours = 24
	}

	cfg := db.GetConfig()
	cfg.EmailBackup = db.EmailBackupConfig{
		Enabled:       enabled,
		Email:         email,
		SMTPServer:    smtpServer,
		SMTPPort:      smtpPort,
		SMTPUser:      smtpUser,
		SMTPPass:      smtpPass,
		IntervalHours: intervalHours,
		ZipPassword:   zipPassword,
	}

	if err := db.SaveConfig(cfg); err != nil {
		http.Error(w, "خطا در ذخیره تنظیمات", http.StatusInternalServerError)
		return
	}

	db.LogEventf("general", "success", "📧 تنظیمات ایمیل بکاپ ذخیره شد (فعال: %v | بازه: %d ساعت)", enabled, intervalHours)

	if r.FormValue("action") == "test" {
		go func() {
			err := SendEmailBackup(cfg.EmailBackup)
			logMsg := "ارسال بکاپ تست ایمیل"
			success := true
			if err != nil {
				success = false
				logMsg += fmt.Sprintf(" ناموفق بود: %v", err)
				db.LogEventf("general", "error", "❌ %s", logMsg)
			} else {
				logMsg += " با موفقیت انجام شد."
				db.LogEvent("general", "success", "✅ "+logMsg)
			}
			db.AddBackupLog(db.BackupLog{
				Time:    db.FormatTehranUTC(time.Now().UTC().Format(time.RFC3339)),
				Success: success,
				Details: logMsg,
			})
		}()
		http.Redirect(w, r, AdminBasePath()+"/settings?msg=email_test", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, AdminBasePath()+"/settings?msg=email_saved", http.StatusSeeOther)
}

// TelegramBackupHandler تنظیمات و تست بکاپ تلگرام
func TelegramBackupHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	botToken := strings.TrimSpace(r.FormValue("bot_token"))
	chatID := strings.TrimSpace(r.FormValue("chat_id"))
	intervalHours, _ := strconv.Atoi(r.FormValue("interval_hours"))
	zipPassword := r.FormValue("zip_password")

	telegramEnabledStr := r.FormValue("telegram_enabled")
	enabled := telegramEnabledStr == "1"

	if intervalHours <= 0 {
		intervalHours = 24
	}

	cfg := db.GetConfig()
	cfg.TelegramBackup = db.TelegramBackupConfig{
		Enabled:       enabled,
		BotToken:      botToken,
		ChatID:        chatID,
		IntervalHours: intervalHours,
		ZipPassword:   zipPassword,
	}

	if err := db.SaveConfig(cfg); err != nil {
		http.Error(w, "خطا در ذخیره تنظیمات", http.StatusInternalServerError)
		return
	}

	db.LogEventf("general", "success", "✈️ تنظیمات بکاپ تلگرام ذخیره شد (فعال: %v | بازه: %d ساعت)", enabled, intervalHours)

	if r.FormValue("action") == "test" {
		go func() {
			err := SendTelegramBackup(cfg.TelegramBackup)
			logMsg := "ارسال بکاپ تست تلگرام"
			success := true
			if err != nil {
				success = false
				logMsg += fmt.Sprintf(" ناموفق بود: %v", err)
				db.LogEventf("general", "error", "❌ %s", logMsg)
			} else {
				logMsg += " با موفقیت انجام شد."
				db.LogEvent("general", "success", "✅ "+logMsg)
			}
			db.AddBackupLog(db.BackupLog{
				Time:    db.FormatTehranUTC(time.Now().UTC().Format(time.RFC3339)),
				Success: success,
				Details: logMsg,
			})
		}()
		http.Redirect(w, r, AdminBasePath()+"/settings?msg=telegram_test", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, AdminBasePath()+"/settings?msg=telegram_saved", http.StatusSeeOther)
}

// AddCardHandler افزودن شماره کارت
func AddCardHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	number := strings.TrimSpace(r.FormValue("card_number"))
	holder := strings.TrimSpace(r.FormValue("card_holder"))

	if number == "" {
		http.Error(w, "شماره کارت الزامی است", http.StatusBadRequest)
		return
	}

	cfg := db.GetConfig()
	cfg.Cards = append(cfg.Cards, db.CardInfo{Number: number, Holder: holder})

	if err := db.SaveConfig(cfg); err != nil {
		http.Error(w, "خطا در ذخیره", http.StatusInternalServerError)
		return
	}

	db.LogEventf("general", "success", "💳 کارت جدید اضافه شد: %s", number)
	http.Redirect(w, r, AdminBasePath()+"/settings", http.StatusSeeOther)
}

// DeleteCardHandler حذف شماره کارت
func DeleteCardHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idx, err := strconv.Atoi(r.FormValue("card_id"))
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	cfg := db.GetConfig()
	if idx >= 0 && idx < len(cfg.Cards) {
		cfg.Cards = append(cfg.Cards[:idx], cfg.Cards[idx+1:]...)
		if err := db.SaveConfig(cfg); err != nil {
			http.Error(w, "خطا در ذخیره", http.StatusInternalServerError)
			return
		}
		db.LogEvent("general", "warning", "🗑️ یک کارت حذف شد")
	}

	http.Redirect(w, r, AdminBasePath()+"/settings", http.StatusSeeOther)
}

// UpdateCleanupHandler تنظیم مهلت حذف سفارش‌های پرداخت‌نشده
func UpdateCleanupHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hours, err := strconv.Atoi(r.FormValue("order_expire_hours"))
	if err != nil || hours <= 0 {
		hours = 48
	}

	cfg := db.GetConfig()
	cfg.Cleanup.OrderExpireHours = hours

	if err := db.SaveConfig(cfg); err != nil {
		http.Error(w, "خطا در ذخیره", http.StatusInternalServerError)
		return
	}

	db.LogEventf("general", "success", "🕐 مهلت حذف سفارش‌های پرداخت‌نشده: %d ساعت", hours)
	http.Redirect(w, r, AdminBasePath()+"/settings", http.StatusSeeOther)
}
