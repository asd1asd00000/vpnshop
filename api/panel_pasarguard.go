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

// ─────────────  مدل گروه ────────────

// GroupInfo اطلاعات یک گروه (id + name)
type GroupInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ───────────── 🧠 کش گروه‌ها با نوسازی پس‌زمینه ─────────────

type groupCacheEntry struct {
	ids       []int
	names     map[int]string // 🆕 نام گروه‌ها هم کش می‌شه
	fetchedAt time.Time
	source    string // "simple" | "groups" | "manual"
}

var (
	groupCacheMu sync.Mutex
	groupCache   = map[string]groupCacheEntry{} // key: آدرس پنل
)

// 🎯 زمان نوسازی کش: هر ۱ ساعت
const groupRefreshInterval = 1 * time.Hour

// StartGroupCache نوسازی خودکار گروه‌ها (مستقل از خرید)
// 🆕 الان با /api/groups/simple کار می‌کنه → خودکار لود می‌شه
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

func refreshAllGroups() {
	cfg := db.GetConfig()
	for _, panel := range cfg.Panels {
		if panel.Type != "marzban" {
			continue
		}
		// 🎯 احترام به انتخاب دستی ادمین: اگه GroupIDs تنظیم شده، کش نزن
		if len(panel.GroupIDs) > 0 {
			log.Printf("ℹ️ [کش گروه] %s → انتخاب دستی ادمین فعال است، کش خودکار نادیده گرفته شد", panel.URL)
			continue
		}
		// 🆕 استفاده از تابع مشترک (simple → groups fallback)
		groups, source, err := fetchPasarguardGroups(panel)
		if err != nil || len(groups) == 0 {
			log.Printf("⚠️ [کش گروه] %s → نوسازی ناموفق (%v)، کش قبلی نگه داشته شد", panel.URL, err)
			continue
		}
		ids := make([]int, len(groups))
		names := make(map[int]string, len(groups))
		for i, g := range groups {
			ids[i] = g.ID
			names[g.ID] = g.Name
		}
		storeGroupCache(panel.URL, ids, names, source)
		log.Printf("🧠 [کش گروه] %s → %d گروه از %s ذخیره شد", panel.URL, len(ids), source)
	}
}

// ─────────────  تابع مشترک: گرفتن گروه‌ها با زنجیره fallback ─────────────

// fetchPasarguardGroups گروه‌ها رو با اولویت:
//  1. /api/groups/simple  (سبک، مخصوص ربات‌ها، بدون scope سنگین)
//  2. /api/groups         (fallback برای پنل‌های قدیمی)
// برمی‌گردونه. source نشون می‌ده کدوم endpoint جواب داده.
func fetchPasarguardGroups(panel db.PanelConfig) ([]GroupInfo, string, error) {
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return nil, "", fmt.Errorf("احراز هویت ناموفق: %v", err)
	}
	baseURL := strings.TrimRight(panel.URL, "/")

	// 🥇 تلاش ۱: /api/groups/simple
	groups, err := fetchGroupsFromEndpoint(baseURL, token, "/api/groups/simple")
	if err == nil && len(groups) > 0 {
		return groups, "simple", nil
	}
	// اگه 404 بود (پنل قدیمی)، لاگ کن و ادامه بده
	if err != nil && !strings.Contains(err.Error(), "404") {
		log.Printf("⚠️ [پاسارگاد] /api/groups/simple خطا داد: %v — تلاش /api/groups", err)
	} else {
		log.Printf("ℹ️ [پاسارگاد] /api/groups/simple موجود نیست (404) — تلاش /api/groups")
	}

	// 🥈 تلاش ۲: /api/groups (قدیمی)
	groups, err2 := fetchGroupsFromEndpoint(baseURL, token, "/api/groups")
	if err2 == nil && len(groups) > 0 {
		return groups, "groups", nil
	}

	// هر دو ناموفق
	if err2 != nil {
		return nil, "", fmt.Errorf("هر دو endpoint ناموفق: simple=%v, groups=%v", err, err2)
	}
	return nil, "", fmt.Errorf("پاسخ خالی از هر دو endpoint")
}

