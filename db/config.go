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
	Type     string `json:"type"`     // "guards" | "marzban"
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	IsBackup bool   `json:"is_backup"` // اگه true، فقط به عنوان زاپاس استفاده میشه
	BackupGB int    `json:"backup_gb"` // حجم زاپاس به گیگابایت (فقط وقتی IsBackup = true)
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

func LoadConfig() *AppConfig {
	configMu.Lock()
	defer configMu.Unlock()

	config = &AppConfig{
		Admin:       AdminConfig{},
		Panels:      []PanelConfig{},
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
