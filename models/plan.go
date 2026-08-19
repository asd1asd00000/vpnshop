package models

import (
	"encoding/json"
	"os"
)

type Plan struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Price     int    `json:"price"`
	VolumeGB  int    `json:"volume_gb"`
	Days      int    `json:"days"`
	IsSpecial bool   `json:"is_special"`

	// ✅ فیلدهای جدید
	BackupGB int    `json:"backup_gb"` // حجم زاپاس (پنل نقش backup)
	GiftGB   int    `json:"gift_gb"`   // حجم هدیه (پنل نقش gift)
	GiftNote string `json:"gift_note"` // توضیح هدیه (مثلاً "پشتیبانی ندارد")
}

// LoadPlans تابع کمکی برای خواندن سریع فایل پلن‌ها
func LoadPlans() ([]Plan, error) {
	file, err := os.ReadFile("plans.json")
	if err != nil {
		return nil, err
	}
	var plans []Plan
	err = json.Unmarshal(file, &plans)
	return plans, err
}
