package api

import (
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

const webhookSecret = "KHIHgu1451lhgugiu54DFG51FDLOI"

func WebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "فقط متد POST مجاز است", http.StatusMethodNotAllowed)
		return
	}

	if r.URL.Query().Get("token") != webhookSecret {
		db.LogEvent("general", "warning", "⚠️ دسترسی غیرمجاز به وب‌هوک: توکن نامعتبر")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	defer r.Body.Close()
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		db.LogEventf("general", "error", "❌ خطا در خواندن body: %v", err)
		http.Error(w, "خطا در خواندن داده‌ها", http.StatusBadRequest)
		return
	}
	smsText := string(bodyBytes)

	db.LogEventf("general", "info", "📩 پیامک دریافتی: [%s]", smsText)

	normalizedText := normalizeText(smsText)
	englishText := convertPersianNumbersToEnglish(normalizedText)

	amountRial := extractAmount(englishText)
	if amountRial == 0 {
		db.LogEvent("general", "warning", "❌ مبلغی در پیام یافت نشد")
		w.WriteHeader(http.StatusOK)
		return
	}

	amountToman := amountRial / 10
	db.LogEventf("general", "info", "💰 ریال: %d | تومان: %d", amountRial, amountToman)

	order := verifyPaymentInDB(amountToman)
	if order == nil {
		db.LogEventf("general", "warning", "❌ فاکتوری برای %d تومان یافت نشد", amountToman)
		w.WriteHeader(http.StatusOK)
		return
	}

	// ✅ لاگ مهم: تراکنش موفق (۳۰ روز نگهداری)
	db.LogEventf("transaction", "success", "✅ سفارش %s تایید شد | مبلغ: %d تومان", order.TrackingCode, amountToman)

	link, err := GenerateConfigFromOrder(*order)
	if err != nil {
		db.LogEventf("config", "error", "❌ خطا در ساخت کانفیگ برای %s: %v", order.TrackingCode, err)
	} else {
		// ✅ لاگ مهم: ساخت کانفیگ (۳۰ روز نگهداری)
		db.LogEventf("config", "success", "🔗 کانفیگ برای %s ساخته شد: %s", order.TrackingCode, link)
		db.DB.Exec(`UPDATE orders SET config_link = ? WHERE id = ?`, link, order.ID)
	}

	w.WriteHeader(http.StatusOK)
}

func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\t", " ")
	text = strings.ReplaceAll(text, "\u200c", " ")
	text = strings.ReplaceAll(text, "\u200f", "")
	text = strings.ReplaceAll(text, "\u200e", "")
	text = strings.ReplaceAll(text, "*", "")

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

func extractAmount(text string) int {
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
				return val
			}
		}
	}
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
