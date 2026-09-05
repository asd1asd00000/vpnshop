package api

import (
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/asd1asd00000/vpnshop/db"
)

// SettingsHandler صفحه تنظیمات
func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	tmpl, err := template.ParseFiles("templates/settings.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب تنظیمات", http.StatusInternalServerError)
		return
	}

	cfg := db.GetConfig()
	tmpl.Execute(w, map[string]interface{}{
		"Config":    cfg,
		"AdminBase": AdminBasePath(),
	})
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

	if url == "" || username == "" || password == "" || panelType == "" {
		http.Error(w, "فیلدهای اجباری را پر کنید", http.StatusBadRequest)
		return
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

// EmailBackupHandler ذخیره تنظیمات ایمیل بکاپ + تست ارسال
func EmailBackupHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	enabled := r.FormValue("email_enabled") == "on"
	email := strings.TrimSpace(r.FormValue("email"))
	smtpServer := strings.TrimSpace(r.FormValue("smtp_server"))
	smtpPortStr := r.FormValue("smtp_port")
	smtpUser := strings.TrimSpace(r.FormValue("smtp_user"))
	smtpPass := r.FormValue("smtp_pass")

	smtpPort, _ := strconv.Atoi(smtpPortStr)
	if smtpPort <= 0 {
		smtpPort = 587
	}

	cfg := db.GetConfig()
	cfg.EmailBackup = db.EmailBackupConfig{
		Enabled:    enabled,
		Email:      email,
		SMTPServer: smtpServer,
		SMTPPort:   smtpPort,
		SMTPUser:   smtpUser,
		SMTPPass:   smtpPass,
	}

	if err := db.SaveConfig(cfg); err != nil {
		http.Error(w, "خطا در ذخیره تنظیمات", http.StatusInternalServerError)
		return
	}

	db.LogEventf("general", "success", "📧 تنظیمات ایمیل بکاپ بروزرسانی شد (فعال: %v)", enabled)

	// اگه دکمه تست بود، ایمیل تستی بفرست
	if r.FormValue("action") == "test" {
		go func() {
			if err := SendTestEmail(cfg.EmailBackup); err != nil {
				db.LogEventf("general", "error", "❌ خطا در ارسال ایمیل تست: %v", err)
			} else {
				db.LogEvent("general", "success", "✅ ایمیل تست با موفقیت ارسال شد")
			}
		}()
		db.LogEvent("general", "info", "📧 ایمیل تست در پس‌زمینه ارسال شد")
	}

	http.Redirect(w, r, AdminBasePath()+"/settings", http.StatusSeeOther)
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
