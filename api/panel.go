package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/asd1asd00000/vpnshop/models"
)

// TokenResponse ساختار دریافت توکن لاگین
type TokenResponse struct {
	AccessToken string `json:"access_token"`
}

// SubscriptionCreate ساختار ارسال اطلاعات کاربر جدید بر اساس مستندات شما
type SubscriptionCreate struct {
	Username    string `json:"username"`
	LimitUsage  int64  `json:"limit_usage"`
	LimitExpire int64  `json:"limit_expire"`
	ServiceIDs  []int  `json:"service_ids"`
	Note        string `json:"note"`
}

// SubscriptionResponse ساختار دریافت لینک بعد از ساخته شدن کاربر
type SubscriptionResponse struct {
	Link string `json:"link"`
}

// getPanelToken دریافت توکن لاگین از API پنل
func getPanelToken() (string, error) {
	panelURL := os.Getenv("PANEL_URL")
	user := os.Getenv("PANEL_USER")
	pass := os.Getenv("PANEL_PASS")

	if panelURL == "" || user == "" || pass == "" {
		return "", fmt.Errorf("اطلاعات ورود به پنل در متغیرهای محیطی تنظیم نشده است")
	}

	endpoint := fmt.Sprintf("%s/api/admins/token", strings.TrimRight(panelURL, "/"))

	// مستندات می‌گوید باید با فرمت Form Data ارسال شود
	data := url.Values{}
	data.Set("grant_type", "password")
	data.Set("username", user)
	data.Set("password", pass)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("خطا در لاگین به پنل، کد وضعیت: %d", resp.StatusCode)
	}

	var tokenRes TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenRes); err != nil {
		return "", err
	}

	return tokenRes.AccessToken, nil
}

// CreateSubscription اتصال به پنل و ساخت کانفیگ
func CreateSubscription(order models.Order) (string, error) {
	token, err := getPanelToken()
	if err != nil {
		return "", err
	}

	panelURL := os.Getenv("PANEL_URL")
	endpoint := fmt.Sprintf("%s/api/subscriptions", strings.TrimRight(panelURL, "/"))

	// تبدیل پلن به بایت و زمان (به صورت پیش‌فرض 20 گیگ و 30 روز)
	// در آینده می‌توانید این مقادیر را بر اساس order.PlanName تغییر دهید
	var limitUsage int64 = 20 * 1073741824 // 20 GB to Bytes
	limitExpire := time.Now().AddDate(0, 1, 0).Unix() // 30 روز آینده

	// ساخت نام کاربری یکتا بر اساس کد پیگیری
	username := fmt.Sprintf("user_%s", order.TrackingCode)

	payload := SubscriptionCreate{
		Username:    username,
		LimitUsage:  limitUsage,
		LimitExpire: limitExpire,
		ServiceIDs:  []int{1}, // شناسه سرویس پیش‌فرض، اگر در پنل شما متفاوت است آن را تغییر دهید
		Note:        fmt.Sprintf("ساخته شده خودکار - فاکتور: %d", order.ID),
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("خطا در ساخت کاربر: %s", string(bodyBytes))
	}

	var subRes SubscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&subRes); err != nil {
		return "", err
	}

	return subRes.Link, nil
}