// fetchGroupsFromEndpoint یک endpoint خاص رو می‌زنه و گروه‌ها رو استخراج می‌کنه
func fetchGroupsFromEndpoint(baseURL, token, endpoint string) ([]GroupInfo, error) {
	req, _ := http.NewRequest("GET", baseURL+endpoint, nil)
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Accept", "application/json")

	client := makeHTTPClient(20 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("خطای شبکه: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("404 endpoint موجود نیست")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("وضعیت %d: %s", resp.StatusCode, truncateBody(bodyBytes, 200))
	}

	return parseGroupsResponse(bodyBytes), nil
}

// parseGroupsResponse پاسخ JSON رو پارس می‌کنه (هر دو فرمت: آرایه خالی یا wrapper)
func parseGroupsResponse(body []byte) []GroupInfo {
	var result []GroupInfo

	// 🥇 فرمت آرایه خالی: [{"id":1,"name":"..."}, ...]
	var arr []map[string]interface{}
	if err := json.Unmarshal(body, &arr); err == nil {
		for _, g := range arr {
			id, _ := g["id"].(float64)
			name, _ := g["name"].(string)
			if id > 0 {
				if name == "" {
					name = fmt.Sprintf("Group-%d", int(id))
				}
				result = append(result, GroupInfo{ID: int(id), Name: name})
			}
		}
		if len(result) > 0 {
			return result
		}
	}

	// 🥈 فرمت wrapper: {"groups": [...], "total": 6}
	var wrapper map[string]interface{}
	if err := json.Unmarshal(body, &wrapper); err == nil {
		for _, key := range []string{"groups", "items", "data", "results"} {
			if list, ok := wrapper[key].([]interface{}); ok {
				for _, item := range list {
					if m, ok := item.(map[string]interface{}); ok {
						id, _ := m["id"].(float64)
						name, _ := m["name"].(string)
						if id > 0 {
							if name == "" {
								name = fmt.Sprintf("Group-%d", int(id))
							}
							result = append(result, GroupInfo{ID: int(id), Name: name})
						}
					}
				}
				if len(result) > 0 {
					return result
				}
			}
		}
	}

	return result
}

func storeGroupCache(url string, ids []int, names map[int]string, source string) {
	groupCacheMu.Lock()
	groupCache[url] = groupCacheEntry{ids: ids, names: names, fetchedAt: time.Now(), source: source}
	groupCacheMu.Unlock()
}

func getCachedGroups(url string) []int {
	groupCacheMu.Lock()
	defer groupCacheMu.Unlock()
	if e, ok := groupCache[url]; ok {
		return e.ids
	}
	return nil
}

// getCachedGroupNames نام گروه‌های کش‌شده رو برمی‌گردونه (برای UI)
func getCachedGroupNames(url string) map[int]string {
	groupCacheMu.Lock()
	defer groupCacheMu.Unlock()
	if e, ok := groupCache[url]; ok {
		return e.names
	}
	return nil
}

// clearGroupCache خالی کردن کامل کش
func clearGroupCache() {
	groupCacheMu.Lock()
	groupCache = map[string]groupCacheEntry{}
	groupCacheMu.Unlock()
}

// ClearGroupCacheHandler دکمه کلین کش در تنظیمات (غیرهمزمان)
func ClearGroupCacheHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clearGroupCache()

	db.LogEvent("general", "success", "🧹 کش گروه‌ها پاک شد — نوسازی خودکار در پس‌زمینه شروع شد")
	log.Printf("🧹 [کش گروه] نوسازی خودکار شروع شد (با /api/groups/simple)")

	go func() {
		refreshAllGroups()
		log.Printf("🧹 [کش گروه] نوسازی خودکار کامل شد")
	}()

	http.Redirect(w, r, AdminBasePath()+"/settings", http.StatusSeeOther)
}

// ───────────── 🔧 HTTP Transport سفارشی ─────────────

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
	return &http.Client{Transport: transport, Timeout: timeout}
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

