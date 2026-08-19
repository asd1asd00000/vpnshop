package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/asd1asd00000/vpnshop/models"
	"github.com/asd1asd00000/vpnshop/db"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// generateRandomUsername یه نام کاربری تصادفی ۶ حرفی می‌سازه
// فرمت: user_xxxxxx (حروف کوچک + اعداد)
// مثال: user_f2d5tg
func generateRandomUsername() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	randomPart := make([]byte, 6)
	for i := range randomPart {
		randomPart[i] = charset[rand.Intn(len(charset))]
	}
	return fmt.Sprintf("user_%s", string(randomPart))
}

// isDuplicateError بررسی می‌کنه ارور مربوط به تکراری بودن یوزرنیم هست
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "already exists") ||
		strings.Contains(errMsg, "duplicate") ||
		strings.Contains(errMsg, "conflict") ||
		strings.Contains(errMsg, "409")
}

func GenerateConfigFromOrder(order models.Order) (string, error) {
	// اولویت ۱: از config.json (اولین پنل)
	cfg := db.GetConfig()
	var panelURL, user, pass string

	if len(cfg.Panels) > 0 {
		panelURL = cfg.Panels[0].URL
		user = cfg.Panels[0].Username
		pass = cfg.Panels[0].Password
	} else {
		// اولویت ۲: از متغیرهای محیطی (fallback)
		panelURL = os.Getenv("PANEL_URL")
		user = os.Getenv("PANEL_USER")
		pass = os.Getenv("PANEL_PASS")
	}

	if panelURL == "" || user == "" || pass == "" {
		return "", fmt.Errorf("متغیرهای محیطی پنل تنظیم نشده‌اند")
	}

	token, err := GetToken(panelURL, user, pass)
	if err != nil {
		return "", err
	}

	// ... بقیه تابع بدون تغییر

	// 🎯 خواندن حجم و انقضا از فایل JSON
	plans, _ := models.LoadPlans()
	var limitUsage int64 = 20 * 1073741824
	days := 30

	for _, p := range plans {
		if p.ID == order.PlanName {
			limitUsage = int64(p.VolumeGB) * 1073741824
			days = p.Days
			break
		}
	}

	limitExpire := time.Now().AddDate(0, 0, days).Unix()

	// 🎯 ساخت نام کاربری تصادفی
	baseUsername := generateRandomUsername()

	// لیست نام‌های کاربری: base, base2, base3, ..., base9
	candidates := []string{baseUsername}
	for i := 2; i <= 9; i++ {
		candidates = append(candidates, fmt.Sprintf("%s%d", baseUsername, i))
	}

	// 🎯 تلاش برای ساخت کاربر
	for _, username := range candidates {
		fmt.Printf("🔄 تلاش برای ساخت کاربر: %s\n", username)

		link, err := CreateSubscription(panelURL, token, username, limitUsage, limitExpire)
		if err == nil {
			fmt.Printf("✅ کاربر %s با موفقیت ساخته شد\n", username)
			return link, nil
		}

		if isDuplicateError(err) {
			fmt.Printf("⚠️ کاربر %s تکراری بود، امتحان بعدی...\n", username)
			continue
		}

		return "", fmt.Errorf("خطا در ساخت کاربر %s: %w", username, err)
	}

	return "", fmt.Errorf("تمام نام‌های کاربری تکراری بودند")
}

func GetToken(nodeURL, username, password string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "password")
	data.Set("username", username)
	data.Set("password", password)

	req, err := http.NewRequest("POST", nodeURL+"/api/admins/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth failed, status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if token, ok := result["access_token"].(string); ok {
		return token, nil
	}
	return "", fmt.Errorf("token not found")
}

func GetNodeServiceIDs(nodeURL, token string) []int {
	req, _ := http.NewRequest("GET", nodeURL+"/api/services", nil)
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return []int{1}
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	var listResult []map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &listResult); err == nil {
		var ids []int
		for _, s := range listResult {
			if id, ok := s["id"].(float64); ok {
				ids = append(ids, int(id))
			}
		}
		if len(ids) > 0 {
			return ids
		}
	}
	return []int{1}
}

func CreateSubscription(nodeURL, token, username string, nodeVolumeLimit int64, expireTimestamp int64) (string, error) {
	serviceIDs := GetNodeServiceIDs(nodeURL, token)

	payload := []map[string]interface{}{
		{
			"username":     username,
			"limit_usage":  nodeVolumeLimit,
			"limit_expire": expireTimestamp,
			"service_ids":  serviceIDs,
		},
	}

	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", nodeURL+"/api/subscriptions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create failed, status: %d, detail: %s", resp.StatusCode, string(bodyBytes))
	}

	var rawResult interface{}
	json.NewDecoder(resp.Body).Decode(&rawResult)

	var firstResult map[string]interface{}
	if listRes, ok := rawResult.([]interface{}); ok && len(listRes) > 0 {
		firstResult, _ = listRes[0].(map[string]interface{})
	} else if mapRes, ok := rawResult.(map[string]interface{}); ok {
		firstResult = mapRes
	}

	if firstResult != nil {
		if link, ok := firstResult["link"].(string); ok && link != "" {
			return link, nil
		}
		secret, _ := firstResult["secret"].(string)
		tag, _ := firstResult["tag"].(string)
		if secret != "" && tag != "" {
			return fmt.Sprintf("%s/%s/%s", strings.TrimRight(nodeURL, "/"), tag, secret), nil
		}
	}
	return "", fmt.Errorf("could not extract subscription link properties")
}
