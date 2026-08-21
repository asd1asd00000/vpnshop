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
	Type     string `json:"type"` // "guards" | "marzban"
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"` // "main" | "backup" | "gift"

	IsBackup bool `json:"is_backup,omitempty"` // legacy
}

// CardInfo یک شماره کارت برای واریز
type CardInfo struct {
	Number string `json:"number"`
	Holder string `json:"holder"`
}

type AdminConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type EmailBackupConfig struct {
	Enabled    bool   `json:"enabled"`
	Email      string `json:"email"`
	SMTPServer string `json:"smtp_server"`
	SMTPPort   int    `json:"smtp_port"`
	SMTPUser   string `json:"smtp_user"`
	SMTPPass   string `json:"smtp_pass"`
}
type CleanupConfig struct {
	OrderExpireHours int `json:"order_expire_hours"`
}

type AppConfig struct {
	Admin       AdminConfig       `json:"admin"`
	Panels      []PanelConfig     `json:"panels"`
	Cards       []CardInfo        `json:"cards"`
	EmailBackup EmailBackupConfig `json:"email_backup"`
		Cleanup     CleanupConfig     `json:"cleanup"`
}

var (
	configMu   sync.RWMutex
	config     *AppConfig
	configPath = "./config.json"
)

func LoadConfig() *AppConfig {
	configMu.Lock()
	defer configMu.Unlock()

	config = &AppConfig{
		Admin:       AdminConfig{},
		Panels:      []PanelConfig{},
		Cards:       []CardInfo{},
		EmailBackup: EmailBackupConfig{SMTPPort: 587},
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Println("ℹ️ فایل config.json وجود ندارد")
		return config
	}

	if err := json.Unmarshal(data, config); err != nil {
		log.Printf("❌ خطا در خواندن config.json: %v", err)
		return config
	}

	for i := range config.Panels {
		if config.Panels[i].Role == "" {
			if config.Panels[i].IsBackup {
				config.Panels[i].Role = "backup"
			} else {
				config.Panels[i].Role = "main"
			}
		}
	}

	// مقدار پیش‌فرض زمان انقضای سفارش
	if config.Cleanup.OrderExpireHours <= 0 {
		config.Cleanup.OrderExpireHours = 48
	}

	return config
}

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

func GetConfig() *AppConfig {
	configMu.RLock()
	defer configMu.RUnlock()
	if config == nil {
		LoadConfig()
	}
	return config
}
