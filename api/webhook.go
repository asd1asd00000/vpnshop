cat << 'EOF' > api/webhook.go
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/asd1asd00000/vpnshop/db"
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
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "خطا در خواندن داده‌ها", http.StatusBadRequest)
		return
	}

	log.Printf("پیامک/نوتیفیکیشن دریافت شد:\n%s", req.Text)

	// اول تمام اعداد فارسی/عربی رو به انگلیسی تبدیل می‌کنیم
	englishText := convertPersianNumbersToEnglish(req.Text)

	amount := extractAmountFromBale(englishText)
	if amount == 0 {
		log.Println("هیچ مبلغ معتبری در پیام یافت نشد.")
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("مبلغ استخراج شده: %d", amount)

	success := verifyPaymentInDB(amount)
	if success {
		log.Printf("تراکنش با مبلغ %d با موفقیت تایید شد! ✅", amount)
	} else {
		log.Printf("فاکتوری برای مبلغ %d یافت نشد یا از قبل تایید شده است. ❌", amount)
	}

	w.WriteHeader(http.StatusOK)
}

// تابع تبدیل اعداد فارسی به انگلیسی
func convertPersianNumbersToEnglish(text string) string {
	persianNumbers := []string{"۰", "۱", "۲", "۳", "۴", "۵", "۶", "۷", "۸", "۹"}
	arabicNumbers := []string{"٠", "١", "٢", "٣", "٤", "٥", "٦", "٧", "٨", "٩"}
	englishNumbers := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

	for i := 0; i < 10; i++ {
		text = strings.ReplaceAll(text, persianNumbers[i], englishNumbers[i])
		text = strings.ReplaceAll(text, arabicNumbers[i], englishNumbers[i])
	}
	return text
}

// استخراج دقیق بر اساس ساختار بانک ملی / بله
func extractAmountFromBale(text string) int {
	// ریجکس می‌گرده دنبال کلمه "مبلغ:" ، ممکنه بعدش فاصله یا ستاره باشه، بعدش عدد میاد، بعدش ممکنه + یا ریال باشه
	// مثال هدف: مبلغ: *2,000,000+* ریال
	reTarget := regexp.MustCompile(`مبلغ[\s:*]*([\d,]+)`)
	match := reTarget.FindStringSubmatch(text)
	
	if len(match) > 1 {
		cleanStr := strings.ReplaceAll(match[1], ",", "")
		val, err := strconv.Atoi(cleanStr)
		if err == nil {
			return val
		}
	}
	
	return 0
}

func verifyPaymentInDB(amount int) bool {
	query := `UPDATE orders SET status = 'paid' WHERE unique_amount = ? AND status = 'pending'`
	result, err := db.DB.Exec(query, amount)
	if err != nil {
		log.Printf("خطا در آپدیت دیتابیس: %v", err)
		return false
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false
	}

	return rowsAffected > 0
}
EOF
