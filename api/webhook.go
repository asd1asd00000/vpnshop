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

// SMSRequest ساختار داده‌ای که گوشی به سرور می‌فرستد
type SMSRequest struct {
	Text string `json:"text"`
}

// WebhookHandler تابعی برای دریافت و پردازش پیامک
func WebhookHandler(w http.ResponseWriter, r *http.Request) {
	// فقط درخواست‌های POST را قبول می‌کنیم
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

	log.Printf("پیامک دریافت شد: %s", req.Text)

	// استخراج مبلغ از متن پیامک
	amount := extractAmountFromSMS(req.Text)
	if amount == 0 {
		log.Println("هیچ مبلغ معتبری در پیامک یافت نشد.")
		w.WriteHeader(http.StatusOK) // به گوشی ارور نمی‌دهیم تا دوباره ارسال نکند
		return
	}

	log.Printf("مبلغ استخراج شده: %d", amount)

	// بررسی مبلغ در دیتابیس و تایید فاکتور
	success := verifyPaymentInDB(amount)
	if success {
		log.Printf("تراکنش با مبلغ %d با موفقیت تایید شد!", amount)
		// در مرحله بعد: اینجا کد ساخت کانفیگ در مرزبان را صدا می‌زنیم
	} else {
		log.Printf("فاکتوری برای مبلغ %d یافت نشد یا از قبل تایید شده است.", amount)
	}

	w.WriteHeader(http.StatusOK)
}

// extractAmountFromSMS استخراج اعداد از متن با ریجکس
func extractAmountFromSMS(text string) int {
	// این ریجکس اعدادی که ممکن است کاما داشته باشند را پیدا می‌کند
	// مثال: "200,045" یا "200045"
	re := regexp.MustCompile(`\b\d{1,3}(?:,\d{3})*\b|\b\d+\b`)
	matches := re.FindAllString(text, -1)

	// ما به دنبال بزرگترین عدد در پیامک می‌گردیم که معمولاً مبلغ واریزی است
	// (بسته به متن دقیق پیامک بانک شما، ممکن است نیاز باشد این منطق کمی تغییر کند)
	var maxAmount int
	for _, match := range matches {
		// حذف کاماها از عدد
		cleanStr := strings.ReplaceAll(match, ",", "")
		val, err := strconv.Atoi(cleanStr)
		if err == nil {
			// چک می‌کنیم عدد شبیه یک مبلغ معتبر باشد (مثلا بیشتر از هزار تومان)
			// و از شماره کارت‌ها (اعداد 16 رقمی) صرف نظر می‌کنیم
			if val > 1000 && val < 9999999999 {
				if val > maxAmount {
					maxAmount = val
				}
			}
		}
	}
	return maxAmount
}

// verifyPaymentInDB بررسی و آپدیت دیتابیس
func verifyPaymentInDB(amount int) bool {
	// به دنبال فاکتوری می‌گردیم که مبلغ یکتای آن با پیامک برابر بوده و در حالت pending باشد
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

	// اگر حداقل یک سطر تغییر کرده باشد، یعنی فاکتور پیدا و تایید شده است
	return rowsAffected > 0
}
