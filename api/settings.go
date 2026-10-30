package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
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

	tmpl, err := template.ParseFiles("templates/settings.html")
	if err != nil {
		http.Error(w, "خطا در بارگذاری قالب تنظیمات", http.StatusInternalServerError)
		return
	}

	flash := ""
	switch r.URL.Query().Get("msg") {
	case "email_saved":
		flash = "✅ بکاپ خودکار به ایمیل فعال شد و تنظیمات ذخیره گردید"
	case "email_test":
		flash = "📧 ایمیل تست در پس‌زمینه ارسال شد — اینباکس را بررسی کنید"
	case "groups_saved":
		flash = "✅ گروه‌های پنل ذخیره شد"
	}

	cfg := db.GetConfig()
	tmpl.Execute(w, map[string]interface{}{
		"Config":    cfg,
		"AdminBase": AdminBasePath(),
		"Flash":     flash,
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
	apiKey := r.FormValue("panel_api_key")

	if url == "" || panelType == "" {
		http.Error(w, "فیلدهای اجباری را پر کنید", http.StatusBadRequest)
		return
	}

	// اعتبارسنجی بر اساس نوع پنل:
	switch panelType {
	case "conf", "guards":
		// Conf و Guards: فقط API Key
		if apiKey == "" && password == "" {
			http.Error(w, "برای پنل‌های Guards و Conf، API Key الزامی است", http.StatusBadRequest)
			return
		}
		if apiKey == "" {
			apiKey = password // سازگاری با فرم قدیمی
		}
		username = ""
		password = ""
	case "marzban":
		// Pasarguard: فقط نام کاربری + رمز عبور
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
		// 🆕 GroupIDs اینجا خالی می‌مونه - بعداً از UI گروه‌ها اضافه می‌شن
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

// 🆕 FetchPanelGroupsHandler لیست گروه‌های یک پنل Pasarguard رو از API می‌گیره
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

	// 🎯 گرفتن توکن با credentials پنل
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "احراز هویت ناموفق: " + err.Error(),
		})
		return
	}

	baseURL := strings.TrimRight(panel.URL, "/")
	// 🎯 endpoint بدون query params (چون Pasarguard با ?limit خطا می‌ده)
	reqHTTP, _ := http.NewRequest("GET", baseURL+"/api/groups", nil)
	reqHTTP.Header.Add("Authorization", "Bearer "+token)
	reqHTTP.Header.Add("Accept", "application/json")

	client := makeHTTPClient(15 * time.Second)
	resp, err := client.Do(reqHTTP)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "خطا در اتصال: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("پاسخ %d از پنل: %s", resp.StatusCode, truncateBody(bodyBytes, 300)),
		})
		return
	}

	// 🎯 پارس پاسخ - ممکنه array یا object با فیلد groups باشه
	type GroupInfo struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	var result []GroupInfo

	var groups []map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &groups); err == nil {
		// پاسخ مستقیم array هست
		for _, g := range groups {
			id, _ := g["id"].(float64)
			name, _ := g["name"].(string)
			if name == "" {
				name = fmt.Sprintf("Group-%d", int(id))
			}
			result = append(result, GroupInfo{ID: int(id), Name: name})
		}
	} else {
		// شاید wrapper هست
		var wrapper map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &wrapper); err == nil {
			if g, ok := wrapper["groups"].([]interface{}); ok {
				for _, item := range g {
					if m, ok := item.(map[string]interface{}); ok {
						id, _ := m["id"].(float64)
						name, _ := m["name"].(string)
						if name == "" {
							name = fmt.Sprintf("Group-%d", int(id))
						}
						result = append(result, GroupInfo{ID: int(id), Name: name})
					}
				}
			}
		}
	}

	log.Printf("✅ [گروه‌ها] %d گروه از %s بارگذاری شد", len(result), panel.URL)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"groups":  result,
	})
}

// 🆕 SavePanelGroupsHandler ذخیره group_ids برای یک پنل
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

	// ذخیره group_ids در پنل
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

	if smtpPort <= 0 {
		smtpPort = 587
	}
	if intervalHours <= 0 {
		intervalHours = 24
	}

	// 🎯 فعال‌سازی خودکار: اگه فیلدها پر شده باشن
	enabled := email != "" && smtpServer != "" && smtpUser != ""

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
			if err := SendEmailBackup(cfg.EmailBackup); err != nil {
				db.LogEventf("general", "error", "❌ خطا در ارسال ایمیل تست: %v", err)
			} else {
				db.LogEvent("general", "success", "✅ ایمیل تست با بکاپ واقعی ارسال شد")
			}
		}()
		http.Redirect(w, r, AdminBasePath()+"/settings?msg=email_test", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, AdminBasePath()+"/settings?msg=email_saved", http.StatusSeeOther)
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
