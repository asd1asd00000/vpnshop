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
	BackupGB  int    `json:"backup_gb"` // حجم زاپاس (پنل نقش backup)
	GiftGB    int    `json:"gift_gb"`   // حجم هدیه (پنل نقش gift)
	GiftNote  string `json:"gift_note"` // توضیح هدیه
	Disabled  bool   `json:"disabled"`  // true = غیرفعال موقت و مخفی در فروشگاه
}

// LoadPlansFromFile خواندن مستقیم پلن‌ها از یک فایل JSON (مثل plans.json)
func LoadPlansFromFile(path string) ([]Plan, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plans []Plan
	err = json.Unmarshal(file, &plans)
	return plans, err
}

// LoadPlans اولویت با پلن‌های داخل config.json است؛ اگر نبود از plans.json می‌خواند
func LoadPlans() ([]Plan, error) {
	if data, err := os.ReadFile("config.json"); err == nil {
		var wrapper struct {
			Plans *[]Plan `json:"plans"`
		}
		if jerr := json.Unmarshal(data, &wrapper); jerr == nil && wrapper.Plans != nil {
			return *wrapper.Plans, nil
		}
	}
	return LoadPlansFromFile("plans.json")
}
