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

// GenerateConfigFromOrder با یک نام کاربری یکسان، از همه پنل‌ها کانفیگ می‌گیره
func GenerateConfigFromOrder(order models.Order) (string, error) {
	cfg := db.GetConfig()
	if len(cfg.Panels) == 0 {
		return "", fmt.Errorf("هیچ پنلی در تنظیمات تعریف نشده است")
	}

	volumeGB, days := planToVolumeAndDays(order.PlanName)

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		// 🎯 یک نام کاربری یکسان برای همه پنل‌ها
		username := generateUsername()
		log.Printf("🎯 تلاش ساخت با نام کاربری یکسان: %s", username)

		items, anyDup, err := createOnAllPanels(cfg.Panels, username, volumeGB, days)

		if len(items) > 0 {
			if len(items) == len(cfg.Panels) {
				log.Printf("✅ همه پنل‌ها با نام %s موفق بودند", username)
			} else {
				log.Printf("⚠️ برخی پنل‌ها با نام %s موفق نبودند", username)
			}
			jsonData, _ := json.Marshal(items)
			return string(jsonData), nil
		}

		lastErr = err
		if anyDup {
			log.Printf("⚠️ نام %s تکراری بود، تلاش بعدی...", username)
			continue
		}
		return "", err
	}

	return "", fmt.Errorf("پس از چند تلاش، نام کاربری آزاد یافت نشد: %v", lastErr)
}

// createOnAllPanels با یک نام کاربری، روی همه پنل‌ها کاربر می‌سازه
func createOnAllPanels(panels []db.PanelConfig, username string, volumeGB, days int) ([]ConfigItem, bool, error) {
	var items []ConfigItem
	var errors []string
	anyDup := false

	for _, panel := range panels {
		panelVolume := volumeGB
		if panel.IsBackup && panel.BackupGB > 0 {
			panelVolume = panel.BackupGB
		}

		var link string
		var err error

		switch panel.Type {
		case "guards":
			link, err = CreateGuardsUser(panel, username, panelVolume, days)
		case "marzban":
			link, err = CreateMarzbanUser(panel, username, panelVolume, days)
		default:
			err = fmt.Errorf("نوع پنل ناشناخته: %s", panel.Type)
		}

		if err != nil {
			log.Printf("❌ خطا در پنل %s: %v", panel.Name, err)
			errors = append(errors, fmt.Sprintf("%s: %v", panel.Name, err))
			if isDuplicateError(err) {
				anyDup = true
			}
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
		return nil, anyDup, fmt.Errorf("همه پنل‌ها خطا دادند: %s", strings.Join(errors, " | "))
	}
	if len(errors) > 0 {
		return items, anyDup, fmt.Errorf("برخی پنل‌ها خطا دادند: %s", strings.Join(errors, " | "))
	}
	return items, false, nil
}
