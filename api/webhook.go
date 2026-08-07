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
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "خطا در خواندن داده‌ها", http.StatusBadRequest)
		return
	}

	englishText := convertPersianNumbersToEnglish(req.Text)
	amount := extractAmountFromBale(englishText)

	if amount == 0 {
		log.Println("هیچ مبلغ معتبری در پیام یافت نشد.")
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("مبلغ استخراج شده: %d", amount)

	order := verifyPaymentInDB(amount)
	if order != nil {
		log.Printf("تراکنش فاکتور %s با موفقیت تایید شد! در حال ارتباط با پنل گارد...", order.TrackingCode)

		link, err := GenerateConfigFromOrder(*order)
		if err != nil {
			log.Printf("❌ خطا در ساخت کانفیگ در پنل: %v", err)
		} else {
			log.Printf("✅ کانفیگ با موفقیت ساخته شد: %s", link)
			db.DB.Exec(`UPDATE orders SET config_link = ? WHERE id = ?`, link, order.ID)
		}
	} else {
		log.Printf("فاکتوری برای مبلغ %d در حالت pending یافت نشد یا از قبل تایید شده است. ❌", amount)
	}

	w.WriteHeader(http.StatusOK)
}

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

func extractAmountFromBale(text string) int {
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

func verifyPaymentInDB(amount int) *models.Order {
	var order models.Order
	query := `SELECT id, tracking_code, plan_name, base_price, unique_amount, status, IFNULL(config_link, '') 
	          FROM orders WHERE unique_amount = ? AND status = 'pending'`

	err := db.DB.QueryRow(query, amount).Scan(
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
