package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

// ───────────── ضریب تبدیل GB به روز مخصوص پنل Conf ─────────────
// هر 1GB در پلن فروش = confDaysPerGB روز در پنل Conf
const confDaysPerGB = 1

// ───────────── توابع اختصاصی پنل Conf-to-Sub ─────────────

// confBaseURL نرمال‌سازی URL پایه پنل Conf
func confBaseURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, "/users")
	u = strings.TrimSuffix(u, "/api")
	u = strings.TrimRight(u, "/")
	return u
}

// getConfToken احراز هویت با API Key
func getConfToken(panelURL, apiKey string) error {
	req, _ := http.NewRequest("GET", confBaseURL(panelURL)+"/api/health", nil)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Conf: اتصال به پنل ناموفق: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Conf: پنل پاسخ نداد، status: %d", resp.StatusCode)
	}
	_ = apiKey
	return nil
}

// createConfUserRequest ساخت کاربر در Conf-to-Sub
func createConfUserRequest(panelURL, apiKey, username string, days int) (string, int, error) {
	payload := map[string]interface{}{
		"username": username,
		"days":     days,
	}
	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", confBaseURL(panelURL)+"/api/users", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", 0, err
	}
	req.Header.Add("Authorization", "Bearer "+apiKey)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("Conf: ساخت کاربر ناموفق، status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", 0, fmt.Errorf("Conf: خطا در پارس پاسخ: %v", err)
	}

	subURL, _ := result["subscription_url"].(string)
	userID, _ := result["id"].(float64)

	if subURL == "" {
		return "", 0, fmt.Errorf("Conf: subscription_url در پاسخ یافت نشد")
	}

	return subURL, int(userID), nil
}

// findConfUserIDByUsername پیدا کردن ID کاربر با جستجو در username
func findConfUserIDByUsername(panelURL, apiKey, username string) (int, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/users?q=%s&per_page=100", confBaseURL(panelURL), username), nil)
	req.Header.Add("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Conf: جستجو ناموفق، status: %d", resp.StatusCode)
	}

	var result struct {
		Users []struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
		} `json:"users"`
	}

	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return 0, fmt.Errorf("Conf: خطا در پارس لیست کاربران: %v", err)
	}

	for _, u := range result.Users {
		if u.Username == username {
			return u.ID, nil
		}
	}

	return 0, fmt.Errorf("Conf: کاربر %s یافت نشد", username)
}

// getConfUserByID گرفتن اطلاعات یک کاربر با ID
func getConfUserByID(panelURL, apiKey string, userID int) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/users/%d", confBaseURL(panelURL), userID), nil)
	req.Header.Add("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Conf: گرفتن کاربر ناموفق، status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("Conf: خطا در پارس پاسخ: %v", err)
	}

	return result, nil
}

// renewConfUser تمدید کاربر
func renewConfUser(panelURL, apiKey string, userID int, days int) (string, error) {
	payload := map[string]interface{}{
		"days":    days,
		"hours":   0,
		"minutes": 0,
	}
	jsonData, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/users/%d/renew", confBaseURL(panelURL), userID), bytes.NewBuffer(jsonData))
	req.Header.Add("Authorization", "Bearer "+apiKey)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Conf: تمدید ناموفق، status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", fmt.Errorf("Conf: خطا در پارس پاسخ تمدید: %v", err)
	}

	subURL, _ := result["subscription_url"].(string)
	if subURL == "" {
		return "", fmt.Errorf("Conf: subscription_url در پاسخ تمدید یافت نشد")
	}

	return subURL, nil
}

// CreateConfUser ساخت کاربر در پنل Conf-to-Sub
func CreateConfUser(panel db.PanelConfig, username string, volumeGB int, days int) (string, error) {
	apiKey := panel.Password

	if err := getConfToken(panel.URL, apiKey); err != nil {
		return "", err
	}

	// 🎯 تبدیل حجم پلن به روز برای پنل Conf
	confDays := volumeGB * confDaysPerGB
	if confDays <= 0 {
		confDays = days
	}
	if confDays <= 0 {
		confDays = 1
	}

	log.Printf("🔄 [Conf] ساخت کاربر %s: %dGB × %d = %d روز (حجم نامحدود)", username, volumeGB, confDaysPerGB, confDays)
	link, _, err := createConfUserRequest(panel.URL, apiKey, username, confDays)
	if err != nil {
		return "", err
	}
	log.Printf("✅ [Conf] کاربر %s با موفقیت ساخته شد (%d روز)", username, confDays)
	return link, nil
}

// UpdateConfUser تمدید اشتراک در پنل Conf-to-Sub
func UpdateConfUser(panel db.PanelConfig, username string, volumeGB int, days int) (string, error) {
	apiKey := panel.Password

	if err := getConfToken(panel.URL, apiKey); err != nil {
		return "", err
	}

	userID, err := findConfUserIDByUsername(panel.URL, apiKey, username)
	if err != nil {
		return "", err
	}

	// 🎯 تبدیل حجم پلن به روز برای پنل Conf
	confDays := volumeGB * confDaysPerGB
	if confDays <= 0 {
		confDays = days
	}
	if confDays <= 0 {
		confDays = 1
	}

	log.Printf("🔄 [Conf] تمدید کاربر %s: %dGB × %d = %d روز", username, volumeGB, confDaysPerGB, confDays)
	link, err := renewConfUser(panel.URL, apiKey, userID, confDays)
	if err != nil {
		return "", err
	}
	log.Printf("✅ [Conf] کاربر %s با موفقیت تمدید شد (+%d روز)", username, confDays)
	return link, nil
}

// GetConfUserUsage گرفتن روزهای باقیمانده از پنل Conf-to-Sub
func GetConfUserUsage(panel db.PanelConfig, username string) (limitUsage int64, totalUsage int64, limitExpire int64, err error) {
	apiKey := panel.Password

	if err := getConfToken(panel.URL, apiKey); err != nil {
		return 0, 0, 0, err
	}

	userID, err := findConfUserIDByUsername(panel.URL, apiKey, username)
	if err != nil {
		return 0, 0, 0, err
	}

	user, err := getConfUserByID(panel.URL, apiKey, userID)
	if err != nil {
		return 0, 0, 0, err
	}

	expireDate, _ := user["expire_date"].(string)
	var expireUnix int64
	if expireDate != "" {
		if t, perr := time.Parse(time.RFC3339, expireDate); perr == nil {
			expireUnix = t.Unix()
		} else if t, perr := time.Parse("2006-01-02T15:04:05", expireDate); perr == nil {
			expireUnix = t.Unix()
		}
	}

	limitUsage = 0
	totalUsage = 0
	limitExpire = expireUnix

	return limitUsage, totalUsage, limitExpire, nil
}
