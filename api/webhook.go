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
	Text string `json:"text"`
}

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

	log.Printf("📩 پیامک دریافتی: %s", req.Text)

	// ۱. تبدیل اعداد فارسی به انگلیسی
	englishText := convertPersianNumbersToEnglish(req.Text)

	// ۲. استخراج مبلغ (ریال)
	amountRial := extractAmountFromBale(englishText)
	if amountRial == 0 {
		log.Println("❌ مبلغی یافت نشد")
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

func extractAmountFromBale(text string) int {
	re := regexp.MustCompile(`مبلغ[\s:*]*([\d,]+)`)
	match := re.FindStringSubmatch(text)

	if len(match) > 1 {
		cleanStr := strings.ReplaceAll(match[1], ",", "")
		val, err := strconv.Atoi(cleanStr)
		if err == nil {
			return val
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
