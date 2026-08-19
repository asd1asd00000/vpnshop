package db

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

// PanelConfig اطلاعات یک پنل VPN
type PanelConfig struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// AdminConfig اطلاعات ادمین داشبورد
type AdminConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// EmailBackupConfig تنظیمات بکاپ به ایمیل
type EmailBackupConfig struct {
	Enabled    bool   `json:"enabled"`
	Email      string `json:"email"`
	SMTPServer string `json:"smtp_server"`
	SMTPPort   int    `json:"smtp_port"`
	SMTPUser   string `json:"smtp_user"`
	SMTPPass   string `json:"smtp_pass"`
}

// AppConfig ساختار کلی تنظیمات
type AppConfig struct {
	Admin       AdminConfig       `json:"admin"`
	Panels      []PanelConfig     `json:"panels"`
	EmailBackup EmailBackupConfig `json:"email_backup"`
}

var (
	configMu   sync.RWMutex
	config     *AppConfig
	configPath = "./config.json"
)

// LoadConfig تنظیمات رو از فایل می‌خونه
func LoadConfig() *AppConfig {
	configMu.Lock()
	defer configMu.Unlock()

	// تنظیمات پیش‌فرض
	config = &AppConfig{
		Admin:       AdminConfig{},
		Panels:      []PanelConfig{},
		EmailBackup: EmailBackupConfig{SMTPPort: 587},
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Println("ℹ️ فایل config.json وجود ندارد، از تنظیمات پیش‌فرض استفاده می‌شود")
		return config
	}

	if err := json.Unmarshal(data, config); err != nil {
		log.Printf("❌ خطا در خواندن config.json: %v", err)
		return config
	}

	return config
}

// SaveConfig تنظیمات رو توی فایل ذخیره می‌کنه
func SaveConfig(cfg *AppConfig) error {
	configMu.Lock()
	defer configMu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return err
	}

	config = cfg
	return nil
}

// GetConfig تنظیمات فعلی رو برمی‌گردونه
func GetConfig() *AppConfig {
	configMu.RLock()
	defer configMu.RUnlock()
	if config == nil {
		LoadConfig()
	}
	return config
}
