package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
	"github.com/asd1asd00000/vpnshop/models"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// ───────────── توابع کمکی عمومی (مشترک بین پنل‌ها) ─────────────

// generateUsername یه نام کاربری تصادفی ۶ حرفی می‌سازه
// فرمت: user_xxxxxx (حروف کوچک + اعداد)
func generateUsername() string {
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

// planToVolumeAndDays حجم و روز رو از پلن استخراج می‌کنه
func planToVolumeAndDays(planID string) (volumeGB int, days int) {
	plans, _ := models.LoadPlans()
	for _, p := range plans {
		if p.ID == planID {
			return p.VolumeGB, p.Days
		}
	}
	// مقادیر پیش‌فرض اگه پلن پیدا نشد
	return 20, 30
}

// ───────────── توابع اختصاصی پنل Guards ─────────────

// getGuardsToken احراز هویت در پنل Guards
func getGuardsToken(nodeURL, username, password string) (string, error) {
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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Guards auth failed, status: %d, body: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if token, ok := result["access_token"].(string); ok {
		return token, nil
	}
	return "", fmt.Errorf("Guards token not found in response")
}

// getGuardsServiceIDs شناسه سرویس‌های فعال پنل Guards
func getGuardsServiceIDs(nodeURL, token string) []int {
	req, _ := http.NewRequest("GET", nodeURL+"/api/services", nil)
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
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

// createGuardsSubscription ساخت یه اشتراک در Guards
func createGuardsSubscription(nodeURL, token, username string, nodeVolumeLimit int64, expireTimestamp int64) (string, error) {
	serviceIDs := getGuardsServiceIDs(nodeURL, token)

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

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Guards create failed, status: %d, detail: %s", resp.StatusCode, string(bodyBytes))
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
	return "", fmt.Errorf("Guards: could not extract subscription link")
}

// CreateGuardsUser ساخت کاربر در پنل Guards با تلاش روی چند نام کاربری
func CreateGuardsUser(panel db.PanelConfig, order models.Order, volumeGB int, days int) (string, error) {
	token, err := getGuardsToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return "", err
	}

	// تبدیل GB به bytes
	limitUsage := int64(volumeGB) * 1073741824
	// تبدیل days به timestamp
	limitExpire := time.Now().AddDate(0, 0, days).Unix()

	// ساخت نام کاربری پایه + fallback ها (base, base2, base3, ..., base9)
	baseUsername := generateUsername()
	candidates := []string{baseUsername}
	for i := 2; i <= 9; i++ {
		candidates = append(candidates, fmt.Sprintf("%s%d", baseUsername, i))
	}

	for _, username := range candidates {
		fmt.Printf("🔄 [Guards] تلاش برای ساخت کاربر: %s\n", username)

		link, err := createGuardsSubscription(panel.URL, token, username, limitUsage, limitExpire)
		if err == nil {
			fmt.Printf("✅ [Guards] کاربر %s با موفقیت ساخته شد\n", username)
			return link, nil
		}

		if isDuplicateError(err) {
			fmt.Printf("⚠️ [Guards] کاربر %s تکراری بود، امتحان بعدی...\n", username)
			continue
		}

		return "", fmt.Errorf("خطا در ساخت کاربر %s: %w", username, err)
	}

	return "", fmt.Errorf("تمام نام‌های کاربری در پنل Guards تکراری بودند")
}

// FormatGuardsConfig فرمت‌بندی خروجی برای نمایش به مشتری
func FormatGuardsConfig(panel db.PanelConfig, link string, volumeGB int) string {
	panelName := panel.Name
	if panelName == "" {
		panelName = "Guards"
	}
	if panel.IsBackup {
		return fmt.Sprintf("=== 🛡️ %s (زاپاس %dGB) ===\n%s", panelName, volumeGB, link)
	}
	return fmt.Sprintf("=== 🛡️ %s (%dGB) ===\n%s", panelName, volumeGB, link)
}
