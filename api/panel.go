package api

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

// ConfigItem یک کانفیگ جداگانه برای نمایش به مشتری
type ConfigItem struct {
	Title string `json:"title"`
	Desc  string `json:"desc"`
	Link  string `json:"link"`
}

// GenerateConfigFromOrder از همه پنل‌ها کانفیگ می‌گیره و به صورت JSON برمی‌گردونه
func GenerateConfigFromOrder(order models.Order) (string, error) {
	cfg := db.GetConfig()
	if len(cfg.Panels) == 0 {
		return "", fmt.Errorf("هیچ پنلی در تنظیمات تعریف نشده است")
	}

	volumeGB, days := planToVolumeAndDays(order.PlanName)

	var items []ConfigItem
	var errors []string

	for _, panel := range cfg.Panels {
		panelVolume := volumeGB
		if panel.IsBackup && panel.BackupGB > 0 {
			panelVolume = panel.BackupGB
		}

		var link string
		var err error

		switch panel.Type {
		case "guards":
			link, err = CreateGuardsUser(panel, order, panelVolume, days)
		case "marzban":
			link, err = CreateMarzbanUser(panel, order.TrackingCode, panelVolume, days)
		default:
			err = fmt.Errorf("نوع پنل ناشناخته: %s", panel.Type)
		}

		if err != nil {
			log.Printf("❌ خطا در ساخت کانفیگ برای پنل %s: %v", panel.Name, err)
			errors = append(errors, fmt.Sprintf("%s: %v", panel.Name, err))
			continue
		}

		panelName := panel.Name
		if panelName == "" {
			panelName = panel.Type
		}

		var title, desc string
		if panel.IsBackup {
			title = fmt.Sprintf("🛡️ %s — کانفیگ زاپاس", panelName)
			desc = fmt.Sprintf("حجم: %dGB | برای مواقع اضطراری", panelVolume)
		} else {
			title = fmt.Sprintf("🛡️ %s — کانفیگ اصلی", panelName)
			desc = fmt.Sprintf("حجم: %dGB | مدت: %d روز", panelVolume, days)
		}

		items = append(items, ConfigItem{Title: title, Desc: desc, Link: link})
	}

	if len(items) == 0 {
		return "", fmt.Errorf("همه پنل‌ها خطا دادند: %s", strings.Join(errors, " | "))
	}

	if len(errors) > 0 {
		log.Printf("⚠️ برخی پنل‌ها موفق نبودند: %s", strings.Join(errors, " | "))
	}

	jsonData, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(jsonData), nil
}
