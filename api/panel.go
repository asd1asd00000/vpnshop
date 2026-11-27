package api

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

// ConfigItem یک کانفیگ جداگانه برای نمایش
type ConfigItem struct {
	Title     string `json:"title"`
	Desc      string `json:"desc"`
	Link      string `json:"link"`
	Role      string `json:"role"`       // main | backup | gift
	Volume    int    `json:"volume"`     // حجم به GB
	Note      string `json:"note"`       // توضیح هدیه
	Username  string `json:"username"`   // نام کاربری
	PanelType string `json:"panel_type"` // نوع پنل (guards | marzban | conf)
}

// planVolumes حجم‌های تعریف‌شده در پلن
type planVolumes struct {
	mainGB   int
	backupGB int
	giftGB   int
	days     int
	giftNote string
}

// rolePriority تعیین اولویت نمایش: ۱. اصلی (main) ۲. زاپاس (backup) ۳. هدیه (gift)
func rolePriority(role string) int {
	switch role {
	case "main", "":
		return 1
	case "backup":
		return 2
	case "gift":
		return 3
	default:
		return 4
	}
}

// sortConfigItems مرتب‌سازی کانفیگ‌ها به ترتیب: اصلی -> زاپاس -> هدیه
func sortConfigItems(items []ConfigItem) []ConfigItem {
	sorted := make([]ConfigItem, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		return rolePriority(sorted[i].Role) < rolePriority(sorted[j].Role)
	})
	return sorted
}

// resolveOrderPlanVolumes اول از Snapshot قفل‌شده در خود فاکتور می‌خواند؛ برای فاکتورهای قدیمی به لیست پلن‌ها رجوع می‌کند
func resolveOrderPlanVolumes(order models.Order) (planVolumes, bool) {
	if order.VolumeGB > 0 {
		days := order.PlanDays
		if days <= 0 {
			days = 30
		}
		return planVolumes{
			mainGB:   order.VolumeGB,
			backupGB: order.BackupGB,
			giftGB:   order.GiftGB,
			days:     days,
			giftNote: order.GiftNote,
		}, true
	}

	plans, _ := models.LoadPlans()
	for _, p := range plans {
		if p.ID == order.PlanName {
			return planVolumes{
				mainGB:   p.VolumeGB,
				backupGB: p.BackupGB,
				giftGB:   p.GiftGB,
				days:     p.Days,
				giftNote: p.GiftNote,
			}, true
		}
	}
	return planVolumes{mainGB: 20, days: 30}, false
}