// 🆕 getPasarguardGroupIDs (نگه‌داشته برای سازگاری) — الان از تابع مشترک استفاده می‌کنه
func getPasarguardGroupIDs(baseURL, token string) []int {
	// این تابع دیگه مستقیم استفاده نمی‌شه، ولی اگه جایی صداش زد،
	// از endpoint قدیمی استفاده می‌کنه (بدون fallback)
	req, _ := http.NewRequest("GET", baseURL+"/api/groups", nil)
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Accept", "application/json")

	client := makeHTTPClient(20 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return []int{}
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return []int{}
	}

	groups := parseGroupsResponse(bodyBytes)
	ids := make([]int, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}
	return ids
}

func extractGroupIDs(body []byte) []int {
	groups := parseGroupsResponse(body)
	ids := make([]int, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
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

// doCreateUser ارسال درخواست ساخت کاربر
func doCreateUser(baseURL, token string, payload map[string]interface{}) (map[string]interface{}, error) {
	jsonData, _ := json.Marshal(payload)
	log.Printf("🔍 [پاسارگاد] payload ساخت کاربر: %s", string(jsonData))

	req, err := http.NewRequest("POST", baseURL+"/api/user", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	client := makeHTTPClient(15 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("پاسارگاد: ساخت کاربر ناموفق، کد: %d، پاسخ: %s", resp.StatusCode, truncateBody(bodyBytes, 300))
	}

	var result map[string]interface{}
	json.Unmarshal(bodyBytes, &result)
	return result, nil
}

// finishCreate استخراج لینک اشتراک بعد از ساخت موفق
func finishCreate(baseURL, token, username string, result map[string]interface{}) (string, error) {
	subLink := extractPasarguardSubURL(baseURL, result)
	if subLink != "" {
		log.Printf("✅ [پاسارگاد] کاربر %s با موفقیت ساخته شد", username)
		return subLink, nil
	}

	reqGet, _ := http.NewRequest("GET", baseURL+"/api/user/"+username, nil)
	reqGet.Header.Add("Authorization", "Bearer "+token)
	client := makeHTTPClient(10 * time.Second)
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

// 🎯 resolveGroupIDs تعیین گروه‌ها با زنجیره اولویت:
//  1. panel.GroupIDs (انتخاب دستی ادمین)
//  2. کش خودکار (از /api/groups/simple)
//  3. fetch زنده (اگه کش خالی بود)
//  4. خالی (بدون گروه)
func resolveGroupIDs(panel db.PanelConfig) ([]int, string) {
	// 🥇 انتخاب دستی ادمین
	if len(panel.GroupIDs) > 0 {
		log.Printf("📋 [پاسارگاد] استفاده از %d گروه (انتخاب دستی ادمین)", len(panel.GroupIDs))
		return panel.GroupIDs, "manual"
	}

	// 🥈 کش خودکار
	cached := getCachedGroups(panel.URL)
	if len(cached) > 0 {
		log.Printf(" [پاسارگاد] استفاده از %d گروه (کش خودکار)", len(cached))
		return cached, "cache"
	}

	// 🥉 fetch زنده با fallback
	groups, source, err := fetchPasarguardGroups(panel)
	if err == nil && len(groups) > 0 {
		ids := make([]int, len(groups))
		names := make(map[int]string, len(groups))
		for i, g := range groups {
			ids[i] = g.ID
			names[g.ID] = g.Name
		}
		storeGroupCache(panel.URL, ids, names, source)
		log.Printf("🌐 [پاسارگاد] fetch زنده موفق از %s → %d گروه", source, len(ids))
		return ids, source
	}

	log.Printf("⚠️ [پاسارگاد] هیچ گروهی در دسترس نیست (manual/cache/live هم ناموفق)")
	return nil, "none"
}

// 🎯 CreateMarzbanUser ساخت کاربر با زنجیره گروه‌ها + fallback
func CreateMarzbanUser(panel db.PanelConfig, username string, volumeGB int, days int) (string, error) {
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return "", err
	}
	baseURL := strings.TrimRight(panel.URL, "/")

	volumeBytes := int64(volumeGB) * 1024 * 1024 * 1024
	var expireTimestamp int64
	if days > 0 {
		expireTimestamp = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	}

	buildPayload := func(groupIDs []int) map[string]interface{} {
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
		}
		if expireTimestamp > 0 {
			payload["expire"] = expireTimestamp
		}
		return payload
	}

	// 🎯 تعیین گروه‌ها با زنجیره اولویت
	groupIDs, source := resolveGroupIDs(panel)

	// 🎯 تلاش ۱: با گروه‌ها
	if len(groupIDs) > 0 {
		result, err := doCreateUser(baseURL, token, buildPayload(groupIDs))
		if err == nil {
			log.Printf("✅ [پاسارگاد] کاربر %s با %d گروه (%s) ساخته شد", username, len(groupIDs), source)
			return finishCreate(baseURL, token, username, result)
		}

		// 🎯 اگه خطای گروه بود، کش رو باطل کن و fallback
		errMsg := err.Error()
		if strings.Contains(errMsg, "group") || strings.Contains(errMsg, "400") {
			log.Printf("⚠️ [پاسارگاد] خطای گروه (%s): %v — کش باطل شد", source, err)
			// اگه از کش بود، پاکش کن تا دفعه بعد fetch زنده بزنه
			if source == "cache" || source == "manual" {
				groupCacheMu.Lock()
				delete(groupCache, panel.URL)
				groupCacheMu.Unlock()
			}
			db.LogEventf("general", "warning", "⚠️ گروه‌های پنل %s نامعتبر شدند (منبع: %s). کش باطل شد.", panel.Name, source)
		} else {
			log.Printf("⚠️ [پاسارگاد] ساخت با گروه‌ها ناموفق (%v)", err)
		}
	}

	// 🎯 تلاش : بدون group_ids (fallback نهایی)
	log.Printf("⚠️ [پاسارگاد] تلاش نهایی بدون group_ids برای %s", username)
	result, err := doCreateUser(baseURL, token, buildPayload(nil))
	if err != nil {
		return "", err
	}
	return finishCreate(baseURL, token, username, result)
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

// RenewMarzbanUser تمدید کاربر Marzban با ریست هوشمند حجم
func RenewMarzbanUser(panel db.PanelConfig, targetUser string, volumeGB int, days int) (string, error) {
	token, err := getPasarguardToken(panel.URL, panel.Username, panel.Password)
	if err != nil {
		return "", err
	}

	baseURL := strings.TrimRight(panel.URL, "/")
	client := makeHTTPClient(10 * time.Second)

	usedTraffic, err := GetMarzbanUserUsage(panel, targetUser)
	if err != nil {
		usedTraffic = 0
	}

	newLimit := usedTraffic + int64(volumeGB)*1073741824
	newExpire := time.Now().AddDate(0, 0, days).Unix()

	reqGet, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/user/%s", baseURL, targetUser), nil)
	reqGet.Header.Add("Authorization", "Bearer "+token)
	respGet, err := client.Do(reqGet)
	if err != nil {
		return "", err
	}

	if respGet.StatusCode != http.StatusOK {
		respGet.Body.Close()
		log.Printf("⚠️ [پاسارگاد-تمدید] کاربر %s یافت نشد، ساخت خودکار...", targetUser)
		return CreateMarzbanUser(panel, targetUser, volumeGB, days)
	}

	var user map[string]interface{}
	json.NewDecoder(respGet.Body).Decode(&user)
	respGet.Body.Close()

	user["data_limit"] = newLimit
	user["expire"] = newExpire
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
		return "", fmt.Errorf("پاسارگاد: تمدید ناموفق، کد: %d", respPut.StatusCode)
	}

	log.Printf("✅ [پاسارگاد-تمدید] %s تمدید شد | سقف=%d بایت | روز=%d", targetUser, newLimit, days)

	reqGetLink, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/user/%s", baseURL, targetUser), nil)
	reqGetLink.Header.Add("Authorization", "Bearer "+token)
	respGetLink, err := client.Do(reqGetLink)
	if err == nil && respGetLink.StatusCode == http.StatusOK {
		defer respGetLink.Body.Close()
		var updatedUser map[string]interface{}
		json.NewDecoder(respGetLink.Body).Decode(&updatedUser)
		if subLink := extractPasarguardSubURL(baseURL, updatedUser); subLink != "" {
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
