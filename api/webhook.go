package api

import (
	"encoding/json"
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

	var req SMSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "خطا در خواندن داده‌ها", http.StatusBadRequest)
		return
	}

	if req.Token != webhookSecret {
		log.Println("⚠️ دسترسی غیرمجاز: توکن نامعتبر")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// 🎯 پاک‌سازی `Enter`ها تا کل پیامک تبدیل به یک خط تمیز شود
	flatText := strings.ReplaceAll(req.Text, "\n", " ")
	flatText = strings.ReplaceAll(flatText, "\r", " ")
	
	log.Printf("📩 پیامک دریافتی: %s", flatText)

	// ۱. تبدیل اعداد فارسی/عربی به انگلیسی روی متنِ یک‌خطی شده
	englishText := convertPersianNumbersToEnglish(flatText)

	// ۲. استخراج مبلغ (ریال) - با الگوهای قدرتمندِ جدید
	amountRial := extractAmount(englishText)
	if amountRial == 0 {
		log.Println("❌ مبلغی در پیام یافت نشد")
		w.WriteHeader(http.StatusOK)
		return
	}

	// ۳. تبدیل ریال به تومان
	amountToman := amountRial / 10
	log.Printf("💰 ریال: %d | تومان: %d", amountRial, amountToman)

	// ۴. بررسی در دیتابیس
	order := verifyPaymentInDB(amountToman)
	if order == nil {
		log.Printf("❌ فاکتوری برای %d تومان یافت نشد", amountToman)
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("✅ سفارش %s تایید شد!", order.TrackingCode)

	// ۵. ساخت کانفیگ
	link, err := GenerateConfigFromOrder(*order)
	if err != nil {
		log.Printf("❌ خطا در ساخت کانفیگ: %v", err)
	} else {
		log.Printf("🔗 کانفیگ: %s", link)
		db.DB.Exec(`UPDATE orders SET config_link = ? WHERE id = ?`, link, order.ID)
	}

	w.WriteHeader(http.StatusOK)
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

// extractAmount از چند فرمت مختلف پشتیبانی می‌کنه
func extractAmount(text string) int {
	// لیست پترن‌ها به ترتیب اولویت
	patterns := []*regexp.Regexp{
		// فرمت ۱: مقاوم‌ترین الگو برای کلمه انتقال (رد کردن نشانه‌ها و رسیدن به عدد)
		regexp.MustCompile(`انتقال[^\d]*([\d,]+)`),

		// فرمت ۲: فرمت بله/رسمی
		regexp.MustCompile(`مبلغ[^\d]*([\d,]+)`),

		// فرمت ۳: هر جایی که عدد با علامت + همراه باشه
		regexp.MustCompile(`([\d,]+)[^\d]*\+`),

		// فرمت ۴: مبلغ به ریال
		regexp.MustCompile(`([\d,]+)\s*ریال`),
	}

	for _, re := range patterns {
		match := re.FindStringSubmatch(text)
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
