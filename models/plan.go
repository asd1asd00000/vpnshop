package models

import (
	"encoding/json"
	"os"

	"github.com/asd1asd00000/vpnshop/db"
)

type Plan struct {
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

// LoadPlans تمام پلن‌ها (فعال و غیرفعال) را از config.json (با fallback به plans.json) می‌خواند
func LoadPlans() ([]Plan, error) {
	cfg := db.GetConfig()
	if cfg != nil && len(cfg.Plans) > 0 {
		plans := make([]Plan, 0, len(cfg.Plans))
		for _, p := range cfg.Plans {
			plans = append(plans, Plan{
				ID:        p.ID,
				Title:     p.Title,
				Price:     p.Price,
				VolumeGB:  p.VolumeGB,
				Days:      p.Days,
				IsSpecial: p.IsSpecial,
				BackupGB:  p.BackupGB,
				GiftGB:    p.GiftGB,
				GiftNote:  p.GiftNote,
				Disabled:  p.Disabled,
			})
		}
		return plans, nil
	}

	file, err := os.ReadFile("plans.json")
	if err != nil {
		return nil, err
	}
	var plans []Plan
	err = json.Unmarshal(file, &plans)
	return plans, err
}

// LoadActivePlans فقط پلن‌های فعال را برای نمایش و فروش در فروشگاه برمی‌گرداند
func LoadActivePlans() ([]Plan, error) {
	all, err := LoadPlans()
	if err != nil {
		return nil, err
	}
	active := make([]Plan, 0, len(all))
	for _, p := range all {
		if !p.Disabled {
			active = append(active, p)
		}
	}
	return active, nil
}
