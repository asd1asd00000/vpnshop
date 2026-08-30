package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/asd1asd00000/vpnshop/db"
)

// ───────────── 🧠 کش گروه‌ها با نوسازی پس‌زمینه ─────────────

type groupCacheEntry struct {
	ids       []int
	fetchedAt time.Time
}

var (
	groupCacheMu sync.Mutex
	groupCache   = map[string]groupCacheEntry{} // key: آدرس پنل
)

// 🎯 زمان نوسازی کش: هر ۱ ساعت
const groupRefreshInterval = 1 * time.Hour

// StartGroupCache نوسازی متناوب گروه‌ها (مستقل از خرید)
func StartGroupCache() {
	go func() {
		refreshAllGroups() // بار اول، همین الان
		ticker := time.NewTicker(groupRefreshInterval)
		defer ticker.Stop()
		for range ticker.C {
			refreshAllGroups()
		}
	}()
}

// refreshAllGroups برای همه پنل‌های marzban گروه‌ها رو نو می‌کنه
func refreshAllGroups() {
	cfg := db.GetConfig()
	for _, panel := range cfg.Panels {
		if panel.Type != "marzban" {
			continue
		}
		ids := fetchGroupsWithRetry(panel)
		if len(ids) > 0 {
			storeGroupCache(panel.URL, ids)
			log.Printf("🧠 [کش گروه] %s → %d گروه ذخیره شد", panel.URL, len(ids))
		} else {
			log.Printf("⚠️ [کش گروه] %s → نوسازی ناموفق، کش قبلی نگه داشته شد", panel.URL)
		}
	}
}

// fetchGroupsWithRetry تا ۳ بار با فاصله تلاش می‌کنه
func fetchGroupsWithRetry(panel db.PanelConfig) []int {
	baseURL := strings.TrimRight(panel.URL, "/")
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		log.Printf("⚠️ [کش گروه] احراز هویت ناموفق برای %s: %v", panel.URL, err)
		return nil
	}
	for attempt := 1; attempt <= 3; attempt++ {
		ids := getPasarguardGroupIDs(baseURL, token)
		if len(ids) > 0 {
			return ids
		}
		log.Printf("⚠️ [کش گروه] تلاش %d/3 برای %s ناموفق", attempt, panel.URL)
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return nil
}

func storeGroupCache(url string, ids []int) {
	groupCacheMu.Lock()
	groupCache[url] = groupCacheEntry{ids: ids, fetchedAt: time.Now()}
	groupCacheMu.Unlock()
}

// getCachedGroups خواندن از کش در لحظه خرید
func getCachedGroups(url string) []int {
	groupCacheMu.Lock()
	defer groupCacheMu.Unlock()
	if e, ok := groupCache[url]; ok {
		return e.ids
	}
	return nil
}

// ───────────── 🔧 HTTP Transport سفارشی (connection pooling) ─────────────

func makeHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		TLSHandshakeTimeout: 30 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		DisableKeepAlives:   false,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

// ───────────── توابع داخلی پنل پاسارگاد (Marzban) ─────────────

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

	client := makeHTTPClient(15 * time.Second)
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

// getPasarguardGroupIDs شناسه همه گروه‌های پنل پاسارگاد
func getPasarguardGroupIDs(baseURL, token string) []int {
	endpoints := []string{
		"/api/groups?limit=1000&offset=0",
		"/api/user_groups?limit=1000&offset=0",
		"/api/admin/groups?limit=1000&offset=0",
		"/api/nodes/groups?limit=1000&offset=0",
	}

	client := makeHTTPClient(20 * time.Second)

	for _, ep := range endpoints {
		req, _ := http.NewRequest("GET", baseURL+ep, nil)
		req.Header.Add("Authorization", "Bearer "+token)
		req.Header.Add("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("⚠️ [پاسارگاد] خطا در فراخوانی %s: %v", ep, err)
			continue
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("⚠️ [پاسارگاد] پاسخ ناموفق از %s | Status: %d | Body: %s",
				ep, resp.StatusCode, truncateBody(bodyBytes, 300))
			continue
		}

		ids, total := extractGroupIDsWithTotal(bodyBytes)
		if len(ids) > 0 {
			log.Printf("🔍 [پاسارگاد] گروه‌های یافت شده از %s: %v (total: %d)", ep, ids, total)
			if total > len(ids) {
				log.Printf("⚠️ [پاسارگاد] هشدار: تعداد کل گروه‌ها (%d) بیشتر از گروه‌های دریافتی (%d) است", total, len(ids))
			}
			return ids
		}

		log.Printf("⚠️ [پاسارگاد] پاسخ %s موفق بود ولی هیچ groupID استخراج نشد | Body: %s",
			ep, truncateBody(bodyBytes, 300))
	}

	log.Printf("⚠️ [پاسارگاد] هیچ گروهی یافت نشد، بدون group_ids ادامه می‌دهد")
	return []int{}
}

func extractGroupIDs(body []byte) []int {
	var ids []int

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

func extractGroupIDsWithTotal(body []byte) ([]int, int) {
	ids := extractGroupIDs(body)

	total := 0
	var wrapper map[string]interface{}
	if err := json.Unmarshal(body, &wrapper); err == nil {
		if t, ok := wrapper["total"].(float64); ok {
			total = int(t)
		}
	}
	return ids, total
}

func truncateBody(body []byte, n int) string {
	s := string(body)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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

// CreateMarzbanUser ساخت کاربر با استفاده از کش
func CreateMarzbanUser(panel db.PanelConfig, username string, volumeGB int, days int) (string, error) {
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return "", err
	}

	baseURL := strings.TrimRight(panel.URL, "/")

	// 🎯 گروه‌ها از کش (نوسازی پس‌زمینه) — خرید منتظر پنل نمی‌مونه
	groupIDs := getCachedGroups(panel.URL)
	if len(groupIDs) == 0 {
		// فقط اگه کش خالی بود (مثلاً بلافاصله بعد از ریستارت)، همون لحظه با retry بگیر
		log.Printf("🧠 [کش گروه] کش خالی برای %s، گرفتن با retry...", panel.URL)
		groupIDs = fetchGroupsWithRetry(panel)
		if len(groupIDs) > 0 {
			storeGroupCache(panel.URL, groupIDs)
		}
	} else {
		log.Printf("🧠 [کش گروه] استفاده از کش: %d گروه برای %s", len(groupIDs), panel.URL)
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

	if len(groupIDs) > 0 {
		payload["group_ids"] = groupIDs
		log.Printf("🎯 [پاسارگاد] payload با %d گروه برای %s", len(groupIDs), username)
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

	client := makeHTTPClient(15 * time.Second)
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
	client2 := makeHTTPClient(10 * time.Second)
	respGet, err := client2.Do(reqGet)
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
	client := makeHTTPClient(10 * time.Second)

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

	client := makeHTTPClient(5 * time.Second)
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
	client := makeHTTPClient(10 * time.Second)

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
