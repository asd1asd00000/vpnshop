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

const webhookSecret = "YOUR_SECRET_HERE_CHANGE_ME"

func WebhookHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req SMSRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    // ✅ ۱. احراز هویت
    if req.Token != webhookSecret {
        log.Println("⚠️ دسترسی غیرمجاز به webhook")
        http.Error(w, "Forbidden", http.StatusForbidden)
        return
    }

    log.Printf("📩 پیامک دریافتی: %s", req.Text)

    // ✅ ۲. تبدیل اعداد فارسی
    englishText := convertPersianNumbersToEnglish(req.Text)

    // ✅ ۳. استخراج مبلغ (ریالی)
    amountRial := extractAmountFromBale(englishText)
    if amountRial == 0 {
        log.Println("❌ مبلغی در پیام یافت نشد")
        w.WriteHeader(http.StatusOK)
        return
    }

    // ✅ ۴. تبدیل ریال به تومان
    amountToman := amountRial / 10
    log.Printf("💰 مبلغ: %d ریال = %d تومان", amountRial, amountToman)

    // ✅ ۵. تایید در دیتابیس
    order := verifyPaymentInDB(amountToman)
    if order == nil {
        log.Printf("❌ فاکتوری برای %d تومان یافت نشد", amountToman)
        w.WriteHeader(http.StatusOK)
        return
    }

    log.Printf("✅ سفارش %s تایید شد", order.TrackingCode)

    // ✅ ۶. ساخت کانفیگ
    link, err := GenerateConfigFromOrder(*order)
    if err != nil {
        log.Printf("❌ خطا در ساخت کانفیگ: %v", err)
    } else {
        db.DB.Exec(`UPDATE orders SET config_link = ? WHERE id = ?`, link, order.ID)
        log.Printf("🔗 کانفیگ ساخته شد: %s", link)
    }

    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
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
    patterns := []*regexp.Regexp{
        regexp.MustCompile(`مبلغ\s*[:\s]*([\d,]+)`),
        regexp.MustCompile(`([\d,]+)\s*ریال`),
        regexp.MustCompile(`([\d,]+)\s*ريال`),
    }
    for _, re := range patterns {
        match := re.FindStringSubmatch(text)
        if len(match) > 1 {
            cleanStr := strings.ReplaceAll(match[1], ",", "")
            if val, err := strconv.Atoi(cleanStr); err == nil {
                return val
            }
        }
    }
    return 0
}

func verifyPaymentInDB(amountToman int) *models.Order {
    tx, err := db.DB.Begin()
    if err != nil {
        return nil
    }
    defer tx.Rollback()

    var order models.Order
    err = tx.QueryRow(
        `SELECT id, tracking_code, plan_name, base_price, unique_amount, status, IFNULL(config_link, '') 
         FROM orders 
         WHERE unique_amount = ? AND status = 'pending' AND created_at >= datetime('now', '-30 minutes')`,
        amountToman,
    ).Scan(
        &order.ID, &order.TrackingCode, &order.PlanName,
        &order.BasePrice, &order.UniqueAmount, &order.Status, &order.ConfigLink,
    )
    if err != nil {
        return nil
    }

    if _, err := tx.Exec(`UPDATE orders SET status = 'paid' WHERE id = ?`, order.ID); err != nil {
        return nil
    }

    tx.Commit()
    order.Status = "paid"
    return &order
}
