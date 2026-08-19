package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

// ───────────── توابع داخلی پنل پاسارگاد (Marzban) ─────────────

// getPasarguardToken احراز هویت در پنل پاسارگاد
func getPasarguardToken(panelURL, username, password string) (string, error) {
	baseURL := strings.TrimRight(panelURL, "/")
	data := url.Values{}
	data.Set("grant_type", "password")
	data.Set("username", username)
	data.Set("password", password)

	req, err := http.NewRequest("POST", baseURL+"/api/admin/token", strings.NewReader(data.Encode()))
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
		return "", fmt.Errorf("پاسارگاد: احراز هویت ناموفق، کد: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if token, ok := result["access_token"].(string); ok {
		return token, nil
	}
	return "", fmt.Errorf("پاسارگاد: توکن در پاسخ یافت نشد")
}

// getPasarguardGroupIDs شناسه گروه‌های پنل پاسارگاد
// getPasarguardGroupIDs شناسه همه گروه‌های پنل پاسارگاد
func getPasarguardGroupIDs(baseURL, token string) []int {
	endpoints := []string{"/api/groups", "/api/user_groups", "/api/admin/groups", "/api/nodes/groups"}

	client := &http.Client{Timeout: 5 * time.Second}

	for _, ep := range endpoints {
		req, _ := http.NewRequest("GET", baseURL+ep, nil)
		req.Header.Add("Authorization", "Bearer "+token)
		req.Header.Add("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		ids := extractGroupIDs(bodyBytes)
		if len(ids) > 0 {
			log.Printf("🔍 [پاسارگاد] گروه‌های یافت شده از %s: %v", ep, ids)
			return ids
		}
	}

	log.Printf("⚠️ [پاسارگاد] گروهی یافت نشد")
	return []int{}
}

// extractGroupIDs استخراج ID ها از فرمت‌های مختلف پاسخ
func extractGroupIDs(body []byte) []int {
	var ids []int

	// فرمت ۱: آرایه مستقیم [{"id":1},...]
	var arr []map[string]interface{}
	if err := json.Unmarshal(body, &arr); err == nil {
		for _, g := range arr {
			if id, ok := g["id"].(float64); ok {
				ids = append(ids, int(id))
			}
		}
		if len(ids) > 0 {
			return ids
		}
	}

	// فرمت ۲: آبجکت پیچیده {"items":[...]} یا {"groups":[...]}
	var wrapper map[string]interface{}
	if err := json.Unmarshal(body, &wrapper); err == nil {
		for _, key := range []string{"items", "groups", "data", "results"} {
			if list, ok := wrapper[key].([]interface{}); ok {
				for _, item := range list {
					if m, ok := item.(map[string]interface{}); ok {
						if id, ok := m["id"].(float64); ok {
							ids = append(ids, int(id))
						}
					}
				}
				if len(ids) > 0 {
					return ids
				}
			}
		}
	}

	return ids
}

// extractPasarguardSubURL استخراج لینک اشتراک از پاسخ پاسارگاد
func extractPasarguardSubURL(baseURL string, data map[string]interface{}) string {
	// روش ۱: subscription_url
	if subURL, ok := data["subscription_url"].(string); ok && subURL != "" {
		if strings.HasPrefix(subURL, "/") {
			return baseURL + subURL
		}
		return subURL
	}
	// روش ۲: links آرایه
	if links, ok := data["links"].([]interface{}); ok && len(links) > 0 {
		if linkStr, ok := links[0].(string); ok {
			return linkStr
		}
	}
	return ""
}

// CreateMarzbanUser ساخت کاربر در پنل پاسارگاد (Marzban)
// امضا: سازگار با dispatcher
func CreateMarzbanUser(panel db.PanelConfig, username string, volumeGB int, days int) (string, error) {
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return "", err
	}

	baseURL := strings.TrimRight(panel.URL, "/")
	groupIDs := getPasarguardGroupIDs(baseURL, token)
		groupIDs := getPasarguardGroupIDs(baseURL, token)

	payload := map[string]interface{}{
		"username":                  username,
		"data_limit":                volumeBytes,
		"data_limit_reset_strategy": "no_reset",
		"status":                    "active",
	}
	if len(groupIDs) > 0 {
		payload["group_ids"] = groupIDs
	}

	// تبدیل GB به bytes
	volumeBytes := int64(volumeGB) * 1024 * 1024 * 1024

	// تبدیل روز به timestamp
	var expireTimestamp int64
	if days > 0 {
		expireTimestamp = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	}

	payload := map[string]interface{}{
		"username":                  username,
		"data_limit":                volumeBytes,
		"data_limit_reset_strategy": "no_reset",
		"status":                    "active",
		"group_ids":                 groupIDs,
		"proxies": map[string]interface{}{
			"vmess":       map[string]interface{}{},
			"vless":       map[string]interface{}{},
			"trojan":      map[string]interface{}{},
			"shadowsocks": map[string]interface{}{},
		},
	}

	if expireTimestamp > 0 {
		payload["expire"] = expireTimestamp
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", baseURL+"/api/user", bytes.NewBuffer(jsonData))
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
		return "", fmt.Errorf("پاسارگاد: ساخت کاربر ناموفق، کد: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	// تلاش برای استخراج لینک از پاسخ POST
	subLink := extractPasarguardSubURL(baseURL, result)
	if subLink != "" {
		return subLink, nil
	}

	// fallback: GET user
	reqGet, _ := http.NewRequest("GET", baseURL+"/api/user/"+username, nil)
	reqGet.Header.Add("Authorization", "Bearer "+token)
	respGet, err := client.Do(reqGet)
	if err == nil && respGet.StatusCode == http.StatusOK {
		defer respGet.Body.Close()
		var getUser map[string]interface{}
		json.NewDecoder(respGet.Body).Decode(&getUser)
		subLink = extractPasarguardSubURL(baseURL, getUser)
		if subLink != "" {
			return subLink, nil
		}
	}

	return "", fmt.Errorf("پاسارگاد: کاربر ساخته شد اما لینک اشتراک استخراج نشد")
}

// UpdateMarzbanUser بروزرسانی کاربر موجود در پنل پاسارگاد
// اگه کاربر موجود نبود، خودکار می‌سازه
func UpdateMarzbanUser(panel db.PanelConfig, targetUser string, volumeGB int, days int) (string, error) {
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return "", err
	}

	baseURL := strings.TrimRight(panel.URL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

	// چک کن کاربر موجود هست؟
	reqGet, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/user/%s", baseURL, targetUser), nil)
	reqGet.Header.Add("Authorization", "Bearer "+token)
	respGet, err := client.Do(reqGet)
	if err != nil {
		return "", err
	}

	if respGet.StatusCode != http.StatusOK {
		respGet.Body.Close()
		log.Printf("⚠️ [پاسارگاد] کاربر %s یافت نشد. در حال ساخت خودکار...", targetUser)
		return CreateMarzbanUser(panel, targetUser, volumeGB, days)
	}

	var user map[string]interface{}
	json.NewDecoder(respGet.Body).Decode(&user)
	respGet.Body.Close()

	// بروزرسانی مقادیر
	volumeBytes := int64(volumeGB) * 1024 * 1024 * 1024
	user["data_limit"] = volumeBytes
	if days > 0 {
		user["expire"] = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	} else {
		user["expire"] = nil
	}
	user["status"] = "active"

	jsonData, _ := json.Marshal(user)
	reqPut, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/user/%s", baseURL, targetUser), bytes.NewBuffer(jsonData))
	reqPut.Header.Add("Authorization", "Bearer "+token)
	reqPut.Header.Add("Content-Type", "application/json")

	respPut, err := client.Do(reqPut)
	if err != nil {
		return "", err
	}
	defer respPut.Body.Close()

	if respPut.StatusCode != http.StatusOK {
		return "", fmt.Errorf("پاسارگاد: بروزرسانی ناموفق، کد: %d", respPut.StatusCode)
	}

	// بعد از بروزرسانی، لینک اشتراک رو بگیر
	reqGetLink, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/user/%s", baseURL, targetUser), nil)
	reqGetLink.Header.Add("Authorization", "Bearer "+token)
	respGetLink, err := client.Do(reqGetLink)
	if err == nil && respGetLink.StatusCode == http.StatusOK {
		defer respGetLink.Body.Close()
		var updatedUser map[string]interface{}
		json.NewDecoder(respGetLink.Body).Decode(&updatedUser)
		subLink := extractPasarguardSubURL(baseURL, updatedUser)
		if subLink != "" {
			return subLink, nil
		}
	}

	return "", nil
}

// GetMarzbanUserUsage دریافت حجم استفاده‌شده کاربر
func GetMarzbanUserUsage(panel db.PanelConfig, targetUser string) (int64, error) {
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return 0, err
	}

	baseURL := strings.TrimRight(panel.URL, "/")
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/user/%s", baseURL, targetUser), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if used, ok := result["used_traffic"].(float64); ok {
		return int64(used), nil
	}
	return 0, fmt.Errorf("پاسارگاد: حجم استفاده‌شده یافت نشد")
}

