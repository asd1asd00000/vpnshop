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
	Type     string `json:"type"` // "guards" | "marzban" | "conf"
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"` // "main" | "backup" | "gift"
	IsBackup bool   `json:"is_backup,omitempty"` // legacy
	APIKey   string `json:"api_key"`
	GroupIDs []int  `json:"group_ids,omitempty"`
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
	Enabled       bool   `json:"enabled"`
	Email         string `json:"email"`
	SMTPServer    string `json:"smtp_server"`
	SMTPPort      int    `json:"smtp_port"`
	SMTPUser      string `json:"smtp_user"`
	SMTPPass      string `json:"smtp_pass"`
	IntervalHours int    `json:"interval_hours"`
	ZipPassword   string `json:"zip_password"`
}

// 🆕 ساختار بکاپ تلگرام
type TelegramBackupConfig struct {
	Enabled       bool   `json:"enabled"`
	BotToken      string `json:"bot_token"`
	ChatID        string `json:"chat_id"`
	IntervalHours int    `json:"interval_hours"`
	ZipPassword   string `json:"zip_password"`
}

// 🆕 ساختار ذخیره وضعیت آخرین بکاپ‌ها
type BackupLog struct {
	Time    string `json:"time"`
	Success bool   `json:"success"`
	Details string `json:"details"`
}

type CleanupConfig struct {
	OrderExpireHours int `json:"order_expire_hours"`
}

type AppConfig struct {
	Admin          AdminConfig          `json:"admin"`
	Panels         []PanelConfig        `json:"panels"`
	Cards          []CardInfo           `json:"cards"`
	EmailBackup    EmailBackupConfig    `json:"email_backup"`
	TelegramBackup TelegramBackupConfig `json:"telegram_backup"` // 🆕
	BackupLogs     []BackupLog          `json:"backup_logs"`     // 🆕 لیست ۳ بکاپ اخیر
	Cleanup        CleanupConfig        `json:"cleanup"`
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
		Admin:          AdminConfig{},
		Panels:         []PanelConfig{},
		Cards:          []CardInfo{},
		EmailBackup:    EmailBackupConfig{SMTPPort: 587},
		TelegramBackup: TelegramBackupConfig{},
		BackupLogs:     []BackupLog{},
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

	if config.Cleanup.OrderExpireHours <= 0 {
		config.Cleanup.OrderExpireHours = 48
	}
	if config.EmailBackup.IntervalHours <= 0 {
		config.EmailBackup.IntervalHours = 24
	}
	if config.TelegramBackup.IntervalHours <= 0 {
		config.TelegramBackup.IntervalHours = 24
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

// 🆕 تابع کمکی برای ذخیره ۳ لاگ اخیر بکاپ‌ها
func AddBackupLog(log BackupLog) {
	configMu.Lock()
	defer configMu.Unlock()
	
	// اضافه کردن به ابتدای لیست
	config.BackupLogs = append([]BackupLog{log}, config.BackupLogs...)
	
	// نگهداری فقط ۳ تای اول
	if len(config.BackupLogs) > 3 {
		config.BackupLogs = config.BackupLogs[:3]
	}
	
	// ذخیره مستقیم
	data, err := json.MarshalIndent(config, "", "  ")
	if err == nil {
		os.WriteFile(configPath, data, 0600)
	}
}
