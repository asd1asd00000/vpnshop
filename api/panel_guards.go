package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
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
	return 20, 30
}

// ───────────── توابع اختصاصی پنل Guards (GoGuard 1.0) ─────────────

// guardsUserAgent شبیه مرورگر Chrome برای عبور از Cloudflare/WAF
const guardsUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// guardsClient کلاینت مشترک: اجبار به IPv4 + timeout سی ثانیه + بدون keep-alive
// (DisableKeepAlives جلوی خطاهای EOF ناشی از connection های stale رو می‌گیره)
var guardsClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
			return d.DialContext(ctx, "tcp4", addr)
		},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 25 * time.Second,
		DisableKeepAlives:     true,
	},
}

// guardsRequest ارسال درخواست با User-Agent و هدرهای auth
func guardsRequest(method, rawURL string, body []byte, auth map[string]string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewBuffer(body)
	}
	req, err := http.NewRequest(method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", guardsUserAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range auth {
		req.Header.Set(k, v)
	}
	return guardsClient.Do(req)
}

// authMode برای لاگ: نشون می‌ده با APIKey وصل شدیم یا token
func authMode(panel db.PanelConfig) string {
	if strings.TrimSpace(panel.APIKey) != "" {
		return "APIKey"
	}
	return "token"
}

// guardsAuthHeaders اولویت با API Key (هدر X-API-Key)، وگرنه fallback به توکن
func guardsAuthHeaders(panel db.PanelConfig) (map[string]string, error) {
	if key := strings.TrimSpace(panel.APIKey); key != "" {
		return map[string]string{"X-API-Key": key}, nil
	}
	token, err := getGuardsToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return nil, err
	}
	return map[string]string{"Authorization": "Bearer " + token}, nil
}

