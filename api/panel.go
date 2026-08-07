package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/asd1asd00000/vpnshop/models"
)

// GenerateConfigFromOrder پل ارتباطی بین فاکتور پرداخت شده و توابع گارد
func GenerateConfigFromOrder(order models.Order) (string, error) {
	panelURL := os.Getenv("PANEL_URL")
	user := os.Getenv("PANEL_USER")
	pass := os.Getenv("PANEL_PASS")

	if panelURL == "" || user == "" || pass == "" {
		return "", fmt.Errorf("متغیرهای محیطی پنل تنظیم نشده‌اند")
	}

	token, err := GetToken(panelURL, user, pass)
	if err != nil {
		return "", err
	}

	var limitUsage int64 = 20 * 1073741824            // 20 GB
	limitExpire := time.Now().AddDate(0, 1, 0).Unix() // 30 Days

	username := fmt.Sprintf("user_%s", order.TrackingCode)

	return CreateSubscription(panelURL, token, username, limitUsage, limitExpire)
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
			"enabled":      true,
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
		return "", fmt.Errorf("create failed, status: %d", resp.StatusCode)
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

func UpdateSubscription(nodeURL, token, targetUser string, nodeVolumeLimit int64, expireTimestamp int64) (string, error) {
	baseURL := strings.TrimRight(nodeURL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

	reqGet, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/subscriptions/%s", baseURL, targetUser), nil)
	reqGet.Header.Add("Authorization", "Bearer "+token)
	respGet, err := client.Do(reqGet)
	if err != nil {
		return "", err
	}

	if respGet.StatusCode != http.StatusOK {
		respGet.Body.Close()
		log.Printf("⚠️ User %s not found on GuardCore during update. Auto-creating it now...", targetUser)
		newLink, errCreate := CreateSubscription(nodeURL, token, targetUser, nodeVolumeLimit, expireTimestamp)
		if errCreate != nil {
			return "", fmt.Errorf("user not found, and auto-creation failed: %v", errCreate)
		}
		return newLink, nil
	}

	var user map[string]interface{}
	json.NewDecoder(respGet.Body).Decode(&user)
	respGet.Body.Close()

	cleanPayload := map[string]interface{}{
		"limit_usage":  nodeVolumeLimit,
		"limit_expire": expireTimestamp,
	}

	if sIDs, ok := user["service_ids"]; ok {
		cleanPayload["service_ids"] = sIDs
	} else if sID, ok := user["service_id"]; ok {
		cleanPayload["service_ids"] = []interface{}{sID}
	}

	jsonData, _ := json.Marshal(cleanPayload)
	reqPut, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/subscriptions/%s", baseURL, targetUser), bytes.NewBuffer(jsonData))
	reqPut.Header.Add("Authorization", "Bearer "+token)
	reqPut.Header.Add("Content-Type", "application/json")

	respPut, err := client.Do(reqPut)
	if err != nil {
		return "", err
	}
	defer respPut.Body.Close()

	if respPut.StatusCode != http.StatusOK {
		return "", fmt.Errorf("guardcore update failed, status: %d", respPut.StatusCode)
	}

	enablePayload := map[string][]string{"usernames": {targetUser}}
	enableData, _ := json.Marshal(enablePayload)
	reqEnable, _ := http.NewRequest("POST", baseURL+"/api/subscriptions/enable", bytes.NewBuffer(enableData))
	reqEnable.Header.Add("Authorization", "Bearer "+token)
	reqEnable.Header.Add("Content-Type", "application/json")
	respEnable, err := client.Do(reqEnable)
	if err == nil {
		respEnable.Body.Close()
	}

	return "", nil
}

func GetUserUsage(nodeURL, token, targetUser string) (int64, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/subscriptions/%s", nodeURL, targetUser), nil)
	req.Header.Add("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if current, ok := result["current_usage"].(float64); ok {
		return int64(current), nil
	} else if total, ok := result["total_usage"].(float64); ok {
		return int64(total), nil
	}
	return 0, fmt.Errorf("could not find usage data")
}

func DisableSubscriptions(nodeURL, token string, usernames []string) error {
	payload := map[string][]string{"usernames": usernames}
	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", nodeURL+"/api/subscriptions/disable", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