func GenerateConfigFromOrder(order models.Order) (string, error) {
	// خواندن اطلاعات تمدید و Snapshot قفل‌شده پلن مستقیماً از دیتابیس
	if order.ID > 0 {
		db.DB.QueryRow(`SELECT IFNULL(renew_username, ''), IFNULL(carry_gb, 0),
		                       IFNULL(plan_days, 0), IFNULL(volume_gb, 0),
		                       IFNULL(backup_gb, 0), IFNULL(gift_gb, 0), IFNULL(gift_note, '')
		                FROM orders WHERE id = ?`, order.ID).
			Scan(&order.RenewUsername, &order.CarryGB, &order.PlanDays, &order.VolumeGB, &order.BackupGB, &order.GiftGB, &order.GiftNote)
	}

	// اگه تمدید هست، از منطق تمدید با Snapshot استفاده کن
	if order.RenewUsername != "" {
		log.Printf("🔄 [تمدید] شناسایی شد: username=%s, carry=%dGB", order.RenewUsername, order.CarryGB)
		return generateConfigFromRenewalOrder(order)
	}

	cfg := db.GetConfig()
	if len(cfg.Panels) == 0 {
		return "", fmt.Errorf("هیچ پنلی در تنظیمات تعریف نشده است")
	}

	pv, _ := resolveOrderPlanVolumes(order)

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		username := generateUsername()
		log.Printf("🎯 تلاش ساخت با نام کاربری یکسان: %s", username)

		items, anyDup, err := createOnAllPanels(cfg.Panels, username, pv)

		if len(items) > 0 {
			items = sortConfigItems(items)
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

func createOnAllPanels(panels []db.PanelConfig, username string, pv planVolumes) ([]ConfigItem, bool, error) {
	var items []ConfigItem
	var errors []string
	anyDup := false

	for _, panel := range panels {
		var panelVolume int
		switch panel.Role {
		case "backup":
			panelVolume = pv.backupGB
		case "gift":
			panelVolume = pv.giftGB
		default: // main
			panelVolume = pv.mainGB
		}

		if panelVolume <= 0 {
			continue
		}

		var link string
		var err error

		switch panel.Type {
		case "guards":
			link, err = CreateGuardsUser(panel, username, panelVolume, pv.days)
		case "marzban":
			link, err = CreateMarzbanUser(panel, username, panelVolume, pv.days)
		case "conf":
			link, err = CreateConfUser(panel, username, panelVolume, pv.days)
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
		volDesc := fmt.Sprintf("حجم: %dGB", panelVolume)
		if panel.Type == "conf" {
			volDesc = confVolumeDesc(panelVolume)
		}
		switch panel.Role {
		case "backup":
			title = fmt.Sprintf("🛡️ %s — کانفیگ زاپاس", panelName)
			desc = volDesc + " | برای مواقع اضطراری"
		case "gift":
			title = fmt.Sprintf("🎁 %s — کانفیگ هدیه", panelName)
			desc = volDesc + " | " + pv.giftNote
		default:
			title = fmt.Sprintf("🛡️ %s — کانفیگ اصلی", panelName)
			if panel.Type == "conf" {
				desc = volDesc
			} else {
				desc = volDesc + fmt.Sprintf(" | مدت: %d روز", pv.days)
			}
		}

		role := panel.Role
		if role == "" {
			role = "main"
		}

		items = append(items, ConfigItem{
			Title:     title,
			Desc:      desc,
			Link:      link,
			Role:      role,
			Volume:    panelVolume,
			Note:      pv.giftNote,
			Username:  username,
			PanelType: panel.Type,
		})
	}

	items = sortConfigItems(items)

	if len(items) == 0 {
		return nil, anyDup, fmt.Errorf("همه پنل‌ها خطا دادند: %s", strings.Join(errors, " | "))
	}
	if len(errors) > 0 {
		return items, anyDup, fmt.Errorf("برخی پنل‌ها خطا دادند: %s", strings.Join(errors, " | "))
	}
	return items, false, nil
}

// generateConfigFromRenewalOrder تمدید اشتراک با استفاده از Snapshot قفل‌شده در فاکتور
func generateConfigFromRenewalOrder(order models.Order) (string, error) {
	cfg := db.GetConfig()
	if len(cfg.Panels) == 0 {
		return "", fmt.Errorf("هیچ پنلی در تنظیمات تعریف نشده است")
	}

	pv, found := resolveOrderPlanVolumes(order)
	if !found {
		return "", fmt.Errorf("پلن یافت نشد")
	}

	carryGB := order.CarryGB
	if carryGB < 0 {
		carryGB = 0
	}

	mainVolume := pv.mainGB + carryGB
	backupVolume := pv.backupGB
	giftVolume := pv.giftGB

	items, err := renewOnAllPanels(cfg.Panels, order.RenewUsername, mainVolume, backupVolume, giftVolume, pv.days, pv.giftNote)
	if len(items) == 0 {
		return "", err
	}
	if err != nil {
		log.Printf("⚠️ [تمدید] برخی پنل‌های فرعی خطا دادند اما تمدید انجام شد: %v", err)
	}

	items = sortConfigItems(items)
	jsonData, _ := json.Marshal(items)
	return string(jsonData), nil
}

// GenerateConfigFromRenewal تمدید اشتراک با carry-over (سازگاری عقب‌مانده)
func GenerateConfigFromRenewal(renewUsername string, planID string, carryGB int) (string, error) {
	return generateConfigFromRenewalOrder(models.Order{
		RenewUsername: renewUsername,
		PlanName:      planID,
		CarryGB:       carryGB,
	})
}

// renewOnAllPanels تمدید روی همه پنل‌ها
func renewOnAllPanels(panels []db.PanelConfig, username string, mainGB, backupGB, giftGB, days int, giftNote string) ([]ConfigItem, error) {
	var items []ConfigItem
	var errors []string

	for _, panel := range panels {
		var panelVolume int
		switch panel.Role {
		case "backup":
			panelVolume = backupGB
		case "gift":
			panelVolume = giftGB
		default:
			panelVolume = mainGB
		}

		if panelVolume <= 0 {
			continue
		}

		var link string
		var err error

		switch panel.Type {
		case "guards":
			link, err = UpdateGuardsUser(panel, username, panelVolume, days)
		case "marzban":
			link, err = RenewMarzbanUser(panel, username, panelVolume, days)
		case "conf":
			link, err = UpdateConfUser(panel, username, panelVolume, days)
		default:
			err = fmt.Errorf("نوع پنل ناشناخته: %s", panel.Type)
		}

		if err != nil {
			log.Printf("❌ خطا در تمدید پنل %s: %v", panel.Name, err)
			errors = append(errors, fmt.Sprintf("%s: %v", panel.Name, err))
			continue
		}

		panelName := panel.Name
		if panelName == "" {
			panelName = panel.Type
		}

		var title, desc string
		volDesc := fmt.Sprintf("حجم: %dGB", panelVolume)
		if panel.Type == "conf" {
			volDesc = confVolumeDesc(panelVolume)
		}
		switch panel.Role {
		case "backup":
			title = fmt.Sprintf("🛡️ %s — کانفیگ زاپاس", panelName)
			desc = volDesc + " | برای مواقع اضطراری"
		case "gift":
			title = fmt.Sprintf("🎁 %s — کانفیگ هدیه", panelName)
			desc = volDesc + " | " + giftNote
		default:
			title = fmt.Sprintf("🛡️ %s — کانفیگ اصلی", panelName)
			if panel.Type == "conf" {
				desc = volDesc
			} else {
				desc = volDesc + fmt.Sprintf(" | مدت: %d روز", days)
			}
		}

		role := panel.Role
		if role == "" {
			role = "main"
		}

		items = append(items, ConfigItem{
			Title:     title,
			Desc:      desc,
			Link:      link,
			Role:      role,
			Volume:    panelVolume,
			Note:      giftNote,
			Username:  username,
			PanelType: panel.Type,
		})
	}

	items = sortConfigItems(items)

	if len(items) == 0 {
		return nil, fmt.Errorf("همه پنل‌ها خطا دادند: %s", strings.Join(errors, " | "))
	}
	if len(errors) > 0 {
		return items, fmt.Errorf("برخی پنل‌ها خطا دادند: %s", strings.Join(errors, " | "))
	}
	return items, nil
}

// buildTelegramText متن آماده کپی برای تلگرام می‌سازه
func buildTelegramText(items []ConfigItem) string {
	sortedItems := sortConfigItems(items)

	var header strings.Builder
	header.WriteString("🛡️ **Oklavpn** 🛡️\n")
	header.WriteString("👉 https://t.me/oklavpn\n")

	if len(sortedItems) > 0 && sortedItems[0].Username != "" {
		header.WriteString("\n")
		header.WriteString(fmt.Sprintf("👤 نام کاربری: `%s`\n", sortedItems[0].Username))

		for _, it := range sortedItems {
			if it.Role == "main" || it.Role == "" {
				if strings.Contains(it.Desc, "روز") {
					header.WriteString(fmt.Sprintf("📅 اعتبار: %s\n", strings.TrimSpace(it.Desc)))
				}
				break
			}
		}
	}

	header.WriteString("------------------------------------------------------\n")

	var blocks []string

	for _, it := range sortedItems {
		var b strings.Builder

		volText := fmt.Sprintf("%d گیگ", it.Volume)
		if it.PanelType == "conf" {
			volText = fmt.Sprintf("%d روز (حجم نامحدود)", it.Volume*confDaysPerGB)
		}

		switch it.Role {
		case "backup":
			b.WriteString(it.Link + "\n\n")
			b.WriteString(fmt.Sprintf("✅ %s هدیه-زاپاس ✅\n", volText))
			b.WriteString("اگه لینک اصلی مشکل پیدا کرد اطلاع بدین\n")
			b.WriteString("تا مشکل حل بشه از این لینک استفاده کنید")
		case "gift":
			b.WriteString(it.Link + "\n\n")
			b.WriteString(fmt.Sprintf("✅ %s هدیه ✅\n", volText))
			b.WriteString("از پنل آزمایشی\n")
			b.WriteString("تست کنید ببینید در منطقه شما جواب میده ؟\n")
			b.WriteString("پنل آزمایشی پشتیبانی نداره و حجم آن قابل انتقال نیست\n")
			if it.Note != "" {
				b.WriteString(it.Note)
			}
		default: // main (اصلی)
			b.WriteString(" 👇👇👇**لینک اشتراک اصلی (ابتدا این لینک را وارد کنید):** \n\n")
			b.WriteString("**" + it.Link + "**\n\n")
			b.WriteString(fmt.Sprintf("✔️ **%s اشتراک اصلی شما** ✔️\n", volText))
			b.WriteString("⬆️ **لینک بالا اشتراک اصلی شماست ** ⬆️")
		}
		blocks = append(blocks, b.String())
	}

	return header.String() + strings.Join(blocks, "\n------------------------------------------------------\n")
}
