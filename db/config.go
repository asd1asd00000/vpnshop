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

// PlanConfig اطلاعات یک پلن فروش (ذخیره‌شده در config.json برای بکاپ و حفظ در آپدیت)
type PlanConfig struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Price     int    `json:"price"`
	VolumeGB  int    `json:"volume_gb"`
	Days      int    `json:"days"`
	IsSpecial bool   `json:"is_special"`
	BackupGB  int    `json:"backup_gb"`
	GiftGB    int    `json:"gift_gb"`
	GiftNote  string `json:"gift_note"`
	Disabled  bool   `json:"disabled"`
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

type TelegramBackupConfig struct {
	Enabled       bool   `json:"enabled"`
	BotToken      string `json:"bot_token"`
	ChatID        string `json:"chat_id"`
	IntervalHours int    `json:"interval_hours"`
	ZipPassword   string `json:"zip_password"`
}

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
	Plans          []PlanConfig         `json:"plans"`
	Cards          []CardInfo           `json:"cards"`
	EmailBackup    EmailBackupConfig    `json:"email_backup"`
	TelegramBackup TelegramBackupConfig `json:"telegram_backup"`
	BackupLogs     []BackupLog          `json:"backup_logs"`
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
		Plans:          []PlanConfig{},
		Cards:          []CardInfo{},
		EmailBackup:    EmailBackupConfig{SMTPPort: 587},
		TelegramBackup: TelegramBackupConfig{},
		BackupLogs:     []BackupLog{},
	}

	configExists := false
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Println("ℹ️ فایل config.json وجود ندارد")
	} else {
		configExists = true
		if err := json.Unmarshal(data, config); err != nil {
			log.Printf("❌ خطا در خواندن config.json: %v", err)
		}
	}

	// انتقال خودکار پلن‌ها از plans.json به config.json در اولین اجرا
	if len(config.Plans) == 0 {
		if planBytes, perr := os.ReadFile("plans.json"); perr == nil {
			var legacyPlans []PlanConfig
			if jerr := json.Unmarshal(planBytes, &legacyPlans); jerr == nil && len(legacyPlans) > 0 {
				config.Plans = legacyPlans
				log.Printf("✅ %d پلن به‌صورت خودکار از plans.json به config.json منتقل شد", len(legacyPlans))
				if configExists {
					if out, merr := json.MarshalIndent(config, "", "  "); merr == nil {
						os.WriteFile(configPath, out, 0600)
					}
				}
			}
		}
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
	if config != nil {
		defer configMu.RUnlock()
		return config
	}
	configMu.RUnlock()
	return LoadConfig()
}

func AddBackupLog(logEntry BackupLog) {
	configMu.Lock()
	defer configMu.Unlock()

	config.BackupLogs = append([]BackupLog{logEntry}, config.BackupLogs...)

	if len(config.BackupLogs) > 3 {
		config.BackupLogs = config.BackupLogs[:3]
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err == nil {
		os.WriteFile(configPath, data, 0600)
	}
}
