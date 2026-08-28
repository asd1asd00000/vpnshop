package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

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
	req.Header.Add("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("پاسارگاد: خطا در اتصال: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf("🔍 [پاسارگاد] احراز هویت | URL: %s | Status: %d", baseURL+"/api/admin/token", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("پاسارگاد: احراز هویت ناموفق، کد: %d، پاسخ: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	json.Unmarshal(bodyBytes, &result)
	if token, ok := result["access_token"].(string); ok {
		return token, nil
	}
	return "", fmt.Errorf("پاسارگاد: توکن در پاسخ یافت نشد")
}

// 🎯 getPasarguardGroupIDs - چک کردن گروه‌ها با لاگ کامل
// این تابع همیشه لاگ کامل می‌ده تا بفهمیم هر پنل چی داره
func getPasarguardGroupIDs(baseURL, token string) []int {
	endpoints := []string{
		"/api/groups",
		"/api/user_groups",
		"/api/admin/groups",
		"/api/node_groups",
		"/api/nodes/groups",
		"/api/admins/groups",
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for _, ep := range endpoints {
		req, _ := http.NewRequest("GET", baseURL+ep, nil)
		req.Header.Add("Authorization", "Bearer "+token)
		req.Header.Add("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("🔍 [پاسارگاد] %s | خطا: %v", ep, err)
			continue
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		preview := string(bodyBytes)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		log.Printf("🔍 [پاسارگاد] %s | Status: %d | Body: %s", ep, resp.StatusCode, preview)

		if resp.StatusCode != http.StatusOK {
			continue
		}

		ids := extractGroupIDs(bodyBytes)
		if len(ids) > 0 {
			log.Printf("✅ [پاسارگاد] %d گروه از %s یافت شد", len(ids), ep)
			return ids
		}
	}

	log.Printf("⚠️ [پاسارگاد] هیچ گروهی از endpoints موجود یافت نشد")
	return []int{}
}

func extractGroupIDs(body []byte) []int {
	var ids []int

	// فرمت ۱: آرایه مستقیم [{"id":1, "name":"..."},...]
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

	// فرمت ۲: آرایه ساده از اعداد [1, 2, 3]
	var intArr []float64
	if err := json.Unmarshal(body, &intArr); err == nil && len(intArr) > 0 {
		for _, id := range intArr {
			ids = append(ids, int(id))
		}
		if len(ids) > 0 {
			return ids
		}
	}

	// فرمت ۳: آبجکت پیچیده {"items":[...]} یا {"groups":[...]}
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

// 🎯 getAllInbounds - fallback برای پنل‌هایی که group ندارن
func getAllInbounds(baseURL, token string) map[string][]string {
	client := &http.Client{Timeout: 10 * time.Second}

	endpoints := []string{
		"/api/inbounds",
		"/api/proxies",
		"/api/admin/proxies",
		"/api/admin/inbounds",
	}

	for _, ep := range endpoints {
		req, err := http.NewRequest("GET", baseURL+ep, nil)
		if err != nil {
			continue
		}
		req.Header.Add("Authorization", "Bearer "+token)
		req.Header.Add("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("🔍 [پاسارگاد] %s | خطا: %v", ep, err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		preview := string(body)
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		log.Printf("🔍 [پاسارگاد] %s | Status: %d | Body: %s", ep, resp.StatusCode, preview)

		if resp.StatusCode != http.StatusOK {
			continue
		}

		result := parseInboundResponse(body)
		if len(result) > 0 {
			total := 0
			for _, tags := range result {
				total += len(tags)
			}
			log.Printf("✅ [پاسارگاد] %d inbound از %s یافت شد", total, ep)
			return result
		}
	}

	log.Printf("⚠️ [پاسارگاد] هیچ inbound یافت نشد")
	return nil
}

func parseInboundResponse(body []byte) map[string][]string {
	result := make(map[string][]string)

	var protoMap map[string]interface{}
	if err := json.Unmarshal(body, &protoMap); err == nil {
		for proto, val := range protoMap {
			if proto == "detail" || proto == "error" || proto == "message" {
				continue
			}
			if list, ok := val.([]interface{}); ok {
				var tags []string
				for _, item := range list {
					if m, ok := item.(map[string]interface{}); ok {
						if tag, ok := m["tag"].(string); ok && tag != "" {
							tags = append(tags, tag)
						}
					} else if s, ok := item.(string); ok && s != "" {
						tags = append(tags, s)
					}
				}
				if len(tags) > 0 {
					result[proto] = tags
				}
				continue
			}
			if m, ok := val.(map[string]interface{}); ok {
				if tag, ok := m["tag"].(string); ok && tag != "" {
					result[proto] = append(result[proto], tag)
				}
			}
		}
		if len(result) > 0 {
			return result
		}
	}

	var flatArr []map[string]interface{}
	if err := json.Unmarshal(body, &flatArr); err == nil {
		for _, item := range flatArr {
			tag, _ := item["tag"].(string)
			proto, _ := item["protocol"].(string)
			if proto == "" {
				proto, _ = item["type"].(string)
			}
			if tag == "" || proto == "" {
				continue
			}
			result[proto] = append(result[proto], tag)
		}
		if len(result) > 0 {
			return result
		}
	}

	var stringArr []string
	if err := json.Unmarshal(body, &stringArr); err == nil && len(stringArr) > 0 {
		// 🎯 آرایه ساده رو در همه پروتکل‌های استاندارد قرار می‌ده
		standardProtocols := []string{"vless", "vmess", "trojan", "shadowsocks"}
		for _, proto := range standardProtocols {
			result[proto] = append([]string{}, stringArr...)
		}
		return result
	}

	return nil
}

func extractPasarguardSubURL(baseURL string, data map[string]interface{}) string {
	if subURL, ok := data["subscription_url"].(string); ok && subURL != "" {
		if strings.HasPrefix(subURL, "/") {
			return baseURL + subURL
		}
		return subURL
	}
	if links, ok := data["links"].([]interface{}); ok && len(links) > 0 {
		if linkStr, ok := links[0].(string); ok {
			return linkStr
		}
	}
	return ""
}

// CreateMarzbanUser - اولویت با groups (روش استاندارد Marzban)
func CreateMarzbanUser(panel db.PanelConfig, username string, volumeGB int, days int) (string, error) {
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return "", err
	}

	baseURL := strings.TrimRight(panel.URL, "/")

	// 🎯 اولویت ۱: groups (روش استاندارد Marzban)
	log.Printf("🔍 [پاسارگاد] بررسی گروه‌ها برای %s...", baseURL)
	groupIDs := getPasarguardGroupIDs(baseURL, token)

	// 🎯 اولویت ۲: inbounds (fallback - فقط اگه groups نبود)
	var allInbounds map[string][]string
	if len(groupIDs) == 0 {
		log.Printf("🔍 [پاسارگاد] گروه یافت نشد، بررسی inbound ها...")
		allInbounds = getAllInbounds(baseURL, token)
	} else {
		log.Printf("✅ [پاسارگاد] از %d گروه استفاده می‌شود (inbound چک نمی‌شود)", len(groupIDs))
	}

	volumeBytes := int64(volumeGB) * 1024 * 1024 * 1024

	var expireTimestamp int64
	if days > 0 {
		expireTimestamp = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	}

	payload := map[string]interface{}{
		"username":                  username,
		"data_limit":                volumeBytes,
		"data_limit_reset_strategy": "no_reset",
		"status":                    "active",
		"proxies": map[string]interface{}{
			"vmess":       map[string]interface{}{},
			"vless":       map[string]interface{}{},
			"trojan":      map[string]interface{}{},
			"shadowsocks": map[string]interface{}{},
		},
	}

	// 🎯 فقط یکی از این دو رو اضافه کن:
	if len(groupIDs) > 0 {
		payload["group_ids"] = groupIDs
		log.Printf("🎯 [پاسارگاد] payload با %d گروه برای %s", len(groupIDs), username)
	} else if len(allInbounds) > 0 {
		payload["inbounds"] = allInbounds
		total := 0
		for _, tags := range allInbounds {
			total += len(tags)
		}
		log.Printf("🎯 [پاسارگاد] payload با %d inbound برای %s (fallback)", total, username)
	} else {
		log.Printf("⚠️ [پاسارگاد] payload بدون گروه و inbound برای %s", username)
	}

	if expireTimestamp > 0 {
		payload["expire"] = expireTimestamp
	}

	jsonData, _ := json.Marshal(payload)
	log.Printf("🔍 [پاسارگاد] payload: %s", string(jsonData))

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

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("پاسارگاد: ساخت کاربر ناموفق، کد: %d، پاسخ: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	json.Unmarshal(bodyBytes, &result)

	subLink := extractPasarguardSubURL(baseURL, result)
	if subLink != "" {
		log.Printf("✅ [پاسارگاد] کاربر %s با موفقیت ساخته شد", username)
		return subLink, nil
	}

	reqGet, _ := http.NewRequest("GET", baseURL+"/api/user/"+username, nil)
	reqGet.Header.Add("Authorization", "Bearer "+token)
	respGet, err := client.Do(reqGet)
	if err == nil && respGet.StatusCode == http.StatusOK {
		defer respGet.Body.Close()
		var getUser map[string]interface{}
		json.NewDecoder(respGet.Body).Decode(&getUser)
		subLink = extractPasarguardSubURL(baseURL, getUser)
		if subLink != "" {
			log.Printf("✅ [پاسارگاد] کاربر %s با موفقیت ساخته شد (از GET)", username)
			return subLink, nil
		}
	}

	return "", fmt.Errorf("پاسارگاد: کاربر ساخته شد اما لینک اشتراک استخراج نشد")
}

func UpdateMarzbanUser(panel db.PanelConfig, targetUser string, volumeGB int, days int) (string, error) {
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return "", err
	}

	baseURL := strings.TrimRight(panel.URL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

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
