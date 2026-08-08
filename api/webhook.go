package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

type SMSRequest struct {
	Text  string `json:"text"`
	Token string `json:"token"`
}

const webhookSecret = "KHIHgu1451lhgugiu54DFG51FDLOI"

func WebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "فقط متد POST مجاز است", http.StatusMethodNotAllowed)
		return
	}

	// === بخش اصلاح شده: خواندن کامل Body و لاگ کردن آن ===
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("❌ خطا در خواندن body: %v", err)
		http.Error(w, "خطا در خواندن داده‌ها", http.StatusBadRequest)
		return
	}
	log.Printf("📦 body خام: [%s]", string(bodyBytes))

	var req SMSRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		log.Printf("❌ خطای JSON parse: %v", err)
		http.Error(w, "خطا در خواندن داده‌ها", http.StatusBadRequest)
		return
	}
	// =======================================================

	if req.Token != webhookSecret {
		log.Println("⚠️ دسترسی غیرمجاز: توکن نامعتبر")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	log.Printf("📩 پیامک دریافتی (خام): [%s]", req.Text)
	log.Printf("📏 طول پیامک: %d کاراکتر", len(req.Text))

	// ۱. نرمال‌سازی متن: تبدیل newlines به space برای جستجوی آسان‌تر
	normalizedText := normalizeText(req.Text)
	log.Printf("📩 پیامک نرمال‌شده: [%s]", normalizedText)

	// ۲. تبدیل اعداد فارسی/عربی به انگلیسی
	englishText := convertPersianNumbersToEnglish(normalizedText)

	// ۳. استخراج مبلغ
	amountRial := extractAmount(englishText)
	if amountRial == 0 {
		log.Println("❌ مبلغی در پیام یافت نشد")
		w.WriteHeader(http.StatusOK)
		return
	}

	// ۴. تبدیل ریال به تومان
	amountToman := amountRial / 10
	log.Printf("💰 ریال: %d | تومان: %d", amountRial, amountToman)

	// ۵. بررسی در دیتابیس
	order := verifyPaymentInDB(amountToman)
	if order == nil {
		log.Printf("❌ فاکتوری برای %d تومان یافت نشد", amountToman)
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("✅ سفارش %s تایید شد!", order.TrackingCode)

	// ۶. ساخت کانفیگ
	link, err := GenerateConfigFromOrder(*order)
	if err != nil {
		log.Printf("❌ خطا در ساخت کانفیگ: %v", err)
	} else {
		log.Printf("🔗 کانفیگ: %s", link)
		db.DB.Exec(`UPDATE orders SET config_link = ? WHERE id = ?`, link, order.ID)
	}

	w.WriteHeader(http.StatusOK)
}

// ==========================================
// توابع کمکی در ادامه قرار می‌گیرند
// ==========================================

// normalizeText تمام newlines و کاراکترهای اضافی رو به space تبدیل می‌کنه
func normalizeText(text string) string {
	// تبدیل \n و \r به space
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\t", " ")

	// تبدیل نیم‌فاصله و کاراکترهای خاص به space
	text = strings.ReplaceAll(text, "\u200c", " ") // نیم‌فاصله
	text = strings.ReplaceAll(text, "\u200f", "")  // RTL mark
	text = strings.ReplaceAll(text, "\u200e", "")  // LTR mark

	// حذف ستاره‌ها (فرمت بله)
	text = strings.ReplaceAll(text, "*", "")

	// فشرده‌سازی space‌های متعدد به یک space
	spaceRegex := regexp.MustCompile(`\s+`)
	text = spaceRegex.ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

func convertPersianNumbersToEnglish(text string) string {
	persian := []string{"۰", "۱", "۲", "۳", "۴", "۵", "۶", "۷", "۸", "۹"}
	arabic := []string{"٠", "١", "٢", "٣", "٤", "٥", "٦", "٧", "٨", "٩"}
	english := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

	for i := 0; i < 10; i++ {
		text = strings.ReplaceAll(text, persian[i], english[i])
		text = strings.ReplaceAll(text, arabic[i], english[i])
	}
	return text
}

// extractAmount با چندین پترن مختلف مبلغ رو استخراج می‌کنه
func extractAmount(text string) int {
	log.Printf("🔍 جستجوی مبلغ در: [%s]", text)

	// لیست پترن‌ها به ترتیب اولویت
	patterns := []struct {
		name string
		re   *regexp.Regexp
	}{
		{"انتقال با :", regexp.MustCompile(`انتقال[:\s]*([\d,]+)`)},
		{"مبلغ با ریال", regexp.MustCompile(`مبلغ[\s:*]*([\d,]+)`)},
		{"عدد با پلاس", regexp.MustCompile(`([\d,]+)\+`)},
		{"انتقال ساده", regexp.MustCompile(`انتقال\s+([\d,]+)`)},
		{"عدد بزرگ", regexp.MustCompile(`\b(\d{1,3}(?:,\d{3})+|\d{4,})\b`)},
	}

	for _, p := range patterns {
		match := p.re.FindStringSubmatch(text)
		if len(match) > 1 {
			cleanStr := strings.ReplaceAll(match[1], ",", "")
			val, err := strconv.Atoi(cleanStr)
			if err == nil && val > 0 {
				log.Printf("✅ پترن [%s] match شد: %d", p.name, val)
				return val
			}
		}
	}

	log.Println("❌ هیچ پترنی match نشد")
	return 0
}

func verifyPaymentInDB(amountToman int) *models.Order {
	var order models.Order
	query := `SELECT id, tracking_code, plan_name, base_price, unique_amount, status, IFNULL(config_link, '') 
	          FROM orders WHERE unique_amount = ? AND status = 'pending'`

	err := db.DB.QueryRow(query, amountToman).Scan(
		&order.ID, &order.TrackingCode, &order.PlanName, &order.BasePrice,
		&order.UniqueAmount, &order.Status, &order.ConfigLink,
	)

	if err != nil {
		return nil
	}

	db.DB.Exec(`UPDATE orders SET status = 'paid' WHERE id = ?`, order.ID)
	order.Status = "paid"
	return &order
}