// getGuardsToken احراز هویت با یوزر/پسورد (حالت fallback)
func getGuardsToken(nodeURL, username, password string) (string, error) {
	data := url.Values{}
	data.Set("username", username)
	data.Set("password", password)

	req, err := http.NewRequest("POST", nodeURL+"/api/admins/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", guardsUserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := guardsClient.Do(req)
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

	if token, ok := result["token"].(string); ok && token != "" {
		return token, nil
	}
	if token, ok := result["access_token"].(string); ok && token != "" {
		return token, nil
	}
	return "", fmt.Errorf("Guards token not found in response")
}

// getGuardsServiceIDs شناسه سرویس‌های فعال پنل Guards
func getGuardsServiceIDs(panel db.PanelConfig, auth map[string]string) []int {
	resp, err := guardsRequest("GET", panel.URL+"/api/services", nil, auth)
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

// createGuardsSubscription ساخت اشتراک در Guards (GoGuard 1.0)
func createGuardsSubscription(panel db.PanelConfig, auth map[string]string, username string, nodeVolumeLimit int64, expireTimestamp int64) (string, error) {
	serviceIDs := getGuardsServiceIDs(panel, auth)

	payload := map[string]interface{}{
		"username":     username,
		"limit_usage":  nodeVolumeLimit,
		"limit_expire": expireTimestamp,
		"services":     serviceIDs,
	}

	jsonData, _ := json.Marshal(payload)

	resp, err := guardsRequest("POST", panel.URL+"/api/subscriptions", jsonData, auth)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("Guards create failed, status: %d, detail: %s", resp.StatusCode, string(bodyBytes))
	}

	var rawResult interface{}
	if err := json.Unmarshal(bodyBytes, &rawResult); err != nil {
		return "", fmt.Errorf("Guards: خطا در پارس پاسخ: %v", err)
	}

	var firstResult map[string]interface{}
	if listRes, ok := rawResult.([]interface{}); ok && len(listRes) > 0 {
		firstResult, _ = listRes[0].(map[string]interface{})
	} else if mapRes, ok := rawResult.(map[string]interface{}); ok {
		firstResult = mapRes
	}

	if firstResult != nil {
		if link, ok := firstResult["subscription_link"].(string); ok && link != "" {
			return link, nil
		}
		if link, ok := firstResult["link"].(string); ok && link != "" {
			return link, nil
		}
		accessKey, _ := firstResult["access_key"].(string)
		tag, _ := firstResult["tag"].(string)
		serverKey, _ := firstResult["server_key"].(string)
		if serverKey != "" && accessKey != "" {
			return fmt.Sprintf("%s/%s/%s", strings.TrimRight(panel.URL, "/"), serverKey, accessKey), nil
		}
		if tag != "" && accessKey != "" {
			return fmt.Sprintf("%s/%s/%s", strings.TrimRight(panel.URL, "/"), tag, accessKey), nil
		}
	}
	return "", fmt.Errorf("Guards: could not extract subscription link")
}

// CreateGuardsUser ساخت کاربر در پنل Guards
func CreateGuardsUser(panel db.PanelConfig, username string, volumeGB int, days int) (string, error) {
	auth, err := guardsAuthHeaders(panel)
	if err != nil {
		return "", err
	}

	limitUsage := int64(volumeGB) * 1073741824
	limitExpire := time.Now().AddDate(0, 0, days).Unix()

	log.Printf("🔄 [Guards] تلاش برای ساخت کاربر: %s (auth: %s)", username, authMode(panel))
	link, err := createGuardsSubscription(panel, auth, username, limitUsage, limitExpire)
	if err != nil {
		return "", err
	}
	log.Printf("✅ [Guards] کاربر %s با موفقیت ساخته شد", username)
	return link, nil
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

// getGuardsSubscription دریافت اطلاعات اشتراک (لیست + فیلتر)
func getGuardsSubscription(panel db.PanelConfig, auth map[string]string, username string) (map[string]interface{}, error) {
	resp, err := guardsRequest("GET", panel.URL+"/api/subscriptions", nil, auth)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Guards: دریافت لیست اشتراک‌ها ناموفق، status: %d, body: %s", resp.StatusCode, string(body))
	}

	bodyBytes, _ := io.ReadAll(resp.Body)

	var rawResult interface{}
	if err := json.Unmarshal(bodyBytes, &rawResult); err != nil {
		return nil, fmt.Errorf("Guards: خطا در پارس پاسخ: %v", err)
	}

	var subs []interface{}
	switch v := rawResult.(type) {
	case []interface{}:
		subs = v
	case map[string]interface{}:
		if data, ok := v["data"].([]interface{}); ok {
			subs = data
		} else if items, ok := v["items"].([]interface{}); ok {
			subs = items
		}
	}

	for _, s := range subs {
		if sub, ok := s.(map[string]interface{}); ok {
			if u, ok := sub["username"].(string); ok && u == username {
				return sub, nil
			}
		}
	}

	return nil, fmt.Errorf("Guards: اشتراک %s یافت نشد", username)
}

// updateGuardsSubscription بروزرسانی اشتراک (تمدید) - Bulk Update
func updateGuardsSubscription(panel db.PanelConfig, auth map[string]string, username string, newLimitUsage int64, newLimitExpire int64) (string, error) {
	payload := map[string]interface{}{
		"usernames":    []string{username},
		"limit_usage":  newLimitUsage,
		"limit_expire": newLimitExpire,
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := guardsRequest("PUT", panel.URL+"/api/subscriptions", jsonData, auth)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Guards: تمدید ناموفق، status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	sub, err := getGuardsSubscription(panel, auth, username)
	if err != nil {
		return "", fmt.Errorf("تمدید شد ولی خواندن اشتراک ناموفق: %v", err)
	}

	if link, ok := sub["subscription_link"].(string); ok && link != "" {
		return link, nil
	}
	if link, ok := sub["link"].(string); ok && link != "" {
		return link, nil
	}

	accessKey, _ := sub["access_key"].(string)
	serverKey, _ := sub["server_key"].(string)
	tag, _ := sub["tag"].(string)
	if serverKey != "" && accessKey != "" {
		return fmt.Sprintf("%s/%s/%s", strings.TrimRight(panel.URL, "/"), serverKey, accessKey), nil
	}
	if tag != "" && accessKey != "" {
		return fmt.Sprintf("%s/%s/%s", strings.TrimRight(panel.URL, "/"), tag, accessKey), nil
	}

	return "", fmt.Errorf("تمدید شد ولی link استخراج نشد")
}

// UpdateGuardsUser تمدید اشتراک در پنل Guards
func UpdateGuardsUser(panel db.PanelConfig, username string, volumeGB int, days int) (string, error) {
	auth, err := guardsAuthHeaders(panel)
	if err != nil {
		return "", err
	}

	newLimitUsage := int64(volumeGB) * 1073741824
	newLimitExpire := time.Now().AddDate(0, 0, days).Unix()

	log.Printf("🔄 [Guards] تمدید کاربر %s: حجم=%dGB, روز=%d (auth: %s)", username, volumeGB, days, authMode(panel))
	link, err := updateGuardsSubscription(panel, auth, username, newLimitUsage, newLimitExpire)
	if err != nil {
		return "", err
	}
	log.Printf("✅ [Guards] کاربر %s با موفقیت تمدید شد", username)
	return link, nil
}

// GetGuardsUserUsage دریافت حجم و روز باقیمانده از پنل Guards
func GetGuardsUserUsage(panel db.PanelConfig, username string) (limitUsage int64, totalUsage int64, limitExpire int64, err error) {
	auth, err := guardsAuthHeaders(panel)
	if err != nil {
		return 0, 0, 0, err
	}

	sub, err := getGuardsSubscription(panel, auth, username)
	if err != nil {
		return 0, 0, 0, err
	}

	if v, ok := sub["limit_usage"].(float64); ok {
		limitUsage = int64(v)
	}
	if v, ok := sub["total_usage"].(float64); ok {
		totalUsage = int64(v)
	}
	if totalUsage == 0 {
		if v, ok := sub["current_usage"].(float64); ok {
			totalUsage = int64(v)
		}
	}
	if v, ok := sub["limit_expire"].(float64); ok {
		limitExpire = int64(v)
	}

	return limitUsage, totalUsage, limitExpire, nil
}
