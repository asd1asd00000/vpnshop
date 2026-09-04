package api

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

// ConfigItem یک کانفیگ جداگانه برای نمایش
type ConfigItem struct {
	Title    string `json:"title"`
	Desc     string `json:"desc"`
	Link     string `json:"link"`
	Role     string `json:"role"`     // main | backup | gift
	Volume   int    `json:"volume"`   // حجم به GB
	Note     string `json:"note"`     // توضیح هدیه
	Username string `json:"username"` // 🎯 نام کاربری
}

// planVolumes حجم‌های تعریف‌شده در پلن
type planVolumes struct {
	mainGB   int
	backupGB int
	giftGB   int
	days     int
	giftNote string
}

func GenerateConfigFromOrder(order models.Order) (string, error) {
	// 🎯 اگه تمدید هست، از منطق تمدید استفاده کن
	if order.RenewUsername != "" {
		return GenerateConfigFromRenewal(order.RenewUsername, order.PlanName, order.CarryGB)
	}

	// 🎯 خواندن حجم‌ها از پلن
	pv := planVolumes{mainGB: 20, days: 30}
	plans, _ := models.LoadPlans()
	for _, p := range plans {
		if p.ID == order.PlanName {
			pv.mainGB = p.VolumeGB
			pv.days = p.Days
			pv.backupGB = p.BackupGB
			pv.giftGB = p.GiftGB
			pv.giftNote = p.GiftNote
			break
		}
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		username := generateUsername()
		log.Printf("🎯 تلاش ساخت با نام کاربری یکسان: %s", username)

		items, anyDup, err := createOnAllPanels(cfg.Panels, username, pv)

		if len(items) > 0 {
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
		// 🎯 تعیین حجم بر اساس نقش پنل (از پلن)
		var panelVolume int
		switch panel.Role {
		case "backup":
			panelVolume = pv.backupGB
		case "gift":
			panelVolume = pv.giftGB
		default: // main
			panelVolume = pv.mainGB
		}

		// اگه حجمی برای این نقش تعریف نشده، رد شو
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
		switch panel.Role {
		case "backup":
			title = fmt.Sprintf("🛡️ %s — کانفیگ زاپاس", panelName)
			desc = fmt.Sprintf("حجم: %dGB | برای مواقع اضطراری", panelVolume)
		case "gift":
			title = fmt.Sprintf("🎁 %s — کانفیگ هدیه", panelName)
			desc = fmt.Sprintf("حجم: %dGB | %s", panelVolume, pv.giftNote)
		default:
			title = fmt.Sprintf("🛡️ %s — کانفیگ اصلی", panelName)
			desc = fmt.Sprintf("حجم: %dGB | مدت: %d روز", panelVolume, pv.days)
		}

		// 🎯 ساخت ConfigItem با فیلدهای جدید (role, volume, note, username)
		role := panel.Role
		if role == "" {
			role = "main"
		}

		items = append(items, ConfigItem{
			Title:    title,
			Desc:     desc,
			Link:     link,
			Role:     role,
			Volume:   panelVolume,
			Note:     pv.giftNote,
			Username: username,
		})
	}

	if len(items) == 0 {
		return nil, anyDup, fmt.Errorf("همه پنل‌ها خطا دادند: %s", strings.Join(errors, " | "))
	}
	if len(errors) > 0 {
		return items, anyDup, fmt.Errorf("برخی پنل‌ها خطا دادند: %s", strings.Join(errors, " | "))
	}
	return items, false, nil
}
// GenerateConfigFromRenewal تمدید اشتراک با carry-over
func GenerateConfigFromRenewal(renewUsername string, planID string, carryGB int) (string, error) {
	cfg := db.GetConfig()
	if len(cfg.Panels) == 0 {
		return "", fmt.Errorf("هیچ پنلی در تنظیمات تعریف نشده است")
	}

	// خواندن حجم و روز از پلن
	plans, _ := models.LoadPlans()
	var selectedPlan *models.Plan
	for _, p := range plans {
		if p.ID == planID {
			selectedPlan = &p
			break
		}
	}
	if selectedPlan == nil {
		return "", fmt.Errorf("پلن یافت نشد")
	}

	pv := planVolumes{
		mainGB:   selectedPlan.VolumeGB,
		backupGB: selectedPlan.BackupGB,
		giftGB:   selectedPlan.GiftGB,
		days:     selectedPlan.Days,
		giftNote: selectedPlan.GiftNote,
	}

	// حجم‌های نهایی = پلن + carry-over
	mainVolume := pv.mainGB + carryGB
	backupVolume := pv.backupGB + carryGB
	giftVolume := pv.giftGB

	items, err := renewOnAllPanels(cfg.Panels, renewUsername, mainVolume, backupVolume, giftVolume, pv.days, pv.giftNote)
	if err != nil {
		return "", err
	}

	jsonData, _ := json.Marshal(items)
	return string(jsonData), nil
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
			link, err = UpdateMarzbanUser(panel, username, panelVolume, days)
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
		switch panel.Role {
		case "backup":
			title = fmt.Sprintf("🛡️ %s — کانفیگ زاپاس", panelName)
			desc = fmt.Sprintf("حجم: %dGB | برای مواقع اضطراری", panelVolume)
		case "gift":
			title = fmt.Sprintf("🎁 %s — کانفیگ هدیه", panelName)
			desc = fmt.Sprintf("حجم: %dGB | %s", panelVolume, giftNote)
		default:
			title = fmt.Sprintf("🛡️ %s — کانفیگ اصلی", panelName)
			desc = fmt.Sprintf("حجم: %dGB | مدت: %d روز", panelVolume, days)
		}

		role := panel.Role
		if role == "" {
			role = "main"
		}

		items = append(items, ConfigItem{
			Title:    title,
			Desc:     desc,
			Link:     link,
			Role:     role,
			Volume:   panelVolume,
			Note:     giftNote,
			Username: username,
		})
	}

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
	// 1. ساخت بخش هدر (لوگو و آدرس)
	var header strings.Builder
	header.WriteString("🛡️ **Oklavpn** 🛡️\n")
	header.WriteString("👉 https://t.me/oklavpn\n")

	// 🎯 اضافه کردن نام کاربری و مدت اعتبار به هدر
	if len(items) > 0 && items[0].Username != "" {
		header.WriteString("\n")
		header.WriteString(fmt.Sprintf("👤 نام کاربری: `%s`\n", items[0].Username))

		// استخراج مدت اعتبار از کانفیگ اصلی (main)
		for _, it := range items {
			if it.Role == "main" {
				// از Description که شامل "مدت: X روز" هست استفاده می‌کنیم
				if strings.Contains(it.Desc, "روز") {
					header.WriteString(fmt.Sprintf("📅 اعتبار: %s\n", strings.TrimSpace(it.Desc)))
				}
				break
			}
		}
	}

	header.WriteString("----------------------------------------\n\n")

	var blocks []string

	// 2. حلقه اصلی
	for _, it := range items {
		var b strings.Builder
		b.WriteString("`" + it.Link + "`\n\n")

		switch it.Role {
		case "backup":
			b.WriteString(fmt.Sprintf("✅ %d گیگ هدیه-زاپاس ✅\n", it.Volume))
			b.WriteString("اگه لینک اصلی مشکل پیدا کرد اطلاع بدین\n")
			b.WriteString("تا مشکل حل بشه از این لینک استفاده کنید")
		case "gift":
			b.WriteString(fmt.Sprintf("✅ %d گیگ هدیه ✅\n", it.Volume))
			b.WriteString("از پنل آزمایشی\n")
			b.WriteString("تست کنید ببینید در منطقه شما جواب میده ؟\n")
			b.WriteString("پنل آزمایشی پشتیبانی نداره و حجم آن قابل انتقال نیست\n")
			if it.Note != "" {
				b.WriteString(it.Note)
			}
		default: // main
			b.WriteString(fmt.Sprintf("✅ **%d گیگ اشتراک اصلی شما** ✅", it.Volume))
		}
		blocks = append(blocks, b.String())
	}

	// 3. ترکیب هدر با بقیه متن‌ها
	return header.String() + strings.Join(blocks, "\n----------------------------------------\n")
}