// DisableMarzbanUsers غیرفعال کردن چند کاربر به صورت batch
func DisableMarzbanUsers(panel db.PanelConfig, usernames []string) error {
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return err
	}

	baseURL := strings.TrimRight(panel.URL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

	for _, u := range usernames {
		reqGet, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/user/%s", baseURL, u), nil)
		reqGet.Header.Add("Authorization", "Bearer "+token)
		respGet, err := client.Do(reqGet)
		if err != nil {
			continue
		}

		var user map[string]interface{}
		json.NewDecoder(respGet.Body).Decode(&user)
		respGet.Body.Close()

		if user["username"] == nil {
			continue
		}

		user["status"] = "disabled"
		jsonData, _ := json.Marshal(user)

		reqPut, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/user/%s", baseURL, u), bytes.NewBuffer(jsonData))
		reqPut.Header.Add("Authorization", "Bearer "+token)
		reqPut.Header.Add("Content-Type", "application/json")

		respPut, err := client.Do(reqPut)
		if err == nil {
			respPut.Body.Close()
		}
	}
	return nil
}

// FormatMarzbanConfig فرمت‌بندی خروجی برای نمایش به مشتری
func FormatMarzbanConfig(panel db.PanelConfig, link string, volumeGB int) string {
	panelName := panel.Name
	if panelName == "" {
		panelName = "پاسارگاد"
	}
	if panel.IsBackup {
		return fmt.Sprintf("=== 🛡️ %s (زاپاس %dGB) ===\n%s", panelName, volumeGB, link)
	}
	return fmt.Sprintf("=== 🛡️ %s (%dGB) ===\n%s", panelName, volumeGB, link)
}
