package api

import (
	"io"
	"log"
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

	// ۱. بررسی توکن از URL Query String
	// فرمت URL در موبایل: https://yourdomain.com/webhook?token=KHIHgu1451lhgugiu54DFG51FDLOI
	if r.URL.Query().Get("token") != webhookSecret {
		log.Println("⚠️ دسترسی غیرمجاز: توکن نامعتبر")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// ۲. خواندن خام تمام محتوای Body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("❌ خطا در خواندن body: %v", err)
		http.Error(w, "خطا در خواندن داده‌ها", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// تبدیل مستقیم به استرینگ (بدون درگیری با JSON)
	smsText := string(bodyBytes)
	
	if strings.TrimSpace(smsText) == "" {
		log.Println("⚠️ هشدار: پیامک خالی دریافت شد")
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("📩 پیامک دریافتی (خام): [%s]", smsText)
	log.Printf("📏 طول پیامک: %d کاراکتر", len(smsText))

	// ۳. نرمال‌سازی متن
	normalizedText := normalizeText(smsText)
	log.Printf("📩 پیامک نرمال‌شده: [%s]", normalizedText)

	// ۴. تبدیل اعداد فارسی/عربی به انگلیسی
	englishText := convertPersianNumbersToEnglish(normalizedText)

	// ۵. استخراج مبلغ
	amountRial := extractAmount(englishText)
	if amountRial == 0 {
		log.Println("❌ مبلغی در پیام یافت نشد")
		w.WriteHeader(http.StatusOK)
		return
	}

	// ۶. تبدیل ریال به تومان
	amountToman := amountRial / 10
	log.Printf("💰 ریال: %d | تومان: %d", amountRial, amountToman)

	// ۷. بررسی در دیتابیس
	order := verifyPaymentInDB(amountToman)
	if order == nil {
		log.Printf("❌ فاکتوری برای %d تومان یافت نشد", amountToman)
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("✅ سفارش %s تایید شد!", order.TrackingCode)

	// ۸. ساخت کانفیگ
	link, err := GenerateConfigFromOrder(*order)
	if err != nil {
		log.Printf("❌ خطا در ساخت کانفیگ: %v", err)
	} else {
		log.Printf("🔗 کانفیگ: %s", link)
		db.DB.Exec(`UPDATE orders SET config_link = ? WHERE id = ?`, link, order.ID)
	}

	w.WriteHeader(http.StatusOK)
}

// ... بقیه توابع کمکی (normalizeText, convertPersianNumbersToEnglish, extractAmount, verifyPaymentInDB) بدون تغییر باقی می‌مانند ...
