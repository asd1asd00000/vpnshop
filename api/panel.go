package api

import (
	"fmt"
	"log"
	"strings"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

// GenerateConfigFromOrder از همه پنل‌های فعال کانفیگ می‌گیره و ترکیب می‌کنه
func GenerateConfigFromOrder(order models.Order) (string, error) {
	cfg := db.GetConfig()

	if len(cfg.Panels) == 0 {
		return "", fmt.Errorf("هیچ پنلی در تنظیمات تعریف نشده است")
	}

	volumeGB, days := planToVolumeAndDays(order.PlanName)

	var configParts []string
	var errors []string

	for _, panel := range cfg.Panels {
		// تعیین حجم: اگه زاپاس باشه، از BackupGB استفاده کن
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
			// برای Marzban از یه یوزرنیم یکسان بین همه پنل‌ها استفاده می‌کنیم
			link, err = CreateMarzbanUser(panel, order.TrackingCode, panelVolume, days)
		default:
			err = fmt.Errorf("نوع پنل ناشناخته: %s", panel.Type)
		}

		if err != nil {
			log.Printf("❌ خطا در ساخت کانفیگ برای پنل %s: %v", panel.Name, err)
			errors = append(errors, fmt.Sprintf("%s: %v", panel.Name, err))
			continue
		}

		// فرمت‌بندی بر اساس نوع
		var formatted string
		switch panel.Type {
		case "guards":
			formatted = FormatGuardsConfig(panel, link, panelVolume)
		case "marzban":
			formatted = FormatMarzbanConfig(panel, link, panelVolume)
		default:
			formatted = link
		}

		configParts = append(configParts, formatted)
	}

	if len(configParts) == 0 {
		return "", fmt.Errorf("همه پنل‌ها خطا دادند: %s", strings.Join(errors, " | "))
	}

	// ترکیب همه کانفیگ‌ها با خط خالی
	finalConfig := strings.Join(configParts, "\n\n")

	if len(errors) > 0 {
		log.Printf("⚠️ برخی پنل‌ها موفق نبودند: %s", strings.Join(errors, " | "))
	}

	return finalConfig, nil
}
